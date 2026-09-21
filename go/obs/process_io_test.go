package obs

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func gatherCollector(t *testing.T, c prometheus.Collector) []*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	return gatherAll(t, reg)
}

func counterValue(t *testing.T, mfs []*dto.MetricFamily, name string) float64 {
	t.Helper()
	for _, mf := range mfs {
		if mf == nil || mf.GetName() != name {
			continue
		}
		require.Len(t, mf.Metric, 1, "%s should have exactly one series", name)
		return mf.Metric[0].GetCounter().GetValue()
	}
	t.Fatalf("%s not present", name)
	return 0
}

func TestProcessIOCollector_EmitsStorageAndCharCounters(t *testing.T) {
	t.Parallel()
	c := newProcessIOCollectorForTest(func() (procIO, error) {
		return procIO{ReadBytes: 100, WriteBytes: 40, RChar: 1000, WChar: 400}, nil
	})
	mfs := gatherCollector(t, c)
	requireNoDuplicates(t, mfs)
	require.Equal(t, 100.0, counterValue(t, mfs, "process_io_read_bytes_total"))
	require.Equal(t, 40.0, counterValue(t, mfs, "process_io_write_bytes_total"))
	require.Equal(t, 1000.0, counterValue(t, mfs, "process_io_rchar_bytes_total"))
	require.Equal(t, 400.0, counterValue(t, mfs, "process_io_wchar_bytes_total"))
}

func TestProcessIOCollector_ReadErrorOmitsSeries(t *testing.T) {
	t.Parallel()
	c := newProcessIOCollectorForTest(func() (procIO, error) {
		return procIO{}, errors.New("no /proc")
	})
	mfs := gatherCollector(t, c)
	require.False(t, hasFamilyWithPrefix(mfs, "process_io_"))
}
