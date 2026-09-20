package obs

import (
	"runtime/debug"
	"strings"

	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// defaultServiceName mirrors the fallback used by the tracer and profiler.
const defaultServiceName = "auth-sdks"

// buildVersion reads the main module version stamped into the binary. It is a
// variable so tests can substitute it, like startProfiler.
var buildVersion = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
		return v
	}
	return ""
}

// identity is the single resolved service identity shared by every signal
// (metrics, traces, profiles, logs), so the four cannot drift apart.
type identity struct {
	service          string
	env              string
	serviceName      string
	serviceNamespace string
	serviceVersion   string
}

func resolveIdentity(cfg Config) identity {
	service := strings.TrimSpace(cfg.Service)
	id := identity{
		service:          service,
		env:              strings.TrimSpace(cfg.Env),
		serviceNamespace: strings.TrimSpace(cfg.ServiceNamespace),
		serviceVersion:   strings.TrimSpace(cfg.ServiceVersion),
	}
	// service_name must be byte-identical to the legacy service label whenever
	// that label is present, otherwise the two are not aliases and a dashboard
	// filtering one would silently disagree with the other.
	if service == "" {
		id.serviceName = defaultServiceName
	} else {
		id.serviceName = service
	}
	if id.serviceVersion == "" {
		id.serviceVersion = buildVersion()
	}
	return id
}

// labels returns the identity labels attached to every remote-written series.
//
// service_version is deliberately absent: it changes on every deploy, so
// carrying it on every series would churn the entire series set on each
// release. It rides on target_info only, per the OTel convention.
//
// deployment_environment and deployment_environment_name carry the same value
// on purpose. semconv v1.37.0 renamed deployment.environment to
// deployment.environment.name, and an OTel Collector normalises that to
// deployment_environment_name, so emitting both keeps the locked spelling and
// the OTel-native one working. Both are constant, so neither costs cardinality.
func (id identity) labels() []prompb.Label {
	out := make([]prompb.Label, 0, 6)
	add := func(name, value string) {
		if value != "" {
			out = append(out, prompb.Label{Name: name, Value: value})
		}
	}
	add("service", id.service)
	add("service_name", id.serviceName)
	add("env", id.env)
	add("deployment_environment", id.env)
	add("deployment_environment_name", id.env)
	add("service_namespace", id.serviceNamespace)
	return out
}

// semconvAttributes returns the OTel resource attribute set for traces and
// profiles, derived from the same fields as labels so those signals cannot
// disagree with the metric identity.
func (id identity) semconvAttributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 4)
	attrs = append(attrs, semconv.ServiceName(id.serviceName))
	if id.serviceNamespace != "" {
		attrs = append(attrs, semconv.ServiceNamespace(id.serviceNamespace))
	}
	if id.serviceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(id.serviceVersion))
	}
	if id.env != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(id.env))
	}
	return attrs
}

// extraLabels is retained for callers that build the label set straight from a
// Config. New code should resolve the identity once and reuse it.
func extraLabels(cfg Config) []prompb.Label {
	return resolveIdentity(cfg).labels()
}
