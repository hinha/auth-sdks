package obs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestHandler_ServesTheSameViewThatIsRemoteWritten(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{Service: "svc", Env: "dev"})
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "app_events_total", Help: "e"})
	c.Inc()
	h.Registerer().MustRegister(c)

	rec := httptest.NewRecorder()
	h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "app_events_total", "application metrics must be exposed")
	require.Contains(t, body, "go_goroutines", "default collectors must be exposed")
	require.Contains(t, body, "target_info", "the service identity must be exposed")
}

func TestHandler_NilHandle(t *testing.T) {
	t.Parallel()
	var h *Handle
	rec := httptest.NewRecorder()
	require.NotPanics(t, func() {
		h.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	})
}
