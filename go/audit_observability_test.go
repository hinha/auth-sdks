package authsdk

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/hinha/auth-sdks/go/logging"
)

type captureLog struct {
	mu   sync.Mutex
	recs []captured
}

type captured struct {
	level  logging.Level
	msg    string
	fields map[string]any
}

func (c *captureLog) Log(_ context.Context, level logging.Level, msg string, fields ...logging.Field) {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, captured{level: level, msg: msg, fields: m})
}

func (c *captureLog) With(fields ...logging.Field) logging.Logger { return c }

func (c *captureLog) audits() []captured {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []captured
	for _, r := range c.recs {
		if r.msg == logging.AuditMessage {
			out = append(out, r)
		}
	}
	return out
}

func TestLogin_EmitsClosedAuditWithoutSecrets(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/login", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{
			"access_token":  "access-secret",
			"refresh_token": "refresh-secret",
			"token_type":    "Bearer",
			"expires_in":    900,
			"session_id":    "sess-1",
		})
	})
	cap := &captureLog{}
	client, _ := newTestClient(t, mux, WithLogger(cap), WithRetryCount(0))
	_, err := client.Login(context.Background(), LoginInput{Email: "andi@acme.com", Password: "super-secret"})
	if err != nil {
		t.Fatal(err)
	}
	audits := cap.audits()
	if len(audits) == 0 {
		t.Fatal("expected audit event")
	}
	last := audits[len(audits)-1]
	if last.fields[logging.FieldEventType] != logging.EventAuthLogin {
		t.Fatalf("%v", last.fields)
	}
	if last.fields[logging.FieldKind] != logging.KindAudit {
		t.Fatalf("kind=%v", last.fields[logging.FieldKind])
	}
	if _, ok := last.fields["password"]; ok {
		t.Fatal("password must not appear")
	}
	if _, ok := last.fields["access_token"]; ok {
		t.Fatal("access_token must not appear")
	}
}

func TestLogin_FailedEmitsAudit(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/login", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusUnauthorized, "PLT-ASP-401", "invalid credentials")
	})
	cap := &captureLog{}
	client, _ := newTestClient(t, mux, WithLogger(cap), WithRetryCount(0))
	_, err := client.Login(context.Background(), LoginInput{Email: "a@b.c", Password: "x"})
	if !IsUnauthorized(err) {
		t.Fatalf("err=%v", err)
	}
	found := false
	for _, a := range cap.audits() {
		if a.fields[logging.FieldEventType] == logging.EventAuthLoginFailed &&
			a.fields[logging.FieldDecision] == logging.DecisionError {
			found = true
		}
	}
	if !found {
		t.Fatalf("audits=%v", cap.audits())
	}
}

func TestAllow_EmitsAllowAndDenyAudit(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/authorize-action", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{
			"allowed": false,
			"reason":  "permission_denied",
		})
	})
	cap := &captureLog{}
	client, _ := newTestClient(t, mux, WithLogger(cap), WithRetryCount(0))
	ok, err := client.Allow(context.Background(), "jwt", "reports:export")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	found := false
	for _, a := range cap.audits() {
		if a.fields[logging.FieldEventType] == logging.EventAuthAuthorizeAction &&
			a.fields[logging.FieldDecision] == logging.DecisionDeny {
			found = true
		}
	}
	if !found {
		t.Fatalf("audits=%v", cap.audits())
	}
}

func TestVerifyAPIKey_RedactsActorID(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/verify", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{"plan": "pro"})
	})
	cap := &captureLog{}
	client, _ := newTestClient(t, mux, WithLogger(cap), WithRetryCount(0))
	if _, err := client.VerifyAPIKey(context.Background(), "sa_super_secret_machine_key"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range cap.audits() {
		if a.fields[logging.FieldEventType] != logging.EventAuthAPIKeyVerify {
			continue
		}
		id, _ := a.fields[logging.FieldActorID].(string)
		if id == "sa_super_secret_machine_key" {
			t.Fatal("full key leaked")
		}
		if id == "" {
			t.Fatal("expected redacted actor_id")
		}
		found = true
	}
	if !found {
		t.Fatalf("audits=%v", cap.audits())
	}
}

func TestEvaluateFeature_EmitsAuditWithoutNATS(t *testing.T) {
	t.Parallel()
	cap := &captureLog{}
	client, _ := newTestClient(t, http.NewServeMux(), WithLogger(cap), WithRetryCount(0))
	ent := &EntitlementsResult{
		Plan: "pro",
		Features: map[string]any{
			"reports.export.enabled": true,
		},
	}
	if !client.EvaluateFeature(context.Background(), ent, "reports.export.enabled") {
		t.Fatal("expected enabled")
	}
	found := false
	for _, a := range cap.audits() {
		if a.fields[logging.FieldEventType] == logging.EventEntitlementFeature &&
			a.fields[logging.FieldDecision] == logging.DecisionAllow {
			found = true
		}
	}
	if !found {
		t.Fatalf("audits=%v", cap.audits())
	}
}
