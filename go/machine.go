package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdks/go/internal/api"
)

type authorizeEndpointRequest struct {
	ApplicationService string `json:"application_service"`
	Method             string `json:"method"`
	Path               string `json:"path"`
}

// VerifyAPIKey validates a machine credential (sa_*).
// When apiKey is empty, the client Credentials key is used.
func (c *Client) VerifyAPIKey(ctx context.Context, apiKey string) (MachineVerifyResult, error) {
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	var out MachineVerifyResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/verify"), nil, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuthorizeEndpoint checks whether a machine key may access an endpoint.
// When apiKey is empty, the client Credentials key is used.
func (c *Client) AuthorizeEndpoint(ctx context.Context, apiKey string, in AuthorizeEndpointInput) (*AuthorizeEndpointResult, error) {
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	var out AuthorizeEndpointResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/authorize-endpoint"), authorizeEndpointRequest{
		ApplicationService: c.cfg.ApplicationService,
		Method:             in.Method,
		Path:               in.Path,
	}, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &out, nil
}
