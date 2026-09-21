package obs

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestHopSemconvAttrs_Redis(t *testing.T) {
	t.Parallel()
	attrs := attrMap(hopSemconvAttrs(Hop{Component: "redis", Operation: "GET", Peer: "cache"}))
	require.Equal(t, "cache", attrs["peer.service"])
	require.Equal(t, "redis", attrs["db.system"])
	_, hasRoute := attrs["http.route"]
	require.False(t, hasRoute)
}

func TestHopSemconvAttrs_DBPostgres(t *testing.T) {
	t.Parallel()
	attrs := attrMap(hopSemconvAttrs(Hop{Component: "db", Operation: "SELECT users", Peer: "postgres"}))
	require.Equal(t, "postgres", attrs["peer.service"])
	require.Equal(t, "postgresql", attrs["db.system"])
}

func TestHopSemconvAttrs_DBSystemFromPeer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		peer string
		sys  string
	}{
		{peer: "postgresql", sys: "postgresql"},
		{peer: "mysql", sys: "mysql"},
		{peer: "sqlite", sys: "sqlite"},
		{peer: "warehouse", sys: "other_sql"},
	}
	for _, tc := range cases {
		t.Run(tc.peer, func(t *testing.T) {
			t.Parallel()
			attrs := attrMap(hopSemconvAttrs(Hop{Component: "db", Operation: "SELECT", Peer: tc.peer}))
			require.Equal(t, tc.peer, attrs["peer.service"])
			require.Equal(t, tc.sys, attrs["db.system"])
		})
	}
}

func TestHopSemconvAttrs_HTTPClient(t *testing.T) {
	t.Parallel()
	attrs := attrMap(hopSemconvAttrs(Hop{Component: hopComponentHTTPClient, Operation: "GET", Peer: "x-engine"}))
	require.Equal(t, "x-engine", attrs["peer.service"])
	_, hasDB := attrs["db.system"]
	require.False(t, hasDB)
}

func TestHopSemconvAttrs_HTTPServerOmitsPeerService(t *testing.T) {
	t.Parallel()
	attrs := attrMap(hopSemconvAttrs(Hop{
		Component: hopComponentHTTPServer,
		Operation: "GET /v1/users/:id",
		Peer:      hopStatusUnknown,
	}))
	_, hasPeer := attrs["peer.service"]
	require.False(t, hasPeer)
	require.Equal(t, "GET", attrs["http.request.method"])
	require.Equal(t, "/v1/users/:id", attrs["http.route"])
}

func TestHopSemconvAttrs_UnknownPeerOmitsPeerService(t *testing.T) {
	t.Parallel()
	attrs := attrMap(hopSemconvAttrs(Hop{Component: "redis", Operation: "GET", Peer: hopStatusUnknown}))
	_, hasPeer := attrs["peer.service"]
	require.False(t, hasPeer)
	require.Equal(t, "redis", attrs["db.system"])
}

func TestObserve_RedisEmitsPeerServiceAndClientKind(t *testing.T) {
	h, sr := recordingHandle(t)
	require.NoError(t, h.Observe(context.Background(), Hop{
		Component: "redis",
		Operation: "GET",
		Peer:      "cache",
	}, func(context.Context) error { return nil }))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	attrs := attrMap(spans[0].Attributes())
	require.Equal(t, "cache", attrs["peer.service"])
	require.Equal(t, "redis", attrs["db.system"])
	require.Equal(t, "redis", attrs["component"])
	require.Equal(t, "cache", attrs["peer"])
}

func TestObserve_DBPostgresEmitsPeerService(t *testing.T) {
	h, sr := recordingHandle(t)
	require.NoError(t, h.Observe(context.Background(), Hop{
		Component: "db",
		Operation: "SELECT users",
		Peer:      "postgres",
	}, func(context.Context) error { return nil }))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	attrs := attrMap(spans[0].Attributes())
	require.Equal(t, "postgres", attrs["peer.service"])
	require.Equal(t, "postgresql", attrs["db.system"])
}

func TestObserve_HTTPServerOmitsPeerService(t *testing.T) {
	h, sr := recordingHandle(t)
	require.NoError(t, h.Observe(context.Background(), Hop{
		Component: hopComponentHTTPServer,
		Operation: "GET /v1/users/:id",
	}, func(context.Context) error { return nil }))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, trace.SpanKindServer, spans[0].SpanKind())
	attrs := attrMap(spans[0].Attributes())
	_, hasPeer := attrs["peer.service"]
	require.False(t, hasPeer)
	require.Equal(t, "GET", attrs["http.request.method"])
	require.Equal(t, "/v1/users/:id", attrs["http.route"])
	require.Equal(t, hopComponentHTTPServer, attrs["component"])
}

func TestWrapTransport_PeerServiceAndServerAddress(t *testing.T) {
	h, sr := recordingHandle(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	u, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	client := &http.Client{Transport: h.WrapTransport(nil, WithPeer("upstream"))}
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/users/123?x=1", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	attrs := attrMap(spans[0].Attributes())
	require.Equal(t, "upstream", attrs["peer.service"])
	require.Equal(t, u.Hostname(), attrs["server.address"])
	require.Equal(t, "upstream", attrs["peer"])
	require.Equal(t, hopComponentHTTPClient, attrs["component"])
	_, hasQuery := attrs["url.full"]
	require.False(t, hasQuery)
}
