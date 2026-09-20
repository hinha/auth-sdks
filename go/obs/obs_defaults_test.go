package obs_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/hinha/auth-sdks/go/obs"
	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"github.com/stretchr/testify/require"
)

func hasSeriesNamed(wr prompb.WriteRequest, name string) bool {
	for _, ts := range wr.Timeseries {
		for _, l := range ts.Labels {
			if l.Name == "__name__" && l.Value == name {
				return true
			}
		}
	}
	return false
}

// TestRemoteWrite_PushesImmediatelyWithDefaultsAndMetadata is the acceptance
// test for issue #9: a service that registers no metric of its own, and has
// served no traffic, must still arrive in the remote store — without waiting a
// full interval — carrying its identity and metric metadata.
func TestRemoteWrite_PushesImmediatelyWithDefaultsAndMetadata(t *testing.T) {
	t.Parallel()
	var (
		mu    sync.Mutex
		got   prompb.WriteRequest
		first = make(chan struct{})
	)
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
		mu.Lock()
		got = body
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		select {
		case <-first:
		default:
			close(first)
		}
	}))
	t.Cleanup(srv.Close)

	// The interval is an hour on purpose: proving the first push does not wait
	// for it is the entire point of this test.
	h, err := obs.New(obs.Config{
		URL:             srv.URL,
		Service:         "auth-service",
		Env:             "production",
		ServiceVersion:  "v1.2.3",
		MetricsInterval: time.Hour,
		DisableTraces:   true,
		DisableProfiles: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, h.Shutdown(context.Background())) })

	// Nothing is registered on Registerer: this is the no-HTTP-traffic case that
	// used to be invisible.
	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("no push arrived; the first push must not wait for MetricsInterval")
	}

	mu.Lock()
	defer mu.Unlock()

	require.True(t, hasSeriesNamed(got, "target_info"),
		"target_info is what makes a service visible with no application metrics")
	require.True(t, hasSeriesNamed(got, "go_goroutines"), "Go runtime series must be present")
	require.True(t, hasSeriesNamed(got, "process_start_time_seconds"),
		"process series must be present so uptime works without an app gauge")
	require.NotEmpty(t, got.Metadata,
		"metric metadata must accompany the samples so Grafana Cloud shows HELP and units")

	// Identity travels on every series, so dashboards filtering either spelling
	// of the service name match.
	for _, ts := range got.Timeseries {
		labels := map[string]string{}
		for _, l := range ts.Labels {
			labels[l.Name] = l.Value
		}
		require.Equal(t, "auth-service", labels["service"])
		require.Equal(t, "auth-service", labels["service_name"])
		require.Equal(t, "production", labels["env"])
		require.Equal(t, "production", labels["deployment_environment"])
		require.Equal(t, "production", labels["deployment_environment_name"], "")
		if labels["__name__"] == "target_info" {
			// Only target_info carries the version, so a deploy does not churn
			// the identity of every other series.
			require.Equal(t, "v1.2.3", labels["service_version"])
		}
	}

	var sawProcessMetadata bool
	for _, md := range got.Metadata {
		if md.MetricFamilyName != "process_start_time_seconds" {
			continue
		}
		sawProcessMetadata = true
		require.Equal(t, prompb.MetricTypeGauge, md.Type)
		require.NotEmpty(t, md.Help)
	}
	require.True(t, sawProcessMetadata, "default families must advertise metadata")
}
