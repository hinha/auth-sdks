package stdlog

import "context"

type ctxKey int

const (
	ctxLoggerKey ctxKey = iota
	ctxRequestIDKey
)

// ContextWithLogger stores a Logger in ctx.
func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxLoggerKey, l)
}

// FromContext returns the Logger from ctx, or Nop().
func FromContext(ctx context.Context) Logger {
	if ctx == nil {
		return Nop()
	}
	if l, ok := ctx.Value(ctxLoggerKey).(Logger); ok && l != nil {
		return l
	}
	return Nop()
}

// ContextWithRequestID stores request_id in ctx.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxRequestIDKey, requestID)
}

// RequestIDFromContext returns request_id from ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(ctxRequestIDKey).(string); ok {
		return id
	}
	return ""
}

// LoggerWithRequestID returns l.With(request_id=...) when requestID is non-empty.
func LoggerWithRequestID(l Logger, requestID string) Logger {
	if l == nil {
		l = Nop()
	}
	if requestID == "" {
		return l
	}
	return l.With(String(FieldRequestID, requestID))
}
