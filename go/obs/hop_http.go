package obs

import (
	"bufio"
	"net"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// HTTPOption configures HTTPMiddleware.
type HTTPOption func(*httpOptions)

type httpOptions struct {
	route func(*http.Request) string
	peer  string
}

// WithRoute supplies a low-cardinality route template (Echo c.Path(), Gin full
// path). The raw URL path is never the default operation.
func WithRoute(fn func(*http.Request) string) HTTPOption {
	return func(o *httpOptions) { o.route = fn }
}

// TransportOption configures WrapTransport.
type TransportOption func(*transportOptions)

type transportOptions struct {
	peer string
	path string
}

// WithPeer sets the logical destination for Tempo service graphs. It must match
// the downstream process's Config.Service (its resource service.name), not a
// raw hostname or a URL with query.
func WithPeer(peer string) TransportOption {
	return func(o *transportOptions) { o.peer = peer }
}

// WithPathTemplate adds an optional path template to the client operation
// (METHOD + template). Do not pass a raw URL with ids or query strings.
func WithPathTemplate(path string) TransportOption {
	return func(o *transportOptions) { o.path = path }
}

// HTTPMiddleware records an http_server hop and starts a SpanKindServer span
// named "METHOD route". Build it once at process start; do not reconstruct per
// request. Nested WrapTransport on the same Handle shares one histogram family.
func (h *Handle) HTTPMiddleware(opts ...HTTPOption) func(http.Handler) http.Handler {
	cfg := httpOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		}
		if h == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			if p := h.textMapPropagator(); p != nil {
				ctx = p.Extract(ctx, propagation.HeaderCarrier(r.Header))
			}
			hop := Hop{
				Component: hopComponentHTTPServer,
				Operation: serverOperation(r, cfg.route),
				Peer:      cfg.peer,
			}
			ctx, timer := h.Start(ctx, hop)
			r = r.WithContext(ctx)
			sw := wrapHopWriter(w)
			defer func() {
				if rec := recover(); rec != nil {
					timer.endHTTP(http.StatusInternalServerError, errHopPanic)
					panic(rec)
				}
				timer.endHTTP(statusCode(sw), nil)
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

// WrapTransport records http_client hops and injects W3C traceparent.
// Nil base uses http.DefaultTransport. Wrap once at startup; wrapping an
// already-wrapped transport double-counts duration.
func (h *Handle) WrapTransport(base http.RoundTripper, opts ...TransportOption) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if h == nil {
		return base
	}
	cfg := transportOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &hopTransport{h: h, base: base, cfg: cfg}
}

type hopTransport struct {
	h    *Handle
	base http.RoundTripper
	cfg  transportOptions
}

func (t *hopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.h == nil || t.base == nil {
		if t != nil && t.base != nil {
			return t.base.RoundTrip(req)
		}
		return http.DefaultTransport.RoundTrip(req)
	}
	if req == nil {
		return t.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	op := clientOperation(req, t.cfg.path)
	peer := t.cfg.peer
	if peer == "" && req.URL != nil {
		peer = req.URL.Host
	}
	ctx, timer := t.h.Start(req.Context(), Hop{
		Component: hopComponentHTTPClient,
		Operation: op,
		Peer:      peer,
	})
	if timer.span != nil && req.URL != nil {
		if host := req.URL.Hostname(); host != "" {
			timer.span.SetAttributes(semconv.ServerAddress(host))
		}
	}
	req = req.WithContext(ctx)
	if p := t.h.textMapPropagator(); p != nil {
		p.Inject(ctx, propagation.HeaderCarrier(req.Header))
	}
	resp, err := t.base.RoundTrip(req)
	code := 0
	if resp != nil {
		code = resp.StatusCode
	}
	timer.endHTTP(code, err)
	return resp, err
}

func (h *Handle) textMapPropagator() propagation.TextMapPropagator {
	if h == nil {
		return defaultTextMapPropagator()
	}
	if h.propagator != nil {
		return h.propagator
	}
	return defaultTextMapPropagator()
}

func serverOperation(r *http.Request, routeFn func(*http.Request) string) string {
	method := requestMethod(r)
	route := "unmatched"
	if routeFn != nil && r != nil {
		if s := strings.TrimSpace(routeFn(r)); s != "" {
			route = s
		}
	}
	return boundLabel(method+" "+route, maxHopOperation)
}

func clientOperation(r *http.Request, pathTemplate string) string {
	method := requestMethod(r)
	pathTemplate = strings.TrimSpace(pathTemplate)
	if pathTemplate == "" {
		return boundLabel(method, maxHopOperation)
	}
	return boundLabel(method+" "+pathTemplate, maxHopOperation)
}

func requestMethod(r *http.Request) string {
	if r == nil || strings.TrimSpace(r.Method) == "" {
		return hopStatusUnknown
	}
	return r.Method
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func wrapHopWriter(w http.ResponseWriter) *statusWriter {
	if w == nil {
		w = discardResponseWriter{}
	}
	return &statusWriter{ResponseWriter: w}
}

func statusCode(w *statusWriter) int {
	if w == nil || w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *statusWriter) WriteHeader(code int) {
	if w == nil {
		return
	}
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w == nil {
		return 0, http.ErrNotSupported
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if w == nil {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w == nil {
		return nil, nil, http.ErrNotSupported
	}
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if w == nil {
		return http.ErrNotSupported
	}
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	if w == nil {
		return nil
	}
	return w.ResponseWriter
}

type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header         { return make(http.Header) }
func (discardResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponseWriter) WriteHeader(int)             {}
