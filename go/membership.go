package authsdk

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
)

// Membership is the caller's active application_membership for one service.
type Membership struct {
	ID                   uint     `json:"id"`
	ApplicationServiceID uint     `json:"application_service_id"`
	ApplicationService   string   `json:"application_service"`
	OrganizationID       *uint    `json:"organization_id,omitempty"`
	Status               string   `json:"status"`
	PermissionKeys       []string `json:"permission_keys"`
}

// MembershipBundle is one YAML permission-bundle binding on a membership.
type MembershipBundle struct {
	ID                            uint     `json:"id"`
	ApplicationPermissionBundleID uint     `json:"application_permission_bundle_id"`
	BundleCode                    string   `json:"bundle_code"`
	BundleName                    string   `json:"bundle_name"`
	Permissions                   []string `json:"permissions"`
}

type bindMembershipBundleRequest struct {
	BundleCode string `json:"bundle_code"`
}

// GetMyMembership returns the authenticated user's active membership for the
// client-bound application_service.
func (c *Client) GetMyMembership(ctx context.Context, accessToken string) (*Membership, error) {
	if accessToken == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "access token required",
		}}
	}
	q := url.Values{}
	q.Set("application_service", c.cfg.ApplicationService)
	path := c.path("/me/membership") + "?" + q.Encode()

	var out Membership
	err := c.api.DoJSON(ctx, http.MethodGet, path, nil, &out, api.WithBearer(accessToken))
	if err != nil {
		logging.Warn(ctx, c.log, "get_my_membership_failed", logging.Err(err))
		return nil, err
	}
	logging.Info(ctx, c.log, "get_my_membership_ok", logging.Int("membership_id", int(out.ID)))
	return &out, nil
}

// ListMembershipBundles lists YAML bundle bindings on a membership the caller owns.
func (c *Client) ListMembershipBundles(ctx context.Context, accessToken string, membershipID uint) ([]MembershipBundle, error) {
	if accessToken == "" || membershipID == 0 {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "access token and membership id required",
		}}
	}
	var out []MembershipBundle
	err := c.api.DoJSON(ctx, http.MethodGet, c.path(fmt.Sprintf("/memberships/%d/bundles", membershipID)), nil, &out, api.WithBearer(accessToken))
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []MembershipBundle{}
	}
	return out, nil
}

// BindMembershipBundle assigns a catalog bundle by code to an owned membership.
func (c *Client) BindMembershipBundle(ctx context.Context, accessToken string, membershipID uint, bundleCode string) (*MembershipBundle, error) {
	if accessToken == "" || membershipID == 0 || bundleCode == "" {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "access token, membership id, and bundle_code required",
		}}
	}
	var out MembershipBundle
	err := c.api.DoJSON(ctx, http.MethodPost, c.path(fmt.Sprintf("/memberships/%d/bundles", membershipID)), bindMembershipBundleRequest{
		BundleCode: bundleCode,
	}, &out, api.WithBearer(accessToken))
	if err != nil {
		return nil, err
	}
	logging.Info(ctx, c.log, "bind_membership_bundle_ok",
		logging.Int("membership_id", int(membershipID)),
		logging.String("bundle_code", bundleCode),
	)
	return &out, nil
}

// UnbindMembershipBundle revokes a bundle from an owned membership.
func (c *Client) UnbindMembershipBundle(ctx context.Context, accessToken string, membershipID, bundleID uint) (*MembershipBundle, error) {
	if accessToken == "" || membershipID == 0 || bundleID == 0 {
		return nil, &ValidationError{APIError: &APIError{
			StatusCode: http.StatusBadRequest,
			Code:       "400",
			Message:    "access token, membership id, and bundle id required",
		}}
	}
	var out MembershipBundle
	err := c.api.DoJSON(ctx, http.MethodDelete, c.path(fmt.Sprintf("/memberships/%d/bundles/%d", membershipID, bundleID)), nil, &out, api.WithBearer(accessToken))
	if err != nil {
		return nil, err
	}
	return &out, nil
}
