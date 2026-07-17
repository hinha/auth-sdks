package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/transport"
	"go.uber.org/zap"
)

func TestNew_RequiresConfig(t *testing.T) {
	t.Parallel()
	if _, err := New("", "memoo", Credentials(testAPIKey)); err == nil {
		t.Fatal("expected BaseURL error")
	}
	if _, err := New("https://auth.example.com", "", Credentials(testAPIKey)); err == nil {
		t.Fatal("expected ApplicationService error")
	}
	if _, err := New("https://auth.example.com", "memoo"); err == nil {
		t.Fatal("expected APIKey error")
	}
	if _, err := New("https://auth.example.com", strings.Repeat("a", 33), Credentials(testAPIKey)); err == nil {
		t.Fatal("expected ApplicationService max length error")
	}
	if _, err := New("https://auth.example.com", "memoo", Credentials(testAPIKey), WithHTTPDoer(nil)); err == nil {
		t.Fatal("expected nil doer error")
	}
}

func TestNew_OptionsAndAccessors(t *testing.T) {
	t.Parallel()
	zl := zap.NewNop()
	client, err := New(" https://auth.example.com/ ", " memoo ",
		Credentials("  "+testAPIKey+"  "),
		WithLogger(logging.NewZap(zl)),
		WithLogger(nil), // no-op
		WithTimeout(3*time.Second),
		WithRetryCount(0),
		WithJWKSCacheTTL(30*time.Second),
		WithUserAgent("sdk-test/1.0"),
		WithHeader("X-Trace", "abc"),
		WithTransportConfig(transport.Config{
			Timeout:               2 * time.Second,
			RetryCount:            1,
			BackoffInterval:       10 * time.Millisecond,
			MaximumJitterInterval: 5 * time.Millisecond,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.ApplicationService() != "memoo" {
		t.Fatalf("service=%q", client.ApplicationService())
	}
	if client.BaseURL() != "https://auth.example.com" {
		t.Fatalf("base=%q", client.BaseURL())
	}
	if client.cfg.APIKey != testAPIKey {
		t.Fatalf("apikey=%q", client.cfg.APIKey)
	}
	if client.cfg.UserAgent != "sdk-test/1.0" {
		t.Fatalf("ua=%q", client.cfg.UserAgent)
	}
}

func TestNew_APIPrefixNormalization(t *testing.T) {
	t.Parallel()
	o := &options{cfg: Config{APIPrefix: "custom"}}
	cfg := o.cfg
	cfg.BaseURL = "https://x"
	cfg.ApplicationService = "memoo"
	cfg.APIKey = testAPIKey
	cfg.APIPrefix = "custom"
	got := cfg.withDefaults()
	if got.APIPrefix != "/custom" {
		t.Fatalf("prefix=%q", got.APIPrefix)
	}
	got2 := Config{RetryCount: -1, JWKSCacheTTL: -1, Timeout: -1}.withDefaults()
	if got2.RetryCount != 2 || got2.Timeout != 15*time.Second || got2.JWKSCacheTTL != defaultJWKSCacheTTL {
		t.Fatalf("defaults=%+v", got2)
	}
}

func TestHeimdallTransport_Constructs(t *testing.T) {
	t.Parallel()
	client, err := New("https://auth.example.com", "memoo", Credentials(testAPIKey))
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL() != "https://auth.example.com" {
		t.Fatal(client.BaseURL())
	}
}

func TestLogin_Authorize_Logout(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/login", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["application_service"] != "memoo" {
			t.Fatalf("service=%v", body["application_service"])
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"access_token":  "access-1",
			"refresh_token": "refresh-1",
			"token_type":    "Bearer",
			"expires_in":    900,
			"session_id":    "sess-1",
		})
	})
	mux.HandleFunc("/v1/consumer-auth/authorize-action", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Fatalf("auth header=%q", got)
		}
		requireAPIKey(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		allowed := body["permission"] == "reports:read"
		writeEnvelope(w, http.StatusOK, map[string]any{
			"allowed":    allowed,
			"reason":     map[bool]string{true: "permission_granted", false: "permission_denied"}[allowed],
			"permission": body["permission"],
		})
	})
	mux.HandleFunc("/v1/consumer-auth/logout", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	session, err := client.Login(ctx, LoginInput{Email: "andi@acme.com", Password: "secret", DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-1" {
		t.Fatalf("session=%+v", session)
	}

	allow, err := client.Allow(ctx, session.AccessToken, "reports:read")
	if err != nil || !allow {
		t.Fatalf("allow=%v err=%v", allow, err)
	}
	deny, err := client.Allow(ctx, session.AccessToken, "reports:export")
	if err != nil || deny {
		t.Fatalf("deny=%v err=%v", deny, err)
	}
	if err := client.Logout(ctx, session.RefreshToken, session.SessionID); err != nil {
		t.Fatal(err)
	}
}

func TestLogin_MapsUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusUnauthorized, "PLT-ASP-401", "invalid credentials")
	}))
	t.Cleanup(srv.Close)

	client, err := New(srv.URL, "memoo", Credentials(testAPIKey), WithHTTPDoer(http.DefaultClient))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Login(context.Background(), LoginInput{Email: "a@b.c", Password: "x"})
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestLogin_MapsFirstLogin(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/login", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeErrData(w, http.StatusForbidden, "PLT-ASP-403", "First login required", map[string]string{
			"refer":               "/v1/consumer-auth/first-login",
			"application_service": "memoo",
		})
	})
	client, _ := newTestClient(t, mux)

	_, err := client.Login(context.Background(), LoginInput{Email: "andi@acme.com", Password: "Temp#1"})
	if !IsFirstLogin(err) {
		t.Fatalf("expected first-login, got %v", err)
	}
	fl, ok := AsFirstLogin(err)
	if !ok || fl.Refer != "/v1/consumer-auth/first-login" || fl.ApplicationService != "memoo" {
		t.Fatalf("%+v ok=%v", fl, ok)
	}
	if !IsForbidden(err) {
		t.Fatal("FirstLoginError should also match IsForbidden")
	}
}

func TestConfigError_Error(t *testing.T) {
	t.Parallel()
	err := &ConfigError{Field: "APIKey", Message: "required"}
	if got := err.Error(); !strings.Contains(got, "APIKey") {
		t.Fatalf("msg=%q", got)
	}
}

func TestWithClientKey_Empty(t *testing.T) {
	t.Parallel()
	c := &Client{cfg: Config{APIKey: ""}}
	if opts := c.withClientKey(); len(opts) != 0 {
		t.Fatalf("opts=%d", len(opts))
	}
	c.cfg.APIKey = testAPIKey
	if opts := c.withClientKey(); len(opts) != 1 {
		t.Fatalf("opts=%d", len(opts))
	}
}
