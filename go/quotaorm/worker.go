package quotaorm

import (
	"context"
	"fmt"
	"time"

	authsdk "github.com/hinha/auth-sdks/go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StartOutboxWorker polls auth_sdk_quota_outbox and Confirm/Release against Auth.
// Blocks until ctx is cancelled. Call from a dedicated goroutine.
func (m *Manager) StartOutboxWorker(ctx context.Context) {
	ticker := time.NewTicker(m.pollEvery)
	defer ticker.Stop()
	for {
		_ = m.ProcessOutboxOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ProcessOutboxOnce claims and processes one batch (useful for tests).
func (m *Manager) ProcessOutboxOnce(ctx context.Context) error {
	now := time.Now().UTC()
	lockExpiry := now.Add(-m.lockTTL)

	var rows []QuotaOutbox
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("published_at IS NULL AND dead_lettered_at IS NULL AND available_at <= ?", now).
			Where("(locked_at IS NULL OR locked_at < ?)", lockExpiry).
			Order("available_at ASC").
			Limit(m.batchSize)
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, len(rows))
		for i := range rows {
			ids[i] = rows[i].ID
		}
		return tx.Model(&QuotaOutbox{}).Where("id IN ?", ids).Updates(map[string]any{
			"locked_by": m.workerID,
			"locked_at": now,
		}).Error
	})
	if err != nil {
		return err
	}

	for i := range rows {
		if procErr := m.processRow(ctx, &rows[i]); procErr != nil {
			_ = m.scheduleRetry(ctx, &rows[i], procErr)
		}
	}
	return nil
}

func (m *Manager) processRow(ctx context.Context, row *QuotaOutbox) error {
	apiKey := m.apiKey
	var err error
	switch row.Action {
	case outboxActionConfirm:
		_, err = m.client.ConfirmUsage(ctx, apiKey, authsdk.ConfirmUsageInput{ReservationID: row.ReservationID})
	case outboxActionRelease:
		_, err = m.client.ReleaseUsage(ctx, apiKey, authsdk.ReleaseUsageInput{ReservationID: row.ReservationID})
	case outboxActionLocalFollowUp:
		if m.followUp == nil {
			err = fmt.Errorf("quotaorm: no FollowUpHandler registered for local_followup")
		} else {
			err = m.followUp(ctx, m.db, row.ReservationID, row.Payload)
		}
	default:
		err = fmt.Errorf("unknown outbox action %q", row.Action)
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return m.db.WithContext(ctx).Model(&QuotaOutbox{}).Where("id = ?", row.ID).Updates(map[string]any{
		"published_at": now,
		"locked_by":    nil,
		"locked_at":    nil,
	}).Error
}

func (m *Manager) scheduleRetry(ctx context.Context, row *QuotaOutbox, cause error) error {
	attempts := row.Attempts + 1
	updates := map[string]any{
		"attempts":  attempts,
		"locked_by": nil,
		"locked_at": nil,
	}
	if attempts >= m.maxAttempts {
		now := time.Now().UTC()
		updates["dead_lettered_at"] = now
		updates["dead_letter_reason"] = cause.Error()
	} else {
		// exponential-ish backoff: 2^attempts seconds, capped at 5m
		delay := time.Duration(1<<min(attempts, 8)) * time.Second
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		updates["available_at"] = time.Now().UTC().Add(delay)
	}
	return m.db.WithContext(ctx).Model(&QuotaOutbox{}).Where("id = ?", row.ID).Updates(updates).Error
}
