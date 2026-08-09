package echoadapter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/ratelimit"
	echoadapter "github.com/hinha/auth-sdks/go/ratelimit/echo"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/ulule/limiter/v3"
)

func TestEchoMiddleware_RateLimit(t *testing.T) {
	t.Parallel()
	l, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
	})
	require.NoError(t, err)

	e := echo.New()
	e.Use(echoadapter.Middleware(l))
	e.GET("/x", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.1.1.1:9"

	rr1 := httptest.NewRecorder()
	e.ServeHTTP(rr1, req)
	require.Equal(t, http.StatusNoContent, rr1.Code)
	require.NotEmpty(t, rr1.Header().Get(ratelimit.HeaderLimit))

	rr2 := httptest.NewRecorder()
	e.ServeHTTP(rr2, req)
	require.Equal(t, http.StatusTooManyRequests, rr2.Code)
	require.NotEmpty(t, rr2.Header().Get(ratelimit.HeaderRetryAfter))
}

func TestEchoMiddleware_NilDisabled(t *testing.T) {
	t.Parallel()
	e := echo.New()
	e.Use(echoadapter.Middleware(nil))
	e.GET("/ok", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ok", nil))
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestEchoMiddleware_SkipAndFailPolicies(t *testing.T) {
	t.Parallel()

	skipLim, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		SkipFunc: ratelimit.SkipPrefixes("/health"),
	})
	require.NoError(t, err)
	e := echo.New()
	e.Use(echoadapter.Middleware(skipLim))
	e.GET("/health", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Empty(t, rr.Header().Get(ratelimit.HeaderLimit))

	openLim, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		FailOpen: ratelimit.BoolPtr(true),
		Store:    &errStore{},
	})
	require.NoError(t, err)
	e2 := echo.New()
	e2.Use(echoadapter.Middleware(openLim))
	e2.GET("/x", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	rr2 := httptest.NewRecorder()
	e2.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusNoContent, rr2.Code)

	closedLim, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		FailOpen: ratelimit.BoolPtr(false),
		Store:    &errStore{},
	})
	require.NoError(t, err)
	e3 := echo.New()
	e3.Use(echoadapter.Middleware(closedLim))
	e3.GET("/x", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	rr3 := httptest.NewRecorder()
	e3.ServeHTTP(rr3, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr3.Code)
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
