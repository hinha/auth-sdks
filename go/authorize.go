package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdk-go/internal/api"
	"github.com/hinha/auth-sdk-go/logging"
)

type authorizeActionRequest struct {
	ApplicationService string `json:"application_service"`
	Permission         string `json:"permission"`
	Method             string `json:"method,omitempty"`
	Path               string `json:"path,omitempty"`
	UserID             *uint  `json:"user_id,omitempty"`
}

// AuthorizeAction evaluates hybrid RBAC for a permission key.
// Pass accessToken for the end-user JWT path, or leave it empty and set
// MachineAPIKey via AuthorizeActionWithAPIKey.
func (c *Client) AuthorizeAction(ctx context.Context, accessToken string, in AuthorizeActionInput) (*AuthorizeActionResult, error) {
	return c.authorizeAction(ctx, accessToken, "", in)
}

// AuthorizeActionWithAPIKey uses the machine path (sa_* + optional user_id).
func (c *Client) AuthorizeActionWithAPIKey(ctx context.Context, apiKey string, in AuthorizeActionInput) (*AuthorizeActionResult, error) {
	return c.authorizeAction(ctx, "", apiKey, in)
}

func (c *Client) authorizeAction(ctx context.Context, bearer, apiKey string, in AuthorizeActionInput) (*AuthorizeActionResult, error) {
	if in.Permission == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "permission required",
		}}
	}

	var opts []api.CallOption
	if bearer != "" {
		opts = append(opts, api.WithBearer(bearer))
	}
	if apiKey != "" {
		opts = append(opts, api.WithAPIKey(apiKey))
	} else {
		opts = c.withClientKey(opts...)
	}

	var out AuthorizeActionResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/authorize-action"), authorizeActionRequest{
		ApplicationService: c.cfg.ApplicationService,
		Permission:         in.Permission,
		Method:             in.Method,
		Path:               in.Path,
		UserID:             in.UserID,
	}, &out, opts...)
	if err != nil {
		return nil, err
	}
	logging.Debug(ctx, c.log, "authorize_action",
		logging.String("permission", in.Permission),
		logging.Bool("allowed", out.Allowed),
		logging.String("reason", out.Reason),
	)
	return &out, nil
}

// Allow is a convenience wrapper: true when AuthorizeAction allows.
func (c *Client) Allow(ctx context.Context, accessToken, permission string) (bool, error) {
	res, err := c.AuthorizeAction(ctx, accessToken, AuthorizeActionInput{Permission: permission})
	if err != nil {
		return false, err
	}
	return res.Allowed, nil
}
