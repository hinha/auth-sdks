package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
)

type registerRequest struct {
	ApplicationService string `json:"application_service"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	Name               string `json:"name"`
	OrganizationID     *uint  `json:"organization_id,omitempty"`
	InviteToken        string `json:"invite_token,omitempty"`
	DeviceID           string `json:"device_id,omitempty"`
}

type verifyEmailRequest struct {
	ApplicationService string `json:"application_service"`
	Email              string `json:"email"`
	Token              string `json:"token"`
}

type forgotPasswordRequest struct {
	ApplicationService string `json:"application_service"`
	Email              string `json:"email"`
}

type resetPasswordRequest struct {
	ApplicationService string `json:"application_service"`
	Token              string `json:"token"`
	NewPassword        string `json:"new_password"`
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// Register creates an app_user for the configured application_service.
func (c *Client) Register(ctx context.Context, in RegisterInput) (*RegisterResult, error) {
	var out RegisterResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/register"), registerRequest{
		ApplicationService: c.cfg.ApplicationService,
		Email:              in.Email,
		Password:           in.Password,
		Name:               in.Name,
		OrganizationID:     in.OrganizationID,
		InviteToken:        in.InviteToken,
		DeviceID:           in.DeviceID,
	}, &out, c.withClientKey()...)
	if err != nil {
		return nil, err
	}
	logging.Info(ctx, c.log, "register_ok",
		logging.Bool("verification_required", out.VerificationRequired),
	)
	return &out, nil
}

// VerifyEmail completes email verification.
func (c *Client) VerifyEmail(ctx context.Context, email, token string) error {
	return c.api.DoJSON(ctx, http.MethodPost, c.path("/verify-email"), verifyEmailRequest{
		ApplicationService: c.cfg.ApplicationService,
		Email:              email,
		Token:              token,
	}, nil, c.withClientKey()...)
}

// ForgotPassword triggers a password-reset email (always generic success).
func (c *Client) ForgotPassword(ctx context.Context, email string) error {
	return c.api.DoJSON(ctx, http.MethodPost, c.path("/forgot-password"), forgotPasswordRequest{
		ApplicationService: c.cfg.ApplicationService,
		Email:              email,
	}, nil, c.withClientKey()...)
}

// ResetPassword completes password reset; Auth revokes all sessions.
func (c *Client) ResetPassword(ctx context.Context, token, newPassword string) error {
	return c.api.DoJSON(ctx, http.MethodPost, c.path("/reset-password"), resetPasswordRequest{
		ApplicationService: c.cfg.ApplicationService,
		Token:              token,
		NewPassword:        newPassword,
	}, nil, c.withClientKey()...)
}

// ChangePassword changes password for the authenticated consumer JWT.
func (c *Client) ChangePassword(ctx context.Context, accessToken, oldPassword, newPassword string) error {
	return c.api.DoJSON(ctx, http.MethodPost, c.path("/change-password"), changePasswordRequest{
		OldPassword: oldPassword,
		NewPassword: newPassword,
	}, nil, c.withClientKey(api.WithBearer(accessToken))...)
}
