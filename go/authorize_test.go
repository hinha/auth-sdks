package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuthorizeAction_Variants(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/authorize-action", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		apiKey := r.Header.Get("X-API-Key")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		switch {
		case auth == "Bearer user-jwt":
			// JWT path must not also send machine X-API-Key (Auth prefers it → 400).
			if apiKey != "" {
				t.Fatalf("unexpected X-API-Key on JWT path: %q", apiKey)
			}
		case apiKey == "sa_override":
			if auth != "Bearer sa_override" {
				t.Fatalf("auth=%q", auth)
			}
		default:
			t.Fatalf("unexpected headers auth=%q key=%q", auth, apiKey)
		}

		writeEnvelope(w, http.StatusOK, map[string]any{
			"allowed":    true,
			"reason":     "permission_granted",
			"permission": body["permission"],
			"user_id":    9,
		})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	if _, err := client.AuthorizeAction(ctx, "tok", AuthorizeActionInput{}); !IsValidation(err) {
		t.Fatalf("empty permission: %v", err)
	}

	uid := uint(9)
	res, err := client.AuthorizeAction(ctx, "user-jwt", AuthorizeActionInput{
		Permission: "reports:read",
		Method:     "GET",
		Path:       "/reports",
		UserID:     &uid,
	})
	if err != nil || !res.Allowed {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	res2, err := client.AuthorizeActionWithAPIKey(ctx, "sa_override", AuthorizeActionInput{
		Permission: "reports:read",
		UserID:     &uid,
	})
	if err != nil || !res2.Allowed {
		t.Fatalf("res2=%+v err=%v", res2, err)
	}
}

func TestAllow_PropagatesError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/authorize-action", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "bad")
	})
	client, _ := newTestClient(t, mux)
	ok, err := client.Allow(context.Background(), "tok", "x:y")
	if ok || !IsValidation(err) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
