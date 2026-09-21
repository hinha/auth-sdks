package obs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestDefaultCollectors_RepeatedGatherStableCardinality(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{})
	first := gatherAll(t, h.collectorSet())
	requireNoDuplicates(t, first)
	ioN := familyCount(first, metricProcessIOReadBytes)
	fsN := familyCount(first, metricProcessFilesystemAvail)
	for i := 0; i < 200; i++ {
		got := gatherAll(t, h.collectorSet())
		requireNoDuplicates(t, got)
		require.Equal(t, ioN, familyCount(got, metricProcessIOReadBytes))
		require.Equal(t, fsN, familyCount(got, metricProcessFilesystemAvail))
	}
}

func TestDefaultCollectors_RepeatedGatherDoesNotSpawnGoroutines(t *testing.T) {
	h := newTestHandle(t, Config{})
	for i := 0; i < 5; i++ {
		_ = gatherAll(t, h.collectorSet())
	}
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		_ = gatherAll(t, h.collectorSet())
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	require.LessOrEqual(t, after, before+5, "default collectors must not leak goroutines (before=%d after=%d)", before, after)
}

func TestShutdown_StopsMetricsLoop(t *testing.T) {
	var pushes atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	h, err := New(Config{
		URL:             srv.URL,
		Service:         "leak-test",
		DisableTraces:   true,
		DisableProfiles: true,
		MetricsInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return pushes.Load() >= 1 }, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, h.Shutdown(context.Background()))
	n := pushes.Load()
	time.Sleep(80 * time.Millisecond)
	require.Equal(t, n, pushes.Load(), "metrics loop must not push after Shutdown")
}

func TestHTTPMiddleware_DoesNotSpawnGoroutines(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	inner := h.HTTPMiddleware(WithRoute(func(*http.Request) string { return "/v1/x" }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	for i := 0; i < 5; i++ {
		inner.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/v1/x", nil))
	}
	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 200; i++ {
		inner.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/v1/x", nil))
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	require.LessOrEqual(t, after, before+5, "HTTPMiddleware must not leak goroutines (before=%d after=%d)", before, after)
	require.Equal(t, 1, hopSeriesCardinality(t, h))
}

func TestWrapTransport_BoundedSeriesForSamePeer(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	client := &http.Client{Transport: h.WrapTransport(http.DefaultTransport, WithPeer("upstream"))}
	for i := 0; i < 50; i++ {
		resp, err := client.Get(upstream.URL)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	require.Equal(t, 1, hopSeriesCardinality(t, h))
}

func familyCount(mfs []*dto.MetricFamily, name string) int {
	n := 0
	for _, mf := range mfs {
		if mf != nil && mf.GetName() == name {
			n += len(mf.Metric)
		}
	}
	return n
}
