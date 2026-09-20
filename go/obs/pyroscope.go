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
	tags := map[string]string{}
	if cfg.Service != "" {
		tags["service"] = cfg.Service
	}
	if cfg.Env != "" {
		tags["env"] = cfg.Env
	}
	pcfg := pyroscope.Config{
		ApplicationName:   nonEmpty(cfg.Service, "auth-sdks"),
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
