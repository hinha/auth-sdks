package obs

import (
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// gatherAll returns every family the gatherer produced. The error is ignored on
// purpose: Registry.Gather and Gatherers.Gather both return usable families
// alongside a partial-failure error, and the invariants under test are about
// what ended up in the output, not about whether a clash was reported.
func gatherAll(t *testing.T, g prometheus.Gatherer) []*dto.MetricFamily {
	t.Helper()
	mfs, _ := g.Gather()
	return mfs
}

// seriesCounts keys every series by family name plus its sorted label pairs, so
// a value that appears twice under the same identity shows up as a count of 2.
func seriesCounts(mfs []*dto.MetricFamily) map[string]int {
	counts := map[string]int{}
	for _, mf := range mfs {
		if mf == nil {
			continue
		}
		for _, m := range mf.Metric {
			pairs := make([]string, 0, len(m.Label))
			for _, lp := range m.Label {
				pairs = append(pairs, lp.GetName()+"="+lp.GetValue())
			}
			sort.Strings(pairs)
			counts[mf.GetName()+"\x00"+strings.Join(pairs, "\x00")]++
		}
	}
	return counts
}

func requireNoDuplicates(t *testing.T, mfs []*dto.MetricFamily) {
	t.Helper()
	var dupes []string
	for key, n := range seriesCounts(mfs) {
		if n > 1 {
			dupes = append(dupes, key)
		}
	}
	require.Empty(t, dupes, "a series identity was emitted more than once")
}

func hasFamilyWithPrefix(mfs []*dto.MetricFamily, prefix string) bool {
	for _, mf := range mfs {
		if mf != nil && strings.HasPrefix(mf.GetName(), prefix) {
			return true
		}
	}
	return false
}

func hasFamily(mfs []*dto.MetricFamily, name string) bool {
	for _, mf := range mfs {
		if mf != nil && mf.GetName() == name {
			return true
		}
	}
	return false
}

// gaugeValue returns the value of the single metric in the named gauge family.
func gaugeValue(t *testing.T, mfs []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, mf := range mfs {
		if mf == nil || mf.GetName() != name {
			continue
		}
		require.Len(t, mf.Metric, 1, "%s should have exactly one series", name)
		return mf.Metric[0].GetGauge().GetValue()
	}
	t.Fatalf("%s not present", name)
	return 0
}

// newTestHandle builds a Handle with no URL, so no remote-write goroutine
// exists and nothing touches the network.
func newTestHandle(t *testing.T, cfg Config) *Handle {
	t.Helper()
	if cfg.Service == "" {
		cfg.Service = "svc"
	}
	h, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, h.Shutdown(t.Context())) })
	return h
}

