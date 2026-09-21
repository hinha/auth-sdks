package obs

import (
	"context"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
)

func TestObserve_PanicStillEndsSpan(t *testing.T) {
	h, sr := recordingHandle(t)
	require.Panics(t, func() {
		_ = h.Observe(context.Background(), Hop{
			Component: "redis",
			Operation: "GET",
			Peer:      "cache",
		}, func(context.Context) error {
			panic("boom")
		})
	})
	require.Equal(t, uint64(1), hopSampleCount(t, h, "redis", "GET", "cache", "error"))
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestObserve_BoundedSeriesForRepeatedHops(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	hop := Hop{Component: "redis", Operation: "GET", Peer: "cache"}
	for i := 0; i < 1000; i++ {
		require.NoError(t, h.Observe(context.Background(), hop, func(context.Context) error { return nil }))
	}
	require.Equal(t, uint64(1000), hopSampleCount(t, h, "redis", "GET", "cache", "ok"))
	require.Equal(t, 1, hopSeriesCardinality(t, h))
}

func TestObserve_DoesNotSpawnGoroutines(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	runtime.GC()
	before := runtime.NumGoroutine()
	hop := Hop{Component: "db", Operation: "SELECT users", Peer: "postgres"}
	for i := 0; i < 200; i++ {
		require.NoError(t, h.Observe(context.Background(), hop, func(context.Context) error { return nil }))
	}
	runtime.GC()
	after := runtime.NumGoroutine()
	require.LessOrEqual(t, after, before+5, "Observe must not leak goroutines (before=%d after=%d)", before, after)
}

func TestObserve_CapsDistinctSeries(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	for i := 0; i < maxHopSeries+200; i++ {
		op := "op-" + strconv.Itoa(i)
		require.NoError(t, h.Observe(context.Background(), Hop{
			Component: "db",
			Operation: op,
			Peer:      "postgres",
		}, func(context.Context) error { return nil }))
	}
	n := hopSeriesCardinality(t, h)
	require.LessOrEqual(t, n, maxHopSeries+1,
		"unbounded hop labels must collapse instead of leaking HistogramVec series (got %d)", n)
	require.Greater(t, hopSampleCount(t, h, hopStatusUnknown, hopStatusUnknown, hopStatusUnknown, hopStatusUnknown), uint64(0),
		"overflow samples still record on the unknown series")
}

func hopSeriesCardinality(t *testing.T, h *Handle) int {
	t.Helper()
	n := 0
	for _, mf := range gatherAll(t, h.reg) {
		if mf.GetName() != metricClientRequestDuration {
			continue
		}
		n += len(mf.Metric)
	}
	return n
}
