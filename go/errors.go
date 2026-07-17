package authsdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hinha/auth-sdks/go/internal/api"
)

// Re-export typed errors so consumers depend only on the root package.
type (
	APIError          = api.APIError
	UnauthorizedError = api.UnauthorizedError
	ForbiddenError    = api.ForbiddenError
	ValidationError   = api.ValidationError
	NetworkError      = api.NetworkError
)

// FirstLoginError is returned by Login when Auth Service signals that the
// app_user must change a temp password before a session may be issued
// (HTTP 403 + data.refer pointing at /v1/consumer-auth/first-login).
type FirstLoginError struct {
	*ForbiddenError
	Refer              string
	ApplicationService string
}

func (e *FirstLoginError) Error() string {
	if e == nil {
		return "auth first-login required"
	}
	if e.ForbiddenError != nil {
		return fmt.Sprintf("auth first-login required: %s (refer=%s)", e.ForbiddenError.Message, e.Refer)
	}
	return fmt.Sprintf("auth first-login required (refer=%s)", e.Refer)
}

func (e *FirstLoginError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.ForbiddenError
}

// IsUnauthorized reports whether err is (or wraps) UnauthorizedError.
func IsUnauthorized(err error) bool {
	var target *UnauthorizedError
	return errors.As(err, &target)
}

// IsForbidden reports whether err is (or wraps) ForbiddenError.
func IsForbidden(err error) bool {
	var target *ForbiddenError
	return errors.As(err, &target)
}

// IsValidation reports whether err is (or wraps) ValidationError.
func IsValidation(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

// IsFirstLogin reports whether err is (or wraps) FirstLoginError.
func IsFirstLogin(err error) bool {
	var target *FirstLoginError
	return errors.As(err, &target)
}

// AsFirstLogin extracts a FirstLoginError from err, including a raw
// ForbiddenError whose body carries data.refer with "first-login".
func AsFirstLogin(err error) (*FirstLoginError, bool) {
	var typed *FirstLoginError
	if errors.As(err, &typed) {
		return typed, true
	}
	return parseFirstLoginFromForbidden(err)
}

func parseFirstLoginFromForbidden(err error) (*FirstLoginError, bool) {
	var forbid *ForbiddenError
	if !errors.As(err, &forbid) || forbid == nil || forbid.APIError == nil {
		return nil, false
	}
	var env struct {
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(forbid.Body, &env) != nil || len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, false
	}
	var data map[string]string
	if json.Unmarshal(env.Data, &data) != nil {
		return nil, false
	}
	refer := data["refer"]
	if !strings.Contains(refer, "first-login") {
		return nil, false
	}
	return &FirstLoginError{
		ForbiddenError:     forbid,
		Refer:              refer,
		ApplicationService: data["application_service"],
	}, true
}

// ConfigError is raised for invalid SDK configuration.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("auth-sdk config %s: %s", e.Field, e.Message)
}
