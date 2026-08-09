package echoadapter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hinha/auth-sdks/go/ratelimit"
	echoadapter "github.com/hinha/auth-sdks/go/ratelimit/echo"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
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
