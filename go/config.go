package authsdk

import (
	"net/http"
	"strings"
	"time"

	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/transport"
)

const (
	defaultAPIPrefix    = "/v1/consumer-auth"
	defaultJWKSCacheTTL = 10 * time.Minute
)

// Config holds immutable client settings (Value Object).
type Config struct {
	// BaseURL is the Auth Service origin, e.g. https://auth.example.com
	BaseURL string
	// ApplicationService is the technical service identifier (max 32).
	ApplicationService string
	// APIKey is the sa_* client credential (first gate for public consumer auth).
	APIKey string
	// APIPrefix overrides /v1/consumer-auth when needed.
	APIPrefix string
	// Timeout for Heimdall HTTP calls.
	Timeout time.Duration
	// RetryCount for Heimdall retrier.
	RetryCount int
	// JWKSCacheTTL controls JWKS cache freshness.
	JWKSCacheTTL time.Duration
	// UserAgent is sent on every request when non-empty.
	UserAgent string
}

func (c Config) withDefaults() Config {
	out := c
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	out.ApplicationService = strings.TrimSpace(out.ApplicationService)
	out.APIKey = strings.TrimSpace(out.APIKey)
	if out.APIPrefix == "" {
		out.APIPrefix = defaultAPIPrefix
	} else if !strings.HasPrefix(out.APIPrefix, "/") {
		out.APIPrefix = "/" + out.APIPrefix
	}
	if out.Timeout <= 0 {
		out.Timeout = 15 * time.Second
	}
	if out.RetryCount < 0 {
		out.RetryCount = 2
	}
	if out.JWKSCacheTTL <= 0 {
		out.JWKSCacheTTL = defaultJWKSCacheTTL
	}
	return out
}

func (c Config) validate() error {
	if c.BaseURL == "" {
		return &ConfigError{Field: "BaseURL", Message: "required"}
	}
	if c.ApplicationService == "" {
		return &ConfigError{Field: "ApplicationService", Message: "required"}
	}
	if len(c.ApplicationService) > 32 {
		return &ConfigError{Field: "ApplicationService", Message: "max 32 characters"}
	}
	if c.APIKey == "" {
		return &ConfigError{Field: "APIKey", Message: "required (use authsdk.Credentials)"}
	}
	return nil
}

// Option configures the Client (Functional Options pattern).
type Option func(*options)

type options struct {
	cfg          Config
	logger       logging.Logger
	doer         transport.Doer
	header       http.Header
	transport    transport.Config
	skipHeimdall bool
	nats         NATSConfig
}

// Credentials sets the sa_* client API key used as the first gate for
// register/login/forgot/verify-email/reset (and sent on other calls as X-API-Key).
func Credentials(apiKey string) Option {
	return func(o *options) {
		o.cfg.APIKey = strings.TrimSpace(apiKey)
	}
}

// WithLogger injects a logging.Logger Strategy (Zap / slog / Nop).
func WithLogger(logger logging.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// WithHTTPDoer injects a custom transport.Doer (tests / custom proxies).
// When set, Heimdall is not constructed.
func WithHTTPDoer(doer transport.Doer) Option {
	return func(o *options) {
		o.doer = doer
		o.skipHeimdall = true
	}
}

// WithTimeout overrides HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.cfg.Timeout = d }
}

// WithRetryCount overrides Heimdall retry count.
func WithRetryCount(n int) Option {
	return func(o *options) { o.cfg.RetryCount = n }
}

// WithJWKSCacheTTL overrides JWKS cache TTL.
func WithJWKSCacheTTL(d time.Duration) Option {
	return func(o *options) { o.cfg.JWKSCacheTTL = d }
}

// WithUserAgent sets the User-Agent request header.
func WithUserAgent(ua string) Option {
	return func(o *options) { o.cfg.UserAgent = ua }
}

// WithHeader adds a static header on every request.
func WithHeader(key, value string) Option {
	return func(o *options) {
		if o.header == nil {
			o.header = make(http.Header)
		}
		o.header.Set(key, value)
	}
}

// WithTransportConfig overrides Heimdall backoff/retry tuning.
func WithTransportConfig(cfg transport.Config) Option {
	return func(o *options) { o.transport = cfg }
}
