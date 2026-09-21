package obs

import (
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricProcessFilesystemAvail = "process_filesystem_avail_bytes"
	metricProcessFilesystemSize  = "process_filesystem_size_bytes"
	maxFilesystemPaths           = 8
	fsPathLabel                  = "path"
)

type fsStat struct {
	Avail, Size uint64
}

type filesystemCollector struct {
	paths  []string
	statfs func(path string) (fsStat, error)
	avail  *prometheus.Desc
	size   *prometheus.Desc
}

func newFilesystemCollector(paths []string) prometheus.Collector {
	return newFilesystemCollectorForTest(paths, statfsPath)
}

func newFilesystemCollectorForTest(paths []string, statfs func(path string) (fsStat, error)) prometheus.Collector {
	return &filesystemCollector{
		paths:  normalizeFilesystemPaths(paths),
		statfs: statfs,
		avail: prometheus.NewDesc(
			metricProcessFilesystemAvail,
			"Filesystem space available to an unprivileged process (statfs Bavail * Bsize).",
			[]string{fsPathLabel}, nil,
		),
		size: prometheus.NewDesc(
			metricProcessFilesystemSize,
			"Filesystem size in bytes (statfs Blocks * Bsize).",
			[]string{fsPathLabel}, nil,
		),
	}
}

func normalizeFilesystemPaths(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	out := make([]string, 0, maxFilesystemPaths)
	seen := make(map[string]struct{}, maxFilesystemPaths)
	for _, p := range paths {
		p = filepath.Clean(p)
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) == maxFilesystemPaths {
			break
		}
	}
	if len(out) == 0 {
		return []string{"."}
	}
	return out
}

func (c *filesystemCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.avail
	ch <- c.size
}

func (c *filesystemCollector) Collect(ch chan<- prometheus.Metric) {
	for _, p := range c.paths {
		st, err := c.statfs(p)
		if err != nil {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.avail, prometheus.GaugeValue, float64(st.Avail), p)
		ch <- prometheus.MustNewConstMetric(c.size, prometheus.GaugeValue, float64(st.Size), p)
	}
}
