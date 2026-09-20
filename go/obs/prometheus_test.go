package obs

import (
	"context"
	"testing"

	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestFamiliesToWriteRequest_GaugeAndCounter(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "queue_depth", Help: "q"})
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "jobs_total", Help: "j"})
	reg.MustRegister(g, c)
	g.Set(9)
	c.Add(2)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	wr := familiesToWriteRequest(mfs, extraLabels(Config{Service: "svc", Env: "dev"}), 42)
	got := map[string]float64{}
	for _, ts := range wr.Timeseries {
		labels := map[string]string{}
		for _, l := range ts.Labels {
			labels[l.Name] = l.Value
		}
		require.Equal(t, "svc", labels["service"])
		require.Equal(t, "dev", labels["env"])
		got[labels["__name__"]] = ts.Samples[0].Value
		require.Equal(t, int64(42), ts.Samples[0].Timestamp)
	}
	require.Equal(t, 9.0, got["queue_depth"])
	require.Equal(t, 2.0, got["jobs_total"])
}

func TestFamiliesToWriteRequest_Summary(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	s := prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "latency_seconds",
		Help:       "l",
		Objectives: map[float64]float64{0.5: 0.05},
	})
	reg.MustRegister(s)
	s.Observe(0.2)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	wr := familiesToWriteRequest(mfs, nil, 1)
	var hasQuantile, hasSum, hasCount bool
	for _, ts := range wr.Timeseries {
		for _, l := range ts.Labels {
			if l.Name == "__name__" && l.Value == "latency_seconds" {
				hasQuantile = true
			}
			if l.Name == "__name__" && l.Value == "latency_seconds_sum" {
				hasSum = true
			}
			if l.Name == "__name__" && l.Value == "latency_seconds_count" {
				hasCount = true
			}
		}
	}
	require.True(t, hasQuantile && hasSum && hasCount)
}

func TestShutdown_NilAndIdempotent(t *testing.T) {
	t.Parallel()
	require.NoError(t, (*Handle)(nil).Shutdown(context.Background()))
	h, err := New(Config{})
	require.NoError(t, err)
	require.NoError(t, h.Shutdown(context.Background()))
	require.NoError(t, h.Shutdown(context.Background()))
}

func TestMergeLabels_SortsAndOverwrites(t *testing.T) {
	t.Parallel()
	labels := mergeLabels("m", nil, []prompb.Label{{Name: "service", Value: "a"}}, []prompb.Label{{Name: "service", Value: "b"}})
	require.Equal(t, "__name__", labels[0].Name)
	require.Equal(t, "m", labels[0].Value)
	require.Equal(t, "service", labels[1].Name)
	require.Equal(t, "b", labels[1].Value)
	empty := mergeLabels("m", []*dto.LabelPair{{}}, []prompb.Label{{}}, []prompb.Label{{Name: "x", Value: ""}})
	require.Equal(t, 1, len(empty))
	require.Equal(t, "__name__", empty[0].Name)
}

func TestFamiliesToWriteRequest_NilAndUntyped(t *testing.T) {
	t.Parallel()
	name := "u"
	val := 1.5
	typ := dto.MetricType_UNTYPED
	wr := familiesToWriteRequest([]*dto.MetricFamily{
		nil,
		{Name: nil},
		{
			Name: &name,
			Type: &typ,
			Metric: []*dto.Metric{
				{Untyped: &dto.Untyped{Value: &val}},
				{},
			},
		},
	}, nil, 7)
	require.Len(t, wr.Timeseries, 1)
	require.Equal(t, 1.5, wr.Timeseries[0].Samples[0].Value)
}

func TestHistogramSeries_AddsInfWhenMissing(t *testing.T) {
	t.Parallel()
	ub := 0.1
	cnt := uint64(2)
	sum := 0.2
	h := &dto.Histogram{
		SampleCount: &cnt,
		SampleSum:   &sum,
		Bucket:      []*dto.Bucket{{CumulativeCount: &cnt, UpperBound: &ub}},
	}
	ts := histogramSeries("lat", nil, nil, h, 1)
	var les []string
	for _, s := range ts {
		m := map[string]string{}
		for _, l := range s.Labels {
			m[l.Name] = l.Value
		}
		if m["__name__"] == "lat_bucket" {
			les = append(les, m["le"])
		}
	}
	require.Contains(t, les, "0.1")
	require.Contains(t, les, "+Inf")
}

func TestMergeLabels_DropsSecretNames(t *testing.T) {
	t.Parallel()
	token := "token"
	val := "secret"
	labels := mergeLabels("m", []*dto.LabelPair{{Name: &token, Value: &val}}, nil, nil)
	for _, l := range labels {
		require.NotEqual(t, "token", l.Name)
	}
}

func TestRegistererAndTracer_NilHandle(t *testing.T) {
	t.Parallel()
	require.NotNil(t, (*Handle)(nil).Registerer())
	require.NotNil(t, (*Handle)(nil).Tracer("x"))
}



