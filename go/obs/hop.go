package obs

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	metricClientRequestDuration = "client_request_duration_seconds"
	hopTracerName               = "obs"

	maxHopComponent = 32
	maxHopOperation = 128
	maxHopPeer      = 64
	maxHopStatus    = 16
	maxHopSeries    = 1024

	hopComponentHTTPServer = "http_server"
	hopComponentHTTPClient = "http_client"

	hopStatusOK      = "ok"
	hopStatusError   = "error"
	hopStatusUnknown = "unknown"
)

var errHopPanic = errors.New("panic")

// Hop is one client or server step in a request (HTTP, DB, Redis, …).
// Label values must be templates / classes, never raw keys, SQL, or URLs with ids.
type Hop struct {
	Component string
	Operation string
	Peer      string
}

// Timer records one hop started with Start. End is idempotent.
type Timer struct {
	h     *Handle
	hop   Hop
	span  trace.Span
	start time.Time
	ended atomic.Bool
}

// Observe runs fn, records duration on the Handle registry, and starts a child
// span on Handle.Tracer using ctx. Nil Handle and empty-URL handles do not panic;
// fn still runs. A nil fn is a no-op.
func (h *Handle) Observe(ctx context.Context, hop Hop, fn func(context.Context) error) (err error) {
	if fn == nil {
		return nil
	}
	if h == nil {
		return fn(ctx)
	}
	ctx, timer := h.Start(ctx, hop)
	defer func() {
		if r := recover(); r != nil {
			timer.End(errHopPanic)
			panic(r)
		}
		timer.End(err)
	}()
	err = fn(ctx)
	return err
}

// Start begins a hop for split Before/After hooks (GORM, go-redis). The caller
// must End the Timer; End is safe to call twice.
func (h *Handle) Start(ctx context.Context, hop Hop) (context.Context, *Timer) {
	hop = normalizeHop(hop)
	if ctx == nil {
		ctx = context.Background()
	}
	t := &Timer{h: h, hop: hop, start: time.Now()}
	if h == nil {
		return ctx, t
	}
	ctx, t.span = h.startHopSpan(ctx, hop)
	return ctx, t
}

// End records metrics and closes the span. A nil Timer is a no-op.
func (t *Timer) End(err error) {
	if t == nil || !t.ended.CompareAndSwap(false, true) {
		return
	}
	t.finish(statusFromHop(t.hop.Component, err, 0), err)
}

func (t *Timer) endHTTP(code int, err error) {
	if t == nil || !t.ended.CompareAndSwap(false, true) {
		return
	}
	t.finish(httpStatusLabel(code), err)
}

func (t *Timer) finish(status string, err error) {
	status = boundLabel(status, maxHopStatus)
	if t.span != nil {
		t.span.SetAttributes(
			attribute.String("component", t.hop.Component),
			attribute.String("operation", t.hop.Operation),
			attribute.String("peer", t.hop.Peer),
			attribute.String("status", status),
		)
		if err != nil && !errors.Is(err, errHopPanic) {
			t.span.RecordError(err)
		}
		if err != nil || isHTTP5xx(status) {
			msg := status
			if err != nil && !errors.Is(err, errHopPanic) {
				msg = err.Error()
			}
			t.span.SetStatus(codes.Error, msg)
		}
		t.span.End()
	}
	if t.h != nil {
		t.h.observeHop(t.hop, status, time.Since(t.start))
	}
}

func (h *Handle) startHopSpan(ctx context.Context, hop Hop) (context.Context, trace.Span) {
	kind := trace.SpanKindClient
	if hop.Component == hopComponentHTTPServer {
		kind = trace.SpanKindServer
	}
	return h.Tracer(hopTracerName).Start(ctx, hop.Operation,
		trace.WithSpanKind(kind),
		trace.WithAttributes(hopSemconvAttrs(hop)...),
	)
}

func (h *Handle) observeHop(hop Hop, status string, d time.Duration) {
	if h == nil || h.cfg.DisableMetrics {
		return
	}
	hv := h.hopMetrics()
	if hv == nil {
		return
	}
	hop, status = h.limitHopSeries(hop, status)
	hv.WithLabelValues(hop.Component, hop.Operation, hop.Peer, status).Observe(d.Seconds())
}

func (h *Handle) limitHopSeries(hop Hop, status string) (Hop, string) {
	key := hop.Component + "\x00" + hop.Operation + "\x00" + hop.Peer + "\x00" + status
	h.hopSeriesMu.Lock()
	defer h.hopSeriesMu.Unlock()
	if h.hopSeries == nil {
		h.hopSeries = make(map[string]struct{}, maxHopSeries)
	}
	if _, ok := h.hopSeries[key]; ok {
		return hop, status
	}
	if len(h.hopSeries) >= maxHopSeries {
		return Hop{
			Component: hopStatusUnknown,
			Operation: hopStatusUnknown,
			Peer:      hopStatusUnknown,
		}, hopStatusUnknown
	}
	h.hopSeries[key] = struct{}{}
	return hop, status
}

func (h *Handle) hopMetrics() *prometheus.HistogramVec {
	if h == nil || h.reg == nil {
		return nil
	}
	h.hopOnce.Do(func() {
		h.hopDuration = registerOrReuse(h.reg, prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    metricClientRequestDuration,
			Help:    "Duration of a client hop (HTTP, DB, Redis, …) in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"component", "operation", "peer", "status"}))
	})
	return h.hopDuration
}

func normalizeHop(hop Hop) Hop {
	return Hop{
		Component: boundLabel(hop.Component, maxHopComponent),
		Operation: boundLabel(hop.Operation, maxHopOperation),
		Peer:      boundLabel(hop.Peer, maxHopPeer),
	}
}

func boundLabel(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return hopStatusUnknown
	}
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

func statusFromHop(component string, err error, httpCode int) string {
	if component == hopComponentHTTPServer || component == hopComponentHTTPClient {
		if httpCode != 0 {
			return httpStatusLabel(httpCode)
		}
		if err != nil {
			return "500"
		}
		return "200"
	}
	if err != nil {
		return hopStatusError
	}
	return hopStatusOK
}

func httpStatusLabel(code int) string {
	if code < 100 || code > 599 {
		return hopStatusUnknown
	}
	return boundLabel(strconv.Itoa(code), maxHopStatus)
}

func isHTTP5xx(status string) bool {
	return len(status) == 3 && status[0] == '5'
}
