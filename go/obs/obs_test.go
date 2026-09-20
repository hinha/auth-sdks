package obs_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/hinha/auth-sdks/go/obs"
	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestNew_EmptyURLIsNoop(t *testing.T) {
	t.Parallel()
	h, err := obs.New(obs.Config{})
	require.NoError(t, err)
	require.NotNil(t, h)
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "noop_total"})
	h.Registerer().MustRegister(c)
	c.Inc()
	_, span := h.Tracer("t").Start(context.Background(), "noop")
	span.End()
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestPrometheusRemoteWrite_CounterLabelsAndAuth(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var gotPath, gotAuth, gotUA, gotCE, gotCT, gotVer string
	var wr prompb.WriteRequest
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		dec, err := snappy.Decode(nil, raw)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := prompb.Unmarshal(dec)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// The handle pushes once immediately, which can land before this test
		// registers its counter, so wait for the request carrying it.
		if !hasSeriesNamed(body, "jobs_total") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mu.Lock()
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		gotCE = r.Header.Get("Content-Encoding")
		gotCT = r.Header.Get("Content-Type")
		gotVer = r.Header.Get("X-Prometheus-Remote-Write-Version")
		wr = body
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		select {
		case <-done:
		default:
			close(done)
		}
	}))
	t.Cleanup(srv.Close)

	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		Username:        "u",
		Password:        "p",
		Service:         "money-tracker",
		Env:             "prod",
		MetricsInterval: 20 * time.Millisecond,
		DisableTraces:   true,
		DisableProfiles: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "jobs_total", Help: "jobs"})
	h.Registerer().MustRegister(c)
	c.Add(3)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote write")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/api/v1/prom/remote/write", gotPath)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")), gotAuth)
	require.Equal(t, "auth-sdks-obs/1", gotUA)
	require.Equal(t, "snappy", gotCE)
	require.Equal(t, "application/x-protobuf", gotCT)
	require.Equal(t, "0.1.0", gotVer)
	require.NotEmpty(t, wr.Timeseries)
	var found bool
	for _, ts := range wr.Timeseries {
		labels := map[string]string{}
		for _, l := range ts.Labels {
			labels[l.Name] = l.Value
		}
		if labels["__name__"] != "jobs_total" {
			continue
		}
		found = true
		require.Equal(t, "money-tracker", labels["service"])
		require.Equal(t, "prod", labels["env"])
		require.Equal(t, 3.0, ts.Samples[0].Value)
	}
	require.True(t, found, "jobs_total not in write request")
}

func TestPrometheusRemoteWrite_HTTP401DoesNotPanic(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	var warned atomic.Bool
	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		MetricsInterval: 20 * time.Millisecond,
		DisableTraces:   true,
		DisableProfiles: true,
		Logger:          func(string) { warned.Store(true) },
	})
	require.NoError(t, err)
	h.Registerer().MustRegister(prometheus.NewCounter(prometheus.CounterOpts{Name: "fail_total"}))
	require.Eventually(t, warned.Load, 2*time.Second, 20*time.Millisecond)
	require.NoError(t, h.Shutdown(context.Background()))
	require.True(t, warned.Load())
}

func TestTempoOTLP_PostsTracesWithBasicAuth(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var gotPath, gotAuth, gotMethod string
	var bodyLen int
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		bodyLen = len(raw)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		select {
		case <-done:
		default:
			close(done)
		}
	}))
	t.Cleanup(srv.Close)

	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		Username:        "u",
		Password:        "p",
		Service:         "money-tracker",
		DisableMetrics:  true,
		DisableProfiles: true,
	})
	require.NoError(t, err)

	ctx, span := h.Tracer("http").Start(context.Background(), "GET /v1/reports")
	span.End()
	_ = ctx
	require.NoError(t, h.Shutdown(context.Background()))

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OTLP export")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/v1/traces", gotPath)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")), gotAuth)
	require.Greater(t, bodyLen, 0)
}

func TestHistogramConversion_IncludesNameAndLe(t *testing.T) {
	t.Parallel()
	// exercised via remote write of a histogram
	var mu sync.Mutex
	var names []string
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		dec, err := snappy.Decode(nil, raw)
		require.NoError(t, err)
		body, err := prompb.Unmarshal(dec)
		require.NoError(t, err)
		mu.Lock()
		var sawHistogram bool
		for _, ts := range body.Timeseries {
			for _, l := range ts.Labels {
				if l.Name != "__name__" {
					continue
				}
				names = append(names, l.Value)
				if l.Value == "http_request_duration_seconds_bucket" {
					sawHistogram = true
				}
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		// The handle pushes once immediately, which can land before this test
		// registers its histogram, so wait for the request that carries it
		// instead of asserting on whichever request happened to arrive first.
		if !sawHistogram {
			return
		}
		select {
		case <-done:
		default:
			close(done)
		}
	}))
	t.Cleanup(srv.Close)

	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		MetricsInterval: 20 * time.Millisecond,
		DisableTraces:   true,
		DisableProfiles: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "http_request_duration_seconds", Help: "d"})
	h.Registerer().MustRegister(hist)
	hist.Observe(0.05)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, names, "http_request_duration_seconds_bucket")
	require.Contains(t, names, "http_request_duration_seconds_sum")
	require.Contains(t, names, "http_request_duration_seconds_count")
}

func TestDisableMetrics_DoesNotRemoteWriteOnShutdown(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		DisableMetrics:  true,
		DisableTraces:   true,
		DisableProfiles: true,
	})
	require.NoError(t, err)
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "should_not_push_total"})
	h.Registerer().MustRegister(c)
	c.Inc()
	require.NoError(t, h.Shutdown(context.Background()))
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(0), hits.Load())
}
