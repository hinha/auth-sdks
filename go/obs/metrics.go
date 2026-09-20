package obs

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

const metricTargetInfo = "target_info"

// registerOrReuse registers c and returns the collector that ended up in the
// registry. Building the same middleware twice must share one collector rather
// than panic, and must not silently drop observations, so on a duplicate the
// already-registered instance is returned.
func registerOrReuse[T prometheus.Collector](reg prometheus.Registerer, c T) T {
	if reg == nil {
		return c
	}
	if err := reg.Register(c); err != nil {
		var already prometheus.AlreadyRegisteredError
		if errors.As(err, &already) {
			if existing, ok := already.ExistingCollector.(T); ok {
				return existing
			}
		}
	}
	return c
}

// registerTargetInfo publishes the constant metadata target Grafana Cloud joins
// the other signals against.
//
// The identity labels are attached at push time from identity.labels, so the
// only label owned here is service_version — the one identity attribute that
// must not ride on every series, since it changes on every deploy.
func registerTargetInfo(reg prometheus.Registerer, id identity) {
	target := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: metricTargetInfo,
		Help: "Target metadata. Constant 1; the service identity is carried in the labels.",
	}, []string{"service_version"})
	target.WithLabelValues(id.serviceVersion).Set(1)
	_ = registerOrReuse(reg, target)
}
