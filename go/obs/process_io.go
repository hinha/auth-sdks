package obs

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricProcessIOReadBytes  = "process_io_read_bytes_total"
	metricProcessIOWriteBytes = "process_io_write_bytes_total"
	metricProcessIORchar      = "process_io_rchar_bytes_total"
	metricProcessIOWchar      = "process_io_wchar_bytes_total"
)

type procIO struct {
	RChar, WChar, ReadBytes, WriteBytes uint64
}

type processIOCollector struct {
	read       func() (procIO, error)
	readBytes  *prometheus.Desc
	writeBytes *prometheus.Desc
	rchar      *prometheus.Desc
	wchar      *prometheus.Desc
}

func newProcessIOCollector() prometheus.Collector {
	return newProcessIOCollectorForTest(readProcIO)
}

func newProcessIOCollectorForTest(read func() (procIO, error)) prometheus.Collector {
	return &processIOCollector{
		read: read,
		readBytes: prometheus.NewDesc(
			metricProcessIOReadBytes,
			"Number of bytes read from storage by the process (/proc/self/io read_bytes).",
			nil, nil,
		),
		writeBytes: prometheus.NewDesc(
			metricProcessIOWriteBytes,
			"Number of bytes written to storage by the process (/proc/self/io write_bytes).",
			nil, nil,
		),
		rchar: prometheus.NewDesc(
			metricProcessIORchar,
			"Number of bytes read by the process, including page cache (/proc/self/io rchar).",
			nil, nil,
		),
		wchar: prometheus.NewDesc(
			metricProcessIOWchar,
			"Number of bytes written by the process, including page cache (/proc/self/io wchar).",
			nil, nil,
		),
	}
}

func (c *processIOCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.readBytes
	ch <- c.writeBytes
	ch <- c.rchar
	ch <- c.wchar
}

func (c *processIOCollector) Collect(ch chan<- prometheus.Metric) {
	io, err := c.read()
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.readBytes, prometheus.CounterValue, float64(io.ReadBytes))
	ch <- prometheus.MustNewConstMetric(c.writeBytes, prometheus.CounterValue, float64(io.WriteBytes))
	ch <- prometheus.MustNewConstMetric(c.rchar, prometheus.CounterValue, float64(io.RChar))
	ch <- prometheus.MustNewConstMetric(c.wchar, prometheus.CounterValue, float64(io.WChar))
}
