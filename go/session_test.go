package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestRefresh_Error(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusUnauthorized, "PLT-ASP-401", "expired")
	})
	client, _ := newTestClient(t, mux)
	if _, err := client.Refresh(context.Background(), "bad"); !IsUnauthorized(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestIntrospect_Error(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/introspect", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "bad")
	})
	client, _ := newTestClient(t, mux)
	if _, err := client.Introspect(context.Background(), "t", ""); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestRefresh_Introspect_LogoutValidation(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "rt-1" {
			t.Fatalf("body=%v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"access_token":       "access-2",
			"refresh_token":      "rt-2",
			"token_type":         "Bearer",
			"expires_in":         900,
			"refresh_expires_in": 86400,
			"session_id":         "sess-2",
		})
	})
	mux.HandleFunc("/v1/consumer-auth/introspect", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		exp := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		writeEnvelope(w, http.StatusOK, map[string]any{
			"active":     true,
			"reason":     "active",
			"session_id": "sess-2",
			"user_id":    7,
			"expires_at": exp.Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/v1/consumer-auth/logout", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "PLT-ASP-403", "forbidden")
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	session, err := client.Refresh(ctx, "rt-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.AccessToken != "access-2" {
		t.Fatalf("%+v", session)
	}
	if session.RefreshExpiresIn != 86400 {
		t.Fatalf("refresh_expires_in=%d", session.RefreshExpiresIn)
	}

	info, err := client.Introspect(ctx, "access-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Active || info.UserID != 7 {
		t.Fatalf("%+v", info)
	}

	if err := client.Logout(ctx, "", ""); !IsValidation(err) {
		t.Fatalf("logout validation: %v", err)
	}
	if _, err := client.Introspect(ctx, "", ""); !IsValidation(err) {
		t.Fatalf("introspect validation: %v", err)
	}
	if err := client.Logout(ctx, "rt", "sess"); !IsForbidden(err) {
		t.Fatalf("logout forbidden: %v", err)
	}
}

func TestRedactEmail(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":            "",
		"a":           "***",
		"@x.com":      "***",
		"ab@x.com":    "a***@x.com",
		"andi@acme.com": "a***@acme.com",
	}
	for in, want := range cases {
		if got := redactEmail(in); got != want {
			t.Fatalf("redactEmail(%q)=%q want %q", in, got, want)
		}
	}
}
