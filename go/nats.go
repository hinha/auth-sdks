package authsdk

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hinha/auth-sdks/go/logging"
)

// DefaultEntitlementAuditSubject is the default JetStream subject Auth Service
// listens on (PLATFORM_ENTITLEMENT_AUDIT stream, subjects platform.entitlements.audit.v1.>).
const DefaultEntitlementAuditSubject = "platform.entitlements.audit.v1.raised"

// NATSConfig enables best-effort publishing of entitlement audit events to
// Auth Service's entitlement audit JetStream stream. Disabled by default.
//
// Publishing never fails or blocks the originating HTTP call: connect and
// publish errors are logged (when a Logger is configured) and swallowed.
type NATSConfig struct {
	// Enabled turns on the NATS producer. Default false (no-op).
	Enabled bool
	// URL is the NATS server URL, e.g. "nats://localhost:4222".
	URL string
	// Username / Password are optional NATS auth credentials.
	Username string
	Password string
	// Subject overrides the publish subject. Default DefaultEntitlementAuditSubject.
	Subject string
}

func (cfg NATSConfig) withDefaults() NATSConfig {
	out := cfg
	if out.Subject == "" {
		out.Subject = DefaultEntitlementAuditSubject
	}
	return out
}

// WithNATS enables the optional entitlement audit event producer. When
// cfg.Enabled is false (the zero value), the SDK behaves exactly as before —
// no connection is attempted and helpers/GetEntitlements never publish.
func WithNATS(cfg NATSConfig) Option {
	return func(o *options) { o.nats = cfg }
}

// entitlementAuditEvent mirrors auth-service's entitlementnats.RaisedEvent
// wire contract (PLATFORM_ENTITLEMENT_AUDIT JetStream payload).
type entitlementAuditEvent struct {
	EventID            string          `json:"event_id"`
	EventType          string          `json:"event_type"`
	ApplicationService string          `json:"application_service"`
	Source             string          `json:"source,omitempty"`
	SubjectType        string          `json:"subject_type,omitempty"`
	SubjectID          string          `json:"subject_id,omitempty"`
	PlanCode           string          `json:"plan_code,omitempty"`
	DimensionKey       string          `json:"dimension_key,omitempty"`
	Decision           string          `json:"decision"`
	OccurredAt         time.Time       `json:"occurred_at"`
	Payload            json.RawMessage `json:"payload,omitempty"`
}

// entitlementAuditProducer lazily connects to NATS/JetStream and publishes
// best-effort audit events. nil-safe: every method tolerates a nil receiver
// so call sites don't need to branch on whether NATS is enabled.
type entitlementAuditProducer struct {
	cfg NATSConfig
	log logging.Logger

	mu   sync.Mutex
	conn *gonats.Conn
	js   jetstream.JetStream
}

// newEntitlementAuditProducer returns nil when cfg.Enabled is false, so the
// Client can hold a *entitlementAuditProducer unconditionally.
func newEntitlementAuditProducer(cfg NATSConfig, log logging.Logger) *entitlementAuditProducer {
	if !cfg.Enabled {
		return nil
	}
	return &entitlementAuditProducer{cfg: cfg.withDefaults(), log: log}
}

func (p *entitlementAuditProducer) ensureConn(ctx context.Context) (jetstream.JetStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.js != nil && p.conn != nil && p.conn.IsConnected() {
		return p.js, nil
	}
	opts := []gonats.Option{
		gonats.Name("auth-sdk-go-entitlement-audit"),
		gonats.Timeout(5 * time.Second),
		gonats.ReconnectWait(2 * time.Second),
		gonats.MaxReconnects(-1),
	}
	if p.cfg.Username != "" {
		opts = append(opts, gonats.UserInfo(p.cfg.Username, p.cfg.Password))
	}
	conn, err := gonats.Connect(p.cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("entitlement audit: connect: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("entitlement audit: jetstream: %w", err)
	}
	if p.conn != nil {
		p.conn.Close()
	}
	p.conn = conn
	p.js = js
	return js, nil
}

// publish sends an audit event best-effort. It never returns an error to the
// caller; failures are logged (if a Logger was configured) and swallowed so
// the originating HTTP call is never affected by NATS availability.
func (p *entitlementAuditProducer) publish(ctx context.Context, ev entitlementAuditEvent) {
	if p == nil {
		return
	}
	if ev.EventID == "" {
		ev.EventID = newUUIDv4()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Decision == "" {
		ev.Decision = "info"
	}

	js, err := p.ensureConn(ctx)
	if err != nil {
		logging.Warn(ctx, p.log, "entitlement_audit_connect_failed", logging.Err(err))
		return
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		logging.Warn(ctx, p.log, "entitlement_audit_marshal_failed", logging.Err(err))
		return
	}
	msg := &gonats.Msg{
		Subject: p.cfg.Subject,
		Data:    raw,
		Header:  gonats.Header{"Nats-Msg-Id": []string{ev.EventID}},
	}
	if _, err := js.PublishMsg(ctx, msg); err != nil {
		logging.Warn(ctx, p.log, "entitlement_audit_publish_failed", logging.Err(err))
		return
	}
	logging.Debug(ctx, p.log, "entitlement_audit_published",
		logging.String("event_id", ev.EventID),
		logging.String("event_type", ev.EventType),
		logging.String("decision", ev.Decision),
	)
}

// close releases the underlying NATS connection, if any. Safe on a nil receiver.
func (p *entitlementAuditProducer) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
		p.js = nil
	}
}

// newUUIDv4 generates a random RFC 4122 version-4 UUID without adding a
// dependency on github.com/google/uuid.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failures are effectively unrecoverable on supported
		// platforms; fall back to a time-derived value rather than panic.
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
