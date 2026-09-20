package prompb

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestUnmarshal_SkipsUnknownAndRejectsJunk(t *testing.T) {
	t.Parallel()
	in := WriteRequest{Timeseries: []TimeSeries{{
		Labels:  []Label{{Name: "n", Value: "v"}},
		Samples: []Sample{{Value: 1, Timestamp: 2}},
	}}}
	raw := Marshal(in)
	extra := protowire.AppendTag(nil, 2, protowire.VarintType)
	extra = protowire.AppendVarint(extra, 7)
	out, err := Unmarshal(append(raw, extra...))
	require.NoError(t, err)
	require.Equal(t, in, out)

	_, err = Unmarshal([]byte{0xff})
	require.Error(t, err)
	_, err = Unmarshal([]byte{0x0a, 0x7f}) // truncated timeseries
	require.Error(t, err)
}

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	t.Parallel()
	in := WriteRequest{Timeseries: []TimeSeries{{
		Labels: []Label{
			{Name: "__name__", Value: "jobs_total"},
			{Name: "env", Value: "prod"},
			{Name: "service", Value: "money-tracker"},
		},
		Samples: []Sample{{Value: 3, Timestamp: 1_700_000_000_000}},
	}}}
	raw := Marshal(in)
	out, err := Unmarshal(raw)
	require.NoError(t, err)
	require.Equal(t, in, out)
}
