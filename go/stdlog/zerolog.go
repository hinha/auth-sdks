package stdlog

import (
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
)

// NewZerolog builds a Zerolog-backed Logger from Config.
func NewZerolog(cfg Config) (Logger, error) {
	n, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	level, err := zerolog.ParseLevel(n.Level)
	if err != nil {
		return nil, fmt.Errorf("stdlog: parse zerolog level: %w", err)
	}
	var out io.Writer = os.Stdout
	if n.Format == FormatConsole {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	base := zerolog.New(out).Level(level).With().
		Timestamp().
		Str(FieldService, n.Service).
		Logger()
	return &zerologLogger{base: &base}, nil
}

type zerologLogger struct {
	base *zerolog.Logger
}

func (l *zerologLogger) Debug(msg string, fields ...Field) {
	writeZerolog(l.base.Debug(), msg, fields)
}
func (l *zerologLogger) Info(msg string, fields ...Field) {
	writeZerolog(l.base.Info(), msg, fields)
}
func (l *zerologLogger) Warn(msg string, fields ...Field) {
	writeZerolog(l.base.Warn(), msg, fields)
}
func (l *zerologLogger) Error(msg string, fields ...Field) {
	writeZerolog(l.base.Error(), msg, fields)
}
func (l *zerologLogger) Fatal(msg string, fields ...Field) {
	writeZerolog(l.base.Fatal(), msg, fields)
}

func (l *zerologLogger) With(fields ...Field) Logger {
	ctx := l.base.With()
	for _, f := range fields {
		ctx = appendZerolog(ctx, f)
	}
	child := ctx.Logger()
	return &zerologLogger{base: &child}
}

func (l *zerologLogger) Named(component string) Logger {
	child := l.base.With().Str(FieldComponent, component).Logger()
	return &zerologLogger{base: &child}
}

func (l *zerologLogger) Sync() error { return nil }

func writeZerolog(ev *zerolog.Event, msg string, fields []Field) {
	for _, f := range fields {
		ev = applyZerolog(ev, f)
	}
	ev.Msg(msg)
}

func appendZerolog(ctx zerolog.Context, f Field) zerolog.Context {
	switch v := f.Value.(type) {
	case string:
		return ctx.Str(f.Key, v)
	case int:
		return ctx.Int(f.Key, v)
	case int64:
		return ctx.Int64(f.Key, v)
	case bool:
		return ctx.Bool(f.Key, v)
	case error:
		if f.Key == FieldError {
			return ctx.Err(v)
		}
		return ctx.AnErr(f.Key, v)
	default:
		return ctx.Interface(f.Key, v)
	}
}

func applyZerolog(ev *zerolog.Event, f Field) *zerolog.Event {
	switch v := f.Value.(type) {
	case string:
		return ev.Str(f.Key, v)
	case int:
		return ev.Int(f.Key, v)
	case int64:
		return ev.Int64(f.Key, v)
	case bool:
		return ev.Bool(f.Key, v)
	case error:
		if f.Key == FieldError {
			return ev.Err(v)
		}
		return ev.AnErr(f.Key, v)
	default:
		return ev.Interface(f.Key, v)
	}
}
