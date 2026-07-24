package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestGetEntitlements(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/entitlements", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("method=%q", got)
		}
		q := r.URL.Query()
		if q.Get("application_service") != "memoo" {
			t.Fatalf("application_service=%q", q.Get("application_service"))
		}
		if q.Get("subject_type") != "organization" || q.Get("subject_id") != "acme-corp" {
			t.Fatalf("subject query=%v", q)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"application_service": "memoo",
			"plan":                "pro",
			"plan_name":           "Pro",
			"source":              "subscription",
			"subject_type":        "organization",
			"subject_id":          "acme-corp",
			"limits": map[string]any{
				"sqlite_db_count": 5,
				"api_rate_limit":  nil,
			},
			"features": map[string]any{
				"reports.export.enabled": true,
			},
			"entitlements": []map[string]any{
				{"key": "sqlite_db_count", "type": "quota", "unit": "count", "unlimited": false, "value": 5.0},
				{"key": "api_rate_limit", "type": "rate", "unlimited": true},
				{"key": "reports.export.enabled", "type": "boolean", "unlimited": false, "value": true},
			},
		})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	out, err := client.GetEntitlements(ctx, "", EntitlementsInput{SubjectType: "organization", SubjectID: "acme-corp"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan != "pro" || out.PlanName != "Pro" || out.Source != "subscription" {
		t.Fatalf("out=%+v", out)
	}
	if len(out.Entitlements) != 3 {
		t.Fatalf("entitlements=%v", out.Entitlements)
	}
	if !out.IsFeatureEnabled("reports.export.enabled") {
		t.Fatal("expected feature enabled")
	}
	if out.IsFeatureEnabled("missing.flag") {
		t.Fatal("expected missing feature to be disabled")
	}

	allowed, limit, unlimited := out.CheckQuota("sqlite_db_count", 3)
	if !allowed || unlimited || limit == nil || *limit != 5 {
		t.Fatalf("allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}
	allowed, limit, unlimited = out.CheckQuota("sqlite_db_count", 5)
	if allowed || unlimited || limit == nil || *limit != 5 {
		t.Fatalf("expected denied at limit: allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}

	allowed, limit, unlimited = out.CheckQuota("api_rate_limit", 1000)
	if !allowed || !unlimited || limit != nil {
		t.Fatalf("expected unlimited: allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}

	// Unknown dimension: fail open.
	allowed, limit, unlimited = out.CheckQuota("unknown_dimension", 1000)
	if !allowed || !unlimited || limit != nil {
		t.Fatalf("expected fail-open for unknown dimension: allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}
}

func TestGetEntitlements_FeaturesMapFallback(t *testing.T) {
	t.Parallel()
	out := &EntitlementsResult{
		Features: map[string]any{"beta_ui": true, "legacy_flag": "true"},
	}
	if !out.IsFeatureEnabled("beta_ui") {
		t.Fatal("expected beta_ui enabled via Features map")
	}
	if !out.IsFeatureEnabled("legacy_flag") {
		t.Fatal("expected legacy_flag truthy string to be enabled")
	}
	if out.IsFeatureEnabled("nope") {
		t.Fatal("expected missing key disabled")
	}
}

func TestGetEntitlements_LimitsMapFallback(t *testing.T) {
	t.Parallel()
	out := &EntitlementsResult{
		Limits: map[string]any{"sqlite_db_count": 2.0, "api_rate_limit": nil},
	}
	allowed, limit, unlimited := out.CheckQuota("sqlite_db_count", 1)
	if !allowed || unlimited || limit == nil || *limit != 2 {
		t.Fatalf("allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}
	allowed, _, unlimited = out.CheckQuota("api_rate_limit", 100)
	if !allowed || !unlimited {
		t.Fatalf("expected unlimited via null limit: allowed=%v unlimited=%v", allowed, unlimited)
	}
}

func TestCheckQuota_NilResult(t *testing.T) {
	t.Parallel()
	var out *EntitlementsResult
	if out.IsFeatureEnabled("x") {
		t.Fatal("nil result should never report feature enabled")
	}
	allowed, limit, unlimited := out.CheckQuota("x", 10)
	if !allowed || !unlimited || limit != nil {
		t.Fatalf("nil result should fail open: allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}
}

func TestGetEntitlements_Forbidden(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/entitlements", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "PLT-ASP-403", "denied")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.GetEntitlements(context.Background(), "", EntitlementsInput{}); !IsForbidden(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestSyncPlans(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans/sync", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("method=%q", got)
		}
		requireAPIKey(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["application_service"] != "memoo" {
			t.Fatalf("application_service=%v", body["application_service"])
		}
		dims, _ := body["dimensions"].([]any)
		if len(dims) != 1 {
			t.Fatalf("dimensions=%v", body["dimensions"])
		}
		plans, _ := body["plans"].([]any)
		if len(plans) != 1 {
			t.Fatalf("plans=%v", body["plans"])
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"dimensions_upserted": 1,
			"plans_upserted":      1,
		})
	})

	client, _ := newTestClient(t, mux)

	valueNumber := 5.0
	out, err := client.SyncPlans(context.Background(),
		[]PlanDimensionInput{{
			Key:             "sqlite_db_count",
			Name:            "SQLite DB count",
			ValueType:       "quota",
			Unit:            "count",
			AllowsUnlimited: true,
			EnforcementHint: "hard",
			Status:          "active",
		}},
		[]PlanSyncInput{{
			Code:   "lite",
			Name:   "Lite",
			Status: "active",
			EntitlementValues: []PlanEntitlementValueInput{{
				DimensionKey: "sqlite_db_count",
				ValueNumber:  &valueNumber,
			}},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.DimensionsUpserted != 1 || out.PlansUpserted != 1 {
		t.Fatalf("out=%+v", out)
	}
}

func TestSyncPlans_WithAPIKeyOverride(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans/sync", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "sa_other" {
			t.Fatalf("key=%q", got)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{"dimensions_upserted": 0, "plans_upserted": 0})
	})
	client, _ := newTestClient(t, mux)

	_, err := client.SyncPlans(context.Background(), nil, nil, WithSyncPlansAPIKey("sa_other"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncPlans_ValidationError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans/sync", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "unknown entitlement dimension")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.SyncPlans(context.Background(), nil, nil); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

// TestEvaluateHelpers_NoNATSIsNoop ensures EvaluateFeature/EvaluateQuota work
// (and never block/fail) when WithNATS was not configured.
func TestEvaluateHelpers_NoNATSIsNoop(t *testing.T) {
	t.Parallel()
	client, err := New("https://auth.example.com", "memoo", Credentials(testAPIKey))
	if err != nil {
		t.Fatal(err)
	}
	ent := &EntitlementsResult{
		Plan:     "pro",
		Features: map[string]any{"beta_ui": true},
		Limits:   map[string]any{"sqlite_db_count": 5.0},
	}
	if !client.EvaluateFeature(context.Background(), ent, "beta_ui") {
		t.Fatal("expected enabled")
	}
	allowed, limit, unlimited := client.EvaluateQuota(context.Background(), ent, "sqlite_db_count", 1)
	if !allowed || unlimited || limit == nil || *limit != 5 {
		t.Fatalf("allowed=%v limit=%v unlimited=%v", allowed, limit, unlimited)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
