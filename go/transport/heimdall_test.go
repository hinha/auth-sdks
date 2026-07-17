package transport_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gojek/heimdall/v7"
	"github.com/hinha/auth-sdks/go/logging"
	"github.com/hinha/auth-sdks/go/transport"
)

type capturePlugin struct {
	starts, ends, errs int
}

func (p *capturePlugin) OnRequestStart(*http.Request)                         { p.starts++ }
func (p *capturePlugin) OnRequestEnd(*http.Request, *http.Response)            { p.ends++ }
func (p *capturePlugin) OnError(*http.Request, error)                          { p.errs++ }

func TestNewHeimdall_SuccessAndDo(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)

	cap := &capturePlugin{}
	client, err := transport.NewHeimdall(
		transport.WithConfig(transport.Config{
			Timeout:               time.Second,
			RetryCount:            0,
			BackoffInterval:       10 * time.Millisecond,
			MaximumJitterInterval: 5 * time.Millisecond,
		}),
		transport.WithLogger(logging.NewSlog(nil)),
		transport.WithServiceName("auth-service"),
		transport.WithCustomDoer(http.DefaultClient),
		transport.WithPlugin(cap),
	)
	if err != nil {
		t.Fatal(err)
	}

	res, err := client.Get(srv.URL+"/v1/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport.DrainBody(res.Body)
	if cap.starts == 0 || cap.ends == 0 {
		t.Fatalf("plugin starts=%d ends=%d", cap.starts, cap.ends)
	}
}

func TestNewHeimdall_Validation(t *testing.T) {
	t.Parallel()
	if _, err := transport.NewHeimdall(transport.WithConfig(transport.Config{Timeout: 0})); err == nil {
		t.Fatal("expected timeout error")
	}
	if _, err := transport.NewHeimdall(transport.WithConfig(transport.Config{
		Timeout:    time.Second,
		RetryCount: -1,
	})); err == nil {
		t.Fatal("expected retry error")
	}
}

func TestDrainBody(t *testing.T) {
	t.Parallel()
	transport.DrainBody(nil)
	transport.DrainBody(io.NopCloser(strings.NewReader("abc")))
}

func TestRequestLoggerPlugin(t *testing.T) {
	t.Parallel()
	plugin := transport.NewRequestLogger(nil, "")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/v1/token/x", nil)
	plugin.OnRequestStart(req)
	plugin.OnRequestEnd(req, &http.Response{StatusCode: 200})
	plugin.OnRequestEnd(req, &http.Response{StatusCode: 400})
	plugin.OnRequestEnd(req, &http.Response{StatusCode: 500})
	plugin.OnRequestEnd(req, nil)
	plugin.OnError(req, errors.New("boom"))

	// Empty path / nil URL branches via crafted request.
	req2 := &http.Request{Method: http.MethodGet, URL: nil}
	_ = transport.NewRequestLogger(logging.NewNop(), "svc")
	p := transport.NewRequestLogger(logging.NewNop(), "svc").(interface {
		OnRequestStart(*http.Request)
		OnRequestEnd(*http.Request, *http.Response)
		OnError(*http.Request, error)
	})
	// safePath covered via OnRequestStart with empty Path.
	req3 := httptest.NewRequest(http.MethodPost, "http://example.com", bytes.NewReader(nil))
	req3.URL.Path = ""
	p.OnRequestStart(req3)
	p.OnRequestEnd(req3, &http.Response{StatusCode: 201})
	_ = req2
	_ = heimdall.Plugin(plugin)
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := transport.DefaultConfig()
	if cfg.Timeout <= 0 || cfg.RetryCount < 0 {
		t.Fatalf("%+v", cfg)
	}
}
