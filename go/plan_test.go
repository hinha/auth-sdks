package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestCreateOrganization(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/organizations", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("method=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-jwt" {
			t.Fatalf("auth=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "Acme Corp" || body["slug"] != "acme-corp" || body["set_membership_organization"] != true {
			t.Fatalf("body=%v", body)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		writeEnvelope(w, http.StatusCreated, map[string]any{
			"id":         3,
			"name":       "Acme Corp",
			"slug":       "acme-corp",
			"status":     "active",
			"created_at": now,
		})
	})

	client, _ := newTestClient(t, mux)
	ctx := context.Background()

	out, err := client.CreateOrganization(ctx, "user-jwt", CreateOrganizationInput{
		Name:                      "Acme Corp",
		Slug:                      "acme-corp",
		SetMembershipOrganization: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 3 || out.Name != "Acme Corp" || out.Slug != "acme-corp" || out.Status != "active" {
		t.Fatalf("out=%+v", out)
	}
}

func TestCreateOrganization_NoAccessToken(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.CreateOrganization(context.Background(), "", CreateOrganizationInput{Name: "x", Slug: "x"}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateOrganization_Conflict(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/organizations", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "slug already exists")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.CreateOrganization(context.Background(), "user-jwt", CreateOrganizationInput{Name: "x", Slug: "x"}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestListOfferPlans_WithBearer(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer user-jwt" {
			t.Fatalf("auth=%q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("unexpected X-API-Key=%q", got)
		}
		q := r.URL.Query()
		if q.Get("page") != "2" || q.Get("limit") != "5" {
			t.Fatalf("query=%v", q)
		}
		writeEnvelope(w, http.StatusOK, []map[string]any{
			{
				"id":                     1,
				"application_service_id": 1,
				"code":                   "free",
				"name":                   "Free",
				"status":                 "active",
				"limits":                 map[string]any{"sqlite_db_count": 1.0},
				"features":               map[string]any{"beta_ui": false},
			},
			{"id": 2, "code": "pro", "name": "Pro", "status": "active"},
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.ListOfferPlans(context.Background(), "user-jwt", WithOfferPlansPage(2), WithOfferPlansLimit(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Code != "free" || out[1].Code != "pro" {
		t.Fatalf("out=%+v", out)
	}
	if out[0].Limits["sqlite_db_count"] != 1.0 {
		t.Fatalf("limits=%v", out[0].Limits)
	}
}

func TestListOfferPlans_WithAPIKey(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		if got := r.URL.Query().Get("application_service"); got != "memoo" {
			t.Fatalf("application_service=%q", got)
		}
		writeEnvelope(w, http.StatusOK, []map[string]any{{"id": 1, "code": "free", "name": "Free", "status": "active"}})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.ListOfferPlans(context.Background(), "", WithOfferPlansApplicationService("memoo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Code != "free" {
		t.Fatalf("out=%+v", out)
	}
}

func TestListOfferPlans_BearerPrefixedAPIKey(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != testAPIKey {
			t.Fatalf("X-API-Key=%q", got)
		}
		writeEnvelope(w, http.StatusOK, []map[string]any{})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.ListOfferPlans(context.Background(), "Bearer "+testAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("out=%+v", out)
	}
}

func TestListOfferPlans_Empty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/plans", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, nil)
	})
	client, _ := newTestClient(t, mux)

	out, err := client.ListOfferPlans(context.Background(), "user-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("out=%+v", out)
	}
}

func TestGetMyPlan(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/plan", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("method=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-jwt" {
			t.Fatalf("auth=%q", got)
		}
		q := r.URL.Query()
		if q.Get("subject_type") != "organization" || q.Get("subject_id") != "3" {
			t.Fatalf("query=%v", q)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"application_service": "memoo",
			"subject_type":        "organization",
			"subject_id":          "3",
			"plan_id":             1,
			"plan_code":           "free",
			"plan_name":           "Free",
			"organization": map[string]any{
				"id":     3,
				"name":   "Acme Corp",
				"slug":   "acme-corp",
				"status": "active",
			},
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.GetMyPlan(context.Background(), "user-jwt", "organization", "3")
	if err != nil {
		t.Fatal(err)
	}
	if out.PlanCode != "free" || out.SubjectType != "organization" || out.SubjectID != "3" {
		t.Fatalf("out=%+v", out)
	}
	if out.Organization == nil || out.Organization.Name != "Acme Corp" || out.Organization.Slug != "acme-corp" {
		t.Fatalf("organization=%+v", out.Organization)
	}
}

func TestGetMyPlan_NoAccessToken(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.GetMyPlan(context.Background(), "", "", ""); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestGetMyPlan_NotFound(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/plan", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "PLT-ASP-404", "not found")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.GetMyPlan(context.Background(), "user-jwt", "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectPlan(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/plan", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPut {
			t.Fatalf("method=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-jwt" {
			t.Fatalf("auth=%q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["plan_code"] != "pro" {
			t.Fatalf("body=%v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"application_service": "memoo",
			"subject_type":        "user",
			"subject_id":          "5",
			"plan_id":             2,
			"plan_code":           "pro",
			"plan_name":           "Pro",
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.SelectPlan(context.Background(), "user-jwt", SelectPlanInput{PlanCode: "pro"})
	if err != nil {
		t.Fatal(err)
	}
	if out.PlanCode != "pro" || out.PlanName != "Pro" {
		t.Fatalf("out=%+v", out)
	}
}

func TestSelectPlan_NoAccessToken(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.SelectPlan(context.Background(), "", SelectPlanInput{PlanCode: "pro"}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectPlan_ValidationError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/plan", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "active plan not found for application service")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.SelectPlan(context.Background(), "user-jwt", SelectPlanInput{PlanCode: "nope"}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}
