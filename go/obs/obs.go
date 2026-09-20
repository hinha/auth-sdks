package obs

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Handle is the Gigapipe metrics / traces / profiles runtime.
type Handle struct {
	cfg            Config
	client         *http.Client
	reg            *prometheus.Registry
	defaults       *prometheus.Registry
	gatherer       prometheus.Gatherer
	extra          []prompb.Label
	remoteWriteURL string
	tp             trace.TracerProvider
	tpShutdown     func(context.Context) error
	profiler       profilerStopper
	stopCh         chan struct{}
	metricsWG      sync.WaitGroup
	pushMu         sync.Mutex
	snappyBuf      []byte
	warnMu         sync.Mutex
	warned         map[string]struct{}
	closed         sync.Once
}

// New starts optional Prometheus remote-write, OTLP traces, and Pyroscope ingest.
// Empty URL returns a no-op Handle (local registry, noop tracer).
func New(cfg Config) (*Handle, error) {
	reg := prometheus.NewRegistry()
	id := resolveIdentity(cfg)
	h := &Handle{
		cfg:      cfg,
		reg:      reg,
		defaults: newDefaultRegistry(cfg, id),
		extra:    id.labels(),
		tp:       noop.NewTracerProvider(),
		stopCh:   make(chan struct{}),
	}
	// Built before the empty-URL return so target_info exists even in no-op mode.
	h.gatherer = h.collectorSet()
	if strings.TrimSpace(cfg.URL) == "" {
		return h, nil
	}
	h.client = httpClient(cfg)
	h.remoteWriteURL = joinAPI(cfg.URL, pathRemoteWrite)

	if !cfg.DisableMetrics {
		interval := cfg.MetricsInterval
		if interval <= 0 {
			interval = 15 * time.Second
		}
		h.metricsWG.Add(1)
		go h.loopMetrics(interval)
	}
	if !cfg.DisableTraces {
		tp, shutdown, err := newTracer(context.Background(), cfg)
		if err != nil {
			// Fail open. Returning nil here used to stop the metrics loop and
			// hand the caller no handle at all, so a tracing problem silently
			// removed the service's metrics and profiles too — the exact
			// disappearance this release exists to prevent. Traces degrade to
			// no-op; everything else keeps running.
			h.warn("gigapipe tempo start failed", err.Error())
		} else {
			h.tp = tp
			h.tpShutdown = shutdown
		}
	}
	if !cfg.DisableProfiles {
		p, err := startProfiles(cfg)
		if err != nil {
			h.warn("gigapipe pyroscope start failed", err.Error())
		} else {
			h.profiler = p
		}
	}
	return h, nil
}

func (h *Handle) Registerer() prometheus.Registerer {
	if h == nil || h.reg == nil {
		return prometheus.NewRegistry()
	}
	return h.reg
}

func (h *Handle) Shutdown(ctx context.Context) error {
	if h == nil {
		return nil
	}
	var err error
	h.closed.Do(func() {
		h.forceStopMetrics()
		if !h.cfg.DisableMetrics {
			h.pushMetrics()
		}
		if h.profiler != nil {
			if e := h.profiler.Stop(); e != nil && err == nil {
				err = e
			}
		}
		if h.tpShutdown != nil {
			if e := h.tpShutdown(ctx); e != nil && err == nil {
				err = e
			}
		}
	})
	return err
}

func (h *Handle) forceStopMetrics() {
	select {
	case <-h.stopCh:
	default:
		close(h.stopCh)
	}
	h.metricsWG.Wait()
}

// warn reports a condition once per distinct key.
//
// Fail-open failures recur on every interval, so an unbounded logger would drown
// the log. A single one-shot warning is wrong in the other direction: the metric
// push now runs at startup, exactly when DNS and TLS may not be ready, so the
// first failure would consume the only warning and silence every later, real
// one — turning a visible outage into a silent one.
func (h *Handle) warn(key, detail string) {
	if h == nil {
		return
	}
	h.warnMu.Lock()
	if h.warned == nil {
		h.warned = make(map[string]struct{})
	}
	_, seen := h.warned[key]
	h.warned[key] = struct{}{}
	h.warnMu.Unlock()
	if seen {
		return
	}
	msg := key
	if detail != "" {
		msg = key + ": " + detail
	}
	if h.cfg.Logger != nil {
		h.cfg.Logger(msg)
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, msg)
}
