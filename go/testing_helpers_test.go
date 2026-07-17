package authsdk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testAPIKey = "sa_test_key"

func newTestClient(t *testing.T, handler http.Handler, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base := []Option{
		Credentials(testAPIKey),
		WithHTTPDoer(http.DefaultClient),
		WithRetryCount(0),
	}
	base = append(base, opts...)
	client, err := New(srv.URL, "memoo", base...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, srv
}

func writeEnvelope(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "OK",
		"data":    data,
		"errors":  nil,
		"code":    "PLT-ASP-200",
	})
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeErrData(w, status, code, msg, nil)
}

func writeErrData(w http.ResponseWriter, status int, code, msg string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": msg,
		"data":    data,
		"errors":  nil,
		"code":    code,
	})
}

func requireAPIKey(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-API-Key"); got != testAPIKey {
		t.Fatalf("X-API-Key=%q", got)
	}
}
