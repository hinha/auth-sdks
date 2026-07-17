// Package api contains internal HTTP helpers for Auth Service envelopes.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/transport"
)

// Envelope is the standard Auth Service JSON response wrapper.
type Envelope[T any] struct {
	Message  string `json:"message"`
	Data     T      `json:"data"`
	Metadata any    `json:"metadata,omitempty"`
	Errors   any    `json:"errors"`
	Code     string `json:"code"`
}

// Requester performs JSON HTTP calls against Auth Service (Gateway).
type Requester struct {
	BaseURL string
	Doer    transport.Doer
	Logger  logging.Logger
	Header  http.Header
}

// DoJSON executes a JSON request and decodes a successful envelope into out.
func (r *Requester) DoJSON(ctx context.Context, method, path string, body any, out any, opts ...CallOption) error {
	cfg := callConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("api: marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	url := strings.TrimRight(r.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("api: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vals := range r.Header {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if cfg.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.bearer)
	}
	if cfg.apiKey != "" {
		req.Header.Set("X-API-Key", cfg.apiKey)
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
		}
	}

	logging.Debug(ctx, r.Logger, "auth_api_call",
		logging.String("method", method),
		logging.String("path", path),
	)

	res, err := r.Doer.Do(req)
	if err != nil {
		return &NetworkError{Op: method + " " + path, Err: err}
	}
	defer transport.DrainBody(res.Body)

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return &NetworkError{Op: method + " " + path, Err: err}
	}

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out == nil || len(raw) == 0 {
			return nil
		}
		env := Envelope[json.RawMessage]{}
		if err := json.Unmarshal(raw, &env); err != nil {
			// Some endpoints (JWKS) return raw JSON without envelope.
			if cfg.rawBody {
				return json.Unmarshal(raw, out)
			}
			return fmt.Errorf("api: decode envelope: %w", err)
		}
		if cfg.rawBody {
			return json.Unmarshal(raw, out)
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("api: decode data: %w", err)
		}
		return nil
	}

	return mapHTTPError(res.StatusCode, raw)
}

// CallOption customizes a single HTTP call.
type CallOption func(*callConfig)

type callConfig struct {
	bearer  string
	apiKey  string
	rawBody bool
}

// WithBearer sets Authorization: Bearer <token>.
func WithBearer(token string) CallOption {
	return func(c *callConfig) { c.bearer = token }
}

// WithAPIKey sets machine credential headers.
func WithAPIKey(key string) CallOption {
	return func(c *callConfig) { c.apiKey = key }
}

// WithRawBody decodes the full response body (no Auth envelope), e.g. JWKS.
func WithRawBody() CallOption {
	return func(c *callConfig) { c.rawBody = true }
}

func mapHTTPError(status int, raw []byte) error {
	var env Envelope[json.RawMessage]
	_ = json.Unmarshal(raw, &env)

	msg := env.Message
	if msg == "" {
		msg = http.StatusText(status)
	}
	code := env.Code
	if code == "" {
		code = strconv.Itoa(status)
	}

	base := &APIError{
		StatusCode: status,
		Code:       code,
		Message:    msg,
		Errors:     env.Errors,
		Body:       append([]byte(nil), raw...),
	}

	switch {
	case status == http.StatusUnauthorized:
		return &UnauthorizedError{APIError: base}
	case status == http.StatusForbidden:
		return &ForbiddenError{APIError: base}
	case status == http.StatusBadRequest:
		return &ValidationError{APIError: base}
	default:
		return base
	}
}

// APIError is the base Auth Service HTTP error.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Errors     any
	Body       []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return "api: unknown error"
	}
	return fmt.Sprintf("auth api %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// UnauthorizedError maps HTTP 401.
type UnauthorizedError struct{ *APIError }

func (e *UnauthorizedError) Error() string { return e.APIError.Error() }
func (e *UnauthorizedError) Unwrap() error { return e.APIError }

// ForbiddenError maps HTTP 403.
type ForbiddenError struct{ *APIError }

func (e *ForbiddenError) Error() string { return e.APIError.Error() }
func (e *ForbiddenError) Unwrap() error { return e.APIError }

// ValidationError maps HTTP 400.
type ValidationError struct{ *APIError }

func (e *ValidationError) Error() string { return e.APIError.Error() }
func (e *ValidationError) Unwrap() error { return e.APIError }

// NetworkError wraps transport failures.
type NetworkError struct {
	Op  string
	Err error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("auth network %s: %v", e.Op, e.Err)
}
func (e *NetworkError) Unwrap() error { return e.Err }
