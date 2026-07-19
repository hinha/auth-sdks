package authsdk

import "time"

// Session is returned by Login / Refresh (and optionally Register).
type Session struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in,omitempty"`
	SessionID        string `json:"session_id"`
}

// LoginInput is the password login payload.
type LoginInput struct {
	Email          string
	Password       string
	OrganizationID *uint
	DeviceID       string
}

// RegisterInput self-registers an app_user for the configured service.
type RegisterInput struct {
	Email          string
	Password       string
	Name           string
	OrganizationID *uint
	InviteToken    string
	DeviceID       string
}

// RegisterResult is the outcome of Register.
type RegisterResult struct {
	UserID               uint     `json:"user_id"`
	Status               string   `json:"status"`
	VerificationRequired bool     `json:"verification_required"`
	Session              *Session `json:"session,omitempty"`
}

// FirstLoginInput completes the forced password change for an
// operator-provisioned app_user (temp password / null last_login).
// No session is issued — call Login again after success.
type FirstLoginInput struct {
	Email           string
	CurrentPassword string
	NewPassword     string
	ConfirmPassword string
}

// FirstLoginResult is returned by FirstLogin.
type FirstLoginResult struct {
	Message string `json:"message"`
	Refer   string `json:"refer"`
}

// AuthorizeActionInput evaluates a permission key (hybrid RBAC).
type AuthorizeActionInput struct {
	Permission string
	Method     string
	Path       string
	UserID     *uint
}

// AuthorizeActionResult is the allow/deny decision.
type AuthorizeActionResult struct {
	Allowed            bool     `json:"allowed"`
	Reason             string   `json:"reason"`
	UserID             uint     `json:"user_id,omitempty"`
	Permission         string   `json:"permission,omitempty"`
	MatchedPermissions []string `json:"matched_permissions,omitempty"`
	Plan               string   `json:"plan,omitempty"`
	EndpointID         uint     `json:"endpoint_id,omitempty"`
	MatchedPath        string   `json:"matched_path,omitempty"`
}

// IntrospectResult reports whether a session is still active.
type IntrospectResult struct {
	Active    bool       `json:"active"`
	Reason    string     `json:"reason,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	UserID    uint       `json:"user_id,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Claims are the verified consumer JWT claims exposed by VerifyAccessToken.
type Claims struct {
	Subject   string
	UserID    uint
	SessionID string
	Issuer    string
	Audience  []string
	ExpiresAt time.Time
	IssuedAt  time.Time
	Raw       map[string]any
}

// MachineVerifyResult is returned by VerifyAPIKey.
type MachineVerifyResult map[string]any

// AuthorizeEndpointInput checks machine endpoint access.
type AuthorizeEndpointInput struct {
	Method string
	Path   string
}

// AuthorizeEndpointResult is the endpoint allow/deny decision.
type AuthorizeEndpointResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}
