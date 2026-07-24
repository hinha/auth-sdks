// Package quotaorm provides Reserve→local TX (+outbox)→Confirm/Release
// orchestration for Auth Service quota mutations. Consumers inject *gorm.DB;
// table auth_sdk_quota_outbox lives in the consumer database.
package quotaorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	authsdk "github.com/hinha/auth-sdks/go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UsageClient is the Auth HTTP surface quotaorm needs (satisfied by *authsdk.Client).
type UsageClient interface {
	ReserveUsage(ctx context.Context, apiKey string, in authsdk.ReserveUsageInput) (*authsdk.UsageReservation, error)
	ConfirmUsage(ctx context.Context, apiKey string, in authsdk.ConfirmUsageInput) (*authsdk.UsageReservation, error)
	ReleaseUsage(ctx context.Context, apiKey string, in authsdk.ReleaseUsageInput) (*authsdk.UsageReservation, error)
	ReportUsage(ctx context.Context, apiKey string, in authsdk.ReportUsageInput) ([]authsdk.UsageMeter, error)
}

// UsageReserver is kept as an alias for older call sites / mocks.
type UsageReserver = UsageClient

// Mutation describes one quota hold tied to a local business write.
type Mutation struct {
	IdempotencyKey string
	SubjectType    string
	SubjectID      string
	Dimension      string
	Delta          float64
	PeriodKey      string
	ResourceRef    string
	TTLSeconds     int
	// APIKey overrides the client's default sa_* key when non-empty.
	APIKey string
}

// Manager owns AutoMigrate, RunQuotaMutation, and the outbox worker.
type Manager struct {
	client UsageClient
	db     *gorm.DB
	apiKey string // default sa_* key — set via WithAPIKey

	workerID    string
	maxAttempts int
	batchSize   int
	pollEvery   time.Duration
	lockTTL     time.Duration
	followUp    FollowUpHandler
}

// Option configures Manager.
type Option func(*Manager)

// WithAPIKey sets the default sa_* key used when Mutation.APIKey is empty.
func WithAPIKey(key string) Option {
	return func(m *Manager) { m.apiKey = key }
}

// WithWorkerID sets the outbox claim owner id (default random UUID).
func WithWorkerID(id string) Option {
	return func(m *Manager) { m.workerID = id }
}

// New binds an Auth usage client and the consumer *gorm.DB.
func New(client UsageClient, db *gorm.DB, opts ...Option) (*Manager, error) {
	if client == nil {
		return nil, errors.New("quotaorm: nil UsageClient")
	}
	if db == nil {
		return nil, errors.New("quotaorm: nil *gorm.DB")
	}
	m := &Manager{
		client:      client,
		db:          db,
		workerID:    uuid.NewString(),
		maxAttempts: 8,
		batchSize:   32,
		pollEvery:   2 * time.Second,
		lockTTL:     30 * time.Second,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// AutoMigrate creates/updates auth_sdk_quota_outbox in the consumer database.
func (m *Manager) AutoMigrate(ctx context.Context) error {
	return m.db.WithContext(ctx).AutoMigrate(&QuotaOutbox{})
}

// RunQuotaMutation reserves quota on Auth, then runs fn inside a GORM
// transaction that also inserts a confirm outbox row. On TX failure it
// releases the reservation (sync, with release outbox fallback).
func (m *Manager) RunQuotaMutation(ctx context.Context, mut Mutation, fn func(tx *gorm.DB) error) error {
	if fn == nil {
		return errors.New("quotaorm: nil mutation callback")
	}
	if mut.IdempotencyKey == "" || mut.Dimension == "" || mut.Delta <= 0 {
		return errors.New("quotaorm: IdempotencyKey, Dimension, and Delta>0 are required")
	}
	apiKey := mut.APIKey
	if apiKey == "" {
		apiKey = m.apiKey
	}

	res, err := m.client.ReserveUsage(ctx, apiKey, authsdk.ReserveUsageInput{
		SubjectType:    mut.SubjectType,
		SubjectID:      mut.SubjectID,
		DimensionKey:   mut.Dimension,
		Delta:          mut.Delta,
		IdempotencyKey: mut.IdempotencyKey,
		PeriodKey:      mut.PeriodKey,
		ResourceRef:    mut.ResourceRef,
		TTLSeconds:     mut.TTLSeconds,
	})
	if err != nil {
		return fmt.Errorf("quotaorm: reserve: %w", err)
	}
	if res == nil || res.ReservationID == "" {
		return errors.New("quotaorm: empty reservation response")
	}

	payload, _ := json.Marshal(map[string]any{
		"reservation_id":  res.ReservationID,
		"idempotency_key": mut.IdempotencyKey,
		"api_key_set":     apiKey != "",
	})

	outboxID := uuid.NewString()
	txErr := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fn(tx); err != nil {
			return err
		}
		row := QuotaOutbox{
			ID:             outboxID,
			EventID:        "confirm:" + res.ReservationID,
			Action:         outboxActionConfirm,
			ReservationID:  res.ReservationID,
			IdempotencyKey: mut.IdempotencyKey,
			Payload:        string(payload),
			AvailableAt:    time.Now().UTC(),
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	})
	if txErr != nil {
		if relErr := m.releaseOrEnqueue(ctx, apiKey, res.ReservationID, mut.IdempotencyKey); relErr != nil {
			return fmt.Errorf("quotaorm: local tx failed (%v); release also failed: %w", txErr, relErr)
		}
		return txErr
	}

	// Best-effort sync Confirm so meters update immediately; outbox covers Auth-down.
	if _, cErr := m.client.ConfirmUsage(ctx, apiKey, authsdk.ConfirmUsageInput{ReservationID: res.ReservationID}); cErr == nil {
		now := time.Now().UTC()
		_ = m.db.WithContext(ctx).Model(&QuotaOutbox{}).Where("id = ?", outboxID).Updates(map[string]any{
			"published_at": now,
			"locked_by":    nil,
			"locked_at":    nil,
		}).Error
	}
	return nil
}

// ReconcileSet absolute-sets a gauge dimension (e.g. namespace_count after soft-delete)
// via ReportUsage mode=set. Use this for deletions; creates stay on Reserve→Confirm.
func (m *Manager) ReconcileSet(ctx context.Context, subjectType, subjectID, dimension string, value float64) error {
	if dimension == "" {
		return errors.New("quotaorm: dimension required")
	}
	if value < 0 {
		return errors.New("quotaorm: value must be >= 0")
	}
	apiKey := m.apiKey
	_, err := m.client.ReportUsage(ctx, apiKey, authsdk.ReportUsageInput{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Items: []authsdk.UsageReportItem{{
			DimensionKey: dimension,
			Value:        value,
			Mode:         authsdk.UsageReportModeSet,
		}},
	})
	if err != nil {
		return fmt.Errorf("quotaorm: reconcile set: %w", err)
	}
	return nil
}

func (m *Manager) releaseOrEnqueue(ctx context.Context, apiKey, reservationID, idempotencyKey string) error {
	_, err := m.client.ReleaseUsage(ctx, apiKey, authsdk.ReleaseUsageInput{ReservationID: reservationID})
	if err == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"reservation_id": reservationID})
	row := QuotaOutbox{
		ID:             uuid.NewString(),
		EventID:        "release:" + reservationID,
		Action:         outboxActionRelease,
		ReservationID:  reservationID,
		IdempotencyKey: idempotencyKey,
		Payload:        string(payload),
		AvailableAt:    time.Now().UTC(),
	}
	if dbErr := m.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; dbErr != nil {
		return fmt.Errorf("release http: %v; enqueue: %w", err, dbErr)
	}
	return nil
}
