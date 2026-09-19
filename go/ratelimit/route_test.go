package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestMatchPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/v1/search", "/api/v1/search", true},
		{"/api/v1/search", "/api/v1/search/", true}, // trailing slash normalized
		{"/api/v1/jobs/:id", "/api/v1/jobs/abc-123", true},
		{"/api/v1/jobs/{id}", "/api/v1/jobs/abc-123", true},
		{"/api/v1/jobs/:id", "/api/v1/jobs/abc/extra", false},
		{"/api/v1/jobs/:id/events", "/api/v1/jobs/1/events", true},
		{"/api/v1/files/*", "/api/v1/files/a", true},
		{"/api/v1/files/*", "/api/v1/files/a/b/c", true},
		{"/api/v1/files/*", "/api/v1/files", false},
		{"/api/v1/search", "/api/v1/other", false},
		{"", "/x", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ratelimit.MatchPath(tc.pattern, tc.path), "%s vs %s", tc.pattern, tc.path)
	}
}

func TestRouteTable_Pick(t *testing.T) {
	t.Parallel()
	table := ratelimit.RouteTable{
		{Method: http.MethodGet, Pattern: "/api/v1/search", Profile: "search"},
		{Method: http.MethodPost, Pattern: "/api/v1/tweets", Profile: "ingest"},
		{Method: http.MethodGet, Pattern: "/api/v1/jobs/:id", Profile: "job_detail"},
		{Method: http.MethodGet, Pattern: "/api/v1/jobs/:id/events", Profile: "job_events"},
		{Pattern: "/api/v1/webhooks/*", Profile: "webhooks"}, // any method
	}

	get := func(path string) *http.Request {
		return httptest.NewRequest(http.MethodGet, path, nil)
	}
	post := func(path string) *http.Request {
		return httptest.NewRequest(http.MethodPost, path, nil)
	}

	require.Equal(t, "search", table.Pick(get("/api/v1/search")))
	require.Equal(t, "ingest", table.Pick(post("/api/v1/tweets")))
	require.Equal(t, "job_detail", table.Pick(get("/api/v1/jobs/uuid-1")))
	require.Equal(t, "job_events", table.Pick(get("/api/v1/jobs/uuid-1/events")))
	require.Equal(t, "webhooks", table.Pick(post("/api/v1/webhooks/stripe/x")))
	require.Equal(t, "", table.Pick(get("/api/v1/unknown")))
	require.Equal(t, "", table.Pick(nil))
}

func TestRouteTable_WithLimiter(t *testing.T) {
	t.Parallel()
	table := ratelimit.RouteTable{
		{Method: http.MethodGet, Pattern: "/api/v1/jobs/:id", Profile: "job"},
	}
	lim, err := ratelimit.New(ratelimit.Config{
		Enabled: true,
		Profiles: map[string]string{
			"default": "100-M",
			"job":     "1-M",
		},
		DefaultProfile: "default",
		PickProfile:    table.Pick,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/abc", nil)
	req.RemoteAddr = "1.1.1.1:9"
	res, profile, err := lim.Allow(req.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "job", profile)
	require.True(t, res.Allowed)
	require.Equal(t, int64(1), res.Limit)

	other := httptest.NewRequest(http.MethodGet, "/api/v1/other", nil)
	other.RemoteAddr = "1.1.1.1:9"
	res, profile, err = lim.Allow(other.Context(), other)
	require.NoError(t, err)
	require.Equal(t, "default", profile)
	require.Equal(t, int64(100), res.Limit)
}
