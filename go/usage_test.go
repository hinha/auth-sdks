package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestReportUsage(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/usage/report", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodPost {
			t.Fatalf("method=%q", got)
		}
		requireAPIKey(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["subject_type"] != "user" || body["subject_id"] != "5" {
			t.Fatalf("body=%v", body)
		}
		items, _ := body["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items=%v", body["items"])
		}
		writeEnvelope(w, http.StatusOK, []map[string]any{
			{
				"id":                     1,
				"application_service_id": 1,
				"subject_type":           "user",
				"subject_id":             "5",
				"dimension_key":          "api_calls",
				"period_key":             "lifetime",
				"value":                  5.0,
			},
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.ReportUsage(context.Background(), "", ReportUsageInput{
		SubjectType: "user",
		SubjectID:   "5",
		Items: []UsageReportItem{{
			DimensionKey: "api_calls",
			Value:        5,
			Mode:         UsageReportModeIncrement,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DimensionKey != "api_calls" || out[0].Value != 5 {
		t.Fatalf("out=%+v", out)
	}
}

func TestReportUsage_WithAPIKeyOverride(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/usage/report", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "sa_other" {
			t.Fatalf("key=%q", got)
		}
		writeEnvelope(w, http.StatusOK, []map[string]any{})
	})
	client, _ := newTestClient(t, mux)

	out, err := client.ReportUsage(context.Background(), "sa_other", ReportUsageInput{
		Items: []UsageReportItem{{DimensionKey: "api_calls", Value: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("out=%+v", out)
	}
}

func TestReportUsage_UnknownDimension(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/usage/report", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusBadRequest, "PLT-ASP-400", "unknown or non-quota/rate dimension key(s): nope")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.ReportUsage(context.Background(), "", ReportUsageInput{
		Items: []UsageReportItem{{DimensionKey: "nope", Value: 1}},
	}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestGetMyUsage(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/usage", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method; got != http.MethodGet {
			t.Fatalf("method=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-jwt" {
			t.Fatalf("auth=%q", got)
		}
		q := r.URL.Query()
		if q.Get("subject_type") != "user" || q.Get("subject_id") != "5" {
			t.Fatalf("query=%v", q)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"application_service": "memoo",
			"subject_type":        "user",
			"subject_id":          "5",
			"plan_code":           "pro",
			"plan_name":           "Pro",
			"meters": []map[string]any{
				{"dimension_key": "api_calls", "unit": "count", "used": 120.0, "limit": 1000.0, "remaining": 880.0, "unlimited": false, "reported": true},
				{"dimension_key": "storage_gb", "used": 0.0, "unlimited": true, "reported": false},
			},
		})
	})

	client, _ := newTestClient(t, mux)
	out, err := client.GetMyUsage(context.Background(), "user-jwt", "user", "5")
	if err != nil {
		t.Fatal(err)
	}
	if out.PlanCode != "pro" || len(out.Meters) != 2 {
		t.Fatalf("out=%+v", out)
	}
	if out.Meters[0].Limit == nil || *out.Meters[0].Limit != 1000 {
		t.Fatalf("meter0=%+v", out.Meters[0])
	}
	if !out.Meters[1].Unlimited || out.Meters[1].Reported {
		t.Fatalf("meter1=%+v", out.Meters[1])
	}
}

func TestGetMyUsage_NoAccessToken(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.GetMyUsage(context.Background(), "", "", ""); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestGetMyUsage_Forbidden(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/me/usage", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusForbidden, "PLT-ASP-403", "denied")
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.GetMyUsage(context.Background(), "user-jwt", "organization", "3"); !IsForbidden(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestReserveUsage(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/usage/reserve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q", r.Method)
		}
		requireAPIKey(t, r)
		var body ReserveUsageInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.DimensionKey != "namespace_count" || body.IdempotencyKey == "" || body.Delta != 1 {
			t.Fatalf("body=%+v", body)
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"reservation_id":  "11111111-1111-1111-1111-111111111111",
			"status":          UsageReservationStatusReserved,
			"subject_type":    "user",
			"subject_id":      "5",
			"dimension_key":   "namespace_count",
			"delta":           1.0,
			"period_key":      "lifetime",
			"idempotency_key": body.IdempotencyKey,
			"expires_at":      "2026-07-24T10:00:00Z",
		})
	})
	client, _ := newTestClient(t, mux)
	out, err := client.ReserveUsage(context.Background(), "", ReserveUsageInput{
		SubjectType: "user", SubjectID: "5", DimensionKey: "namespace_count",
		Delta: 1, IdempotencyKey: "memoo:ns:abc:namespace_count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReservationID == "" || out.Status != UsageReservationStatusReserved {
		t.Fatalf("out=%+v", out)
	}
}

func TestReserveUsage_Validation(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.ReserveUsage(context.Background(), "", ReserveUsageInput{}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestConfirmAndReleaseUsage(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/usage/confirm", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{
			"reservation_id": "rid-1", "status": UsageReservationStatusConfirmed,
			"dimension_key": "namespace_count", "delta": 1.0, "period_key": "lifetime",
			"subject_type": "user", "subject_id": "5", "idempotency_key": "k",
		})
	})
	mux.HandleFunc("/v1/consumer-auth/usage/release", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{
			"reservation_id": "rid-1", "status": UsageReservationStatusReleased,
			"dimension_key": "namespace_count", "delta": 1.0, "period_key": "lifetime",
			"subject_type": "user", "subject_id": "5", "idempotency_key": "k",
		})
	})
	client, _ := newTestClient(t, mux)
	conf, err := client.ConfirmUsage(context.Background(), "", ConfirmUsageInput{ReservationID: "rid-1"})
	if err != nil || conf.Status != UsageReservationStatusConfirmed {
		t.Fatalf("confirm=%+v err=%v", conf, err)
	}
	rel, err := client.ReleaseUsage(context.Background(), "", ReleaseUsageInput{ReservationID: "rid-1"})
	if err != nil || rel.Status != UsageReservationStatusReleased {
		t.Fatalf("release=%+v err=%v", rel, err)
	}
}

func TestConfirmUsage_MissingID(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, http.NewServeMux())
	if _, err := client.ConfirmUsage(context.Background(), "", ConfirmUsageInput{}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
	if _, err := client.ReleaseUsage(context.Background(), "", ReleaseUsageInput{}); !IsValidation(err) {
		t.Fatalf("err=%v", err)
	}
}
