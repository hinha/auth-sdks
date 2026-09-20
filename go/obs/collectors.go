package obs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// newDefaultRegistry builds the private registry that holds the always-on
// series: Go runtime, process, build info, and target_info.
//
// It deliberately is NOT the registry the application registers on. Putting the
// Go and process collectors on the application's registry would make an
// existing Handle.Registerer().MustRegister(collectors.NewGoCollector()) panic
// at startup — precisely the workaround kvist and auth-service already run.
// Keeping them separate and merging at gather time (see collectorSet) leaves
// that code working, and lets load-bearing issue #9 stop being a per-consumer
// responsibility.
func newDefaultRegistry(cfg Config, id identity) *prometheus.Registry {
	reg := prometheus.NewRegistry()
	// target_info is the service identity rather than a runtime collector, so it
	// is registered regardless: without it a service with no HTTP traffic is
	// invisible, which is the bug this exists to fix.
	registerTargetInfo(reg, id)
	if cfg.DisableDefaultCollectors {
		return reg
	}
	// A fresh registry cannot already hold these, so the errors are impossible.
	_ = reg.Register(collectors.NewGoCollector())
	_ = reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	_ = reg.Register(collectors.NewBuildInfoCollector())
	return reg
}

// collectorSet merges the application registry with the private defaults one.
//
// The application registry comes FIRST, and that ordering is what makes
// double-emission structurally impossible: prometheus.Gatherers.Gather shares a
// single metric-hash set across all gatherers and skips — never appends — a
// series identity it has already seen. When the help or type of a family
// disagrees, the whole default family is dropped and an error is recorded,
// which is also the direction that keeps the application's data intact.
func (h *Handle) collectorSet() prometheus.Gatherer {
	if h == nil || h.reg == nil {
		return prometheus.NewRegistry()
	}
	if h.defaults == nil {
		return h.reg
	}
	return prometheus.Gatherers{h.reg, h.defaults}
}
