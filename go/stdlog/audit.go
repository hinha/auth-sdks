package stdlog

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Loki stream kind values (low cardinality).
const (
	KindAudit  = "audit"
	KindAccess = "access"
	KindApp    = "app"
	KindSDK    = "sdk"

	FieldKind         = "kind"
	FieldEventID      = "event_id"
	FieldEventType    = "event_type"
	FieldDecision     = "decision"
	FieldSource       = "source"
	FieldActorType    = "actor_type"
	FieldActorID      = "actor_id"
	FieldSubjectType  = "subject_type"
	FieldSubjectID    = "subject_id"
	FieldAction       = "action"
	FieldResource     = "resource"
	FieldPlanCode     = "plan_code"
	FieldDimensionKey = "dimension_key"
	FieldOccurredAt   = "occurred_at"
	FieldEnv          = "env"
	FieldLevel        = "level"

	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionInfo  = "info"
	DecisionError = "error"

	ActorUser      = "user"
	ActorMachine   = "machine"
	ActorAnonymous = "anonymous"

	EventAuthLogin             = "auth.login"
	EventAuthLoginFailed       = "auth.login_failed"
	EventAuthRefresh           = "auth.refresh"
	EventAuthLogout            = "auth.logout"
	EventAuthAuthorizeAction   = "auth.authorize_action"
	EventAuthAuthorizeEndpoint = "auth.authorize_endpoint"
	EventAuthAPIKeyVerify      = "auth.api_key_verify"
	EventEntitlementFetched    = "entitlement.fetched"
	EventEntitlementFeature    = "entitlement.feature_checked"
	EventEntitlementQuota      = "entitlement.quota_checked"

	SourceAuthSDKs = "auth-sdks"

	// AuditLogMessage is the fixed line message for audit events.
	AuditLogMessage = "audit event"
)

// AuditEvent is the closed observability-audit payload (Gigapipe Loki).
type AuditEvent struct {
	EventID       string
	EventType     string
	Kind          string
	Service       string
	Source        string
	ActorType     string
	ActorID       string
	SubjectType   string
	SubjectID     string
	Action        string
	Resource      string
	Decision      string
	RequestID     string
	Status        int
	Error         string
	PlanCode      string
	DimensionKey  string
	OccurredAt    time.Time
}

// AuditLogAllowedKeys is the closed set of keys an audit event may emit.
var AuditLogAllowedKeys = map[string]struct{}{
	FieldKind:         {},
	FieldEventID:      {},
	FieldEventType:    {},
	FieldDecision:     {},
	FieldSource:       {},
	FieldActorType:    {},
	FieldActorID:      {},
	FieldSubjectType:  {},
	FieldSubjectID:    {},
	FieldAction:       {},
	FieldResource:     {},
	FieldPlanCode:     {},
	FieldDimensionKey: {},
	FieldOccurredAt:   {},
	FieldRequestID:    {},
	FieldStatus:       {},
	FieldError:        {},
	FieldService:      {},
}

// IsAuditLogKey reports whether key is allowed on an audit event.
func IsAuditLogKey(key string) bool {
	_, ok := AuditLogAllowedKeys[key]
	return ok
}

// FilterAuditLogFields keeps only canonical audit keys.
func FilterAuditLogFields(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if IsAuditLogKey(k) {
			out[k] = v
		}
	}
	return out
}

// RedactActorID shortens sa_* keys for audit lines.
func RedactActorID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if !strings.HasPrefix(id, "sa_") {
		return id
	}
	keep := 8
	if len(id) <= keep {
		if len(id) <= 4 {
			return "sa_…"
		}
		return id[:4] + "…"
	}
	return id[:keep] + "…"
}

// Fields returns only canonical audit fields.
func (e AuditEvent) Fields() []Field {
	kind := e.Kind
	if kind == "" {
		kind = KindAudit
	}
	source := e.Source
	if source == "" {
		source = SourceAuthSDKs
	}
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	eventID := strings.TrimSpace(e.EventID)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	raw := map[string]any{
		FieldKind:       kind,
		FieldEventID:    eventID,
		FieldEventType:  e.EventType,
		FieldSource:     source,
		FieldOccurredAt: occurred.UTC().Format(time.RFC3339Nano),
	}
	if e.Service != "" {
		raw[FieldService] = e.Service
	}
	if e.ActorType != "" {
		raw[FieldActorType] = e.ActorType
	}
	if e.ActorID != "" {
		raw[FieldActorID] = RedactActorID(e.ActorID)
	}
	if e.SubjectType != "" {
		raw[FieldSubjectType] = e.SubjectType
	}
	if e.SubjectID != "" {
		raw[FieldSubjectID] = e.SubjectID
	}
	if e.Action != "" {
		raw[FieldAction] = e.Action
	}
	if e.Resource != "" {
		raw[FieldResource] = e.Resource
	}
	if e.Decision != "" {
		raw[FieldDecision] = e.Decision
	}
	if e.RequestID != "" {
		raw[FieldRequestID] = e.RequestID
	}
	if e.Status != 0 {
		raw[FieldStatus] = e.Status
	}
	if e.Error != "" {
		raw[FieldError] = e.Error
	}
	if e.PlanCode != "" {
		raw[FieldPlanCode] = e.PlanCode
	}
	if e.DimensionKey != "" {
		raw[FieldDimensionKey] = e.DimensionKey
	}
	filtered := FilterAuditLogFields(raw)
	out := make([]Field, 0, len(filtered))
	for k, v := range filtered {
		out = append(out, Field{Key: k, Value: v})
	}
	return out
}

// LogAudit writes a closed-schema audit event.
func LogAudit(l Logger, e AuditEvent) {
	if l == nil {
		return
	}
	fields := e.Fields()
	switch e.Decision {
	case DecisionDeny:
		l.Warn(AuditLogMessage, fields...)
	case DecisionError:
		l.Error(AuditLogMessage, fields...)
	default:
		l.Info(AuditLogMessage, fields...)
	}
}
