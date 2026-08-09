package authsdk

import (
	"context"
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

// DefaultPresenceSubject is the default JetStream subject for service heartbeats
// (PLATFORM_SERVICE_PRESENCE stream, subjects platform.services.presence.v1.>).
const DefaultPresenceSubject = "platform.services.presence.v1.heartbeat"

// NATSConfig enables best-effort JetStream publishing (entitlement audit +
// optional service presence). Disabled by default.
//
// Publishing never fails or blocks the originating HTTP call: connect and
// publish errors are logged (when a Logger is configured) and swallowed.
type NATSConfig struct {
	// Enabled turns on the shared NATS connection. Default false (no-op).
	Enabled bool
	// URL is the NATS server URL, e.g. "nats://localhost:4222".
	URL string
	// Username / Password are optional NATS auth credentials.
	Username string
	Password string
	// Subject overrides the entitlement-audit publish subject.
	// Default DefaultEntitlementAuditSubject.
	Subject string
	// PresenceSubject overrides the presence heartbeat subject.
	// Default DefaultPresenceSubject.
	PresenceSubject string
}

func (cfg NATSConfig) withDefaults() NATSConfig {
	out := cfg
	if out.Subject == "" {
		out.Subject = DefaultEntitlementAuditSubject
	}
	if out.PresenceSubject == "" {
		out.PresenceSubject = DefaultPresenceSubject
	}
	return out
}

// WithNATS enables the optional shared NATS producer (entitlement audit +
// presence). When cfg.Enabled is false, no connection is attempted.
func WithNATS(cfg NATSConfig) Option {
	return func(o *options) { o.nats = cfg }
}

// natsBus owns a single dial shared by entitlement audit and presence publishers.
type natsBus struct {
	cfg NATSConfig
	log logging.Logger

	mu   sync.Mutex
	conn *gonats.Conn
	js   jetstream.JetStream
}

func newNATSBus(cfg NATSConfig, log logging.Logger) *natsBus {
	if !cfg.Enabled {
		return nil
	}
	return &natsBus{cfg: cfg.withDefaults(), log: log}
}

func (b *natsBus) jetStream(ctx context.Context) (jetstream.JetStream, error) {
	if b == nil {
		return nil, fmt.Errorf("nats bus disabled")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.js != nil && b.conn != nil && b.conn.IsConnected() {
		return b.js, nil
	}
	opts := []gonats.Option{
		gonats.Name("auth-sdk-go"),
		gonats.Timeout(5 * time.Second),
		gonats.ReconnectWait(2 * time.Second),
		gonats.MaxReconnects(-1),
	}
	if b.cfg.Username != "" {
		opts = append(opts, gonats.UserInfo(b.cfg.Username, b.cfg.Password))
	}
	conn, err := gonats.Connect(b.cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats jetstream: %w", err)
	}
	if b.conn != nil {
		b.conn.Close()
	}
	b.conn = conn
	b.js = js
	return js, nil
}

func (b *natsBus) publish(ctx context.Context, subject string, data []byte, msgID string) error {
	js, err := b.jetStream(ctx)
	if err != nil {
		return err
	}
	msg := &gonats.Msg{
		Subject: subject,
		Data:    data,
		Header:  gonats.Header{"Nats-Msg-Id": []string{msgID}},
	}
	_, err = js.PublishMsg(ctx, msg)
	return err
}

func (b *natsBus) close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
		b.js = nil
	}
}
