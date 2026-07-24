package quotaorm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LocalFollowUp is a deferred local write that must succeed after a remote
// Auth mutation (e.g. set active_organization_id after CreateOrganization).
const outboxActionLocalFollowUp = "local_followup"

// FollowUpHandler runs a local compensate/retry for a follow-up outbox row.
// Register via Manager.SetFollowUpHandler before StartOutboxWorker.
type FollowUpHandler func(ctx context.Context, db *gorm.DB, eventID, payload string) error

// SetFollowUpHandler registers the P1 saga local-follow-up processor.
func (m *Manager) SetFollowUpHandler(h FollowUpHandler) {
	m.followUp = h
}

// EnqueueLocalFollowUp inserts a durable retry row after a remote Auth
// success when the local side still needs to catch up.
func (m *Manager) EnqueueLocalFollowUp(ctx context.Context, tx *gorm.DB, eventID, payload string) error {
	if eventID == "" {
		return fmt.Errorf("quotaorm: follow-up eventID required")
	}
	db := m.db
	if tx != nil {
		db = tx
	}
	row := QuotaOutbox{
		ID:            uuid.NewString(),
		EventID:       "followup:" + eventID,
		Action:        outboxActionLocalFollowUp,
		ReservationID: eventID,
		Payload:       payload,
		AvailableAt:   time.Now().UTC(),
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}
