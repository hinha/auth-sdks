package stdlog

import (
	"context"
	"log/slog"
	"os"
)

// NewSlog builds a slog-backed Logger from Config.
func NewSlog(cfg Config) (Logger, error) {
	n, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	level := slog.LevelInfo
	switch n.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if n.Format == FormatConsole {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	base := slog.New(handler).With(FieldService, n.Service)
	return &slogLogger{base: base}, nil
}

type slogLogger struct {
	base *slog.Logger
}

func (l *slogLogger) Debug(msg string, fields ...Field) {
	l.base.Log(context.Background(), slog.LevelDebug, msg, toSlogArgs(fields)...)
}
func (l *slogLogger) Info(msg string, fields ...Field) {
	l.base.Log(context.Background(), slog.LevelInfo, msg, toSlogArgs(fields)...)
}
func (l *slogLogger) Warn(msg string, fields ...Field) {
	l.base.Log(context.Background(), slog.LevelWarn, msg, toSlogArgs(fields)...)
}
func (l *slogLogger) Error(msg string, fields ...Field) {
	l.base.Log(context.Background(), slog.LevelError, msg, toSlogArgs(fields)...)
}
func (l *slogLogger) Fatal(msg string, fields ...Field) {
	l.base.Log(context.Background(), slog.LevelError, msg, toSlogArgs(fields)...)
}

func (l *slogLogger) With(fields ...Field) Logger {
	return &slogLogger{base: l.base.With(toSlogArgs(fields)...)}
}

func (l *slogLogger) Named(component string) Logger {
	return &slogLogger{base: l.base.With(FieldComponent, component)}
}

func (l *slogLogger) Sync() error { return nil }

func toSlogArgs(fields []Field) []any {
	out := make([]any, 0, len(fields)*2)
	for _, f := range fields {
		out = append(out, f.Key, f.Value)
	}
	return out
}
