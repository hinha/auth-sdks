package obs

import (
	"errors"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestNormalizeFilesystemPaths_EmptyDefaultsToCwd(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"."}, normalizeFilesystemPaths(nil))
	require.Equal(t, []string{"."}, normalizeFilesystemPaths([]string{}))
}

func TestNormalizeFilesystemPaths_CleansUniquesAndCaps(t *testing.T) {
	t.Parallel()
	got := normalizeFilesystemPaths([]string{
		"/var/lib/app",
		"/var/lib/app/",
		"/data",
		"a", "b", "c", "d", "e", "f", "g", "h", "i",
	})
	require.Equal(t, []string{
		"/var/lib/app",
		"/data",
		"a", "b", "c", "d", "e", "f",
	}, got)
}

func TestFilesystemCollector_EmitsAvailAndSize(t *testing.T) {
	t.Parallel()
	c := newFilesystemCollectorForTest(nil, func(path string) (fsStat, error) {
		require.Equal(t, ".", path)
		return fsStat{Avail: 50, Size: 100}, nil
	})
	mfs := gatherCollector(t, c)
	requireNoDuplicates(t, mfs)
	require.Equal(t, 50.0, gaugeLabeled(t, mfs, metricProcessFilesystemAvail, "path", "."))
	require.Equal(t, 100.0, gaugeLabeled(t, mfs, metricProcessFilesystemSize, "path", "."))
}

func TestFilesystemCollector_SkipsFailedPath(t *testing.T) {
	t.Parallel()
	c := newFilesystemCollectorForTest([]string{"/ok", "/missing"}, func(path string) (fsStat, error) {
		if path == "/missing" {
			return fsStat{}, errors.New("statfs")
		}
		return fsStat{Avail: 1, Size: 2}, nil
	})
	mfs := gatherCollector(t, c)
	requireNoDuplicates(t, mfs)
	require.Equal(t, 1.0, gaugeLabeled(t, mfs, metricProcessFilesystemAvail, "path", "/ok"))
	require.Equal(t, 2.0, gaugeLabeled(t, mfs, metricProcessFilesystemSize, "path", "/ok"))
	require.False(t, hasLabeled(mfs, metricProcessFilesystemAvail, "path", "/missing"))
}

func gaugeLabeled(t *testing.T, mfs []*dto.MetricFamily, name, label, value string) float64 {
	t.Helper()
	for _, mf := range mfs {
		if mf == nil || mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			for _, lp := range m.Label {
				if lp.GetName() == label && lp.GetValue() == value {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	t.Fatalf("%s{%s=%q} not present", name, label, value)
	return 0
}

func hasLabeled(mfs []*dto.MetricFamily, name, label, value string) bool {
	for _, mf := range mfs {
		if mf == nil || mf.GetName() != name {
			continue
		}
		for _, m := range mf.Metric {
			for _, lp := range m.Label {
				if lp.GetName() == label && lp.GetValue() == value {
					return true
				}
			}
		}
	}
	return false
}
