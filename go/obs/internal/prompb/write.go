package prompb

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// WriteRequest is the Prometheus remote-write v1 payload (subset).
//
// Field numbers follow prompb/remote.proto: timeseries = 1, reserved 2,
// metadata = 3.
type WriteRequest struct {
	Timeseries []TimeSeries
	Metadata   []MetricMetadata
}

type TimeSeries struct {
	Labels  []Label
	Samples []Sample
}

type Label struct {
	Name  string
	Value string
}

type Sample struct {
	Value     float64
	Timestamp int64
}

// MetricType mirrors prometheus.MetricMetadata.MetricType. Its numbering is NOT
// the same as io.prometheus.client.MetricType, which is what the gathered
// client_model families use.
type MetricType int32

const (
	MetricTypeUnknown        MetricType = 0
	MetricTypeCounter        MetricType = 1
	MetricTypeGauge          MetricType = 2
	MetricTypeHistogram      MetricType = 3
	MetricTypeGaugeHistogram MetricType = 4
	MetricTypeSummary        MetricType = 5
	MetricTypeInfo           MetricType = 6
	MetricTypeStateSet       MetricType = 7
)

// MetricMetadata mirrors prometheus.MetricMetadata. Its field numbers are
// 1, 2, 4, 5: field 3 does not exist in that message.
type MetricMetadata struct {
	Type             MetricType
	MetricFamilyName string
	Help             string
	Unit             string
}

func Marshal(wr WriteRequest) []byte {
	var buf []byte
	for _, ts := range wr.Timeseries {
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, marshalTS(ts))
	}
	for _, md := range wr.Metadata {
		buf = protowire.AppendTag(buf, 3, protowire.BytesType)
		buf = protowire.AppendBytes(buf, marshalMetadata(md))
	}
	return buf
}

func marshalMetadata(md MetricMetadata) []byte {
	var buf []byte
	if md.Type != MetricTypeUnknown {
		buf = protowire.AppendTag(buf, 1, protowire.VarintType)
		buf = protowire.AppendVarint(buf, uint64(md.Type))
	}
	if md.MetricFamilyName != "" {
		buf = protowire.AppendTag(buf, 2, protowire.BytesType)
		buf = protowire.AppendString(buf, md.MetricFamilyName)
	}
	if md.Help != "" {
		buf = protowire.AppendTag(buf, 4, protowire.BytesType)
		buf = protowire.AppendString(buf, md.Help)
	}
	if md.Unit != "" {
		buf = protowire.AppendTag(buf, 5, protowire.BytesType)
		buf = protowire.AppendString(buf, md.Unit)
	}
	return buf
}

func marshalTS(ts TimeSeries) []byte {
	var buf []byte
	for _, l := range ts.Labels {
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, marshalLabel(l))
	}
	for _, s := range ts.Samples {
		buf = protowire.AppendTag(buf, 2, protowire.BytesType)
		buf = protowire.AppendBytes(buf, marshalSample(s))
	}
	return buf
}

func marshalLabel(l Label) []byte {
	var buf []byte
	buf = protowire.AppendTag(buf, 1, protowire.BytesType)
	buf = protowire.AppendString(buf, l.Name)
	buf = protowire.AppendTag(buf, 2, protowire.BytesType)
	buf = protowire.AppendString(buf, l.Value)
	return buf
}

func marshalSample(s Sample) []byte {
	var buf []byte
	buf = protowire.AppendTag(buf, 1, protowire.Fixed64Type)
	buf = protowire.AppendFixed64(buf, math.Float64bits(s.Value))
	buf = protowire.AppendTag(buf, 2, protowire.VarintType)
	buf = protowire.AppendVarint(buf, uint64(s.Timestamp))
	return buf
}

func Unmarshal(b []byte) (WriteRequest, error) {
	var wr WriteRequest
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return wr, fmt.Errorf("prompb: tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		if typ != protowire.BytesType || (num != 1 && num != 3) {
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return wr, fmt.Errorf("prompb: skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
			continue
		}
		payload, n := protowire.ConsumeBytes(b)
		if n < 0 {
			return wr, fmt.Errorf("prompb: field %d: %v", num, protowire.ParseError(n))
		}
		b = b[n:]
		switch num {
		case 1:
			ts, err := unmarshalTS(payload)
			if err != nil {
				return wr, err
			}
			wr.Timeseries = append(wr.Timeseries, ts)
		case 3:
			md, err := unmarshalMetadata(payload)
			if err != nil {
				return wr, err
			}
			wr.Metadata = append(wr.Metadata, md)
		}
	}
	return wr, nil
}

func unmarshalMetadata(b []byte) (MetricMetadata, error) {
	var md MetricMetadata
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return md, fmt.Errorf("prompb: metadata tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return md, fmt.Errorf("prompb: metadata type: %v", protowire.ParseError(n))
			}
			md.Type = MetricType(v)
			b = b[n:]
		case typ == protowire.BytesType:
			s, n := protowire.ConsumeString(b)
			if n < 0 {
				return md, fmt.Errorf("prompb: metadata string: %v", protowire.ParseError(n))
			}
			b = b[n:]
			switch num {
			case 2:
				md.MetricFamilyName = s
			case 4:
				md.Help = s
			case 5:
				md.Unit = s
			}
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return md, fmt.Errorf("prompb: metadata skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
		}
	}
	return md, nil
}

func unmarshalTS(b []byte) (TimeSeries, error) {
	var ts TimeSeries
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return ts, fmt.Errorf("prompb: ts tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		if typ != protowire.BytesType {
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return ts, fmt.Errorf("prompb: ts skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
			continue
		}
		payload, n := protowire.ConsumeBytes(b)
		if n < 0 {
			return ts, fmt.Errorf("prompb: ts bytes: %v", protowire.ParseError(n))
		}
		b = b[n:]
		switch num {
		case 1:
			l, err := unmarshalLabel(payload)
			if err != nil {
				return ts, err
			}
			ts.Labels = append(ts.Labels, l)
		case 2:
			s, err := unmarshalSample(payload)
			if err != nil {
				return ts, err
			}
			ts.Samples = append(ts.Samples, s)
		}
	}
	return ts, nil
}

func unmarshalLabel(b []byte) (Label, error) {
	var l Label
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return l, fmt.Errorf("prompb: label tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		if typ != protowire.BytesType {
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return l, fmt.Errorf("prompb: label skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
			continue
		}
		s, n := protowire.ConsumeString(b)
		if n < 0 {
			return l, fmt.Errorf("prompb: label string: %v", protowire.ParseError(n))
		}
		b = b[n:]
		switch num {
		case 1:
			l.Name = s
		case 2:
			l.Value = s
		}
	}
	return l, nil
}

func unmarshalSample(b []byte) (Sample, error) {
	var s Sample
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return s, fmt.Errorf("prompb: sample tag: %v", protowire.ParseError(n))
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.Fixed64Type:
			v, n := protowire.ConsumeFixed64(b)
			if n < 0 {
				return s, fmt.Errorf("prompb: sample value: %v", protowire.ParseError(n))
			}
			s.Value = math.Float64frombits(v)
			b = b[n:]
		case num == 2 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return s, fmt.Errorf("prompb: sample ts: %v", protowire.ParseError(n))
			}
			s.Timestamp = int64(v)
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return s, fmt.Errorf("prompb: sample skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
		}
	}
	return s, nil
}