// TestDefaultCollectors_NoDuplicateSeries is the load-bearing test for the
// two-registry design: default collectors must never produce a second series
// for an identity the application already publishes, whatever the application
// registered and whichever options it used.
func TestDefaultCollectors_NoDuplicateSeries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		byApp  func(t *testing.T, reg prometheus.Registerer)
		verify func(t *testing.T, mfs []*dto.MetricFamily)
	}{
		{
			name:  "app registers nothing",
			byApp: func(*testing.T, prometheus.Registerer) {},
			verify: func(t *testing.T, mfs []*dto.MetricFamily) {
				require.True(t, hasFamilyWithPrefix(mfs, "go_"))
				require.True(t, hasFamilyWithPrefix(mfs, "process_"))
				require.True(t, hasFamily(mfs, metricTargetInfo))
			},
		},
		{
			name: "app registers an identical Go collector",
			byApp: func(t *testing.T, reg prometheus.Registerer) {
				reg.MustRegister(collectors.NewGoCollector())
			},
			verify: func(t *testing.T, mfs []*dto.MetricFamily) {
				require.True(t, hasFamily(mfs, "go_goroutines"))
				require.True(t, hasFamily(mfs, "process_start_time_seconds"))
			},
		},
		{
			name: "app registers a variant Go collector (memstats disabled)",
			byApp: func(t *testing.T, reg prometheus.Registerer) {
				reg.MustRegister(collectors.NewGoCollector(
					collectors.WithGoCollectorMemStatsMetricsDisabled(),
				))
			},
			verify: func(t *testing.T, mfs []*dto.MetricFamily) {
				// The variant covers only part of the surface; the defaults must
				// fill the gap rather than be skipped wholesale.
				require.True(t, hasFamilyWithPrefix(mfs, "go_memstats_"),
					"defaults should supply the families the app's variant omits")
			},
		},
		{
			name: "app registers a custom go_goroutines gauge",
			byApp: func(t *testing.T, reg prometheus.Registerer) {
				g := prometheus.NewGauge(prometheus.GaugeOpts{
					Name: "go_goroutines",
					Help: "app-owned goroutine gauge",
				})
				g.Set(42)
				reg.MustRegister(g)
			},
			verify: func(t *testing.T, mfs []*dto.MetricFamily) {
				require.Equal(t, 42.0, gaugeValue(t, mfs, "go_goroutines"),
					"the application's series must win over the default")
			},
		},
		{
			name: "app registers process_io_read_bytes_total",
			byApp: func(t *testing.T, reg prometheus.Registerer) {
				c := prometheus.NewCounter(prometheus.CounterOpts{
					Name: metricProcessIOReadBytes,
					Help: "app-owned io read",
				})
				c.Add(7)
				reg.MustRegister(c)
			},
			verify: func(t *testing.T, mfs []*dto.MetricFamily) {
				require.Equal(t, 7.0, counterValue(t, mfs, metricProcessIOReadBytes),
					"the application's series must win over the default")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHandle(t, Config{})
			tc.byApp(t, h.Registerer())
			mfs := gatherAll(t, h.collectorSet())
			requireNoDuplicates(t, mfs)
			tc.verify(t, mfs)
		})
	}
}

func TestDefaultCollectors_DisabledSkipsDefaults(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{DisableDefaultCollectors: true})
	reg := h.Registerer()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "app_events_total", Help: "e"})
	c.Inc()
	reg.MustRegister(c)

	mfs := gatherAll(t, h.collectorSet())
	requireNoDuplicates(t, mfs)
	require.True(t, hasFamily(mfs, "app_events_total"), "app metrics must still be sent")
	require.False(t, hasFamilyWithPrefix(mfs, "go_"))
	require.False(t, hasFamilyWithPrefix(mfs, "process_"))
	require.False(t, hasFamilyWithPrefix(mfs, "process_io_"))
	require.False(t, hasFamilyWithPrefix(mfs, "process_filesystem_"))
	// target_info is the service identity, not a runtime collector, so the
	// opt-out does not remove it.
	require.True(t, hasFamily(mfs, metricTargetInfo))
}

func TestTargetInfo_PresentWithNoAppRegistration(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{Service: "auth-service", Env: "production"})
	mfs := gatherAll(t, h.collectorSet())
	require.Equal(t, 1.0, gaugeValue(t, mfs, metricTargetInfo),
		"target_info must be 1 so the service is visible with zero HTTP traffic")
}

// TestTargetInfo_CarriesIdentityLabels checks the wire view, which is where the
// identity labels are merged in.
func TestTargetInfo_CarriesIdentityLabels(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{
		Service:          "auth-service",
		Env:              "production",
		ServiceNamespace: "platform",
		ServiceVersion:   "v1.2.3",
	})
	mfs := gatherAll(t, h.collectorSet())
	wr := familiesToWriteRequest(mfs, h.extra, 1000)

	var found bool
	for _, ts := range wr.Timeseries {
		labels := map[string]string{}
		for _, l := range ts.Labels {
			labels[l.Name] = l.Value
		}
		if labels["__name__"] != metricTargetInfo {
			continue
		}
		found = true
		require.Equal(t, "auth-service", labels["service"])
		require.Equal(t, "auth-service", labels["service_name"])
		require.Equal(t, "production", labels["env"])
		require.Equal(t, "production", labels["deployment_environment"])
		require.Equal(t, "production", labels["deployment_environment_name"])
		require.Equal(t, "platform", labels["service_namespace"])
		require.Equal(t, "v1.2.3", labels["service_version"])
	}
	require.True(t, found, "target_info missing from the write request")
}

func TestCollectorSet_NilHandle(t *testing.T) {
	t.Parallel()
	var h *Handle
	require.NotPanics(t, func() { _ = h.collectorSet() })
}
