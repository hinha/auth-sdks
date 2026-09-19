package logging

import (
	"context"
	"strings"
)

// Loki / Gigapipe audit contract. Message and kind are fixed so LogQL
// does not depend on free-form text.
const (
	AuditMessage = "audit event"

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
	FieldStatus       = "status"
	FieldRequestID    = "request_id"
	FieldService      = "service"

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
)

// Audit emits a closed-schema observability audit event at info level.
// The log line message is always AuditMessage and kind is always KindAudit.
func Audit(ctx context.Context, l Logger, fields ...Field) {
	AuditAt(ctx, l, LevelInfo, fields...)
}

// AuditAt is Audit with an explicit severity (warn for deny, error for failures).
func AuditAt(ctx context.Context, l Logger, level Level, fields ...Field) {
	if l == nil {
		return
	}
	out := make([]Field, 0, len(fields)+1)
	sawKind := false
	for _, f := range fields {
		if f.Key == FieldKind {
			sawKind = true
			out = append(out, String(FieldKind, KindAudit))
			continue
		}
		out = append(out, f)
	}
	if !sawKind {
		out = append([]Field{String(FieldKind, KindAudit)}, out...)
	}
	l.Log(ctx, level, AuditMessage, out...)
}

// RedactMachineID shortens sa_* credentials for audit lines. Non-machine
// identifiers are returned unchanged. Never returns the full API key.
func RedactMachineID(id string) string {
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
