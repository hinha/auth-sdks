package quotaorm

import (
	"time"

	"gorm.io/gorm"
)

const (
	outboxActionConfirm = "confirm"
	outboxActionRelease = "release"
)

// QuotaOutbox is the consumer-DB outbox row managed by this package.
// Table name is stable across SDK versions so AutoMigrate stays additive.
type QuotaOutbox struct {
	ID             string         `gorm:"column:id;type:varchar(36);primaryKey"`
	EventID        string         `gorm:"column:event_id;type:varchar(64);uniqueIndex;not null"`
	Action         string         `gorm:"column:action;type:varchar(16);not null"` // confirm|release
	ReservationID  string         `gorm:"column:reservation_id;type:varchar(64);not null;index"`
	IdempotencyKey string         `gorm:"column:idempotency_key;type:varchar(255);not null"`
	Payload        string         `gorm:"column:payload;type:text;not null;default:'{}'"`
	Attempts       int            `gorm:"column:attempts;not null;default:0"`
	AvailableAt    time.Time      `gorm:"column:available_at;not null;index"`
	PublishedAt    *time.Time     `gorm:"column:published_at"`
	DeadLetteredAt *time.Time     `gorm:"column:dead_lettered_at"`
	DeadLetterReason string       `gorm:"column:dead_letter_reason;type:text"`
	LockedBy       *string        `gorm:"column:locked_by;type:varchar(64)"`
	LockedAt       *time.Time     `gorm:"column:locked_at"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (QuotaOutbox) TableName() string { return "auth_sdk_quota_outbox" }
