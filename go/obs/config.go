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

	Timeout         time.Duration
	HTTPClient      *http.Client
	MetricsInterval time.Duration

	// Disable* skip that signal. When URL is set, all three default on.
	DisableMetrics  bool
	DisableTraces   bool
	DisableProfiles bool

	// InstallGlobal calls otel.SetTracerProvider. Off by default.
	InstallGlobal bool

	// Logger is optional; push failures are fail-open (once).
	Logger func(msg string)
}
