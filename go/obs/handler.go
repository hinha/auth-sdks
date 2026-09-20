package obs

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler serves the same merged registry that is remote-written, so a local
// scrape and the remote store agree on what the service publishes. It is the
// quickest way to answer "what is this process actually sending?" when a
// dashboard panel comes up empty.
//
// It is not a query API and does not replace Grafana or Gigapipe View.
func (h *Handle) Handler() http.Handler {
	return promhttp.HandlerFor(h.collectorSet(), promhttp.HandlerOpts{
		// One inconsistent collector must not blank the whole page; the same
		// reasoning applies to the partial gather in pushMetrics.
		ErrorHandling: promhttp.ContinueOnError,
	})
}
