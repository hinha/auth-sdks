package logging

import (
	"context"
	"log/slog"
)

// Slog adapts log/slog to the Logger Strategy.
type Slog struct {
	base   *slog.Logger
	fields []Field
}

// NewSlog wraps a slog.Logger. If base is nil, slog.Default() is used.
func NewSlog(base *slog.Logger) Logger {
	if base == nil {
		base = slog.Default()
	}
	return &Slog{base: base}
}

// Log implements Logger.
func (s *Slog) Log(ctx context.Context, level Level, msg string, fields ...Field) {
	attrs := make([]any, 0, (len(s.fields)+len(fields))*2)
	for _, f := range s.fields {
		attrs = append(attrs, f.Key, f.Value)
	}
	for _, f := range fields {
		attrs = append(attrs, f.Key, f.Value)
	}
	s.base.Log(ctx, toSlogLevel(level), msg, attrs...)
}

// With implements Logger.
func (s *Slog) With(fields ...Field) Logger {
	merged := make([]Field, 0, len(s.fields)+len(fields))
	merged = append(merged, s.fields...)
	merged = append(merged, fields...)
	return &Slog{base: s.base, fields: merged}
}

func toSlogLevel(level Level) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
