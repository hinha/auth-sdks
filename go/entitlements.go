package authsdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/hinha/auth-sdks/go/internal/api"
)

// EntitlementsInput narrows GetEntitlements to a subject. ApplicationService
// is always taken from the bound Client (matches AuthorizeEndpoint).
type EntitlementsInput struct {
	SubjectType string // default | api_key | user | organization
	SubjectID   string
}

// EntitlementItem is one structured entitlement dimension resolved for a
// plan/subject (matches Auth Service domains.EntitlementItem).
type EntitlementItem struct {
	Key       string `json:"key"`
	Type      string `json:"type"` // boolean | quota | rate | enum
	Unit      string `json:"unit,omitempty"`
	Unlimited bool   `json:"unlimited"`
	Value     any    `json:"value,omitempty"`
}

// EntitlementsResult is returned by GetEntitlements.
type EntitlementsResult struct {
	ApplicationService string            `json:"application_service"`
	Plan               string            `json:"plan"`
	PlanName           string            `json:"plan_name"`
	Source             string            `json:"source"`
	SubjectType        string            `json:"subject_type,omitempty"`
	SubjectID          string            `json:"subject_id,omitempty"`
	Limits             map[string]any    `json:"limits"`
	Features           map[string]any    `json:"features"`
	Entitlements       []EntitlementItem `json:"entitlements,omitempty"`
}

// GetEntitlements resolves the effective plan limits/features for a subject
// via GET /v1/consumer-auth/entitlements. When apiKey is empty, the client
// Credentials key is used (same convention as AuthorizeEndpoint/VerifyAPIKey).
//
// When WithNATS is enabled, a best-effort "entitlement.fetched" audit event
// is published after a successful fetch; publish failures are logged and
// never surfaced to the caller.
func (c *Client) GetEntitlements(ctx context.Context, apiKey string, in EntitlementsInput) (*EntitlementsResult, error) {
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	q := url.Values{}
	q.Set("application_service", c.cfg.ApplicationService)
	if in.SubjectType != "" {
		q.Set("subject_type", in.SubjectType)
	}
	if in.SubjectID != "" {
		q.Set("subject_id", in.SubjectID)
	}

	var out EntitlementsResult
	err := c.api.DoJSON(ctx, http.MethodGet, c.path("/entitlements")+"?"+q.Encode(), nil, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}

	c.audit.publish(ctx, entitlementAuditEvent{
		EventType:          "entitlement.fetched",
		ApplicationService: c.cfg.ApplicationService,
		Source:             out.Source,
		SubjectType:        firstNonEmptyStr(out.SubjectType, in.SubjectType),
		SubjectID:          firstNonEmptyStr(out.SubjectID, in.SubjectID),
		PlanCode:           out.Plan,
		Decision:           "info",
	})
	return &out, nil
}

// PlanDimensionInput upserts one packing dimension for SyncPlans (matches
// Auth Service domains.EntitlementDimensionCreateRequest).
type PlanDimensionInput struct {
	Key             string   `json:"key"`
	Name            string   `json:"name,omitempty"`
	Description     string   `json:"description,omitempty"`
	ValueType       string   `json:"value_type"` // boolean | quota | rate | enum
	Unit            string   `json:"unit,omitempty"`
	AllowsUnlimited bool     `json:"allows_unlimited,omitempty"`
	EnumOptions     []string `json:"enum_options,omitempty"`
	EnforcementHint string   `json:"enforcement_hint,omitempty"` // hard (default) | soft | informational
	DisplayGroup    string   `json:"display_group,omitempty"`
	SortOrder       int      `json:"sort_order,omitempty"`
	Status          string   `json:"status,omitempty"` // active (default)
}

// PlanEntitlementValueInput sets one dimension value on a plan, addressed by
// DimensionID or DimensionKey (matches domains.PlanEntitlementValueInput).
type PlanEntitlementValueInput struct {
	DimensionID  *uint    `json:"dimension_id,omitempty"`
	DimensionKey string   `json:"dimension_key,omitempty"`
	Unlimited    bool     `json:"unlimited,omitempty"`
	ValueBool    *bool    `json:"value_bool,omitempty"`
	ValueNumber  *float64 `json:"value_number,omitempty"`
	ValueText    *string  `json:"value_text,omitempty"`
}

// PlanSyncInput upserts one plan (by code) and its entitlement values.
type PlanSyncInput struct {
	Code              string                      `json:"code"`
	Name              string                      `json:"name"`
	Status            string                      `json:"status,omitempty"` // active (default)
	EntitlementValues []PlanEntitlementValueInput `json:"entitlement_values,omitempty"`
}

// PlansSyncResult is returned by SyncPlans.
type PlansSyncResult struct {
	DimensionsUpserted int `json:"dimensions_upserted"`
	PlansUpserted      int `json:"plans_upserted"`
}

type plansSyncRequest struct {
	ApplicationService string               `json:"application_service"`
	Dimensions         []PlanDimensionInput `json:"dimensions,omitempty"`
	Plans              []PlanSyncInput      `json:"plans,omitempty"`
}

