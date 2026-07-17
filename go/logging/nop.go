package logging

import "context"

// Nop is a no-op Logger Strategy.
type Nop struct{}

// NewNop returns a Logger that discards all events.
func NewNop() Logger { return Nop{} }

// Log implements Logger.
func (Nop) Log(context.Context, Level, string, ...Field) {}

// With implements Logger.
func (n Nop) With(...Field) Logger { return n }
