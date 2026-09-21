package obs

import (
	"testing"

	"github.com/hinha/auth-sdks/go/obs/internal/prompb"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func labelMap(t *testing.T, labels []prompb.Label) map[string]string {
	t.Helper()
	out := make(map[string]string, len(labels))
	for _, l := range labels {
		require.NotEmpty(t, l.Name, "empty label name must never be emitted")
		require.NotEmpty(t, l.Value, "empty label value must never be emitted")
		out[l.Name] = l.Value
	}
	return out
}

func TestResolveIdentity_LegacyLabelsUnchanged(t *testing.T) {
	t.Parallel()
	id := resolveIdentity(Config{Service: "svc", Env: "dev"})
	got := labelMap(t, id.labels())
	require.Equal(t, "svc", got["service"])
	require.Equal(t, "dev", got["env"])
}

func TestResolveIdentity_SemconvAliases(t *testing.T) {
	t.Parallel()
	id := resolveIdentity(Config{Service: "svc", Env: "dev"})
	got := labelMap(t, id.labels())
	require.Equal(t, "svc", got["service_name"])
	require.Equal(t, "dev", got["deployment_environment"])
	// semconv v1.37.0 renamed deployment.environment -> deployment.environment.name,
	// which an OTel Collector normalises to this label.
	require.Equal(t, "dev", got["deployment_environment_name"])
}

func TestResolveIdentity_EmptyConfigFallsBackToDefaultServiceName(t *testing.T) {
	t.Parallel()
	got := labelMap(t, resolveIdentity(Config{}).labels())
	require.Equal(t, defaultServiceName, got["service_name"])
	// The legacy label stays verbatim, so it is still absent when unset.
	_, hasService := got["service"]
	require.False(t, hasService, "legacy service must stay omitted when unset")
	_, hasEnv := got["env"]
	require.False(t, hasEnv, "legacy env must stay omitted when unset")
}

func TestResolveIdentity_Full(t *testing.T) {
	t.Parallel()
	id := resolveIdentity(Config{
		Service:          "money-tracker",
		Env:              "production",
		ServiceNamespace: "payments",
		ServiceVersion:   "v1.4.2",
	})
	got := labelMap(t, id.labels())
	require.Equal(t, "money-tracker", got["service"])
	require.Equal(t, "money-tracker", got["service_name"])
	require.Equal(t, "production", got["env"])
	require.Equal(t, "production", got["deployment_environment"])
	require.Equal(t, "production", got["deployment_environment_name"])
	require.Equal(t, "payments", got["service_namespace"])

	// service_version changes on every deploy and therefore must NOT ride on
	// every series, or the whole series set churns per release.
	_, hasVersion := got["service_version"]
	require.False(t, hasVersion, "service_version belongs on target_info only")
	require.Equal(t, "v1.4.2", id.serviceVersion)
}

func TestIdentityLabels_TrimWhitespaceAndOmitEmpties(t *testing.T) {
	t.Parallel()
	id := resolveIdentity(Config{Service: "  svc  ", Env: "   ", ServiceNamespace: " "})
	got := labelMap(t, id.labels())
	require.Equal(t, "svc", got["service"])
	require.Equal(t, "svc", got["service_name"])
	// "   " must be treated as unset, not as a value.
	_, hasEnv := got["env"]
	require.False(t, hasEnv)
	_, hasNs := got["service_namespace"]
	require.False(t, hasNs)
}

// Sequential on purpose: it swaps a package-level seam.
func TestResolveIdentity_BuildVersionFallback(t *testing.T) {
	orig := buildVersion
	defer func() { buildVersion = orig }()

	buildVersion = func() string { return "v9.9.9" }
	require.Equal(t, "v9.9.9", resolveIdentity(Config{}).serviceVersion)
	// An explicit Config value wins over the build stamp.
	require.Equal(t, "v1.0.0", resolveIdentity(Config{ServiceVersion: "v1.0.0"}).serviceVersion)

	buildVersion = func() string { return "" }
	require.Empty(t, resolveIdentity(Config{}).serviceVersion)
}

func TestIdentitySemconvAttributes(t *testing.T) {
	t.Parallel()
	id := resolveIdentity(Config{
		Service:          "money-tracker",
		Env:              "production",
		ServiceNamespace: "payments",
		ServiceVersion:   "v1.4.2",
	})
	byKey := map[attribute.Key]string{}
	for _, kv := range id.semconvAttributes() {
		byKey[kv.Key] = kv.Value.AsString()
	}
	require.Equal(t, "money-tracker", byKey["service.name"])
	require.Equal(t, "payments", byKey["service.namespace"])
	require.Equal(t, "v1.4.2", byKey["service.version"])
	require.Equal(t, "production", byKey["deployment.environment.name"])
}

func TestIdentitySemconvAttributes_OmitsUnset(t *testing.T) {
	t.Parallel()
	attrs := resolveIdentity(Config{}).semconvAttributes()
	byKey := map[attribute.Key]struct{}{}
	for _, kv := range attrs {
		byKey[kv.Key] = struct{}{}
	}
	require.Contains(t, byKey, attribute.Key("service.name"))
	require.NotContains(t, byKey, attribute.Key("service.namespace"))
	require.NotContains(t, byKey, attribute.Key("service.version"))
	require.NotContains(t, byKey, attribute.Key("deployment.environment.name"))
}

func TestResourceAttributes_MatchesIdentity(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Service:          "auth-service",
		Env:              "staging",
		ServiceNamespace: "platform",
		ServiceVersion:   "v1.2.3",
	}
	got := attrMap(resourceAttributes(cfg))
	require.Equal(t, "auth-service", got["service.name"])
	require.Equal(t, "platform", got["service.namespace"])
	require.Equal(t, "v1.2.3", got["service.version"])
	require.Equal(t, "staging", got["deployment.environment.name"])
}
