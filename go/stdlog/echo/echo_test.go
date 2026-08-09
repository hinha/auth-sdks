package echoadapter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/stdlog"
	echoadapter "github.com/hinha/auth-sdks/go/stdlog/echo"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

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

func TestEchoMiddleware_AccessLog(t *testing.T) {
	t.Parallel()
	recLog := &recordingLogger{}
	e := echo.New()
	e.Use(echoadapter.Middleware(recLog, stdlog.AccessLogConfig{
		UserIDFunc: func(*http.Request) string { return "7" },
	}))
	e.GET("/v1/items/:id", func(c echo.Context) error {
		require.NotEmpty(t, stdlog.RequestIDFromContext(c.Request().Context()))
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/items/9", nil)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotEmpty(t, rr.Header().Get(stdlog.RequestIDHeader))
	require.Equal(t, stdlog.AccessLogMessage, recLog.lastMsg)
	require.Equal(t, "info", recLog.lastLevel)

	keys := map[string]struct{}{}
	for _, f := range recLog.fields {
		require.True(t, stdlog.IsAccessLogKey(f.Key), f.Key)
		keys[f.Key] = struct{}{}
	}
	require.Contains(t, keys, stdlog.FieldRoute)
	require.Contains(t, keys, stdlog.FieldUserID)
}

func TestEchoMiddleware_NilLogger(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(echoadapter.Middleware(nil, stdlog.AccessLogConfig{}))
	e.GET("/health", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusNoContent, rr.Code)
}
