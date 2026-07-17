package authsdk

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestNew_RequiresConfig(t *testing.T) {
	t.Parallel()
	if _, err := New("", "memoo", Credentials("sa_test")); err == nil {
		t.Fatal("expected BaseURL error")
	}
	if _, err := New("https://auth.example.com", "", Credentials("sa_test")); err == nil {
		t.Fatal("expected ApplicationService error")
	}
	if _, err := New("https://auth.example.com", "memoo"); err == nil {
		t.Fatal("expected APIKey / Credentials error")
	}
}

func TestLogin_Authorize_Logout(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method %s", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "sa_test_key" {
			t.Fatalf("X-API-Key=%q", got)
		}
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
		if got := r.Header.Get("X-API-Key"); got != "sa_test_key" {
			t.Fatalf("X-API-Key=%q", got)
		}
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
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := New(srv.URL, "memoo",
		Credentials("sa_test_key"),
		WithHTTPDoer(http.DefaultClient),
		WithRetryCount(0),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	session, err := client.Login(ctx, LoginInput{Email: "andi@acme.com", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "sess-1" || session.AccessToken != "access-1" {
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
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid credentials","code":"PLT-ASP-401","data":null,"errors":null}`))
	}))
	t.Cleanup(srv.Close)

	client, err := New(srv.URL, "memoo", Credentials("sa_test_key"), WithHTTPDoer(http.DefaultClient))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Login(context.Background(), LoginInput{Email: "a@b.c", Password: "x"})
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestVerifyAccessToken_JWKS(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "kid-test-1"
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "42",
		"uid": float64(42),
		"sid": "sess-xyz",
		"iss": "auth-service",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kid": kid,
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"n":   n,
				"e":   e,
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := New(srv.URL, "memoo",
		Credentials("sa_test_key"),
		WithHTTPDoer(http.DefaultClient),
		WithJWKSCacheTTL(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := client.VerifyAccessToken(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 || claims.SessionID != "sess-xyz" {
		t.Fatalf("claims=%+v", claims)
	}
}

func TestHeimdallTransport_Constructs(t *testing.T) {
	t.Parallel()
	client, err := New("https://auth.example.com", "memoo", Credentials("sa_test_key"))
	if err != nil {
		t.Fatal(err)
	}
	if client.ApplicationService() != "memoo" {
		t.Fatal(client.ApplicationService())
	}
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
