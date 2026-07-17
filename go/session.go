package authsdk

import (
	"context"
	"net/http"

	"github.com/hinha/auth-sdks/go/logging"
)

type loginRequest struct {
	Email              string `json:"email"`
	Password           string `json:"password"`
	ApplicationService string `json:"application_service"`
	OrganizationID     *uint  `json:"organization_id,omitempty"`
	DeviceID           string `json:"device_id,omitempty"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

type introspectRequest struct {
	Token     string `json:"token,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Login authenticates an app_user against the configured application_service.
// When the account still has a temp password (null last_login), Auth returns
// 403 with data.refer=/v1/consumer-auth/first-login — Login wraps that as
// FirstLoginError (IsFirstLogin). Call FirstLogin then Login again; no session
// is issued until the password is changed.
func (c *Client) Login(ctx context.Context, in LoginInput) (*Session, error) {
	logging.Info(ctx, c.log, "login_start", logging.String("email", redactEmail(in.Email)))
	var out Session
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/login"), loginRequest{
		Email:              in.Email,
		Password:           in.Password,
		ApplicationService: c.cfg.ApplicationService,
		OrganizationID:     in.OrganizationID,
		DeviceID:           in.DeviceID,
	}, &out, c.withClientKey()...)
	if err != nil {
		if fl, ok := AsFirstLogin(err); ok {
			logging.Warn(ctx, c.log, "login_first_login_required",
				logging.String("refer", fl.Refer),
				logging.String("application_service", fl.ApplicationService),
			)
			return nil, fl
		}
		logging.Warn(ctx, c.log, "login_failed", logging.Err(err))
		return nil, err
	}
	logging.Info(ctx, c.log, "login_ok", logging.String("session_id", out.SessionID))
	return &out, nil
}

// Refresh rotates tokens using a refresh_token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	var out Session
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/refresh"), refreshRequest{
		RefreshToken: refreshToken,
	}, &out, c.withClientKey()...)
	if err != nil {
		return nil, err
	}
	logging.Info(ctx, c.log, "refresh_ok", logging.String("session_id", out.SessionID))
	return &out, nil
}

// Logout revokes a session by refresh token and/or session id.
func (c *Client) Logout(ctx context.Context, refreshToken, sessionID string) error {
	if refreshToken == "" && sessionID == "" {
		return &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "refresh_token or session_id required",
		}}
	}
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/logout"), logoutRequest{
		RefreshToken: refreshToken,
		SessionID:    sessionID,
	}, nil, c.withClientKey()...)
	if err != nil {
		return err
	}
	logging.Info(ctx, c.log, "logout_ok", logging.String("session_id", sessionID))
	return nil
}

// Introspect checks whether a token or session is still active.
func (c *Client) Introspect(ctx context.Context, token, sessionID string) (*IntrospectResult, error) {
	if token == "" && sessionID == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "token or session_id required",
		}}
	}
	var out IntrospectResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/introspect"), introspectRequest{
		Token:     token,
		SessionID: sessionID,
	}, &out, c.withClientKey()...)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func redactEmail(email string) string {
	if email == "" {
		return ""
	}
	at := -1
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			at = i
			break
		}
	}
	if at <= 1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
