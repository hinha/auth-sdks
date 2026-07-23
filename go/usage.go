package authsdk

import (
	"context"
	"net/http"
	"net/url"

	"github.com/hinha/auth-sdks/go/internal/api"
)

// Usage report write modes for ReportUsage items (must match Auth Service).
const (
	UsageReportModeSet       = "set"
	UsageReportModeIncrement = "increment"
)

// UsageReportItem is one dimension's usage value to report. Mode selects
// whether Value replaces the stored total ("set", default) or is added to it
// ("increment"). PeriodKey groups the counter (e.g. a billing month); empty
// defaults to "lifetime".
type UsageReportItem struct {
	DimensionKey string  `json:"dimension_key"`
	Value        float64 `json:"value"`
	Mode         string  `json:"mode,omitempty"`
	PeriodKey    string  `json:"period_key,omitempty"`
}

// ReportUsageInput reports one or more dimension usage values for a subject
// under the sa_* key's application service. An empty SubjectType/SubjectID
// reports against the service-wide "default" subject.
type ReportUsageInput struct {
	SubjectType string            `json:"subject_type,omitempty"`
	SubjectID   string            `json:"subject_id,omitempty"`
	Items       []UsageReportItem `json:"items"`
}

// UsageMeter is one reported usage-meter row (mirrors Auth Service
// models.ApplicationUsageMeter), returned by ReportUsage.
type UsageMeter struct {
	ID                   uint    `json:"id"`
	ApplicationServiceID uint    `json:"application_service_id"`
	SubjectType          string  `json:"subject_type"`
	SubjectID            string  `json:"subject_id"`
	DimensionKey         string  `json:"dimension_key"`
	PeriodKey            string  `json:"period_key"`
	Value                float64 `json:"value"`
}

// ReportUsage records usage for one or more quota|rate entitlement
// dimensions via POST /v1/consumer-auth/usage/report (sa_* machine key
// only). When apiKey is empty, the client Credentials key is used.
func (c *Client) ReportUsage(ctx context.Context, apiKey string, in ReportUsageInput) ([]UsageMeter, error) {
	if apiKey == "" {
		apiKey = c.cfg.APIKey
	}
	var out []UsageMeter
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/usage/report"), in, &out, api.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []UsageMeter{}
	}
	return out, nil
}

// UsageMeterItem is one dimension's used/limit/remaining view for a subject,
// combining reported usage with the resolved plan entitlement (mirrors Auth
// Service domains.UsageMeterItem).
type UsageMeterItem struct {
	DimensionKey string   `json:"dimension_key"`
	Unit         string   `json:"unit,omitempty"`
	Used         float64  `json:"used"`
	Limit        *float64 `json:"limit,omitempty"`
	Remaining    *float64 `json:"remaining,omitempty"`
	Unlimited    bool     `json:"unlimited"`
	// Reported is true when a usage meter row exists for this dimension
	// (Used may legitimately be 0 without a row when nothing was ever
	// reported).
	Reported bool `json:"reported"`
}

// MyUsageResult is the consumer self-service usage-vs-entitlement view
// returned by GetMyUsage (mirrors Auth Service domains.MyUsageResponse).
type MyUsageResult struct {
	ApplicationService string           `json:"application_service"`
	SubjectType        string           `json:"subject_type"`
	SubjectID          string           `json:"subject_id"`
	PlanCode           string           `json:"plan_code"`
	PlanName           string           `json:"plan_name"`
	Meters             []UsageMeterItem `json:"meters"`
}

// GetMyUsage resolves the caller's (or an administered organization's) usage
// meters against their resolved plan entitlements via
// GET /v1/consumer-auth/me/usage. subjectType/subjectID are optional: empty
// defaults to the caller (user); "organization" requires the caller to be an
// owner/admin of subjectID.
func (c *Client) GetMyUsage(ctx context.Context, accessToken, subjectType, subjectID string) (*MyUsageResult, error) {
	if accessToken == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "access token required",
		}}
	}
	q := url.Values{}
	if subjectType != "" {
		q.Set("subject_type", subjectType)
	}
	if subjectID != "" {
		q.Set("subject_id", subjectID)
	}
	path := c.path("/me/usage")
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out MyUsageResult
	err := c.api.DoJSON(ctx, http.MethodGet, path, nil, &out, api.WithBearer(accessToken))
	if err != nil {
		return nil, err
	}
	return &out, nil
}
