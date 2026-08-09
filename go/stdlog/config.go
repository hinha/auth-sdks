package stdlog

import (
	"fmt"
	"strings"
	"time"
)

// FormatJSON and FormatConsole are supported Config.Format values.
const (
	FormatJSON    = "json"
	FormatConsole = "console"
)

// Config configures a service logger factory.
type Config struct {
	// Service is attached to every log event (required for New*).
	Service string
	// Level is debug|info|warn|error (default info).
	Level string
	// Format is json|console (default json).
	Format string
	// TimeLocation is an IANA zone name (optional; defaults to local).
	TimeLocation string
}

func (c Config) normalized() (Config, error) {
	out := c
	out.Service = strings.TrimSpace(out.Service)
	if out.Service == "" {
		return out, fmt.Errorf("stdlog: Config.Service is required")
	}
	out.Level = strings.ToLower(strings.TrimSpace(out.Level))
	if out.Level == "" {
		out.Level = "info"
	}
	switch out.Level {
	case "debug", "info", "warn", "error":
	default:
		return out, fmt.Errorf("stdlog: unsupported level %q", c.Level)
	}
	out.Format = strings.ToLower(strings.TrimSpace(out.Format))
	if out.Format == "" {
		out.Format = FormatJSON
	}
	if out.Format == "pretty" {
		out.Format = FormatConsole
	}
	switch out.Format {
	case FormatJSON, FormatConsole:
	default:
		return out, fmt.Errorf("stdlog: unsupported format %q", c.Format)
	}
	return out, nil
}

func (c Config) location() *time.Location {
	if strings.TrimSpace(c.TimeLocation) == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(c.TimeLocation)
	if err != nil {
		return time.Local
	}
	return loc
}
