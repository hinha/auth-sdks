package authsdk

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
)

// CreateOrganizationInput creates an organization owned by the authenticated
// consumer caller.
type CreateOrganizationInput struct {
	Name string
	Slug string
	// SetMembershipOrganization also attaches the caller's application
	// membership to the newly created organization.
	SetMembershipOrganization bool
}

// Organization mirrors Auth Service models.Organization returned by
// CreateOrganization.
type Organization struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type createOrganizationRequest struct {
	Name                      string `json:"name"`
	Slug                      string `json:"slug"`
	SetMembershipOrganization bool   `json:"set_membership_organization,omitempty"`
}

// CreateOrganization creates an organization for the caller (owner
// membership + free plan assignment when available) via
// POST /v1/consumer-auth/organizations.
func (c *Client) CreateOrganization(ctx context.Context, accessToken string, in CreateOrganizationInput) (*Organization, error) {
	if accessToken == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "access token required",
		}}
	}
	var out Organization
	err := c.api.DoJSON(ctx, http.MethodPost, c.path("/organizations"), createOrganizationRequest{
		Name:                      in.Name,
		Slug:                      in.Slug,
		SetMembershipOrganization: in.SetMembershipOrganization,
	}, &out, api.WithBearer(accessToken))
	if err != nil {
		logging.Warn(ctx, c.log, "create_organization_failed", logging.Err(err))
		return nil, err
	}
	logging.Info(ctx, c.log, "create_organization_ok", logging.Int("organization_id", int(out.ID)))
	return &out, nil
}

// OfferPlan mirrors Auth Service models.Plan for the consumer-facing catalog
// listing (GET /v1/consumer-auth/plans).
type OfferPlan struct {
	ID                   uint           `json:"id"`
	ApplicationServiceID uint           `json:"application_service_id"`
	Code                 string         `json:"code"`
	Name                 string         `json:"name"`
	Limits               map[string]any `json:"limits,omitempty"`
	Features             map[string]any `json:"features,omitempty"`
	Status               string         `json:"status"`
}

// ListOfferPlansOption configures ListOfferPlans.
type ListOfferPlansOption func(*listOfferPlansConfig)

type listOfferPlansConfig struct {
	applicationService string
	page               int
	limit              int
}

// WithOfferPlansApplicationService overrides the application_service query
// parameter (only needed when it cannot be inferred from an sa_* key).
func WithOfferPlansApplicationService(name string) ListOfferPlansOption {
	return func(c *listOfferPlansConfig) { c.applicationService = name }
}

// WithOfferPlansPage sets the page query parameter.
func WithOfferPlansPage(page int) ListOfferPlansOption {
	return func(c *listOfferPlansConfig) { c.page = page }
}

// WithOfferPlansLimit sets the limit (page size) query parameter.
func WithOfferPlansLimit(limit int) ListOfferPlansOption {
	return func(c *listOfferPlansConfig) { c.limit = limit }
}

// ListOfferPlans lists active plans for the bound application_service via
// GET /v1/consumer-auth/plans. apiKeyOrToken accepts either a consumer Bearer
// access token or an sa_* machine API key (raw or "Bearer sa_..."); when
// empty, the client's Credentials key is used.
func (c *Client) ListOfferPlans(ctx context.Context, apiKeyOrToken string, opts ...ListOfferPlansOption) ([]OfferPlan, error) {
	cfg := listOfferPlansConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if apiKeyOrToken == "" {
		apiKeyOrToken = c.cfg.APIKey
	}

	q := url.Values{}
	if cfg.applicationService != "" {
		q.Set("application_service", cfg.applicationService)
	}
	if cfg.page > 0 {
		q.Set("page", strconv.Itoa(cfg.page))
	}
	if cfg.limit > 0 {
		q.Set("limit", strconv.Itoa(cfg.limit))
	}
	path := c.path("/plans")
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out []OfferPlan
	err := c.api.DoJSON(ctx, http.MethodGet, path, nil, &out, credentialCallOption(apiKeyOrToken))
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []OfferPlan{}
	}
	return out, nil
}

// credentialCallOption picks the machine (sa_*) or Bearer transport for a
// dual-auth endpoint, mirroring Auth Service's extractAPIKey/"sa_" gate.
func credentialCallOption(credential string) api.CallOption {
	raw := strings.TrimSpace(credential)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if strings.HasPrefix(raw, "sa_") {
		return api.WithAPIKey(raw)
	}
	return api.WithBearer(raw)
}

// PlanOrganization is the organization identity attached to a plan summary
// when subject_type=organization.
type PlanOrganization struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// PlanSummary is the consumer-facing plan/subscription summary returned by
// GetMyPlan and SelectPlan (mirrors Auth Service domains.ConsumerPlanSummary).
type PlanSummary struct {
	ApplicationService string            `json:"application_service"`
	SubjectType        string            `json:"subject_type"`
	SubjectID          string            `json:"subject_id"`
	PlanID             uint              `json:"plan_id"`
	PlanCode           string            `json:"plan_code"`
	PlanName           string            `json:"plan_name"`
	Organization       *PlanOrganization `json:"organization,omitempty"`
}

// GetMyPlan resolves the caller's active plan via GET /v1/consumer-auth/me/plan.
// subjectType/subjectID are optional: empty defaults to the caller (user);
// "organization" requires the caller to be an owner/admin of subjectID.
func (c *Client) GetMyPlan(ctx context.Context, accessToken, subjectType, subjectID string) (*PlanSummary, error) {
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
	path := c.path("/me/plan")
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out PlanSummary
	err := c.api.DoJSON(ctx, http.MethodGet, path, nil, &out, api.WithBearer(accessToken))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SelectPlanInput changes the caller's (or an administered organization's)
// active plan without payment processing.
type SelectPlanInput struct {
	PlanCode    string `json:"plan_code"`
	SubjectType string `json:"subject_type,omitempty"`
	SubjectID   string `json:"subject_id,omitempty"`
}

type selectPlanRequest struct {
	PlanCode    string `json:"plan_code"`
	SubjectType string `json:"subject_type,omitempty"`
	SubjectID   string `json:"subject_id,omitempty"`
}

// SelectPlan changes the caller's (or an administered organization's) plan
// via PUT /v1/consumer-auth/me/plan.
func (c *Client) SelectPlan(ctx context.Context, accessToken string, in SelectPlanInput) (*PlanSummary, error) {
	if accessToken == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "access token required",
		}}
	}
	var out PlanSummary
	err := c.api.DoJSON(ctx, http.MethodPut, c.path("/me/plan"), selectPlanRequest{
		PlanCode:    in.PlanCode,
		SubjectType: in.SubjectType,
		SubjectID:   in.SubjectID,
	}, &out, api.WithBearer(accessToken))
	if err != nil {
		logging.Warn(ctx, c.log, "select_plan_failed", logging.Err(err))
		return nil, err
	}
	logging.Info(ctx, c.log, "select_plan_ok", logging.String("plan_code", out.PlanCode))
	return &out, nil
}
