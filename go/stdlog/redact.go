package stdlog

import (
	"net/http"
	"strings"
)

// DefaultRedactHeaders are never logged in plain text.
var DefaultRedactHeaders = []string{
	"authorization",
	"cookie",
	"set-cookie",
	"x-api-key",
	"x-auth-token",
	"proxy-authorization",
}

// DefaultRedactJSONFields are replaced with "***" in JSON bodies.
var DefaultRedactJSONFields = []string{
	"password",
	"token",
	"secret",
	"api_key",
	"access_token",
	"refresh_token",
}

// RedactHeaders returns a copy of headers with sensitive keys removed.
func RedactHeaders(h http.Header, extra ...string) map[string]any {
	deny := make(map[string]struct{}, len(DefaultRedactHeaders)+len(extra))
	for _, k := range DefaultRedactHeaders {
		deny[strings.ToLower(k)] = struct{}{}
	}
	for _, k := range extra {
		deny[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	out := make(map[string]any, len(h))
	for key, values := range h {
		if _, skip := deny[strings.ToLower(key)]; skip {
			continue
		}
		if len(values) == 1 {
			out[key] = values[0]
		} else {
			out[key] = values
		}
	}
	return out
}

// RedactJSON recursively replaces sensitive keys with "***".
func RedactJSON(value any, extraFields ...string) any {
	deny := make(map[string]struct{}, len(DefaultRedactJSONFields)+len(extraFields))
	for _, k := range DefaultRedactJSONFields {
		deny[k] = struct{}{}
	}
	for _, k := range extraFields {
		deny[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	return redactValue(value, deny)
}

func redactValue(value any, deny map[string]struct{}) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if _, hit := deny[strings.ToLower(key)]; hit {
				out[key] = "***"
				continue
			}
			out[key] = redactValue(child, deny)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = redactValue(v[i], deny)
		}
		return out
	default:
		return value
	}
}
