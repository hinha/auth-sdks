package authsdk

import (
	"errors"
	"testing"

	"github.com/hinha/auth-sdks/go/internal/api"
)

func TestErrorHelpers(t *testing.T) {
	t.Parallel()

	unauth := &UnauthorizedError{APIError: &APIError{StatusCode: 401, Code: "401", Message: "no"}}
	forbid := &ForbiddenError{APIError: &APIError{StatusCode: 403, Code: "403", Message: "no"}}
	valid := &ValidationError{APIError: &APIError{StatusCode: 400, Code: "400", Message: "no"}}
	netErr := &NetworkError{Op: "POST /x", Err: errors.New("dial")}
	first := &FirstLoginError{
		ForbiddenError:     forbid,
		Refer:              "/v1/consumer-auth/first-login",
		ApplicationService: "memoo",
	}

	if !IsUnauthorized(unauth) || IsUnauthorized(forbid) {
		t.Fatal("unauthorized helper")
	}
	if !IsForbidden(forbid) || IsForbidden(unauth) {
		t.Fatal("forbidden helper")
	}
	if !IsValidation(valid) || IsValidation(unauth) {
		t.Fatal("validation helper")
	}
	if !IsFirstLogin(first) || IsFirstLogin(forbid) {
		t.Fatal("first-login helper")
	}
	if first.Error() == "" || first.Unwrap() == nil {
		t.Fatal("first-login methods")
	}
	if unauth.Error() == "" || unauth.Unwrap() == nil {
		t.Fatal("unauthorized methods")
	}
	if forbid.Error() == "" || forbid.Unwrap() == nil {
		t.Fatal("forbidden methods")
	}
	if valid.Error() == "" || valid.Unwrap() == nil {
		t.Fatal("validation methods")
	}
	if netErr.Error() == "" || netErr.Unwrap() == nil {
		t.Fatal("network error")
	}
	if (&APIError{StatusCode: 500, Code: "500", Message: "x"}).Error() == "" {
		t.Fatal("empty api error")
	}
	var nilAPI *api.APIError
	if nilAPI.Error() != "api: unknown error" {
		t.Fatalf("nil api error: %q", nilAPI.Error())
	}
	var nilFL *FirstLoginError
	if nilFL.Error() != "auth first-login required" {
		t.Fatalf("nil first-login: %q", nilFL.Error())
	}
}
