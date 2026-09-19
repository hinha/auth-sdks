package stdlog_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/stretchr/testify/require"
)

func TestWrapLoki_EmptyURLIsNoop(t *testing.T) {
	t.Parallel()
	base := stdlog.Nop()
	got, err := stdlog.WrapLoki(base, stdlog.LokiConfig{})
	require.NoError(t, err)
	require.Equal(t, base, got)
}

type lokiPush struct {
	Streams []struct {
		Stream map[string]string `json:"stream"`
		Values [][]string        `json:"values"`
	} `json:"streams"`
}

func TestWrapLoki_PushesJSONWithBasicAuthAndKindLabels(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var gotAuth, gotUA, gotCT string
	var bodies []lokiPush
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/loki/api/v1/push", r.URL.Path)
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		var body lokiPush
		require.NoError(t, json.Unmarshal(raw, &body))
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	base := &recordingLogger{}
	log, err := stdlog.WrapLoki(base, stdlog.LokiConfig{
		URL:           srv.URL,
		Username:      "pipeuser",
		Password:      "pipepass",
		Env:           "test",
		Service:       "memoo",
		FlushInterval: time.Hour,
		BatchSize:     100,
		MaxBuffer:     1000,
	})
	require.NoError(t, err)

	stdlog.LogAudit(log, stdlog.AuditEvent{
		EventType: stdlog.EventAuthLogin,
		Decision:  stdlog.DecisionInfo,
		Service:   "memoo",
	})
	log.Info(stdlog.AccessLogMessage, stdlog.Int(stdlog.FieldStatus, 200))
	log.Info("worker started")
	require.NoError(t, log.Sync())

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, gotAuth, "Basic ")
	require.Equal(t, "auth-sdks-stdlog/1", gotUA)
	require.Equal(t, "application/json", gotCT)
	require.NotEmpty(t, bodies)

	kinds := map[string]struct{}{}
	for _, b := range bodies {
		for _, s := range b.Streams {
			require.Equal(t, "memoo", s.Stream["service"])
			require.Equal(t, "test", s.Stream["env"])
			kinds[s.Stream["kind"]] = struct{}{}
			require.NotEmpty(t, s.Values)
			require.Len(t, s.Values[0], 2)
		}
	}
	require.Contains(t, kinds, stdlog.KindAudit)
	require.Contains(t, kinds, stdlog.KindAccess)
	require.Contains(t, kinds, stdlog.KindApp)
	require.Equal(t, "worker started", base.lastMsg)
}

func TestWrapLoki_DropsOldestWhenBufferFull(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var lines []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body lokiPush
		require.NoError(t, json.Unmarshal(raw, &body))
		mu.Lock()
		for _, s := range body.Streams {
			for _, v := range s.Values {
				if len(v) > 1 {
					lines = append(lines, v[1])
				}
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	log, err := stdlog.WrapLoki(stdlog.Nop(), stdlog.LokiConfig{
		URL:           srv.URL,
		FlushInterval: time.Hour,
		BatchSize:     100,
		MaxBuffer:     2,
		Service:       "svc",
	})
	require.NoError(t, err)
	log.Info("one")
	log.Info("two")
	log.Info("three")
	require.NoError(t, log.Sync())

	mu.Lock()
	defer mu.Unlock()
	joined := ""
	for _, l := range lines {
		joined += l
	}
	require.NotContains(t, joined, `"msg":"one"`)
	require.Contains(t, joined, `"msg":"two"`)
	require.Contains(t, joined, `"msg":"three"`)
}

func TestWrapLoki_HTTP401DoesNotPanic(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	log, err := stdlog.WrapLoki(stdlog.Nop(), stdlog.LokiConfig{
		URL:           srv.URL,
		Username:      "u",
		Password:      "p",
		FlushInterval: time.Millisecond,
		BatchSize:     1,
		Service:       "svc",
	})
	require.NoError(t, err)
	require.NotPanics(t, func() {
		log.Info("hello")
		_ = log.Sync()
	})
}
