// Package subnotify injects auth_sdk_subscription_notifications into the
// consumer database and ingests Auth Service notification.created events
// (subscription lifecycle H-7 / renew / downgrade) via NATS JetStream.
package subnotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	createdSubject = "platform.notifications.v1.created"
	defaultStream  = "PLATFORM_NOTIFICATIONS"
)

// InboxRow is the consumer-local notification mirror.
// RecipientUserID is the Auth consumer user id from the NATS event — list/mark
// must scope by this so User A's H-7 never surfaces for User B on the same DB.
type InboxRow struct {
	ID                 string     `gorm:"column:id;type:varchar(36);primaryKey"`
	DedupeKey          string     `gorm:"column:dedupe_key;type:varchar(191);uniqueIndex:idx_subnotify_dedupe_recipient;not null"`
	RecipientUserID    uint       `gorm:"column:recipient_user_id;uniqueIndex:idx_subnotify_dedupe_recipient;index;not null;default:0"`
	EventType          string     `gorm:"column:event_type;type:varchar(100);not null"`
	Severity           string     `gorm:"column:severity;type:varchar(16);not null;default:info"`
	Title              string     `gorm:"column:title;type:varchar(160);not null"`
	Body               string     `gorm:"column:body;type:text;not null;default:''"`
	ActionRoute        string     `gorm:"column:action_route;type:varchar(255);not null;default:''"`
	ApplicationService string     `gorm:"column:application_service;type:varchar(64);not null;default:''"`
	SourceService      string     `gorm:"column:source_service;type:varchar(64);not null;default:''"`
	OccurredAt         time.Time  `gorm:"column:occurred_at;not null"`
	ReadAt             *time.Time `gorm:"column:read_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (InboxRow) TableName() string { return "auth_sdk_subscription_notifications" }

// raisedWire matches Auth Service notification event v1 JSON.
type raisedWire struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Severity       string `json:"severity"`
	Source         string `json:"source"`
	OccurredAt     time.Time `json:"occurred_at"`
	DedupeKey      string `json:"dedupe_key"`
	PayloadVersion int    `json:"payload_version"`
	Recipient      *struct {
		UserID uint `json:"user_id"`
	} `json:"recipient,omitempty"`
	Payload struct {
		Title       string `json:"title"`
		Body        string `json:"body"`
		ActionRoute string `json:"action_route"`
	} `json:"payload"`
}

// Manager owns AutoMigrate, inbox CRUD, and optional NATS consumer.
type Manager struct {
	db                 *gorm.DB
	applicationService string
	natsURL            string
	natsUser           string
	natsPass           string
	stream             string
	durable            string
}

// Option configures Manager.
type Option func(*Manager)

// WithApplicationService filters ingested events when set (optional).
func WithApplicationService(name string) Option {
	return func(m *Manager) { m.applicationService = name }
}

// WithNATS enables JetStream consume of notification.created.
func WithNATS(url, user, pass string) Option {
	return func(m *Manager) {
		m.natsURL = url
		m.natsUser = user
		m.natsPass = pass
	}
}

// WithStreamDurable overrides JetStream stream/durable names.
func WithStreamDurable(stream, durable string) Option {
	return func(m *Manager) {
		m.stream = stream
		m.durable = durable
	}
}

// New binds the consumer *gorm.DB.
func New(db *gorm.DB, opts ...Option) (*Manager, error) {
	if db == nil {
		return nil, errors.New("subnotify: nil *gorm.DB")
	}
	m := &Manager{
		db:      db,
		stream:  defaultStream,
		durable: "auth-sdk-subnotify-v1",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m, nil
}

// AutoMigrate creates/updates auth_sdk_subscription_notifications.
func (m *Manager) AutoMigrate(ctx context.Context) error {
	return m.db.WithContext(ctx).AutoMigrate(&InboxRow{})
}

// IngestPayload upserts one Auth notification.created JSON payload.
func (m *Manager) IngestPayload(ctx context.Context, raw []byte) error {
	var wire raisedWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return fmt.Errorf("subnotify decode: %w", err)
	}
	if wire.Source != "auth-service" {
		return nil
	}
	if !strings.HasPrefix(wire.Type, "subscription.") {
		return nil
	}
	dedupe := wire.DedupeKey
	if dedupe == "" {
		dedupe = wire.ID
	}
	if dedupe == "" {
		dedupe = uuid.NewString()
	}
	id := wire.ID
	if id == "" {
		id = uuid.NewString()
	}
	var recipientUID uint
	if wire.Recipient != nil {
		recipientUID = wire.Recipient.UserID
	}
	if recipientUID == 0 {
		// Without a recipient we cannot isolate per user — skip ingest.
		return nil
	}
	row := InboxRow{
		ID:                 id,
		DedupeKey:          dedupe,
		RecipientUserID:    recipientUID,
		EventType:          wire.Type,
		Severity:           wire.Severity,
		Title:              wire.Payload.Title,
		Body:               wire.Payload.Body,
		ActionRoute:        wire.Payload.ActionRoute,
		ApplicationService: m.applicationService,
		SourceService:      wire.Source,
		OccurredAt:         wire.OccurredAt,
	}
	if row.OccurredAt.IsZero() {
		row.OccurredAt = time.Now().UTC()
	}
	return m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "dedupe_key"},
			{Name: "recipient_user_id"},
		},
		DoNothing: true,
	}).Create(&row).Error
}

// ListUnread returns unread inbox rows for one Auth recipient user, newest first.
func (m *Manager) ListUnread(ctx context.Context, recipientUserID uint, limit int) ([]InboxRow, error) {
	if recipientUserID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	var rows []InboxRow
	err := m.db.WithContext(ctx).
		Where("read_at IS NULL AND recipient_user_id = ?", recipientUserID).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// MarkRead sets read_at for one row id owned by recipientUserID.
func (m *Manager) MarkRead(ctx context.Context, recipientUserID uint, id string) error {
	if recipientUserID == 0 || strings.TrimSpace(id) == "" {
		return nil
	}
	now := time.Now().UTC()
	res := m.db.WithContext(ctx).Model(&InboxRow{}).
		Where("id = ? AND recipient_user_id = ? AND read_at IS NULL", id, recipientUserID).
		Update("read_at", now)
	return res.Error
}

// StartConsumer blocks until ctx is done, consuming notification.created.
func (m *Manager) StartConsumer(ctx context.Context) error {
	if m.natsURL == "" {
		return errors.New("subnotify: NATS URL not configured")
	}
	opts := []nats.Option{nats.Name("auth-sdk-subnotify")}
	if m.natsUser != "" {
		opts = append(opts, nats.UserInfo(m.natsUser, m.natsPass))
	}
	conn, err := nats.Connect(m.natsURL, opts...)
	if err != nil {
		return fmt.Errorf("subnotify nats connect: %w", err)
	}
	defer conn.Close()

	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("subnotify jetstream: %w", err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, m.stream, jetstream.ConsumerConfig{
		Durable:       m.durable,
		FilterSubject: createdSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("subnotify consumer: %w", err)
	}
	_, err = consumer.Consume(func(msg jetstream.Msg) {
		_ = m.IngestPayload(ctx, msg.Data())
		_ = msg.Ack()
	})
	if err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
