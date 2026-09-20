package obs

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golang/snappy"
	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// errGatherer mimics Registry.Gather on a partial failure: usable families
// returned alongside an error.
type errGatherer struct {
	mfs []*dto.MetricFamily
	err error
}

func (g errGatherer) Gather() ([]*dto.MetricFamily, error) { return g.mfs, g.err }

// recordingServer captures every remote-write payload and counts requests.
type recordingServer struct {
	mu       sync.Mutex
	requests int
	bodies   []prompb.WriteRequest
	srv      *httptest.Server
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	rec := &recordingServer{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		dec, err := snappy.Decode(nil, raw)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := prompb.Unmarshal(dec)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.requests++
		rec.bodies = append(rec.bodies, body)
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *recordingServer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

func (r *recordingServer) hasSeries(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, body := range r.bodies {
		for _, ts := range body.Timeseries {
			for _, l := range ts.Labels {
				if l.Name == "__name__" && l.Value == name {
					return true
				}
			}
		}
	}
	return false
}

// newPushHandle builds a Handle with no metrics goroutine, so a test drives
// pushMetrics itself and never races the loop over the gatherer field.
func newPushHandle(rec *recordingServer, service string, logger func(string)) *Handle {
	cfg := Config{Service: service, Logger: logger}
	return &Handle{
		cfg:            cfg,
		client:         rec.srv.Client(),
		remoteWriteURL: rec.srv.URL + pathRemoteWrite,
		gatherer:       errGatherer{},
		extra:          resolveIdentity(cfg).labels(),
		stopCh:         make(chan struct{}),
	}
}

// TestPushMetrics_PartialGatherStillPushes pins the regression where a single
// inconsistent collector caused the entire payload to be discarded.
func TestPushMetrics_PartialGatherStillPushes(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	var (
		mu   sync.Mutex
		msgs []string
	)
	h := newPushHandle(rec, "svc", func(m string) {
		mu.Lock()
		msgs = append(msgs, m)
		mu.Unlock()
	})

	name := "queue_depth"
	val := 7.0
	typ := dto.MetricType_GAUGE
	h.gatherer = errGatherer{
		mfs: []*dto.MetricFamily{{
			Name:   &name,
			Type:   &typ,
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: &val}}},
		}},
		err: errors.New("collected metric was collected before with the same name"),
	}

	h.pushMetrics()

	require.Equal(t, 1, rec.count(), "the write must happen despite the gather error")
	require.True(t, rec.hasSeries("queue_depth"),
		"families returned alongside a gather error must still be written")

	mu.Lock()
	defer mu.Unlock()
	require.True(t, containsPrefix(msgs, "gigapipe prometheus gather failed"),
		"the failure must still be reported, got %v", msgs)
}

func TestPushMetrics_EmptyGatherSendsNothing(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	h := newPushHandle(rec, "svc", nil)
	h.gatherer = errGatherer{mfs: nil, err: nil}

	h.pushMetrics()

	require.Zero(t, rec.count(), "a genuinely empty gather must not post an empty body")
}

// TestPushMetrics_SendsMetadata guards that HELP/type/unit travel with the
// samples, which is what makes Grafana Cloud label panels and units useful.
func TestPushMetrics_SendsMetadata(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	h := newPushHandle(rec, "svc", nil)

	name := "process_start_time_seconds"
	help := "Start time of the process since unix epoch in seconds."
	typ := dto.MetricType_GAUGE
	val := 1.0
	h.gatherer = errGatherer{mfs: []*dto.MetricFamily{{
		Name: &name, Help: &help, Type: &typ,
		Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: &val}}},
	}}}

	h.pushMetrics()

	require.Equal(t, 1, rec.count())
	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Len(t, rec.bodies[0].Metadata, 1)
	require.Equal(t, prompb.MetricTypeGauge, rec.bodies[0].Metadata[0].Type)
	require.Equal(t, help, rec.bodies[0].Metadata[0].Help)
}

func containsPrefix(msgs []string, prefix string) bool {
	for _, m := range msgs {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}
