// Package transport adapts gojek/heimdall as the HTTP gateway for the SDK.
package transport

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gojek/heimdall/v7"
	"github.com/gojek/heimdall/v7/httpclient"

	"github.com/hinha/auth-sdks/go/logging"
)

// Doer is the minimal HTTP port used by the API layer (Dependency Inversion).
// heimdall.Client satisfies this interface.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config tunes the Heimdall HTTP client.
type Config struct {
	Timeout    time.Duration
	RetryCount int
	// BackoffInterval is the constant backoff between retries.
	BackoffInterval time.Duration
	// MaximumJitterInterval caps jitter added to backoff.
	MaximumJitterInterval time.Duration
}

// DefaultConfig returns resilient defaults for Auth Service calls.
func DefaultConfig() Config {
	return Config{
		Timeout:               15 * time.Second,
		RetryCount:            2,
		BackoffInterval:       500 * time.Millisecond,
		MaximumJitterInterval: 200 * time.Millisecond,
	}
}

// Option mutates Heimdall client construction (Functional Options).
type Option func(*heimdallBuilder)

type heimdallBuilder struct {
	cfg      Config
	logger   logging.Logger
	doer     heimdall.Doer
	plugins  []heimdall.Plugin
	service  string
}

// WithConfig overrides transport timeouts/retries.
func WithConfig(cfg Config) Option {
	return func(b *heimdallBuilder) { b.cfg = cfg }
}

// WithLogger attaches a structured request logger plugin.
func WithLogger(logger logging.Logger) Option {
	return func(b *heimdallBuilder) { b.logger = logger }
}

// WithServiceName tags log events with the downstream service name.
func WithServiceName(name string) Option {
	return func(b *heimdallBuilder) { b.service = name }
}

// WithCustomDoer injects a custom net/http-compatible Doer (tests / proxies).
func WithCustomDoer(doer heimdall.Doer) Option {
	return func(b *heimdallBuilder) { b.doer = doer }
}

// WithPlugin appends an extra Heimdall plugin.
func WithPlugin(p heimdall.Plugin) Option {
	return func(b *heimdallBuilder) { b.plugins = append(b.plugins, p) }
}

// NewHeimdall builds a heimdall HTTP client (Factory + Adapter).
// Returns heimdall.Client so callers can AddPlugin later if needed.
func NewHeimdall(opts ...Option) (heimdall.Client, error) {
	b := &heimdallBuilder{
		cfg:     DefaultConfig(),
		logger:  logging.NewNop(),
		service: "auth-service",
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.cfg.Timeout <= 0 {
		return nil, fmt.Errorf("transport: timeout must be > 0")
	}
	if b.cfg.RetryCount < 0 {
		return nil, fmt.Errorf("transport: retry count must be >= 0")
	}

	backoff := heimdall.NewConstantBackoff(b.cfg.BackoffInterval, b.cfg.MaximumJitterInterval)
	retrier := heimdall.NewRetrier(backoff)

	clientOpts := []httpclient.Option{
		httpclient.WithHTTPTimeout(b.cfg.Timeout),
		httpclient.WithRetrier(retrier),
		httpclient.WithRetryCount(b.cfg.RetryCount),
	}
	if b.doer != nil {
		clientOpts = append(clientOpts, httpclient.WithHTTPClient(b.doer))
	}

	client := httpclient.NewClient(clientOpts...)
	client.AddPlugin(NewRequestLogger(b.logger, b.service))
	for _, p := range b.plugins {
		client.AddPlugin(p)
	}
	return client, nil
}

// DrainBody reads and closes a response body (best-effort).
func DrainBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
