package logging

import (
	"context"
	"testing"
)

func TestNopDirect(t *testing.T) {
	t.Parallel()
	var n Nop
	n.Log(context.Background(), LevelInfo, "x")
	if got := n.With(String("a", "b")); got == nil {
		t.Fatal("with")
	}
	NewNop().Log(context.Background(), LevelError, "y", Err(nil))
}
