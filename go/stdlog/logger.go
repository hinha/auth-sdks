// Package stdlog provides a service-grade structured logger with a strict
// access-log field schema. It is separate from github.com/hinha/auth-sdks/go/logging
// (SDK client Strategy).
package stdlog

// Logger is the service logging contract used by memoo, task-hub, and x-engine.
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	Named(component string) Logger
	Sync() error
}

// Nop returns a no-op Logger.
func Nop() Logger { return nopLogger{} }

type nopLogger struct{}

func (nopLogger) Debug(string, ...Field) {}
func (nopLogger) Info(string, ...Field)  {}
func (nopLogger) Warn(string, ...Field)  {}
func (nopLogger) Error(string, ...Field) {}
func (nopLogger) Fatal(string, ...Field) {}
func (nopLogger) With(...Field) Logger   { return nopLogger{} }
func (nopLogger) Named(string) Logger    { return nopLogger{} }
func (nopLogger) Sync() error            { return nil }

// LevelForStatus maps an HTTP status to the access-log severity.
func LevelForStatus(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warn"
	default:
		return "info"
	}
}

// LogAccess writes a strict HTTP access-log event.
func LogAccess(l Logger, e AccessEvent) {
	if l == nil {
		return
	}
	fields := e.Fields()
	switch LevelForStatus(e.Status) {
	case "error":
		l.Error(AccessLogMessage, fields...)
	case "warn":
		l.Warn(AccessLogMessage, fields...)
	default:
		l.Info(AccessLogMessage, fields...)
	}
}
