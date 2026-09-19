package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
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
		c.emitAudit(ctx, logging.LevelError, logging.EventAuthAPIKeyVerify, logging.DecisionError,
			logging.String(logging.FieldActorType, logging.ActorMachine),
			logging.String(logging.FieldActorID, logging.RedactMachineID(apiKey)),
			logging.Int(logging.FieldStatus, apiStatus(err)),
			logging.Err(err),
		)
		return nil, err
	}
	c.emitAudit(ctx, logging.LevelInfo, logging.EventAuthAPIKeyVerify, logging.DecisionInfo,
		logging.String(logging.FieldActorType, logging.ActorMachine),
		logging.String(logging.FieldActorID, logging.RedactMachineID(apiKey)),
	)
	return out, nil
}

// AuthorizeEndpoint checks whether a machine key or consumer JWT may access an
// endpoint. When credential is empty, the client Credentials key is used.
// Pass a consumer access token (JWT) for the membership-permission path, or a
// sa_* key for the machine scope path.
func (c *Client) AuthorizeEndpoint(ctx context.Context, credential string, in AuthorizeEndpointInput) (*AuthorizeEndpointResult, error) {
	return c.authorizeEndpoint(ctx, credential, in)
}

// AuthorizeEndpointWithBearer is an alias that makes the JWT path explicit.
func (c *Client) AuthorizeEndpointWithBearer(ctx context.Context, accessToken string, in AuthorizeEndpointInput) (*AuthorizeEndpointResult, error) {
	return c.authorizeEndpoint(ctx, accessToken, in)
}

func (c *Client) authorizeEndpoint(ctx context.Context, credential string, in AuthorizeEndpointInput) (*AuthorizeEndpointResult, error) {
	var opts []api.CallOption
	if credential != "" {
		opts = append(opts, credentialCallOption(credential))
	} else {
		opts = c.withClientKey(opts...)
	}

	var out AuthorizeEndpointResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/authorize-endpoint"), authorizeEndpointRequest{
		ApplicationService: c.cfg.ApplicationService,
		Method:             in.Method,
		Path:               in.Path,
	}, &out, opts...)
	if err != nil {
		c.emitAudit(ctx, logging.LevelError, logging.EventAuthAuthorizeEndpoint, logging.DecisionError,
			logging.String(logging.FieldAction, in.Method),
			logging.String(logging.FieldResource, in.Path),
			logging.Int(logging.FieldStatus, apiStatus(err)),
			logging.Err(err),
		)
		return nil, err
	}
	logging.Debug(ctx, c.log, "authorize_endpoint",
		logging.String("method", in.Method),
		logging.String("path", in.Path),
		logging.Bool("allowed", out.Allowed),
		logging.String("reason", out.Reason),
	)
	decision, level := allowDecision(out.Allowed)
	c.emitAudit(ctx, level, logging.EventAuthAuthorizeEndpoint, decision,
		logging.String(logging.FieldAction, in.Method),
		logging.String(logging.FieldResource, in.Path),
	)
	return &out, nil
}
