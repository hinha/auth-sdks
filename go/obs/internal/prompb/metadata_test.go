package prompb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// fieldNumbers returns the set of protobuf field numbers present in a message,
// skipping each value. It is how these tests assert the wire layout directly,
// rather than trusting a round-trip to agree with itself.
func fieldNumbers(t *testing.T, b []byte) map[protowire.Number]bool {
	t.Helper()
	seen := map[protowire.Number]bool{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		require.GreaterOrEqual(t, n, 0, "malformed tag")
		b = b[n:]
		seen[num] = true
		skip := protowire.ConsumeFieldValue(num, typ, b)
		require.GreaterOrEqual(t, skip, 0, "malformed field value")
		b = b[skip:]
	}
	return seen
}

func TestMarshal_NoMetadataIsBackwardCompatible(t *testing.T) {
	t.Parallel()
	wr := WriteRequest{Timeseries: []TimeSeries{{
		Labels:  []Label{{Name: "__name__", Value: "m"}},
		Samples: []Sample{{Value: 1, Timestamp: 7}},
	}}}
	seen := fieldNumbers(t, Marshal(wr))
	require.Equal(t, map[protowire.Number]bool{1: true}, seen,
		"a metadata-free payload must contain only field 1, exactly as v0.1.0 sent")
}

func TestMarshal_RoundTripsMetadata(t *testing.T) {
	t.Parallel()
	wr := WriteRequest{
		Timeseries: []TimeSeries{{
			Labels:  []Label{{Name: "__name__", Value: "m"}},
			Samples: []Sample{{Value: 2.5, Timestamp: 11}},
		}},
		Metadata: []MetricMetadata{
			{
				Type:             MetricTypeCounter,
				MetricFamilyName: "http_client_errors_total",
				Help:             "Total outbound request errors.",
				Unit:             "1",
			},
			{
				// Empty help/unit must not be emitted as empty strings.
				Type:             MetricTypeGauge,
				MetricFamilyName: "target_info",
			},
		},
	}
	got, err := Unmarshal(Marshal(wr))
	require.NoError(t, err)
	require.Len(t, got.Timeseries, 1)
	require.Equal(t, 2.5, got.Timeseries[0].Samples[0].Value)
	require.Len(t, got.Metadata, 2)

	require.Equal(t, MetricTypeCounter, got.Metadata[0].Type)
	require.Equal(t, "http_client_errors_total", got.Metadata[0].MetricFamilyName)
	require.Equal(t, "Total outbound request errors.", got.Metadata[0].Help)
	require.Equal(t, "1", got.Metadata[0].Unit)

	require.Equal(t, MetricTypeGauge, got.Metadata[1].Type)
	require.Equal(t, "target_info", got.Metadata[1].MetricFamilyName)
	require.Empty(t, got.Metadata[1].Help)
	require.Empty(t, got.Metadata[1].Unit)
}

// The MetricMetadata field numbers are 1, 2, 4, 5 — field 3 does not exist in
// prometheus.MetricMetadata. Writing help at 3 would be silently dropped by
// every receiver, so pin the layout.
func TestMarshalMetadata_UsesCanonicalFieldNumbers(t *testing.T) {
	t.Parallel()
	raw := marshalMetadata(MetricMetadata{
		Type:             MetricTypeHistogram,
		MetricFamilyName: "n",
		Help:             "h",
		Unit:             "s",
	})
	seen := fieldNumbers(t, raw)
	require.True(t, seen[1], "type")
	require.True(t, seen[2], "metric_family_name")
	require.True(t, seen[4], "help")
	require.True(t, seen[5], "unit")
	require.False(t, seen[3], "field 3 is reserved in MetricMetadata and must never be written")
}

func TestMarshalMetadata_OmitsEmptyOptionals(t *testing.T) {
	t.Parallel()
	seen := fieldNumbers(t, marshalMetadata(MetricMetadata{MetricFamilyName: "n"}))
	require.Equal(t, map[protowire.Number]bool{2: true}, seen,
		"the zero enum plus empty help/unit should be omitted, per proto3")
}
