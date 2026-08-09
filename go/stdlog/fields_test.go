package stdlog_test

import (
	"testing"

	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/stretchr/testify/require"
)

func TestFilterAccessLogFields_DropsUnknown(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		stdlog.FieldRequestID: "r1",
		stdlog.FieldMethod:    "GET",
		"password":            "secret",
		"extra":               1,
	}
	out := stdlog.FilterAccessLogFields(in)
	require.Equal(t, "r1", out[stdlog.FieldRequestID])
	require.Equal(t, "GET", out[stdlog.FieldMethod])
	_, hasPassword := out["password"]
	_, hasExtra := out["extra"]
	require.False(t, hasPassword)
	require.False(t, hasExtra)
}

func TestFilterAccessLogFields_Nil(t *testing.T) {
	t.Parallel()
	out := stdlog.FilterAccessLogFields(nil)
	require.Empty(t, out)
}

func TestIsAccessLogKey(t *testing.T) {
	t.Parallel()
	require.True(t, stdlog.IsAccessLogKey(stdlog.FieldStatus))
	require.False(t, stdlog.IsAccessLogKey("not_a_key"))
}

func TestAccessEventFields_StrictKeysOnly(t *testing.T) {
	t.Parallel()
	e := stdlog.AccessEvent{
		Service:      "x-engine",
		RequestID:    "abc",
		Method:       "POST",
		Path:         "/v1/x",
		Route:        "/v1/x",
		Query:        "a=1",
		Status:       201,
		DurationMS:   12,
		ResponseSize: 4,
		RemoteAddr:   "1.2.3.4:5",
		UserAgent:    "ua",
		UserID:       "u1",
	}
	fields := e.Fields()
	keys := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		require.True(t, stdlog.IsAccessLogKey(f.Key), "unexpected key %q", f.Key)
		keys[f.Key] = struct{}{}
	}
	require.Contains(t, keys, stdlog.FieldComponent)
	require.Contains(t, keys, stdlog.FieldRequestID)
	require.Contains(t, keys, stdlog.FieldMethod)
	require.Contains(t, keys, stdlog.FieldStatus)
}

func TestLevelForStatus(t *testing.T) {
	t.Parallel()
	require.Equal(t, "info", stdlog.LevelForStatus(200))
	require.Equal(t, "warn", stdlog.LevelForStatus(404))
	require.Equal(t, "error", stdlog.LevelForStatus(500))
}

func TestAccessLogMessageConstant(t *testing.T) {
	t.Parallel()
	require.Equal(t, "http request completed", stdlog.AccessLogMessage)
}
