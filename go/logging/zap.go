package logging

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Zap adapts uber/zap to the Logger Strategy.
type Zap struct {
	base   *zap.Logger
	fields []Field
}

// NewZap wraps a zap.Logger. If base is nil, zap.NewNop() is used.
func NewZap(base *zap.Logger) Logger {
	if base == nil {
		base = zap.NewNop()
	}
	return &Zap{base: base}
}

// Log implements Logger.
func (z *Zap) Log(_ context.Context, level Level, msg string, fields ...Field) {
	zf := make([]zap.Field, 0, len(z.fields)+len(fields))
	for _, f := range z.fields {
		zf = append(zf, toZapField(f))
	}
	for _, f := range fields {
		zf = append(zf, toZapField(f))
	}
	if ce := z.base.Check(toZapLevel(level), msg); ce != nil {
		ce.Write(zf...)
	}
}

// With implements Logger.
func (z *Zap) With(fields ...Field) Logger {
	merged := make([]Field, 0, len(z.fields)+len(fields))
	merged = append(merged, z.fields...)
	merged = append(merged, fields...)
	return &Zap{base: z.base, fields: merged}
}

func toZapLevel(level Level) zapcore.Level {
	switch level {
	case LevelDebug:
		return zapcore.DebugLevel
	case LevelWarn:
		return zapcore.WarnLevel
	case LevelError:
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func toZapField(f Field) zap.Field {
	switch v := f.Value.(type) {
	case string:
		return zap.String(f.Key, v)
	case int:
		return zap.Int(f.Key, v)
	case int64:
		return zap.Int64(f.Key, v)
	case bool:
		return zap.Bool(f.Key, v)
	case error:
		if f.Key == "error" {
			return zap.Error(v)
		}
		return zap.NamedError(f.Key, v)
	default:
		return zap.Any(f.Key, v)
	}
}
