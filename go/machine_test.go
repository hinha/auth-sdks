package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestMachine_VerifyAndAuthorizeEndpoint(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/verify", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key != testAPIKey && key != "sa_other" {
			t.Fatalf("key=%q", key)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"application_service": "memoo",
			"plan":                "pro",
		})
	})
	mux.HandleFunc("/v1/consumer-auth/authorize-endpoint", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != testAPIKey {
			t.Fatalf("key=%q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["method"] != "GET" || body["path"] != "/reports" {
			t.Fatalf("body=%v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"allowed": true,
			"reason":  "scope_ok",
		})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	res, err := client.VerifyAPIKey(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if res["plan"] != "pro" {
		t.Fatalf("%v", res)
	}

	res2, err := client.VerifyAPIKey(ctx, "sa_other")
	if err != nil {
		t.Fatal(err)
	}
	if res2["application_service"] != "memoo" {
		t.Fatalf("%v", res2)
	}

	ep, err := client.AuthorizeEndpoint(ctx, "", AuthorizeEndpointInput{Method: "GET", Path: "/reports"})
	if err != nil || !ep.Allowed {
		t.Fatalf("ep=%+v err=%v", ep, err)
	}
}

func TestMachine_Errors(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/verify", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusUnauthorized, "PLT-ASP-401", "bad key")
	})
	mux.HandleFunc("/v1/consumer-auth/authorize-endpoint", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "PLT-ASP-403", "denied")
	})
	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	if _, err := client.VerifyAPIKey(ctx, "sa_bad"); !IsUnauthorized(err) {
		t.Fatalf("verify: %v", err)
	}
	if _, err := client.AuthorizeEndpoint(ctx, "", AuthorizeEndpointInput{Method: "POST", Path: "/x"}); !IsForbidden(err) {
		t.Fatalf("endpoint: %v", err)
	}
}
