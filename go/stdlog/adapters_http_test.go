package stdlog_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestNewZerolog_AndSlog_ShareCanonicalServiceField(t *testing.T) {
	t.Parallel()
	zbuf := &bytes.Buffer{}
	zbase := zerolog.New(zbuf).With().Timestamp().Logger()
	_ = zbase

	zl, err := stdlog.NewZerolog(stdlog.Config{Service: "task-hub", Level: "debug", Format: stdlog.FormatJSON})
	require.NoError(t, err)
	zl.Named("worker").With(stdlog.String(stdlog.FieldRequestID, "r1")).Info("ok", stdlog.Int("n", 1))
	require.NoError(t, zl.Sync())

	sl, err := stdlog.NewSlog(stdlog.Config{Service: "memoo", Level: "warn", Format: stdlog.FormatConsole})
	require.NoError(t, err)
	sl.Named("http").Warn("ok", stdlog.Bool("b", true), stdlog.Int64("ms", 2), stdlog.Any("m", map[string]int{"a": 1}))
	sl.Error("e", stdlog.Err(nil))
	sl.Debug("d") // below level, ok
	sl.Fatal("f")
	require.NoError(t, sl.Sync())
}

func TestNewZerolog_RequiresService(t *testing.T) {
	t.Parallel()
	_, err := stdlog.NewZerolog(stdlog.Config{})
	require.Error(t, err)
	_, err = stdlog.NewSlog(stdlog.Config{})
	require.Error(t, err)
}

func TestHTTPMiddleware_EmitsAccessLogAndRequestID(t *testing.T) {
	t.Parallel()
	recLog := &recordingLogger{}
	mw := stdlog.Middleware(recLog, stdlog.AccessLogConfig{
		LogRequestHeaders:  true,
		LogResponseHeaders: true,
		RouteFunc:          func(r *http.Request) string { return "/v1/{id}" },
		UserIDFunc:         func(r *http.Request) string { return "42" },
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, stdlog.RequestIDFromContext(r.Context()))
		require.NotNil(t, stdlog.FromContext(r.Context()))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/9?x=1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	require.NotEmpty(t, rr.Header().Get(stdlog.RequestIDHeader))
	require.Equal(t, "info", recLog.lastLevel)
	require.Equal(t, stdlog.AccessLogMessage, recLog.lastMsg)

	keys := map[string]struct{}{}
	for _, f := range recLog.fields {
		require.True(t, stdlog.IsAccessLogKey(f.Key), f.Key)
		keys[f.Key] = struct{}{}
	}
	require.Contains(t, keys, stdlog.FieldRequestID)
	require.Contains(t, keys, stdlog.FieldStatus)
	require.Contains(t, keys, stdlog.FieldRoute)
	require.Contains(t, keys, stdlog.FieldUserID)
	require.Contains(t, keys, stdlog.FieldRequestHeaders)
}

func TestHTTPMiddleware_NilLogger(t *testing.T) {
	t.Parallel()
	mw := stdlog.Middleware(nil, stdlog.AccessLogConfig{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodOptions, "/health", nil))
	require.Equal(t, http.StatusNoContent, rr.Code)
}

func TestAccessEvent_RequestBodyReadError(t *testing.T) {
	t.Parallel()
	e := stdlog.AccessEvent{
		Status:               200,
		RequestBodyReadError: errSample{},
	}
	fields := e.Fields()
	var sawBool, sawMsg bool
	for _, f := range fields {
		if f.Key == stdlog.FieldRequestBodyReadError {
			sawBool = true
		}
		if f.Key == stdlog.FieldRequestBodyReadErrMsg {
			sawMsg = true
		}
	}
	require.True(t, sawBool)
	require.True(t, sawMsg)
}

type errSample struct{}

func (errSample) Error() string { return "read fail" }
