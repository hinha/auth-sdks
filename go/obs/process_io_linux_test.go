//go:build linux

package obs

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestDefaultCollectors_EmitsProcessIO(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{})
	mfs := gatherAll(t, h.collectorSet())
	require.True(t, hasFamily(mfs, metricProcessIOReadBytes))
	require.True(t, hasFamily(mfs, metricProcessIOWriteBytes))
	require.True(t, hasFamily(mfs, metricProcessIORchar))
	require.True(t, hasFamily(mfs, metricProcessIOWchar))
	require.GreaterOrEqual(t, counterValue(t, mfs, metricProcessIOReadBytes), 0.0)
	require.GreaterOrEqual(t, counterValue(t, mfs, metricProcessIOWriteBytes), 0.0)
	for _, name := range []string{
		metricProcessIOReadBytes,
		metricProcessIOWriteBytes,
		metricProcessIORchar,
		metricProcessIOWchar,
	} {
		var found bool
		for _, mf := range mfs {
			if mf != nil && mf.GetName() == name {
				found = true
				require.Equal(t, dto.MetricType_COUNTER, mf.GetType())
				require.NotEmpty(t, mf.GetHelp())
			}
		}
		require.True(t, found, "%s metadata missing", name)
	}
}
