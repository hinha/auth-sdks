package stdlog

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewZap builds a Zap-backed Logger from Config.
func NewZap(cfg Config) (Logger, error) {
	n, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	loc := n.location()

	var zcfg zap.Config
	if n.Format == FormatConsole {
		zcfg = zap.NewDevelopmentConfig()
		zcfg.Encoding = "console"
	} else {
		zcfg = zap.NewProductionConfig()
		zcfg.Encoding = "json"
		zcfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.In(loc).Format(time.RFC3339Nano))
		}
	}
	zcfg.DisableStacktrace = true
	if err := zcfg.Level.UnmarshalText([]byte(n.Level)); err != nil {
		return nil, fmt.Errorf("stdlog: parse level: %w", err)
	}

	base, err := zcfg.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, fmt.Errorf("stdlog: build zap: %w", err)
	}
	base = base.With(zap.String(FieldService, n.Service))
	return &zapLogger{base: base}, nil
}

type zapLogger struct {
	base *zap.Logger
}

func (l *zapLogger) Debug(msg string, fields ...Field) { l.base.Debug(msg, toZapFields(fields)...) }
func (l *zapLogger) Info(msg string, fields ...Field)  { l.base.Info(msg, toZapFields(fields)...) }
func (l *zapLogger) Warn(msg string, fields ...Field)  { l.base.Warn(msg, toZapFields(fields)...) }
func (l *zapLogger) Error(msg string, fields ...Field) { l.base.Error(msg, toZapFields(fields)...) }
func (l *zapLogger) Fatal(msg string, fields ...Field) { l.base.Fatal(msg, toZapFields(fields)...) }

func (l *zapLogger) With(fields ...Field) Logger {
	return &zapLogger{base: l.base.With(toZapFields(fields)...)}
}

func (l *zapLogger) Named(component string) Logger {
	return &zapLogger{base: l.base.With(zap.String(FieldComponent, component))}
}

func (l *zapLogger) Sync() error { return l.base.Sync() }

func toZapFields(fields []Field) []zap.Field {
	out := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		out = append(out, toZapField(f))
	}
	return out
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
		if f.Key == FieldError {
			return zap.Error(v)
		}
		return zap.NamedError(f.Key, v)
	default:
		return zap.Any(f.Key, v)
	}
}
