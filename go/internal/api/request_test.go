package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read fail") }
func (errReadCloser) Close() error             { return nil }

func TestDoJSON_SuccessPaths(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "sa_x" {
			t.Fatalf("key=%q", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("Authorization") != "Bearer user-tok" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Custom") != "1" {
			t.Fatalf("custom=%q", r.Header.Get("X-Custom"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "OK",
			"code":    "PLT-ASP-200",
			"data":    map[string]any{"ok": true},
			"errors":  nil,
		})
	}))
	t.Cleanup(srv.Close)

	h := make(http.Header)
	h.Set("X-Custom", "1")
	r := &api.Requester{
		BaseURL: srv.URL,
		Doer:    http.DefaultClient,
		Logger:  logging.NewNop(),
		Header:  h,
	}
	var out map[string]any
	err := r.DoJSON(context.Background(), http.MethodPost, "/v1/x", map[string]string{"a": "b"}, &out,
		api.WithBearer("user-tok"),
		api.WithAPIKey("sa_x"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("%v", out)
	}
}

func TestDoJSON_RawBodyAndEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/null-data", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"OK","code":"200","data":null,"errors":null}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := &api.Requester{BaseURL: srv.URL, Doer: http.DefaultClient, Logger: logging.NewSlog(nil)}

	var jwks map[string]any
	if err := r.DoJSON(context.Background(), http.MethodGet, "/jwks", nil, &jwks, api.WithRawBody()); err != nil {
		t.Fatal(err)
	}
	if err := r.DoJSON(context.Background(), http.MethodGet, "/empty", nil, nil); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := r.DoJSON(context.Background(), http.MethodGet, "/null-data", nil, &out); err != nil {
		t.Fatal(err)
	}
}

func TestDoJSON_ErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		check  func(error) bool
	}{
		{400, func(err error) bool { var e *api.ValidationError; return errors.As(err, &e) }},
		{401, func(err error) bool { var e *api.UnauthorizedError; return errors.As(err, &e) }},
		{403, func(err error) bool { var e *api.ForbiddenError; return errors.As(err, &e) }},
		{500, func(err error) bool { var e *api.APIError; return errors.As(err, &e) }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"m","code":"C","errors":{"x":1}}`))
			}))
			t.Cleanup(srv.Close)
			r := &api.Requester{BaseURL: srv.URL, Doer: http.DefaultClient}
			err := r.DoJSON(context.Background(), http.MethodGet, "/e", nil, &map[string]any{})
			if !tc.check(err) {
				t.Fatalf("err=%v", err)
			}
			_ = err.Error()
		})
	}

	// No JSON body → fallback message/code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`not-json`))
	}))
	t.Cleanup(srv.Close)
	r := &api.Requester{BaseURL: srv.URL, Doer: http.DefaultClient}
	err := r.DoJSON(context.Background(), http.MethodGet, "/e", nil, &map[string]any{})
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 502 {
		t.Fatalf("err=%v", err)
	}
}

func TestDoJSON_NetworkAndDecodeErrors(t *testing.T) {
	t.Parallel()

	r := &api.Requester{
		BaseURL: "http://127.0.0.1:1",
		Doer: doerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial")
		}),
	}
	err := r.DoJSON(context.Background(), http.MethodGet, "/x", nil, nil)
	var netErr *api.NetworkError
	if !errors.As(err, &netErr) || netErr.Unwrap() == nil {
		t.Fatalf("err=%v", err)
	}

	r2 := &api.Requester{
		BaseURL: "http://example.com",
		Doer: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       errReadCloser{},
				Header:     make(http.Header),
			}, nil
		}),
	}
	if err := r2.DoJSON(context.Background(), http.MethodGet, "/x", nil, &map[string]any{}); err == nil {
		t.Fatal("expected read error")
	}

	r3 := &api.Requester{
		BaseURL: "http://example.com",
		Doer: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`not-json`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	if err := r3.DoJSON(context.Background(), http.MethodGet, "/x", nil, &map[string]any{}); err == nil {
		t.Fatal("expected decode envelope error")
	}

	// rawBody with invalid JSON after envelope fail path already covered; raw success invalid:
	r4 := &api.Requester{
		BaseURL: "http://example.com",
		Doer: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`not-json`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	if err := r4.DoJSON(context.Background(), http.MethodGet, "/x", nil, &map[string]any{}, api.WithRawBody()); err == nil {
		t.Fatal("expected raw decode error")
	}

	r5 := &api.Requester{
		BaseURL: "http://example.com",
		Doer: doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"message":"OK","code":"200","data":"oops","errors":null}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	var out map[string]any
	if err := r5.DoJSON(context.Background(), http.MethodGet, "/x", nil, &out); err == nil {
		t.Fatal("expected data decode error")
	}

	// API key only sets Authorization when bearer empty.
	seen := make(chan string, 1)
	r6 := &api.Requester{
		BaseURL: "http://example.com",
		Doer: doerFunc(func(req *http.Request) (*http.Response, error) {
			seen <- req.Header.Get("Authorization")
			return &http.Response{
				StatusCode: 204,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	_ = r6.DoJSON(context.Background(), http.MethodPost, "/x", nil, nil, api.WithAPIKey("sa_only"))
	if got := <-seen; got != "Bearer sa_only" {
		t.Fatalf("auth=%q", got)
	}
}

func TestDoJSON_MarshalError(t *testing.T) {
	t.Parallel()
	r := &api.Requester{BaseURL: "http://example.com", Doer: http.DefaultClient}
	err := r.DoJSON(context.Background(), http.MethodPost, "/x", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("err=%v", err)
	}
}

func TestDoJSON_InvalidURL(t *testing.T) {
	t.Parallel()
	r := &api.Requester{
		BaseURL: "http://example.com",
		Doer:    http.DefaultClient,
	}
	err := r.DoJSON(context.Background(), "GET", "/x\nbad", nil, nil)
	if err == nil {
		t.Fatal("expected create request error")
	}
}

func TestTypedErrorMethods(t *testing.T) {
	t.Parallel()
	base := &api.APIError{StatusCode: 401, Code: "401", Message: "m"}
	u := &api.UnauthorizedError{APIError: base}
	f := &api.ForbiddenError{APIError: base}
	v := &api.ValidationError{APIError: base}
	n := &api.NetworkError{Op: "GET /z", Err: errors.New("e")}

	for _, err := range []error{u, f, v, n} {
		if err.Error() == "" {
			t.Fatalf("empty: %T", err)
		}
	}
	if u.Unwrap() != base || f.Unwrap() != base || v.Unwrap() != base || n.Unwrap() == nil {
		t.Fatal("unwrap")
	}
	var nilAPI *api.APIError
	if nilAPI.Error() != "api: unknown error" {
		t.Fatal(nilAPI.Error())
	}
}
