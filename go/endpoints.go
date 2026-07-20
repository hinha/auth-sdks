package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/routes"
)

type endpointImportRequest struct {
	ApplicationService string         `json:"application_service"`
	ConflictPolicy     string         `json:"conflict_policy"`
	Endpoints          []routes.Route `json:"endpoints"`
}

// EndpointImportResult is returned by ImportEndpoints.
type EndpointImportResult struct {
	Created int                     `json:"created"`
	Updated int                     `json:"updated"`
	Skipped int                     `json:"skipped"`
	Failed  int                     `json:"failed"`
	Items   []EndpointImportItemOut `json:"items"`
}

// EndpointImportItemOut is one row of an import response.
type EndpointImportItemOut struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	ID     uint   `json:"id,omitempty"`
}

// ImportEndpointsOption configures ImportEndpoints.
type ImportEndpointsOption func(*importEndpointsConfig)

type importEndpointsConfig struct {
	conflictPolicy string
	apiKey         string
}

// WithConflictPolicy sets skip (default) or update for existing method+path rows.
func WithConflictPolicy(policy string) ImportEndpointsOption {
	return func(c *importEndpointsConfig) { c.conflictPolicy = policy }
}

// WithImportAPIKey overrides the client Credentials key for this call.
func WithImportAPIKey(apiKey string) ImportEndpointsOption {
	return func(c *importEndpointsConfig) { c.apiKey = apiKey }
}

// ImportEndpoints bulk-upserts discovered routes into Auth Service consumer_endpoints
// via POST /v1/consumer-auth/endpoints/import (sa_* key).
func (c *Client) ImportEndpoints(ctx context.Context, discovered []routes.Route, opts ...ImportEndpointsOption) (*EndpointImportResult, error) {
	cfg := importEndpointsConfig{conflictPolicy: "skip"}
	for _, opt := range opts {
		opt(&cfg)
	}
	apiKey := cfg.apiKey
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	payload := endpointImportRequest{
		ApplicationService: c.cfg.ApplicationService,
		ConflictPolicy:     cfg.conflictPolicy,
		Endpoints:          routes.NormalizeAll(discovered),
	}
	var out EndpointImportResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/endpoints/import"), payload, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SyncHTTPRoutes records routes from a Registry and imports them.
func (c *Client) SyncHTTPRoutes(ctx context.Context, reg *routes.Registry, opts ...ImportEndpointsOption) (*EndpointImportResult, error) {
	if reg == nil {
		return c.ImportEndpoints(ctx, nil, opts...)
	}
	return c.ImportEndpoints(ctx, reg.Routes(), opts...)
}
