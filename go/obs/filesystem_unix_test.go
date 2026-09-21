//go:build linux || darwin

package obs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultCollectors_EmitsFilesystemForCwd(t *testing.T) {
	t.Parallel()
	h := newTestHandle(t, Config{})
	mfs := gatherAll(t, h.collectorSet())
	require.Greater(t, gaugeLabeled(t, mfs, metricProcessFilesystemAvail, "path", "."), 0.0)
	require.Greater(t, gaugeLabeled(t, mfs, metricProcessFilesystemSize, "path", "."), 0.0)
}

func TestDefaultCollectors_FilesystemPathsReplacesCwd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := newTestHandle(t, Config{FilesystemPaths: []string{dir}})
	mfs := gatherAll(t, h.collectorSet())
	require.Greater(t, gaugeLabeled(t, mfs, metricProcessFilesystemAvail, "path", dir), 0.0)
	require.False(t, hasLabeled(mfs, metricProcessFilesystemAvail, "path", "."))
}
