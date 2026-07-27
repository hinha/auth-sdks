package subnotify

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestSubnotify_IngestDedupeAndUnread_ScopedByRecipient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:subnotify?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	mgr, err := New(db, WithApplicationService("memoo"))
	require.NoError(t, err)
	require.NoError(t, mgr.AutoMigrate(context.Background()))

	payloadA := []byte(`{
		"id":"11111111-1111-1111-1111-111111111111",
		"type":"subscription.downgrade_warning",
		"severity":"warning",
		"source":"auth-service",
		"occurred_at":"2026-07-27T00:00:00Z",
		"recipient":{"user_id":5},
		"dedupe_key":"sub:9:h7:2026-08-01",
		"payload_version":1,
		"payload":{"title":"Plan ending soon","body":"Your Pro plan ends soon.","action_route":"/plans"}
	}`)
	payloadB := []byte(`{
		"id":"33333333-3333-3333-3333-333333333333",
		"type":"subscription.renewal_reminder",
		"severity":"info",
		"source":"auth-service",
		"occurred_at":"2026-07-27T00:00:00Z",
		"recipient":{"user_id":6},
		"dedupe_key":"sub:10:h7:2026-08-01",
		"payload_version":1,
		"payload":{"title":"Plan renewal coming up","body":"You're all set.","action_route":"/plans"}
	}`)
	require.NoError(t, mgr.IngestPayload(context.Background(), payloadA))
	require.NoError(t, mgr.IngestPayload(context.Background(), payloadA)) // dedupe
	require.NoError(t, mgr.IngestPayload(context.Background(), payloadB))

	rowsA, err := mgr.ListUnread(context.Background(), 5, 10)
	require.NoError(t, err)
	require.Len(t, rowsA, 1)
	assert.Equal(t, uint(5), rowsA[0].RecipientUserID)
	assert.Equal(t, "subscription.downgrade_warning", rowsA[0].EventType)

	rowsB, err := mgr.ListUnread(context.Background(), 6, 10)
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
	assert.Equal(t, uint(6), rowsB[0].RecipientUserID)

	rowsOther, err := mgr.ListUnread(context.Background(), 99, 10)
	require.NoError(t, err)
	assert.Empty(t, rowsOther)

	require.NoError(t, mgr.MarkRead(context.Background(), 5, rowsA[0].ID))
	// User B cannot dismiss A's row
	require.NoError(t, mgr.MarkRead(context.Background(), 6, rowsA[0].ID))
	rowsA, err = mgr.ListUnread(context.Background(), 5, 10)
	require.NoError(t, err)
	assert.Empty(t, rowsA)
	rowsB, err = mgr.ListUnread(context.Background(), 6, 10)
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
}

func TestSubnotify_IgnoresMissingRecipient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:subnotify-norecip?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	mgr, err := New(db)
	require.NoError(t, err)
	require.NoError(t, mgr.AutoMigrate(context.Background()))

	payload := []byte(`{
		"id":"11111111-1111-1111-1111-111111111111",
		"type":"subscription.downgrade_warning",
		"severity":"warning",
		"source":"auth-service",
		"occurred_at":"2026-07-27T00:00:00Z",
		"dedupe_key":"sub:9:h7:2026-08-01",
		"payload_version":1,
		"payload":{"title":"Plan ending soon","body":"x","action_route":"/plans"}
	}`)
	require.NoError(t, mgr.IngestPayload(context.Background(), payload))
	rows, err := mgr.ListUnread(context.Background(), 1, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestSubnotify_IgnoresNonSubscription(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:subnotify2?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	mgr, err := New(db)
	require.NoError(t, err)
	require.NoError(t, mgr.AutoMigrate(context.Background()))

	payload := []byte(`{
		"id":"22222222-2222-2222-2222-222222222222",
		"type":"task.failed",
		"severity":"warning",
		"source":"task-hub",
		"occurred_at":"` + time.Now().UTC().Format(time.RFC3339) + `",
		"recipient":{"user_id":1},
		"payload_version":1,
		"payload":{"title":"x","body":"y"}
	}`)
	require.NoError(t, mgr.IngestPayload(context.Background(), payload))
	rows, err := mgr.ListUnread(context.Background(), 1, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
