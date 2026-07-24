package authsdk

import (
	"context"
	"testing"
	"time"
)

func TestNewEntitlementAuditProducer_Disabled(t *testing.T) {
	t.Parallel()
	if p := newEntitlementAuditProducer(nil, nil); p != nil {
		t.Fatal("expected nil producer when bus is nil")
	}
	// None of these must panic when the producer is nil (NATS disabled).
	var p *entitlementAuditProducer
	p.publish(context.Background(), entitlementAuditEvent{EventType: "x"})
}

func TestNATSConfig_WithDefaults(t *testing.T) {
	t.Parallel()
	cfg := NATSConfig{Enabled: true, URL: "nats://localhost:4222"}.withDefaults()
	if cfg.Subject != DefaultEntitlementAuditSubject {
		t.Fatalf("subject=%q", cfg.Subject)
	}
	if cfg.PresenceSubject != DefaultPresenceSubject {
		t.Fatalf("presence_subject=%q", cfg.PresenceSubject)
	}
	custom := NATSConfig{Enabled: true, Subject: "custom.subject", PresenceSubject: "custom.presence"}.withDefaults()
	if custom.Subject != "custom.subject" || custom.PresenceSubject != "custom.presence" {
		t.Fatalf("custom=%+v", custom)
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

func TestNATSBus_DisabledPublish(t *testing.T) {
	t.Parallel()
	var b *natsBus
	if err := b.publish(context.Background(), "x", []byte("{}"), "id"); err == nil {
		t.Fatal("expected error on nil bus")
	}
	b = newNATSBus(NATSConfig{Enabled: false}, nil)
	if b != nil {
		t.Fatal("expected nil bus when disabled")
	}
}

func TestPresenceConfig_WithDefaults(t *testing.T) {
	t.Parallel()
	cfg := PresenceConfig{}.withDefaults()
	if cfg.Interval != 15*time.Second {
		t.Fatalf("interval=%v", cfg.Interval)
	}
	if cfg.InstanceID == "" {
		t.Fatal("instance_id should auto-generate")
	}
}

func TestStartPresence_NoopWhenNATSDisabled(t *testing.T) {
	t.Parallel()
	client, err := New("https://auth.example.com", "memoo", Credentials("sa_test_key"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.StartPresence(PresenceConfig{Interval: 50 * time.Millisecond, Port: 8080})
	time.Sleep(80 * time.Millisecond)
	client.StopPresence()
}

func TestPublishUnreachableNATS_DoesNotPanic(t *testing.T) {
	t.Parallel()
	// Unreachable NATS URL: publish must return quickly (bounded by connect
	// timeout) when the configured NATS URL is unreachable.
	bus := newNATSBus(NATSConfig{
		Enabled: true,
		URL:     "nats://127.0.0.1:1",
	}, nil)
	p := newEntitlementAuditProducer(bus, nil)
	done := make(chan struct{})
	go func() {
		p.publish(context.Background(), entitlementAuditEvent{
			EventID:            "e1",
			EventType:          "test",
			ApplicationService: "memoo",
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("publish hung on unreachable NATS")
	}
	bus.close()
}
