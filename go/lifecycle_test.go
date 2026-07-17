package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestLifecycle_RegisterAndPasswordFlows(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/register", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["invite_token"] != "inv_1" || body["name"] != "Andi" {
			t.Fatalf("body=%v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"user_id":               11,
			"status":                "pending",
			"verification_required": true,
			"session":               nil,
		})
	})
	mux.HandleFunc("/v1/consumer-auth/verify-email", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/v1/consumer-auth/forgot-password", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/v1/consumer-auth/reset-password", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/v1/consumer-auth/change-password", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-xyz" {
			t.Fatalf("auth=%q", got)
		}
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{"ok": true})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	org := uint(3)
	reg, err := client.Register(ctx, RegisterInput{
		Email:          "andi@acme.com",
		Password:       "P@ssw0rd!",
		Name:           "Andi",
		InviteToken:    "inv_1",
		OrganizationID: &org,
		DeviceID:       "dev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.UserID != 11 || !reg.VerificationRequired {
		t.Fatalf("%+v", reg)
	}

	if err := client.VerifyEmail(ctx, "andi@acme.com", "vrf_1"); err != nil {
		t.Fatal(err)
	}
	if err := client.ForgotPassword(ctx, "andi@acme.com"); err != nil {
		t.Fatal(err)
	}
	if err := client.ResetPassword(ctx, "rst_1", "N3wP@ss!"); err != nil {
		t.Fatal(err)
	}
	if err := client.ChangePassword(ctx, "access-xyz", "old", "new"); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycle_FirstLogin(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/first-login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method=%s", r.Method)
		}
		requireAPIKey(t, r)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "andi@acme.com" ||
			body["current_password"] != "Temp#1" ||
			body["new_password"] != "N3wP@ss!" ||
			body["confirm_password"] != "N3wP@ss!" ||
			body["application_service"] != "memoo" {
			t.Fatalf("body=%v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"message": "Password changed successfully",
			"refer":   "/v1/consumer-auth/login",
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.FirstLogin(context.Background(), FirstLoginInput{
		Email:           "andi@acme.com",
		CurrentPassword: "Temp#1",
		NewPassword:     "N3wP@ss!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Refer != "/v1/consumer-auth/login" {
		t.Fatalf("%+v", out)
	}
}

func TestLifecycle_RegisterError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/register", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "PLT-ASP-403", "invite required")
	})
	client, _ := newTestClient(t, mux)
	_, err := client.Register(context.Background(), RegisterInput{
		Email: "a@b.c", Password: "x", Name: "A",
	})
	if !IsForbidden(err) {
		t.Fatalf("err=%v", err)
	}
}
