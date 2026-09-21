package obs

import (
	"net/http"
	"time"
)

// Config is shared Gigapipe credentials for metrics, traces, and profiles.
// Empty URL makes New a no-op (local registry + no exporters).
type Config struct {
	URL      string
	Username string
	Password string
	Service  string
	Env      string

	// ServiceNamespace and ServiceVersion complete the OTel service identity.
	// Both are optional: ServiceVersion falls back to the version stamped into
	// the binary. Neither is attached to every series — see identity.labels.
	ServiceNamespace string
	ServiceVersion   string

	Timeout         time.Duration
	HTTPClient      *http.Client
	MetricsInterval time.Duration

	// Disable* skip that signal. When URL is set, all three default on.
	DisableMetrics  bool
	DisableTraces   bool
	DisableProfiles bool

	// DisableDefaultCollectors drops the Go runtime, process, process I/O,
	// filesystem, and build-info collectors. It is an ingest-cost escape hatch,
	// not an identity switch: target_info is still emitted, because without it
	// a service that sees no HTTP traffic is invisible. Application collectors
	// always take precedence on a name clash, so this is rarely needed.
	DisableDefaultCollectors bool

	// FilesystemPaths are volumes to Statfs for avail/size gauges.
	// Empty means []string{"."} (process cwd). Cap at 8 unique cleaned paths.
	// Pass a data dir / PVC mount when cwd is not the disk that matters.
	FilesystemPaths []string

	// InstallGlobal calls otel.SetTracerProvider and sets a W3C TraceContext
	// + Baggage text-map propagator. Off by default.
	InstallGlobal bool

	// Logger is optional; push failures are fail-open (once).
	Logger func(msg string)
}
