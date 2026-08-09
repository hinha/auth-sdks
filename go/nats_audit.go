package authsdk

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hinha/auth-sdks/go/logging"
)

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

// entitlementAuditProducer publishes best-effort audit events via the shared
// natsBus. nil-safe: every method tolerates a nil receiver.
type entitlementAuditProducer struct {
	bus     *natsBus
	subject string
	log     logging.Logger
}

func newEntitlementAuditProducer(bus *natsBus, log logging.Logger) *entitlementAuditProducer {
	if bus == nil {
		return nil
	}
	return &entitlementAuditProducer{bus: bus, subject: bus.cfg.Subject, log: log}
}

// publish sends an audit event best-effort. It never returns an error to the
// caller; failures are logged and swallowed so HTTP calls never depend on NATS.
func (p *entitlementAuditProducer) publish(ctx context.Context, ev entitlementAuditEvent) {
	if p == nil || p.bus == nil {
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

	raw, err := json.Marshal(ev)
	if err != nil {
		logging.Warn(ctx, p.log, "entitlement_audit_marshal_failed", logging.Err(err))
		return
	}
	if err := p.bus.publish(ctx, p.subject, raw, ev.EventID); err != nil {
		logging.Warn(ctx, p.log, "entitlement_audit_publish_failed", logging.Err(err))
		return
	}
	logging.Debug(ctx, p.log, "entitlement_audit_published",
		logging.String("event_id", ev.EventID),
		logging.String("event_type", ev.EventType),
		logging.String("decision", ev.Decision),
	)
}
