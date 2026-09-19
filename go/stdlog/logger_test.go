package stdlog_test

import (
	"net/http"
	"testing"

	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/stretchr/testify/require"
)

func TestNewZap_RequiresService(t *testing.T) {
	t.Parallel()
	_, err := stdlog.NewZap(stdlog.Config{})
	require.Error(t, err)
}

func TestNewZap_InvalidLevel(t *testing.T) {
	t.Parallel()
	_, err := stdlog.NewZap(stdlog.Config{Service: "svc", Level: "nope"})
	require.Error(t, err)
}

func TestNewZap_InvalidFormat(t *testing.T) {
	t.Parallel()
	_, err := stdlog.NewZap(stdlog.Config{Service: "svc", Format: "xml"})
	require.Error(t, err)
}

func TestNewZap_LogsJSONWithService(t *testing.T) {
	t.Parallel()
	l, err := stdlog.NewZap(stdlog.Config{
		Service: "auth-test",
		Level:   "debug",
		Format:  stdlog.FormatJSON,
	})
	require.NoError(t, err)

	child := l.Named("http").With(stdlog.String(stdlog.FieldRequestID, "r1"))
	child.Debug("d")
	child.Info("i")
	child.Warn("w")
	child.Error("e", stdlog.Err(nil))
	_ = child.Sync() // stderr sync may fail in tests
}

func TestNewZap_ConsoleFormat(t *testing.T) {
	t.Parallel()
	l, err := stdlog.NewZap(stdlog.Config{
		Service:      "svc",
		Level:        "info",
		Format:       "pretty",
		TimeLocation: "Asia/Jakarta",
	})
	require.NoError(t, err)
	l.Info("hello")
	_ = l.Sync()
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Authorization", "Bearer x")
	h.Set("X-Request-Id", "r1")
	h.Set("X-Api-Key", "secret")
	out := stdlog.RedactHeaders(h)
	_, hasAuth := out["Authorization"]
	require.False(t, hasAuth)
	require.Equal(t, "r1", out["X-Request-Id"])
	_, hasKey := out["X-Api-Key"]
	require.False(t, hasKey)
}

func TestRedactJSON(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"password": "p",
		"nested": map[string]any{
			"access_token": "t",
			"ok":           1,
		},
		"list": []any{
			map[string]any{"token": "x", "n": 2},
		},
	}
	out := stdlog.RedactJSON(in).(map[string]any)
	require.Equal(t, "***", out["password"])
	nested := out["nested"].(map[string]any)
	require.Equal(t, "***", nested["access_token"])
	require.Equal(t, 1, nested["ok"])
	list := out["list"].([]any)
	item := list[0].(map[string]any)
	require.Equal(t, "***", item["token"])
}

func TestContextRequestIDAndLogger(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", stdlog.RequestIDFromContext(nil))
	require.NotNil(t, stdlog.FromContext(nil))

	l, err := stdlog.NewZap(stdlog.Config{Service: "svc"})
	require.NoError(t, err)
	ctx := stdlog.ContextWithRequestID(nil, "rid")
	ctx = stdlog.ContextWithLogger(ctx, l)
	require.Equal(t, "rid", stdlog.RequestIDFromContext(ctx))
	require.NotNil(t, stdlog.FromContext(ctx))

	bound := stdlog.LoggerWithRequestID(l, "rid2")
	bound.Info("x")
	nopBound := stdlog.LoggerWithRequestID(nil, "")
	nopBound.Info("y")
}

func TestLogAccess_Levels(t *testing.T) {
	t.Parallel()
	rec := &recordingLogger{}
	stdlog.LogAccess(rec, stdlog.AccessEvent{Status: 200, Method: "GET", Path: "/"})
	require.Equal(t, "info", rec.lastLevel)
	require.Equal(t, stdlog.AccessLogMessage, rec.lastMsg)

	stdlog.LogAccess(rec, stdlog.AccessEvent{Status: 404})
	require.Equal(t, "warn", rec.lastLevel)

	stdlog.LogAccess(rec, stdlog.AccessEvent{Status: 500})
	require.Equal(t, "error", rec.lastLevel)

	stdlog.LogAccess(nil, stdlog.AccessEvent{Status: 200})
}

type recordingLogger struct {
	lastLevel string
	lastMsg   string
	fields    []stdlog.Field
}

func (r *recordingLogger) Debug(msg string, fields ...stdlog.Field) {
	r.lastLevel, r.lastMsg, r.fields = "debug", msg, fields
}
func (r *recordingLogger) Info(msg string, fields ...stdlog.Field) {
	r.lastLevel, r.lastMsg, r.fields = "info", msg, fields
}
func (r *recordingLogger) Warn(msg string, fields ...stdlog.Field) {
	r.lastLevel, r.lastMsg, r.fields = "warn", msg, fields
}
func (r *recordingLogger) Error(msg string, fields ...stdlog.Field) {
	r.lastLevel, r.lastMsg, r.fields = "error", msg, fields
}
func (r *recordingLogger) Fatal(msg string, fields ...stdlog.Field) {
	r.lastLevel, r.lastMsg, r.fields = "fatal", msg, fields
}
func (r *recordingLogger) With(...stdlog.Field) stdlog.Logger { return r }
func (r *recordingLogger) Named(string) stdlog.Logger         { return r }
func (r *recordingLogger) Sync() error                        { return nil }

func TestNopLogger(t *testing.T) {
	t.Parallel()
	l := stdlog.Nop()
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Fatal("f")
	require.NoError(t, l.With(stdlog.String("a", "b")).Named("c").Sync())
}
