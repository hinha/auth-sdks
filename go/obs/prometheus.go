package obs

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/snappy"
	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	dto "github.com/prometheus/client_model/go"
)

func familiesToWriteRequest(mfs []*dto.MetricFamily, extra []prompb.Label, now int64) prompb.WriteRequest {
	var wr prompb.WriteRequest
	for _, mf := range mfs {
		if mf == nil || mf.Name == nil {
			continue
		}
		name := mf.GetName()
		for _, m := range mf.Metric {
			ts := now
			if m.TimestampMs != nil && m.GetTimestampMs() > 0 {
				ts = m.GetTimestampMs()
			}
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				if m.Counter == nil {
					continue
				}
				wr.Timeseries = append(wr.Timeseries, series(name, m.Label, extra, m.Counter.GetValue(), ts))
			case dto.MetricType_GAUGE:
				if m.Gauge == nil {
					continue
				}
				wr.Timeseries = append(wr.Timeseries, series(name, m.Label, extra, m.Gauge.GetValue(), ts))
			case dto.MetricType_UNTYPED:
				if m.Untyped == nil {
					continue
				}
				wr.Timeseries = append(wr.Timeseries, series(name, m.Label, extra, m.Untyped.GetValue(), ts))
			case dto.MetricType_HISTOGRAM:
				if m.Histogram == nil {
					continue
				}
				wr.Timeseries = append(wr.Timeseries, histogramSeries(name, m.Label, extra, m.Histogram, ts)...)
			case dto.MetricType_SUMMARY:
				if m.Summary == nil {
					continue
				}
				wr.Timeseries = append(wr.Timeseries, summarySeries(name, m.Label, extra, m.Summary, ts)...)
			}
		}
	}
	return wr
}

func series(name string, metricLabels []*dto.LabelPair, extra []prompb.Label, value float64, ts int64) prompb.TimeSeries {
	return prompb.TimeSeries{
		Labels:  mergeLabels(name, metricLabels, extra, nil),
		Samples: []prompb.Sample{{Value: value, Timestamp: ts}},
	}
}

func histogramSeries(name string, metricLabels []*dto.LabelPair, extra []prompb.Label, h *dto.Histogram, ts int64) []prompb.TimeSeries {
	var out []prompb.TimeSeries
	hasInf := false
	for _, b := range h.Bucket {
		if math.IsInf(b.GetUpperBound(), 1) {
			hasInf = true
		}
		le := strconv.FormatFloat(b.GetUpperBound(), 'f', -1, 64)
		out = append(out, prompb.TimeSeries{
			Labels:  mergeLabels(name+"_bucket", metricLabels, extra, []prompb.Label{{Name: "le", Value: le}}),
			Samples: []prompb.Sample{{Value: float64(b.GetCumulativeCount()), Timestamp: ts}},
		})
	}
	if !hasInf {
		out = append(out, prompb.TimeSeries{
			Labels:  mergeLabels(name+"_bucket", metricLabels, extra, []prompb.Label{{Name: "le", Value: "+Inf"}}),
			Samples: []prompb.Sample{{Value: float64(h.GetSampleCount()), Timestamp: ts}},
		})
	}
	out = append(out, series(name+"_sum", metricLabels, extra, h.GetSampleSum(), ts))
	out = append(out, series(name+"_count", metricLabels, extra, float64(h.GetSampleCount()), ts))
	return out
}

func summarySeries(name string, metricLabels []*dto.LabelPair, extra []prompb.Label, s *dto.Summary, ts int64) []prompb.TimeSeries {
	var out []prompb.TimeSeries
	for _, q := range s.Quantile {
		qv := strconv.FormatFloat(q.GetQuantile(), 'f', -1, 64)
		out = append(out, prompb.TimeSeries{
			Labels:  mergeLabels(name, metricLabels, extra, []prompb.Label{{Name: "quantile", Value: qv}}),
			Samples: []prompb.Sample{{Value: q.GetValue(), Timestamp: ts}},
		})
	}
	out = append(out, series(name+"_sum", metricLabels, extra, s.GetSampleSum(), ts))
	out = append(out, series(name+"_count", metricLabels, extra, float64(s.GetSampleCount()), ts))
	return out
}

func mergeLabels(name string, metricLabels []*dto.LabelPair, extra, more []prompb.Label) []prompb.Label {
	byName := map[string]string{"__name__": name}
	for _, p := range metricLabels {
		if p.GetName() == "" || p.GetValue() == "" || secretLabel(p.GetName()) {
			continue
		}
		byName[p.GetName()] = p.GetValue()
	}
	for _, l := range extra {
		if l.Name == "" || l.Value == "" {
			continue
		}
		byName[l.Name] = l.Value
	}
	for _, l := range more {
		if l.Name == "" || l.Value == "" {
			continue
		}
		byName[l.Name] = l.Value
	}
	out := make([]prompb.Label, 0, len(byName))
	for k, v := range byName {
		out = append(out, prompb.Label{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func secretLabel(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "password", "token", "access_token", "refresh_token", "authorization",
		"api_key", "x-api-key", "current_password", "new_password", "confirm_password":
		return true
	default:
		return false
	}
}

func (h *Handle) pushMetrics() {
	if h == nil || h.cfg.DisableMetrics || h.client == nil || h.remoteWriteURL == "" || h.gatherer == nil {
		return
	}
	mfs, err := h.gatherer.Gather()
	if err != nil {
		// Gather returns the families that succeeded alongside the error, so one
		// inconsistent collector must not blind the entire service. Discarding
		// the partial result here was silently dropping every metric.
		h.warn("gigapipe prometheus gather failed", err.Error())
	}
	wr := familiesToWriteRequest(mfs, h.extra, time.Now().UTC().UnixMilli())
	wr.Metadata = metricMetadata(mfs)
	if len(wr.Timeseries) == 0 {
		return
	}
	protoRaw := prompb.Marshal(wr)
	h.pushMu.Lock()
	h.snappyBuf = snappy.Encode(h.snappyBuf[:cap(h.snappyBuf)], protoRaw)
	raw := make([]byte, len(h.snappyBuf))
	copy(raw, h.snappyBuf)
	h.pushMu.Unlock()
	timeout := 5 * time.Second
	if h.client.Timeout > 0 {
		timeout = h.client.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.remoteWriteURL, bytes.NewReader(raw))
	if err != nil {
		h.warn("gigapipe prometheus write failed", err.Error())
		return
	}
	applyAuth(req, h.cfg)
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	res, err := h.client.Do(req)
	if err != nil {
		h.warn("gigapipe prometheus write failed", err.Error())
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		h.warn("gigapipe prometheus write failed", res.Status)
	}
}

func (h *Handle) loopMetrics(interval time.Duration) {
	defer h.metricsWG.Done()
	// Push once immediately. Waiting a full interval meant a service that had
	// not yet observed any traffic sent nothing at all, so it never appeared in
	// Grafana, and a process that exited before the first tick never reported.
	select {
	case <-h.stopCh:
		// Already shutting down; Shutdown performs the final flush itself.
		return
	default:
	}
	h.pushMetrics()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-h.stopCh:
			return
		case <-t.C:
			h.pushMetrics()
		}
	}
}
