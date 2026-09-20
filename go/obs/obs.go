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
	warnOnce       sync.Once
	closed         sync.Once
}

// New starts optional Prometheus remote-write, OTLP traces, and Pyroscope ingest.
// Empty URL returns a no-op Handle (local registry, noop tracer).
func New(cfg Config) (*Handle, error) {
	reg := prometheus.NewRegistry()
	h := &Handle{
		cfg:      cfg,
		reg:      reg,
		gatherer: reg,
		extra:    extraLabels(cfg),
		tp:       noop.NewTracerProvider(),
		stopCh:   make(chan struct{}),
	}
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
		tp, shutdown, err := newTracerProvider(context.Background(), cfg)
		if err != nil {
			h.forceStopMetrics()
			return nil, err
		}
		h.tp = tp
		h.tpShutdown = shutdown
	}
	if !cfg.DisableProfiles {
		p, err := startProfiles(cfg)
		if err != nil {
			h.warn("gigapipe pyroscope start failed")
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

func (h *Handle) warn(msg string) {
	h.warnOnce.Do(func() {
		if h.cfg.Logger != nil {
			h.cfg.Logger(msg)
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, msg)
	})
}
