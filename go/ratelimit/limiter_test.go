package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/ratelimit"
	"github.com/stretchr/testify/require"
	"github.com/ulule/limiter/v3"
)

func TestNew_RequiresProfilesWhenEnabled(t *testing.T) {
	t.Parallel()
	_, err := ratelimit.New(ratelimit.Config{Enabled: true})
	require.Error(t, err)
}

func TestNew_InvalidRate(t *testing.T) {
	t.Parallel()
	_, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "bad"},
	})
	require.Error(t, err)
}

func TestAllow_DisabledAndSkip(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{Enabled: false})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	res, profile, err := l.Allow(req.Context(), req)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Empty(t, profile)

	l2, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "2-M"},
		SkipFunc: ratelimit.SkipPrefixes("/health"),
	})
	require.NoError(t, err)
	skipReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	res, _, err = l2.Allow(skipReq.Context(), skipReq)
	require.NoError(t, err)
	require.True(t, res.Allowed)

	opt := httptest.NewRequest(http.MethodOptions, "/api", nil)
	res, _, err = l2.Allow(opt.Context(), opt)
	require.NoError(t, err)
	require.True(t, res.Allowed)
}

func TestAllow_MultiProfileAndExhaust(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{
		Enabled: true,
		Profiles: map[string]string{
			"api":    "1-M",
			"ingest": "5-M",
		},
		DefaultProfile: "api",
		PickProfile: func(r *http.Request) string {
			if r.URL.Path == "/ingest" {
				return "ingest"
			}
			return "api"
		},
		CredentialHeader: "X-API-Key",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-API-Key", "k1")

	res1, profile, err := l.Allow(req.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "api", profile)
	require.True(t, res1.Allowed)
	require.Equal(t, int64(1), res1.Limit)

	res2, _, err := l.Allow(req.Context(), req)
	require.NoError(t, err)
	require.False(t, res2.Allowed)
	require.GreaterOrEqual(t, res2.RetryAfterSeconds(), int64(1))

	ingest := httptest.NewRequest(http.MethodPost, "/ingest", nil)
	ingest.RemoteAddr = "10.0.0.1:1234"
	ingest.Header.Set("X-API-Key", "k1")
	res3, profile, err := l.Allow(ingest.Context(), ingest)
	require.NoError(t, err)
	require.Equal(t, "ingest", profile)
	require.True(t, res3.Allowed)
}

func TestDefaultKeyFunc_UsesIPWhenNoCredential(t *testing.T) {
	t.Parallel()
	fn := ratelimit.DefaultKeyFunc("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	key := fn(req, "api")
	require.Contains(t, key, "api:")
	require.NotContains(t, key, "192.168")
}

func TestClientIP_NoPort(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "weird"
	require.Equal(t, "weird", ratelimit.ClientIP(req))
}

func TestMiddleware_SetsHeadersAnd429(t *testing.T) {
	t.Parallel()
	var limited bool
	l, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-S"},
		OnLimit: func(r *http.Request, profile string, res ratelimit.Result) {
			limited = true
		},
	})
	require.NoError(t, err)

	mw := ratelimit.Middleware(l, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "127.0.0.1:1"

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req)
	require.Equal(t, http.StatusOK, rr1.Code)
	require.NotEmpty(t, rr1.Header().Get(ratelimit.HeaderLimit))

	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	require.Equal(t, http.StatusTooManyRequests, rr2.Code)
	require.NotEmpty(t, rr2.Header().Get(ratelimit.HeaderRetryAfter))
	require.True(t, limited)
}

func TestMiddleware_FailOpen(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		FailOpen: ratelimit.BoolPtr(true),
		Store:    &errStore{},
	})
	require.NoError(t, err)

	mw := ratelimit.Middleware(l, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusNoContent, rr.Code)
}

func TestMiddleware_FailClosed(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		FailOpen: ratelimit.BoolPtr(false),
		Store:    &errStore{},
	})
	require.NoError(t, err)

	mw := ratelimit.Middleware(l, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestMiddleware_DisabledNil(t *testing.T) {
	t.Parallel()
	mw := ratelimit.Middleware(nil, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestAllow_UnknownProfile(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"api": "10-M"},
		PickProfile: func(*http.Request) string {
			return "missing"
		},
	})
	require.NoError(t, err)
	_, _, err = l.Allow(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	require.Error(t, err)
}

type errStore struct{}

func (e *errStore) Get(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errBoom{}
}
func (e *errStore) Peek(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errBoom{}
}
func (e *errStore) Reset(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errBoom{}
}
func (e *errStore) Increment(ctx context.Context, key string, count int64, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errBoom{}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
