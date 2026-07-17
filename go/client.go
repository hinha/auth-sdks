// Package authsdk is the official Auth Service consumer SDK for Go.
//
// Design overview:
//   - Facade: Client exposes a stable DX over /v1/consumer-auth/*
//   - Strategy: logging.Logger (Zap / slog / Nop)
//   - Adapter + Factory: transport.NewHeimdall (gojek/heimdall)
//   - Decorator: Heimdall request logger plugin
//   - Functional Options: WithLogger, WithTimeout, …
//   - Gateway: internal/api.Requester for envelope decode + typed errors
package authsdk

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/transport"
)

// Client is the Facade for Auth Service consumer APIs.
// It is safe for concurrent use after construction.
type Client struct {
	cfg    Config
	log    logging.Logger
	api    *api.Requester
	jwks   *jwksCache
	prefix string
}

// New constructs a Client bound to baseURL + applicationService + client API key.
//
//	client, err := authsdk.New("https://auth.example.com", "memoo",
//	    authsdk.Credentials(os.Getenv("AUTH_API_KEY")),
//	    authsdk.WithLogger(logging.NewZap(zapLogger)),
//	)
func New(baseURL, applicationService string, opts ...Option) (*Client, error) {
	o := &options{
		cfg: Config{
			BaseURL:            baseURL,
			ApplicationService: applicationService,
			Timeout:            15 * time.Second,
			RetryCount:         2,
			JWKSCacheTTL:       defaultJWKSCacheTTL,
		},
		logger:    logging.NewNop(),
		transport: transport.DefaultConfig(),
	}
	for _, opt := range opts {
		opt(o)
	}

	cfg := o.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	logger := o.logger.With(
		logging.String("sdk", "auth-sdk-go"),
		logging.String("application_service", cfg.ApplicationService),
	)

	var doer transport.Doer
	if o.skipHeimdall {
		if o.doer == nil {
			return nil, &ConfigError{Field: "HTTPDoer", Message: "nil doer"}
		}
		doer = o.doer
	} else {
		tcfg := transport.DefaultConfig()
		if o.transport.Timeout > 0 {
			tcfg.Timeout = o.transport.Timeout
		} else {
			tcfg.Timeout = cfg.Timeout
		}
		if o.transport.RetryCount > 0 || cfg.RetryCount >= 0 {
			// Prefer explicit client RetryCount (0 = no retries).
			tcfg.RetryCount = cfg.RetryCount
		}
		if o.transport.BackoffInterval > 0 {
			tcfg.BackoffInterval = o.transport.BackoffInterval
		}
		if o.transport.MaximumJitterInterval > 0 {
			tcfg.MaximumJitterInterval = o.transport.MaximumJitterInterval
		}
		h, err := transport.NewHeimdall(
			transport.WithConfig(tcfg),
			transport.WithLogger(logger),
			transport.WithServiceName("auth-service"),
		)
		if err != nil {
			return nil, fmt.Errorf("auth-sdk: heimdall: %w", err)
		}
		doer = h
	}

	header := o.header.Clone()
	if header == nil {
		header = make(http.Header)
	}
	if cfg.UserAgent != "" {
		header.Set("User-Agent", cfg.UserAgent)
	}

	c := &Client{
		cfg:    cfg,
		log:    logger,
		prefix: cfg.APIPrefix,
		api: &api.Requester{
			BaseURL: cfg.BaseURL,
			Doer:    doer,
			Logger:  logger,
			Header:  header,
		},
	}
	c.jwks = newJWKSCache(c, cfg.JWKSCacheTTL)
	return c, nil
}

// ApplicationService returns the bound technical service name.
func (c *Client) ApplicationService() string { return c.cfg.ApplicationService }

// BaseURL returns the Auth Service origin.
func (c *Client) BaseURL() string { return c.cfg.BaseURL }

func (c *Client) path(p string) string {
	return c.prefix + p
}

// withClientKey appends the configured sa_* credential unless an explicit key/bearer opt is enough.
func (c *Client) withClientKey(opts ...api.CallOption) []api.CallOption {
	if c.cfg.APIKey == "" {
		return opts
	}
	return append(opts, api.WithAPIKey(c.cfg.APIKey))
}
