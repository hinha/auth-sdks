package stdlog

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestIDHeader is the canonical HTTP request id header.
const RequestIDHeader = "X-Request-Id"

// AccessLogConfig configures HTTP access-log middleware.
type AccessLogConfig struct {
	// LogRequestHeaders when true includes redacted request headers.
	LogRequestHeaders bool
	// LogResponseHeaders when true includes redacted response headers.
	LogResponseHeaders bool
	// ExtraRedactHeaders are additional header names to strip.
	ExtraRedactHeaders []string
	// RouteFunc optionally resolves a route template (e.g. Echo Path()).
	RouteFunc func(r *http.Request) string
	// UserIDFunc optionally resolves a user id after auth middleware.
	UserIDFunc func(r *http.Request) string
}

// Middleware returns net/http middleware that emits a strict access-log event.
func Middleware(logger Logger, cfg AccessLogConfig) func(http.Handler) http.Handler {
	base := logger
	if base == nil {
		base = Nop()
	}
	base = base.Named("http")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := r.Header.Get(RequestIDHeader)
			if requestID == "" {
				requestID = uuid.NewString()
				r.Header.Set(RequestIDHeader, requestID)
			}

			reqLogger := LoggerWithRequestID(base, requestID)
			ctx := ContextWithRequestID(r.Context(), requestID)
			ctx = ContextWithLogger(ctx, reqLogger)
			r = r.WithContext(ctx)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			rec.Header().Set(RequestIDHeader, requestID)
			next.ServeHTTP(rec, r)

			route := ""
			if cfg.RouteFunc != nil {
				route = cfg.RouteFunc(r)
			}
			userID := ""
			if cfg.UserIDFunc != nil {
				userID = cfg.UserIDFunc(r)
			}

			ev := AccessEvent{
				Component:    "http",
				RequestID:    requestID,
				Method:       r.Method,
				Path:         r.URL.Path,
				Route:        route,
				Query:        r.URL.RawQuery,
				Status:       rec.status,
				DurationMS:   time.Since(start).Milliseconds(),
				ResponseSize: rec.size,
				RemoteAddr:   r.RemoteAddr,
				UserAgent:    r.UserAgent(),
				UserID:       userID,
			}
			if cfg.LogRequestHeaders {
				ev.RequestHeaders = RedactHeaders(r.Header, cfg.ExtraRedactHeaders...)
			}
			if cfg.LogResponseHeaders {
				ev.ResponseHeaders = RedactHeaders(rec.Header(), cfg.ExtraRedactHeaders...)
			}
			LogAccess(reqLogger, ev)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.size += n
	return n, err
}
