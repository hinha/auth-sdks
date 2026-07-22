package authsdk

import (
	"context"
	"regexp"
	"testing"
	"time"
)

func TestNewEntitlementAuditProducer_DisabledIsNil(t *testing.T) {
	t.Parallel()
	if p := newEntitlementAuditProducer(NATSConfig{Enabled: false}, nil); p != nil {
		t.Fatalf("expected nil producer when disabled, got %+v", p)
	}
}

func TestEntitlementAuditProducer_NilSafe(t *testing.T) {
	t.Parallel()
	var p *entitlementAuditProducer
	// None of these must panic when the producer is nil (NATS disabled).
	p.publish(context.Background(), entitlementAuditEvent{EventType: "x"})
	p.close()
}

func TestNATSConfig_WithDefaults(t *testing.T) {
	t.Parallel()
	cfg := NATSConfig{Enabled: true, URL: "nats://localhost:4222"}.withDefaults()
	if cfg.Subject != DefaultEntitlementAuditSubject {
		t.Fatalf("subject=%q", cfg.Subject)
	}
	custom := NATSConfig{Enabled: true, Subject: "custom.subject"}.withDefaults()
	if custom.Subject != "custom.subject" {
		t.Fatalf("subject=%q", custom.Subject)
	}
}

func TestWithNATS_SetsOption(t *testing.T) {
	t.Parallel()
	o := &options{}
	WithNATS(NATSConfig{Enabled: true, URL: "nats://localhost:4222"})(o)
	if !o.nats.Enabled || o.nats.URL != "nats://localhost:4222" {
		t.Fatalf("nats=%+v", o.nats)
	}
}

func TestNewUUIDv4_Format(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := newUUIDv4()
		if !re.MatchString(id) {
			t.Fatalf("uuid %q does not match v4 format", id)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid generated: %s", id)
		}
		seen[id] = true
	}
}

// TestEntitlementAuditProducer_PublishFailsClosedOnUnreachableBroker verifies
// that a connect failure is swallowed (never panics, never blocks the caller
// beyond the connect timeout) when the configured NATS URL is unreachable.
func TestEntitlementAuditProducer_PublishFailsClosedOnUnreachableBroker(t *testing.T) {
	t.Parallel()
	p := newEntitlementAuditProducer(NATSConfig{
		Enabled: true,
		URL:     "nats://127.0.0.1:1", // nothing listens here
		Subject: "test.subject",
	}, nil)
	if p == nil {
		t.Fatal("expected non-nil producer when enabled")
	}
	done := make(chan struct{})
	go func() {
		p.publish(context.Background(), entitlementAuditEvent{
			EventType:          "entitlement.fetched",
			ApplicationService: "memoo",
			Decision:           "info",
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("publish should not block indefinitely on an unreachable broker")
	}
}
