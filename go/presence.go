package authsdk

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hinha/auth-sdks/go/logging"
)

// PresenceConfig configures the background service-presence heartbeat publisher.
type PresenceConfig struct {
	// Interval between heartbeats. Default 15s.
	Interval time.Duration
	// InstanceID uniquely identifies this process. Auto-generated when empty.
	InstanceID string
	// Host is the reported hostname (defaults to os.Hostname).
	Host string
	// IP is an optional advertised address.
	IP string
	// Port is the listen port of the application (0 = omit).
	Port int
	// Version is an optional build/version string.
	Version string
	// Meta is optional free-form labels.
	Meta map[string]string
}

func (c PresenceConfig) withDefaults() PresenceConfig {
	out := c
	if out.Interval <= 0 {
		out.Interval = 15 * time.Second
	}
	if out.InstanceID == "" {
		out.InstanceID = newUUIDv4()
	}
	if out.Host == "" {
		if h, err := os.Hostname(); err == nil {
			out.Host = h
		}
	}
	return out
}

// presenceHeartbeat is the JetStream payload consumed by auth-service
// PLATFORM_SERVICE_PRESENCE.
type presenceHeartbeat struct {
	EventID            string            `json:"event_id"`
	ApplicationService string            `json:"application_service"`
	InstanceID         string            `json:"instance_id"`
	Host               string            `json:"host,omitempty"`
	IP                 string            `json:"ip,omitempty"`
	Port               int               `json:"port,omitempty"`
	Version            string            `json:"version,omitempty"`
	StartedAt          time.Time         `json:"started_at,omitempty"`
	OccurredAt         time.Time         `json:"occurred_at"`
	Meta               map[string]string `json:"meta,omitempty"`
}

type presencePublisher struct {
	bus     *natsBus
	subject string
	app     string
	cfg     PresenceConfig
	log     logging.Logger
	started time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newPresencePublisher(bus *natsBus, applicationService string, log logging.Logger) *presencePublisher {
	if bus == nil {
		return nil
	}
	return &presencePublisher{
		bus:     bus,
		subject: bus.cfg.PresenceSubject,
		app:     applicationService,
		log:     log,
		started: time.Now().UTC(),
	}
}

// StartPresence begins a background ticker that publishes heartbeats over the
// shared NATS connection. Best-effort: failures are logged and ignored.
// No-op when WithNATS is disabled. Calling StartPresence again replaces the
// previous loop.
func (c *Client) StartPresence(cfg PresenceConfig) {
	if c == nil || c.presence == nil {
		return
	}
	c.presence.start(cfg)
}

// StopPresence stops the background heartbeat loop (if any).
func (c *Client) StopPresence() {
	if c == nil || c.presence == nil {
		return
	}
	c.presence.stop()
}

func (p *presencePublisher) start(cfg PresenceConfig) {
	if p == nil {
		return
	}
	cfg = cfg.withDefaults()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.cfg = cfg
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.loop(ctx)
}

func (p *presencePublisher) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *presencePublisher) stopLocked() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.wg.Wait()
}

func (p *presencePublisher) loop(ctx context.Context) {
	defer p.wg.Done()
	p.publishOnce(ctx)
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publishOnce(ctx)
		}
	}
}

func (p *presencePublisher) publishOnce(ctx context.Context) {
	if p == nil || p.bus == nil {
		return
	}
	ev := presenceHeartbeat{
		EventID:            newUUIDv4(),
		ApplicationService: p.app,
		InstanceID:         p.cfg.InstanceID,
		Host:               p.cfg.Host,
		IP:                 p.cfg.IP,
		Port:               p.cfg.Port,
		Version:            p.cfg.Version,
		StartedAt:          p.started,
		OccurredAt:         time.Now().UTC(),
		Meta:               p.cfg.Meta,
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		logging.Warn(ctx, p.log, "presence_marshal_failed", logging.Err(err))
		return
	}
	if err := p.bus.publish(ctx, p.subject, raw, ev.EventID); err != nil {
		logging.Warn(ctx, p.log, "presence_publish_failed", logging.Err(err))
		return
	}
	logging.Debug(ctx, p.log, "presence_published",
		logging.String("instance_id", ev.InstanceID),
		logging.String("application_service", ev.ApplicationService),
	)
}

// newUUIDv4 generates a random RFC 4122 version-4 UUID without adding a
// dependency on github.com/google/uuid.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
