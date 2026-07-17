package transport

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gojek/heimdall/v7"

	"github.com/hinha/auth-sdks/go/logging"
)

type ctxKey string

const reqStartKey ctxKey = "auth_sdk_req_start"

// requestLogger is a Heimdall Plugin (Decorator) that emits structured logs
// without ever logging Authorization headers or request bodies.
type requestLogger struct {
	logger  logging.Logger
	service string
}

// NewRequestLogger builds a heimdall.Plugin backed by logging.Logger.
func NewRequestLogger(logger logging.Logger, service string) heimdall.Plugin {
	if logger == nil {
		logger = logging.NewNop()
	}
	if service == "" {
		service = "auth-service"
	}
	return &requestLogger{logger: logger, service: service}
}

// OnRequestStart records start time.
func (p *requestLogger) OnRequestStart(req *http.Request) {
	ctx := context.WithValue(req.Context(), reqStartKey, time.Now())
	*req = *req.WithContext(ctx)
	logging.Debug(req.Context(), p.logger, "http_request_start",
		logging.String("component", "heimdall"),
		logging.String("service", p.service),
		logging.String("method", req.Method),
		logging.String("path", safePath(req)),
	)
}

// OnRequestEnd logs a completed request.
func (p *requestLogger) OnRequestEnd(req *http.Request, res *http.Response) {
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	level := logging.LevelInfo
	if status >= 500 {
		level = logging.LevelError
	} else if status >= 400 {
		level = logging.LevelWarn
	}
	p.logger.Log(req.Context(), level, "http_request_end",
		logging.String("component", "heimdall"),
		logging.String("service", p.service),
		logging.String("method", req.Method),
		logging.String("path", safePath(req)),
		logging.Int("status", status),
		logging.DurationMS("latency_ms", latencyMS(req.Context())),
	)
}

// OnError logs transport failures.
func (p *requestLogger) OnError(req *http.Request, err error) {
	logging.Error(req.Context(), p.logger, "http_request_error",
		logging.String("component", "heimdall"),
		logging.String("service", p.service),
		logging.String("method", req.Method),
		logging.String("path", safePath(req)),
		logging.DurationMS("latency_ms", latencyMS(req.Context())),
		logging.Err(err),
	)
}

func latencyMS(ctx context.Context) int64 {
	start, ok := ctx.Value(reqStartKey).(time.Time)
	if !ok {
		return 0
	}
	return time.Since(start).Milliseconds()
}

func safePath(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	// Prefer path only — query strings may carry tokens.
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	// Redact JWKS is fine; redact nothing extra for now beyond query strip.
	if strings.Contains(strings.ToLower(path), "token") {
		return path
	}
	return path
}
