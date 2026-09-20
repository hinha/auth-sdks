package prompb

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// WriteRequest is the Prometheus remote-write v1 payload (subset).
type WriteRequest struct {
	Timeseries []TimeSeries
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

func Marshal(wr WriteRequest) []byte {
	var buf []byte
	for _, ts := range wr.Timeseries {
		buf = protowire.AppendTag(buf, 1, protowire.BytesType)
		buf = protowire.AppendBytes(buf, marshalTS(ts))
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
		if num != 1 || typ != protowire.BytesType {
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return wr, fmt.Errorf("prompb: skip: %v", protowire.ParseError(n))
			}
			b = b[n:]
			continue
		}
		payload, n := protowire.ConsumeBytes(b)
		if n < 0 {
			return wr, fmt.Errorf("prompb: timeseries: %v", protowire.ParseError(n))
		}
		b = b[n:]
		ts, err := unmarshalTS(payload)
		if err != nil {
			return wr, err
		}
		wr.Timeseries = append(wr.Timeseries, ts)
	}
	return wr, nil
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
