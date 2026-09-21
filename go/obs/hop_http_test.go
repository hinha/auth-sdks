package obs

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

func TestHTTPMiddleware_UsesRouteTemplateNotRawPath(t *testing.T) {
	h, sr := recordingHandle(t)
	inner := h.HTTPMiddleware(WithRoute(func(r *http.Request) string {
		return "/v1/users/:id"
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.com/v1/users/123?x=1", nil)
	rr := httptest.NewRecorder()
	inner.ServeHTTP(rr, req)

	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPServer, "GET /v1/users/:id", hopStatusUnknown, "200"))
	require.Equal(t, uint64(0), hopSampleCount(t, h, hopComponentHTTPServer, "GET /v1/users/123?x=1", hopStatusUnknown, "200"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "GET /v1/users/:id", spans[0].Name())
	require.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestHTTPMiddleware_DefaultOperationIsUnmatched(t *testing.T) {
	h, _ := recordingHandle(t)
	inner := h.HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.com/users/123?x=1", nil)
	inner.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPServer, "GET unmatched", hopStatusUnknown, "200"))
	require.Equal(t, uint64(0), hopSampleCount(t, h, hopComponentHTTPServer, "GET /users/123?x=1", hopStatusUnknown, "200"))
}

func TestHTTPMiddleware_5xxSetsSpanError(t *testing.T) {
	h, sr := recordingHandle(t)
	inner := h.HTTPMiddleware(WithRoute(func(*http.Request) string { return "/boom" }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)
	inner.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/boom", nil))
	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPServer, "GET /boom", hopStatusUnknown, "500"))
	spans := sr.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestHTTPMiddleware_NilHandleAndNilNext(t *testing.T) {
	ran := false
	mw := (*Handle)(nil).HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		w.WriteHeader(http.StatusNoContent)
	}))
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	require.True(t, ran)

	passthrough := (*Handle)(nil).HTTPMiddleware()(nil)
	require.NotPanics(t, func() {
		passthrough.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	})
}

func TestHTTPMiddleware_DoubleRegisterOneFamily(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	h1 := h.HTTPMiddleware()
	h2 := h.HTTPMiddleware()
	handler := h1(h2(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	require.Equal(t, 1, hopFamilyCount(t, h))
}

func TestWrapTransport_DoesNotUseRawURL(t *testing.T) {
	h, _ := recordingHandle(t)
	var gotPath, gotTraceparent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	client := &http.Client{Transport: h.WrapTransport(nil, WithPeer("upstream"))}
	ctx, timer := h.Start(context.Background(), Hop{
		Component: hopComponentHTTPServer,
		Operation: "GET /v1/proxy",
		Peer:      hopStatusUnknown,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/users/123?x=1", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	timer.endHTTP(http.StatusOK, nil)

	require.Contains(t, gotPath, "/users/123")
	require.NotEmpty(t, gotTraceparent)
	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPClient, "GET", "upstream", "200"))
	require.Equal(t, uint64(0), hopSampleCount(t, h, hopComponentHTTPClient, "GET /users/123?x=1", "upstream", "200"))
}

func TestHTTPMiddleware_AndWrapTransport_NestedSpans(t *testing.T) {
	h, sr := recordingHandle(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	client := &http.Client{Transport: h.WrapTransport(http.DefaultTransport, WithPeer("upstream"))}
	handler := h.HTTPMiddleware(WithRoute(func(*http.Request) string { return "/v1/reports" }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.URL+"/x", nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			require.NoError(t, err)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			w.WriteHeader(http.StatusOK)
		}),
	)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/reports")
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPServer, "GET /v1/reports", hopStatusUnknown, "200"))
	require.Equal(t, uint64(1), hopSampleCount(t, h, hopComponentHTTPClient, "GET", "upstream", "200"))

	spans := sr.Ended()
	require.Len(t, spans, 2)
	var cID, sID string
	for _, s := range spans {
		attrs := attrMap(s.Attributes())
		switch attrs["component"] {
		case hopComponentHTTPClient:
			require.Equal(t, codes.Unset, s.Status().Code)
			require.Equal(t, "upstream", attrs["peer.service"])
			cID = s.Parent().SpanID().String()
		case hopComponentHTTPServer:
			_, hasPeer := attrs["peer.service"]
			require.False(t, hasPeer)
			sID = s.SpanContext().SpanID().String()
		}
	}
	require.Equal(t, sID, cID)
}

func TestHTTPMiddleware_ForwardsFlusher(t *testing.T) {
	h, err := New(Config{DisableDefaultCollectors: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	flushed := false
	handler := h.HTTPMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		require.True(t, ok)
		f.Flush()
		flushed = true
	}))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.True(t, flushed)
}

func TestStatusWriter_HijackerPassthrough(t *testing.T) {
	inner := hijackStub{}
	var w http.ResponseWriter = wrapHopWriter(inner)
	hj, ok := w.(http.Hijacker)
	require.True(t, ok)
	_, _, err := hj.Hijack()
	require.NoError(t, err)
}

type hijackStub struct {
	http.ResponseWriter
}

func (hijackStub) Header() http.Header       { return make(http.Header) }
func (hijackStub) Write([]byte) (int, error) { return 0, nil }
func (hijackStub) WriteHeader(int)           {}
func (hijackStub) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func TestWrapTransport_NilHandleReturnsBase(t *testing.T) {
	base := http.DefaultTransport
	got := (*Handle)(nil).WrapTransport(base)
	require.Equal(t, base, got)
	gotNil := (*Handle)(nil).WrapTransport(nil)
	require.Equal(t, http.DefaultTransport, gotNil)
}

func TestHTTPExtractsIncomingTraceparent(t *testing.T) {
	h, sr := recordingHandle(t)
	prop := defaultTextMapPropagator()
	parentCtx, parent := h.Tracer(hopTracerName).Start(context.Background(), "parent")
	header := http.Header{}
	prop.Inject(parentCtx, propagation.HeaderCarrier(header))
	parent.End()

	inner := h.HTTPMiddleware(WithRoute(func(*http.Request) string { return "/v1/me" }))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/v1/me", nil)
	req.Header = header
	inner.ServeHTTP(httptest.NewRecorder(), req)

	var serverSpanID string
	for _, s := range sr.Ended() {
		if s.Name() == "GET /v1/me" {
			require.Equal(t, parent.SpanContext().TraceID(), s.SpanContext().TraceID())
			serverSpanID = s.SpanContext().SpanID().String()
		}
	}
	require.NotEmpty(t, serverSpanID)
}
