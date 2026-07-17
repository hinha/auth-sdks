// Package logging defines the Strategy interface for structured SDK logging.
// Callers inject Zap, slog, or a no-op implementation via client options.
package logging

import "context"

// Level is a coarse severity for SDK events.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Field is a structured key/value pair. Sensitive values (tokens, passwords)
// must never be attached by SDK internals.
type Field struct {
	Key   string
	Value any
}

// String creates a string field.
func String(key, value string) Field { return Field{Key: key, Value: value} }

// Int creates an int field.
func Int(key string, value int) Field { return Field{Key: key, Value: value} }

// Int64 creates an int64 field.
func Int64(key string, value int64) Field { return Field{Key: key, Value: value} }

// Bool creates a bool field.
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }

// Err creates an error field under key "error".
func Err(err error) Field { return Field{Key: "error", Value: err} }

// DurationMS creates a latency field in milliseconds.
func DurationMS(key string, ms int64) Field { return Field{Key: key, Value: ms} }

// Any creates an arbitrary field.
func Any(key string, value any) Field { return Field{Key: key, Value: value} }

// Logger is the Strategy contract used throughout the SDK.
type Logger interface {
	Log(ctx context.Context, level Level, msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Debug helpers keep call sites terse.
func Debug(ctx context.Context, l Logger, msg string, fields ...Field) {
	if l != nil {
		l.Log(ctx, LevelDebug, msg, fields...)
	}
}

// Info logs at info level.
func Info(ctx context.Context, l Logger, msg string, fields ...Field) {
	if l != nil {
		l.Log(ctx, LevelInfo, msg, fields...)
	}
}

// Warn logs at warn level.
func Warn(ctx context.Context, l Logger, msg string, fields ...Field) {
	if l != nil {
		l.Log(ctx, LevelWarn, msg, fields...)
	}
}

// Error logs at error level.
func Error(ctx context.Context, l Logger, msg string, fields ...Field) {
	if l != nil {
		l.Log(ctx, LevelError, msg, fields...)
	}
}
