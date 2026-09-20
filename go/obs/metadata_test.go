package obs

import (
	"testing"

	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestMetadataType_MapsEnumsWithoutCasting(t *testing.T) {
	t.Parallel()
	// The two enums disagree on numbering, so each case is spelled out.
	require.Equal(t, prompb.MetricTypeCounter, metadataType(dto.MetricType_COUNTER))
	require.Equal(t, prompb.MetricTypeGauge, metadataType(dto.MetricType_GAUGE))
	require.Equal(t, prompb.MetricTypeSummary, metadataType(dto.MetricType_SUMMARY))
	require.Equal(t, prompb.MetricTypeHistogram, metadataType(dto.MetricType_HISTOGRAM))
	require.Equal(t, prompb.MetricTypeUnknown, metadataType(dto.MetricType_UNTYPED))
}

func TestMetricMetadata_CarriesNameHelpAndUnit(t *testing.T) {
	t.Parallel()
	name := "http_server_request_duration_seconds"
	help := "Duration of inbound HTTP requests."
	unit := "s"
	typ := dto.MetricType_HISTOGRAM

	got := metricMetadata([]*dto.MetricFamily{
		nil,
		{Name: nil},
		{Name: &name, Help: &help, Unit: &unit, Type: &typ},
	})
	require.Len(t, got, 1)
	require.Equal(t, prompb.MetricTypeHistogram, got[0].Type)
	require.Equal(t, name, got[0].MetricFamilyName)
	require.Equal(t, help, got[0].Help)
	require.Equal(t, unit, got[0].Unit)
}

func TestMetricMetadata_EmptyForNoFamilies(t *testing.T) {
	t.Parallel()
	require.Empty(t, metricMetadata(nil))
}
