package echoadapter

import (
	"net/http"
	"strconv"

	"github.com/hinha/auth-sdks/go/ratelimit"
	"github.com/labstack/echo/v4"
)

// Middleware wraps ratelimit.Limiter as Echo middleware.
func Middleware(l *ratelimit.Limiter) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !l.Enabled() {
				return next(c)
			}
			res, profile, err := l.Allow(c.Request().Context(), c.Request())
			if err != nil {
				if l.FailOpen() {
					return next(c)
				}
				return c.String(http.StatusServiceUnavailable, "rate limit unavailable")
			}
			if profile == "" && res.Allowed && res.Limit == 0 {
				return next(c)
			}
			ratelimit.SetHeaders(c.Response(), res)
			if !res.Allowed {
				l.NotifyLimit(c.Request(), profile, res)
				if res.RetryAfterSeconds() > 0 {
					c.Response().Header().Set(ratelimit.HeaderRetryAfter, strconv.FormatInt(res.RetryAfterSeconds(), 10))
				}
				return c.String(http.StatusTooManyRequests, "rate limit exceeded")
			}
			return next(c)
		}
	}
}
