package ratelimit

import (
	"net/http"
	"strconv"
)

const (
	HeaderLimit     = "X-RateLimit-Limit"
	HeaderRemaining = "X-RateLimit-Remaining"
	HeaderReset     = "X-RateLimit-Reset"
	HeaderRetryAfter = "Retry-After"
)

// SetHeaders writes standard rate-limit response headers.
func SetHeaders(w http.ResponseWriter, res Result) {
	w.Header().Set(HeaderLimit, strconv.FormatInt(res.Limit, 10))
	w.Header().Set(HeaderRemaining, strconv.FormatInt(res.Remaining, 10))
	w.Header().Set(HeaderReset, strconv.FormatInt(res.Reset, 10))
}

// Middleware returns net/http middleware backed by Limiter.
// onRejected writes the 429 body; if nil, a plain text body is used.
func Middleware(l *Limiter, onRejected func(w http.ResponseWriter, r *http.Request, profile string, res Result)) func(http.Handler) http.Handler {
	if onRejected == nil {
		onRejected = func(w http.ResponseWriter, r *http.Request, profile string, res Result) {
			_ = r
			_ = profile
			if res.RetryAfterSeconds() > 0 {
				w.Header().Set(HeaderRetryAfter, strconv.FormatInt(res.RetryAfterSeconds(), 10))
			}
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l == nil || !l.cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			res, profile, err := l.Allow(r.Context(), r)
			if err != nil {
				if l.FailOpen() {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "rate limit unavailable", http.StatusServiceUnavailable)
				return
			}
			if profile == "" && res.Allowed && res.Limit == 0 {
				// skipped
				next.ServeHTTP(w, r)
				return
			}
			SetHeaders(w, res)
			if !res.Allowed {
				l.NotifyLimit(r, profile, res)
				onRejected(w, r, profile, res)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
