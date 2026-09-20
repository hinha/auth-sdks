package obs

import (
	pyroscope "github.com/grafana/pyroscope-go"
)

type profilerStopper interface {
	Stop() error
}

var startProfiler = func(cfg pyroscope.Config) (profilerStopper, error) {
	p, err := pyroscope.Start(cfg)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func startProfiles(cfg Config) (profilerStopper, error) {
	id := resolveIdentity(cfg)
	// The tag set is derived from the same identity as the Prometheus labels, so
	// profiles and metrics cannot disagree about which service they belong to.
	tags := make(map[string]string, 8)
	for _, l := range id.labels() {
		tags[l.Name] = l.Value
	}
	if id.serviceVersion != "" {
		tags["service_version"] = id.serviceVersion
	}
	pcfg := pyroscope.Config{
		ApplicationName:   id.serviceName,
		ServerAddress:     originURL(cfg.URL),
		BasicAuthUser:     cfg.Username,
		BasicAuthPassword: cfg.Password,
		Tags:              tags,
		ProfileTypes: []pyroscope.ProfileType{
			pyroscope.ProfileCPU,
			pyroscope.ProfileAllocObjects,
			pyroscope.ProfileAllocSpace,
			pyroscope.ProfileInuseObjects,
			pyroscope.ProfileInuseSpace,
		},
	}
	if cfg.HTTPClient != nil {
		pcfg.HTTPClient = cfg.HTTPClient
	}
	return startProfiler(pcfg)
}
