package obs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func recordingHandle(t *testing.T) (*Handle, *tracetest.SpanRecorder) {
	t.Helper()
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	sr := tracetest.NewSpanRecorder()
	h.tp = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return h, sr
}

func hopSampleCount(t *testing.T, h *Handle, component, operation, peer, status string) uint64 {
	t.Helper()
	mfs := gatherAll(t, h.reg)
	for _, mf := range mfs {
		if mf.GetName() != metricClientRequestDuration {
			continue
		}
		for _, m := range mf.Metric {
			got := map[string]string{}
			for _, lp := range m.Label {
				got[lp.GetName()] = lp.GetValue()
			}
			if got["component"] == component &&
				got["operation"] == operation &&
				got["peer"] == peer &&
				got["status"] == status &&
				m.Histogram != nil {
				return m.Histogram.GetSampleCount()
			}
		}
	}
	return 0
}

func hopFamilyCount(t *testing.T, h *Handle) int {
	t.Helper()
	n := 0
	for _, mf := range gatherAll(t, h.reg) {
		if mf.GetName() == metricClientRequestDuration {
			n++
		}
	}
	return n
}

func TestObserve_SuccessRecordsOK(t *testing.T) {
	h, sr := recordingHandle(t)
	ran := false
	err := h.Observe(context.Background(), Hop{
		Component: "redis",
		Operation: "GET",
		Peer:      "cache",
	}, func(ctx context.Context) error {
		ran = true
		require.True(t, trace.SpanFromContext(ctx).IsRecording())
		return nil
	})
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, uint64(1), hopSampleCount(t, h, "redis", "GET", "cache", "ok"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	attrs := attrMap(spans[0].Attributes())
	require.Equal(t, "redis", attrs["component"])
	require.Equal(t, "GET", attrs["operation"])
	require.Equal(t, "cache", attrs["peer"])
	require.Equal(t, "ok", attrs["status"])
}

func TestObserve_ErrorSetsStatusAndSpanError(t *testing.T) {
	h, sr := recordingHandle(t)
	boom := errors.New("timeout")
	err := h.Observe(context.Background(), Hop{
		Component: "db",
		Operation: "SELECT users",
		Peer:      "postgres",
	}, func(context.Context) error {
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, uint64(1), hopSampleCount(t, h, "db", "SELECT users", "postgres", "error"))
	require.Equal(t, uint64(0), hopSampleCount(t, h, "db", "SELECT users", "postgres", "ok"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.NotEmpty(t, spans[0].Events())
	require.Equal(t, "error", attrMap(spans[0].Attributes())["status"])
}

func TestObserve_NestedChildParentIsOuterSpan(t *testing.T) {
	h, sr := recordingHandle(t)
	err := h.Observe(context.Background(), Hop{
		Component: "http_server",
		Operation: "GET /v1/users/:id",
		Peer:      "unknown",
	}, func(ctx context.Context) error {
		return h.Observe(ctx, Hop{
			Component: "redis",
			Operation: "GET",
			Peer:      "cache",
		}, func(context.Context) error {
			return nil
		})
	})
	require.NoError(t, err)

	spans := sr.Ended()
	require.Len(t, spans, 2)
	var redis, server sdktrace.ReadOnlySpan
	for _, s := range spans {
		switch attrMap(s.Attributes())["component"] {
		case "redis":
			redis = s
		case "http_server":
			server = s
		}
	}
	require.NotNil(t, redis)
	require.NotNil(t, server)
	require.Equal(t, server.SpanContext().SpanID(), redis.Parent().SpanID())
	require.Equal(t, server.SpanContext().TraceID(), redis.SpanContext().TraceID())
}

func TestObserve_NilHandleAndEmptyURLStillRunFn(t *testing.T) {
	ran := 0
	require.NoError(t, (*Handle)(nil).Observe(context.Background(), Hop{Component: "db"}, func(context.Context) error {
		ran++
		return nil
	}))

	h, err := New(Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	require.NoError(t, h.Observe(context.Background(), Hop{Component: "db", Operation: "SELECT", Peer: "postgres"}, func(context.Context) error {
		ran++
		return nil
	}))
	require.Equal(t, 2, ran)
	require.Equal(t, uint64(1), hopSampleCount(t, h, "db", "SELECT", "postgres", "ok"))
}

func TestObserve_DisableMetricsSkipsHistogram(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true, DisableMetrics: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	require.NoError(t, h.Observe(context.Background(), Hop{Component: "db", Operation: "SELECT", Peer: "postgres"}, func(context.Context) error {
		return nil
	}))
	require.Equal(t, uint64(0), hopSampleCount(t, h, "db", "SELECT", "postgres", "ok"))
	require.Equal(t, 0, hopFamilyCount(t, h))
}

func TestObserve_NilFnIsNoop(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	require.NoError(t, h.Observe(context.Background(), Hop{Component: "db"}, nil))
}

func TestObserve_DoubleRegisterOneFamily(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	hop := Hop{Component: "redis", Operation: "GET", Peer: "cache"}
	require.NoError(t, h.Observe(context.Background(), hop, func(context.Context) error { return nil }))
	require.NoError(t, h.Observe(context.Background(), hop, func(context.Context) error { return nil }))
	require.Equal(t, 1, hopFamilyCount(t, h))
	require.Equal(t, uint64(2), hopSampleCount(t, h, "redis", "GET", "cache", "ok"))
}

func TestNormalizeHop_BoundsAndUnknown(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	long := stringsRepeat("x", 200)
	require.NoError(t, h.Observe(context.Background(), Hop{}, func(context.Context) error { return nil }))
	require.Equal(t, uint64(1), hopSampleCount(t, h, "unknown", "unknown", "unknown", "ok"))

	require.NoError(t, h.Observe(context.Background(), Hop{
		Component: long,
		Operation: long,
		Peer:      long,
	}, func(context.Context) error { return nil }))
	require.Equal(t, uint64(1), hopSampleCount(t, h,
		stringsRepeat("x", maxHopComponent),
		stringsRepeat("x", maxHopOperation),
		stringsRepeat("x", maxHopPeer),
		"ok"))
}

func TestTimer_GormStyleBeforeAfter(t *testing.T) {
	h, sr := recordingHandle(t)
	ctx, timer := h.Start(context.Background(), Hop{
		Component: "db",
		Operation: "SELECT users",
		Peer:      "postgres",
	})
	require.True(t, trace.SpanFromContext(ctx).IsRecording())
	timer.End(nil)
	require.Equal(t, uint64(1), hopSampleCount(t, h, "db", "SELECT users", "postgres", "ok"))
	require.Len(t, sr.Ended(), 1)
}

func TestTimer_EndTwiceDoesNotDoubleObserve(t *testing.T) {
	h, sr := recordingHandle(t)
	_, timer := h.Start(context.Background(), Hop{
		Component: "redis",
		Operation: "SET",
		Peer:      "cache",
	})
	timer.End(nil)
	timer.End(errors.New("ignored"))
	require.Equal(t, uint64(1), hopSampleCount(t, h, "redis", "SET", "cache", "ok"))
	require.Equal(t, uint64(0), hopSampleCount(t, h, "redis", "SET", "cache", "error"))
	require.Len(t, sr.Ended(), 1)
}

func TestTimer_NilEndIsNoop(t *testing.T) {
	var tmr *Timer
	require.NotPanics(t, func() { tmr.End(errors.New("x")) })
}

func TestTimer_NilHandleStartStillRunsEnd(t *testing.T) {
	ctx, timer := (*Handle)(nil).Start(context.Background(), Hop{Component: "db", Operation: "SELECT"})
	require.NotNil(t, ctx)
	require.NotPanics(t, func() { timer.End(nil) })
}

func attrMap(attrs []attribute.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, a := range attrs {
		out[string(a.Key)] = a.Value.AsString()
	}
	return out
}

func stringsRepeat(s string, n int) string {
	b := make([]byte, 0, n*len(s))
	for i := 0; i < n; i++ {
		b = append(b, s...)
	}
	return string(b)
}
