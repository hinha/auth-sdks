package obs

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWarn_DedupesPerKeyNotPerMessage(t *testing.T) {
	t.Parallel()
	var (
		mu   sync.Mutex
		msgs []string
	)
	h := &Handle{cfg: Config{Logger: func(m string) {
		mu.Lock()
		msgs = append(msgs, m)
		mu.Unlock()
	}}}

	h.warn("gather failed", "first")
	// Same condition, different detail: still one report, otherwise a fail-open
	// loop floods the log with a new line every interval.
	h.warn("gather failed", "second")
	h.warn("write failed", "")
	// A distinct condition must still be reported; an unrelated later failure
	// being silenced is worse than the noise.
	h.warn("pyroscope start failed", "boom")
	h.warn("gather failed", "third")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{
		"gather failed: first",
		"write failed",
		"pyroscope start failed: boom",
	}, msgs)
}

func TestWarn_NilHandle(t *testing.T) {
	t.Parallel()
	var h *Handle
	require.NotPanics(t, func() { h.warn("k", "v") })
}
