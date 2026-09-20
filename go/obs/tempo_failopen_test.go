package obs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

// TestNew_TracerFailureIsFailOpen pins the fail-open change. Before it, a tracer
// that failed to construct stopped the metrics loop and returned a nil handle,
// so a tracing problem silently removed metrics and profiles as well.
//
// The failure is injected through the newTracer seam rather than a bad URL: the
// OTLP exporter validates lazily, so no URL makes the constructor fail.
func TestNew_TracerFailureIsFailOpen(t *testing.T) {
	orig := newTracer
	defer func() { newTracer = orig }()

	var (
		mu   sync.Mutex
		msgs []string
	)
	newTracer = func(context.Context, Config) (trace.TracerProvider, func(context.Context) error, error) {
		return nil, nil, errors.New("exporter unavailable")
	}

	rec := newRecordingServer(t)
	h, err := New(Config{
		URL:             rec.srv.URL,
		Service:         "svc",
		MetricsInterval: time.Hour,
		DisableProfiles: true,
		Logger: func(m string) {
			mu.Lock()
			msgs = append(msgs, m)
			mu.Unlock()
		},
	})
	require.NoError(t, err, "a tracer failure must not fail construction")
	require.NotNil(t, h, "the caller must still receive a usable handle")
	t.Cleanup(func() { require.NoError(t, h.Shutdown(context.Background())) })

	// The tracer degrades to no-op rather than leaving a nil provider.
	require.NotNil(t, h.Tracer("test"))

	// Metrics must keep flowing: the immediate push proves the loop was started,
	// which is precisely what the old code prevented.
	require.Eventually(t, func() bool { return rec.count() > 0 }, 2*time.Second, 10*time.Millisecond,
		"metrics must still be pushed after a tracer failure")
	require.True(t, rec.hasSeries(metricTargetInfo))

	mu.Lock()
	defer mu.Unlock()
	require.True(t, containsPrefix(msgs, "gigapipe tempo start failed"),
		"the tracer failure must be reported, got %v", msgs)
}
