package logging_test

import (
	"context"
	"sync"
	"testing"

	"github.com/hinha/auth-sdks/go/logging"
)

type recLogger struct {
	mu    sync.Mutex
	level logging.Level
	msg   string
	fields []logging.Field
}

func (r *recLogger) Log(_ context.Context, level logging.Level, msg string, fields ...logging.Field) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.level = level
	r.msg = msg
	r.fields = append([]logging.Field(nil), fields...)
}

func (r *recLogger) With(fields ...logging.Field) logging.Logger {
	return r
}

func TestAudit_ForcesMessageAndKind(t *testing.T) {
	t.Parallel()
	rec := &recLogger{}
	logging.Audit(context.Background(), rec,
		logging.String(logging.FieldEventType, logging.EventAuthLogin),
		logging.String(logging.FieldDecision, logging.DecisionInfo),
		logging.String("password", "should-not-matter-caller-must-not"),
	)
	if rec.msg != logging.AuditMessage {
		t.Fatalf("msg=%q", rec.msg)
	}
	if rec.level != logging.LevelInfo {
		t.Fatalf("level=%v", rec.level)
	}
	var kind, eventType string
	for _, f := range rec.fields {
		if f.Key == logging.FieldKind {
			kind, _ = f.Value.(string)
		}
		if f.Key == logging.FieldEventType {
			eventType, _ = f.Value.(string)
		}
	}
	if kind != logging.KindAudit {
		t.Fatalf("kind=%q", kind)
	}
	if eventType != logging.EventAuthLogin {
		t.Fatalf("event_type=%q", eventType)
	}
	logging.Audit(context.Background(), nil, logging.String(logging.FieldEventType, "x"))
}

func TestAuditAt_WarnOnDeny(t *testing.T) {
	t.Parallel()
	rec := &recLogger{}
	logging.AuditAt(context.Background(), rec, logging.LevelWarn,
		logging.String(logging.FieldEventType, logging.EventAuthAuthorizeAction),
		logging.String(logging.FieldDecision, logging.DecisionDeny),
	)
	if rec.level != logging.LevelWarn {
		t.Fatalf("level=%v", rec.level)
	}
	if rec.msg != logging.AuditMessage {
		t.Fatalf("msg=%q", rec.msg)
	}
}

func TestRedactMachineID(t *testing.T) {
	t.Parallel()
	if got := logging.RedactMachineID(""); got != "" {
		t.Fatalf("empty=%q", got)
	}
	if got := logging.RedactMachineID("user-42"); got != "user-42" {
		t.Fatalf("user=%q", got)
	}
	raw := "sa_super_secret_machine_key"
	got := logging.RedactMachineID(raw)
	if got == raw || len(got) >= len(raw) {
		t.Fatalf("redact=%q", got)
	}
}
