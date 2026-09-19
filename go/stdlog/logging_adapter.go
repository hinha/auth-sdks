package stdlog

import (
	"context"

	"github.com/hinha/auth-sdks/go/logging"
)

// LoggingAdapter exposes a stdlog.Logger as the Auth SDK logging.Logger
// Strategy so one Zap/slog instance (optionally wrapped with WrapLoki) can
// capture both service logs and SDK audit events.
func LoggingAdapter(l Logger) logging.Logger {
	if l == nil {
		l = Nop()
	}
	return &loggingAdapter{base: l}
}

type loggingAdapter struct {
	base Logger
}

func (a *loggingAdapter) Log(ctx context.Context, level logging.Level, msg string, fields ...logging.Field) {
	sf := make([]Field, 0, len(fields)+1)
	if id := RequestIDFromContext(ctx); id != "" {
		sf = append(sf, String(FieldRequestID, id))
	}
	for _, f := range fields {
		sf = append(sf, Field{Key: f.Key, Value: f.Value})
	}
	switch level {
	case logging.LevelDebug:
		a.base.Debug(msg, sf...)
	case logging.LevelWarn:
		a.base.Warn(msg, sf...)
	case logging.LevelError:
		a.base.Error(msg, sf...)
	default:
		a.base.Info(msg, sf...)
	}
}

func (a *loggingAdapter) With(fields ...logging.Field) logging.Logger {
	sf := make([]Field, 0, len(fields))
	for _, f := range fields {
		sf = append(sf, Field{Key: f.Key, Value: f.Value})
	}
	return &loggingAdapter{base: a.base.With(sf...)}
}
