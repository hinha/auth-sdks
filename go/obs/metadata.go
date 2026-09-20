package obs

import (
	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	dto "github.com/prometheus/client_model/go"
)

// metricMetadata maps gathered families onto remote-write metadata, which is
// what lets Grafana Cloud show HELP text and units in the metric browser and
// format dashboard axes in real units.
//
// Prometheus sends metadata on its own interval; sending it with every push is
// idempotent on the ingest side and negligible next to the sample payload.
func metricMetadata(mfs []*dto.MetricFamily) []prompb.MetricMetadata {
	out := make([]prompb.MetricMetadata, 0, len(mfs))
	for _, mf := range mfs {
		if mf == nil || mf.Name == nil {
			continue
		}
		out = append(out, prompb.MetricMetadata{
			Type:             metadataType(mf.GetType()),
			MetricFamilyName: mf.GetName(),
			Help:             mf.GetHelp(),
			Unit:             mf.GetUnit(),
		})
	}
	return out
}

// metadataType translates io.prometheus.client.MetricType — what Gather returns
// — into prometheus.MetricMetadata.MetricType, which is what remote write
// carries. The two enums do not share numbering, so a cast would be wrong:
// client COUNTER is 0 while metadata COUNTER is 1.
func metadataType(t dto.MetricType) prompb.MetricType {
	switch t {
	case dto.MetricType_COUNTER:
		return prompb.MetricTypeCounter
	case dto.MetricType_GAUGE:
		return prompb.MetricTypeGauge
	case dto.MetricType_HISTOGRAM:
		return prompb.MetricTypeHistogram
	case dto.MetricType_SUMMARY:
		return prompb.MetricTypeSummary
	default:
		// UNTYPED has no remote-write equivalent.
		return prompb.MetricTypeUnknown
	}
}
