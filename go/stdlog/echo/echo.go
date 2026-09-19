package echoadapter

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hinha/auth-sdks/go/stdlog"
	"github.com/labstack/echo/v4"
)

// Middleware returns Echo access-log middleware with the strict stdlog field schema.
func Middleware(logger stdlog.Logger, cfg stdlog.AccessLogConfig) echo.MiddlewareFunc {
	base := logger
	if base == nil {
		base = stdlog.Nop()
	}
	base = base.Named("http")
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			req := c.Request()
			requestID := req.Header.Get(stdlog.RequestIDHeader)
			if requestID == "" {
				requestID = uuid.NewString()
			}
			req.Header.Set(stdlog.RequestIDHeader, requestID)
			c.Response().Header().Set(stdlog.RequestIDHeader, requestID)

			reqLogger := stdlog.LoggerWithRequestID(base, requestID)
			ctx := stdlog.ContextWithRequestID(req.Context(), requestID)
			ctx = stdlog.ContextWithLogger(ctx, reqLogger)
			c.SetRequest(req.WithContext(ctx))

			err := next(c)

			status := c.Response().Status
			if status == 0 {
				status = http.StatusOK
			}
			userID := ""
			if cfg.UserIDFunc != nil {
				userID = cfg.UserIDFunc(c.Request())
			}
			ev := stdlog.AccessEvent{
				Component:    "http",
				RequestID:    requestID,
				Method:       req.Method,
				Path:         req.URL.Path,
				Route:        c.Path(),
				Query:        req.URL.RawQuery,
				Status:       status,
				DurationMS:   time.Since(start).Milliseconds(),
				ResponseSize: int(c.Response().Size),
				RemoteAddr:   req.RemoteAddr,
				UserAgent:    req.UserAgent(),
				UserID:       userID,
			}
			if cfg.LogRequestHeaders {
				ev.RequestHeaders = stdlog.RedactHeaders(req.Header, cfg.ExtraRedactHeaders...)
			}
			if cfg.LogResponseHeaders {
				ev.ResponseHeaders = stdlog.RedactHeaders(c.Response().Header(), cfg.ExtraRedactHeaders...)
			}
			stdlog.LogAccess(reqLogger, ev)
			return err
		}
	}
}
