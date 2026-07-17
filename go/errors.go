package authsdk

import (
	"errors"
	"fmt"

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

// ConfigError is raised for invalid SDK configuration.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("auth-sdk config %s: %s", e.Field, e.Message)
}
