package logging_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/hinha/auth-sdk-go/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNop(t *testing.T) {
	t.Parallel()
	l := logging.NewNop()
	l.Log(context.Background(), logging.LevelInfo, "x")
	l.With(logging.String("a", "b")).Log(context.Background(), logging.LevelDebug, "y")
}

func TestFieldHelpersAndLevels(t *testing.T) {
	t.Parallel()
	_ = logging.String("s", "v")
	_ = logging.Int("i", 1)
	_ = logging.Int64("i64", 2)
	_ = logging.Bool("b", true)
	_ = logging.Err(errors.New("e"))
	_ = logging.DurationMS("ms", 3)
	_ = logging.Any("any", map[string]int{"k": 1})

	logging.Debug(context.Background(), nil, "skip")
	logging.Info(context.Background(), nil, "skip")
	logging.Warn(context.Background(), nil, "skip")
	logging.Error(context.Background(), nil, "skip")

	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := logging.NewSlog(slog.New(h)).With(logging.String("svc", "auth"))
	logging.Debug(context.Background(), l, "d")
	logging.Info(context.Background(), l, "i")
	logging.Warn(context.Background(), l, "w")
	logging.Error(context.Background(), l, "e", logging.Err(errors.New("boom")))
	if buf.Len() == 0 {
		t.Fatal("expected slog output")
	}

	l2 := logging.NewSlog(nil)
	l2.Log(context.Background(), logging.LevelInfo, "default")
}

func TestZapLogger(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(enc, zapcore.AddSync(buf), zapcore.DebugLevel)
	zl := zap.New(core)
	l := logging.NewZap(zl).With(logging.String("sdk", "go"))
	l.Log(context.Background(), logging.LevelDebug, "d", logging.Int("n", 1))
	l.Log(context.Background(), logging.LevelInfo, "i", logging.Int64("n", 2))
	l.Log(context.Background(), logging.LevelWarn, "w", logging.Bool("ok", true))
	l.Log(context.Background(), logging.LevelError, "e",
		logging.Err(errors.New("x")),
		logging.Any("m", map[string]string{"a": "b"}),
		logging.Field{Key: "named", Value: errors.New("named")},
		logging.Field{Key: "f", Value: 1.5},
	)

	lNil := logging.NewZap(nil)
	lNil.Log(context.Background(), logging.LevelInfo, "nop")
	if buf.Len() == 0 {
		t.Fatal("expected zap output")
	}
}
