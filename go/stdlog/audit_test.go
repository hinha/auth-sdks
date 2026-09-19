package stdlog_test

import (
	"testing"
	"time"

	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/stretchr/testify/require"
)

func TestLogAudit_ClosedSchemaAndMessage(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	stdlog.LogAudit(rec, stdlog.AuditEvent{
		EventType:  stdlog.EventAuthLogin,
		Service:    "memoo",
		ActorType:  stdlog.ActorUser,
		ActorID:    "42",
		Decision:   stdlog.DecisionInfo,
		RequestID:  "rid-1",
		OccurredAt: time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC),
	})
	require.Equal(t, "info", rec.lastLevel)
	require.Equal(t, stdlog.AuditLogMessage, rec.lastMsg)

	keys := fieldKeys(rec.fields)
	require.Contains(t, keys, stdlog.FieldKind)
	require.Equal(t, stdlog.KindAudit, fieldValue(rec.fields, stdlog.FieldKind))
	require.Equal(t, stdlog.EventAuthLogin, fieldValue(rec.fields, stdlog.FieldEventType))
	require.Equal(t, stdlog.DecisionInfo, fieldValue(rec.fields, stdlog.FieldDecision))
	require.Equal(t, "auth-sdks", fieldValue(rec.fields, stdlog.FieldSource))
	require.NotEmpty(t, fieldValue(rec.fields, stdlog.FieldEventID))
	require.NotContains(t, keys, "password")
	require.NotContains(t, keys, "access_token")
}

func TestLogAudit_DropsUnknownKeysAndRedactsMachineID(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	stdlog.LogAudit(rec, stdlog.AuditEvent{
		EventType: stdlog.EventAuthAPIKeyVerify,
		ActorType: stdlog.ActorMachine,
		ActorID:   "sa_super_secret_machine_key",
		Decision:  stdlog.DecisionAllow,
	})
	actor := fieldValue(rec.fields, stdlog.FieldActorID).(string)
	require.NotEqual(t, "sa_super_secret_machine_key", actor)
	require.Contains(t, actor, "sa_")
	require.Contains(t, actor, "…")
	require.Less(t, len(actor), len("sa_super_secret_machine_key"))
}

func TestLogAudit_DenyIsWarn_ErrorIsError(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	stdlog.LogAudit(rec, stdlog.AuditEvent{EventType: stdlog.EventAuthAuthorizeAction, Decision: stdlog.DecisionDeny})
	require.Equal(t, "warn", rec.lastLevel)
	stdlog.LogAudit(rec, stdlog.AuditEvent{EventType: stdlog.EventAuthLoginFailed, Decision: stdlog.DecisionError})
	require.Equal(t, "error", rec.lastLevel)
	stdlog.LogAudit(nil, stdlog.AuditEvent{EventType: stdlog.EventAuthLogin})
}

func TestFilterAuditLogFields_DropsSecrets(t *testing.T) {
	t.Parallel()
	out := stdlog.FilterAuditLogFields(map[string]any{
		stdlog.FieldEventType: "auth.login",
		"password":            "secret",
		"access_token":        "tok",
		"extra":               1,
	})
	require.Equal(t, "auth.login", out[stdlog.FieldEventType])
	_, hasPassword := out["password"]
	_, hasToken := out["access_token"]
	_, hasExtra := out["extra"]
	require.False(t, hasPassword)
	require.False(t, hasToken)
	require.False(t, hasExtra)
}

func fieldKeys(fields []stdlog.Field) map[string]struct{} {
	keys := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		keys[f.Key] = struct{}{}
	}
	return keys
}

func fieldValue(fields []stdlog.Field, key string) any {
	for _, f := range fields {
		if f.Key == key {
			return f.Value
		}
	}
	return nil
}
