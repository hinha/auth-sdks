package obs

import (
	"context"
	"testing"

	pyroscope "github.com/grafana/pyroscope-go"
	"github.com/stretchr/testify/require"
)

type nopStop struct{}

func (nopStop) Stop() error { return nil }

func TestStartProfiles_WiresOriginAndBasicAuth(t *testing.T) {
	orig := startProfiler
	t.Cleanup(func() { startProfiler = orig })
	var got pyroscope.Config
	startProfiler = func(cfg pyroscope.Config) (profilerStopper, error) {
		got = cfg
		return nopStop{}, nil
	}

	p, err := startProfiles(Config{
		URL:      "https://logs.example.com/ingest",
		Username: "u",
		Password: "p",
		Service:  "money-tracker",
		Env:      "prod",
	})
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Equal(t, "https://logs.example.com", got.ServerAddress)
	require.Equal(t, "u", got.BasicAuthUser)
	require.Equal(t, "p", got.BasicAuthPassword)
	require.Equal(t, "money-tracker", got.ApplicationName)
	require.Equal(t, "money-tracker", got.Tags["service"])
	require.Equal(t, "prod", got.Tags["env"])
	// Profiles carry the same identity as the Prometheus labels, so Grafana
	// Cloud can correlate a profile to the trace and metric streams.
	require.Equal(t, "money-tracker", got.Tags["service_name"])
	require.Equal(t, "prod", got.Tags["deployment_environment"])
	require.Equal(t, "prod", got.Tags["deployment_environment_name"])
}

func TestNew_DoesNotStartProfilerWhenURLEmpty(t *testing.T) {
	orig := startProfiler
	t.Cleanup(func() { startProfiler = orig })
	called := false
	startProfiler = func(pyroscope.Config) (profilerStopper, error) {
		called = true
		return nopStop{}, nil
	}
	h, err := New(Config{})
	require.NoError(t, err)
	require.False(t, called)
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestNew_StartsProfilerWhenEnabled(t *testing.T) {
	orig := startProfiler
	t.Cleanup(func() { startProfiler = orig })
	var got pyroscope.Config
	startProfiler = func(cfg pyroscope.Config) (profilerStopper, error) {
		got = cfg
		return nopStop{}, nil
	}
	h, err := New(Config{
		URL:            "https://logs.example.com",
		Service:        "svc",
		DisableMetrics: true,
		DisableTraces:  true,
	})
	require.NoError(t, err)
	require.Equal(t, "https://logs.example.com", got.ServerAddress)
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestStartProfiler_ErrorIsFailOpen(t *testing.T) {
	orig := startProfiler
	t.Cleanup(func() { startProfiler = orig })
	startProfiler = func(pyroscope.Config) (profilerStopper, error) {
		return nil, context.Canceled
	}
	var warned string
	h, err := New(Config{
		URL:            "https://logs.example.com",
		DisableMetrics: true,
		DisableTraces:  true,
		Logger:         func(msg string) { warned = msg },
	})
	require.NoError(t, err)
	require.Contains(t, warned, "pyroscope")
	require.NoError(t, h.Shutdown(context.Background()))
}
