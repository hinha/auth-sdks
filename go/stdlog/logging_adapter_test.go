package stdlog_test

import (
	"context"
	"testing"

	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/stretchr/testify/require"
)

func TestLoggingAdapter_MapsLevelsAndAudit(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	l := stdlog.LoggingAdapter(rec)
	ctx := stdlog.ContextWithRequestID(context.Background(), "rid-9")
	l.Log(ctx, logging.LevelInfo, logging.AuditMessage,
		logging.String(logging.FieldKind, logging.KindAudit),
		logging.String(logging.FieldEventType, logging.EventAuthLogin),
	)
	require.Equal(t, "info", rec.lastLevel)
	require.Equal(t, logging.AuditMessage, rec.lastMsg)
	require.Equal(t, "rid-9", fieldValue(rec.fields, stdlog.FieldRequestID))
	require.Equal(t, logging.EventAuthLogin, fieldValue(rec.fields, stdlog.FieldEventType))

	l.Log(ctx, logging.LevelDebug, "d")
	require.Equal(t, "debug", rec.lastLevel)
	l.Log(ctx, logging.LevelWarn, "w")
	require.Equal(t, "warn", rec.lastLevel)
	l.Log(ctx, logging.LevelError, "e")
	require.Equal(t, "error", rec.lastLevel)

	child := l.With(logging.String("k", "v"))
	child.Log(context.Background(), logging.LevelInfo, "x")
	require.Equal(t, "v", fieldValue(rec.fields, "k"))

	stdlog.LoggingAdapter(nil).Log(context.Background(), logging.LevelInfo, "noop")
}
