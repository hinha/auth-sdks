//go:build !linux

package obs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultCollectors_OmitsProcessIO(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{})
	mfs := gatherAll(t, h.collectorSet())
	require.False(t, hasFamilyWithPrefix(mfs, "process_io_"),
		"process I/O is Linux-only; other GOOS must omit the series")
}