// SyncPlansOption configures SyncPlans.
type SyncPlansOption func(*syncPlansConfig)

type syncPlansConfig struct {
	apiKey string
}

// WithSyncPlansAPIKey overrides the client Credentials key for this call.
func WithSyncPlansAPIKey(apiKey string) SyncPlansOption {
	return func(c *syncPlansConfig) { c.apiKey = apiKey }
}

// SyncPlans bootstraps/upserts entitlement dimensions and plans for the
// client's bound ApplicationService via POST /v1/consumer-auth/plans/sync
// (sa_* key). Mirrors the ImportEndpoints call style.
func (c *Client) SyncPlans(ctx context.Context, dimensions []PlanDimensionInput, plans []PlanSyncInput, opts ...SyncPlansOption) (*PlansSyncResult, error) {
	cfg := syncPlansConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	apiKey := cfg.apiKey
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	payload := plansSyncRequest{
		ApplicationService: c.cfg.ApplicationService,
		Dimensions:         dimensions,
		Plans:              plans,
	}
	var out PlansSyncResult
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/plans/sync"), payload, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// IsFeatureEnabled reports whether a boolean feature dimension is enabled.
// It checks Entitlements first (type=boolean), falling back to the raw
// Features map. No usage/state is stored — this is a pure read of the
// already-fetched EntitlementsResult.
func (e *EntitlementsResult) IsFeatureEnabled(key string) bool {
	if e == nil {
		return false
	}
	for _, item := range e.Entitlements {
		if item.Key != key {
			continue
		}
		return truthy(item.Value)
	}
	if e.Features != nil {
		if v, ok := e.Features[key]; ok {
			return truthy(v)
		}
	}
	return false
}

// CheckQuota evaluates a quota/rate dimension against current usage supplied
// by the caller — the SDK never stores or tracks usage itself.
//
// allowed is true when the dimension is unlimited, unknown (fail-open), or
// current is strictly below the configured limit. limit is nil whenever
// unlimited is true or the limit could not be determined.
func (e *EntitlementsResult) CheckQuota(key string, current float64) (allowed bool, limit *float64, unlimited bool) {
	if e == nil {
		return true, nil, true
	}
	for _, item := range e.Entitlements {
		if item.Key != key {
			continue
		}
		if item.Unlimited {
			return true, nil, true
		}
		if v, ok := numericValue(item.Value); ok {
			return current < v, &v, false
		}
		// Dimension present but value shape is unrecognized: fail open.
		return true, nil, true
	}
	if e.Limits != nil {
		if raw, ok := e.Limits[key]; ok {
			if raw == nil {
				return true, nil, true // null == unlimited
			}
			if v, ok := numericValue(raw); ok {
				return current < v, &v, false
			}
		}
	}
	// Dimension not found on this plan: fail open rather than silently
	// blocking callers for a key the plan simply doesn't define.
	return true, nil, true
}

func truthy(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	case float64:
		return b != 0
	case json.Number:
		f, _ := b.Float64()
		return f != 0
	}
	return false
}

func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// EvaluateFeature checks IsFeatureEnabled and, when WithNATS is enabled,
// publishes a best-effort "entitlement.feature_checked" audit event
// (decision=allow/deny). The feature check itself never fails or blocks on
// NATS availability.
func (c *Client) EvaluateFeature(ctx context.Context, ent *EntitlementsResult, key string) bool {
	enabled := ent.IsFeatureEnabled(key)
	decision := "deny"
	if enabled {
		decision = "allow"
	}
	c.publishDecision(ctx, ent, "entitlement.feature_checked", key, decision, map[string]any{"enabled": enabled})
	return enabled
}

// EvaluateQuota checks CheckQuota and, when WithNATS is enabled, publishes a
// best-effort "entitlement.quota_checked" audit event (decision=allow/deny).
// The quota check itself never fails or blocks on NATS availability.
func (c *Client) EvaluateQuota(ctx context.Context, ent *EntitlementsResult, key string, current float64) (allowed bool, limit *float64, unlimited bool) {
	allowed, limit, unlimited = ent.CheckQuota(key, current)
	decision := "deny"
	if allowed {
		decision = "allow"
	}
	payload := map[string]any{"current": current, "unlimited": unlimited}
	if limit != nil {
		payload["limit"] = *limit
	}
	c.publishDecision(ctx, ent, "entitlement.quota_checked", key, decision, payload)
	return allowed, limit, unlimited
}

func (c *Client) publishDecision(ctx context.Context, ent *EntitlementsResult, eventType, dimensionKey, decision string, payload map[string]any) {
	if c == nil || c.audit == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	ev := entitlementAuditEvent{
		EventType:          eventType,
		ApplicationService: c.cfg.ApplicationService,
		DimensionKey:       dimensionKey,
		Decision:           decision,
		Payload:            raw,
	}
	if ent != nil {
		ev.Source = ent.Source
		ev.SubjectType = ent.SubjectType
		ev.SubjectID = ent.SubjectID
		ev.PlanCode = ent.Plan
	}
	c.audit.publish(ctx, ev)
}
