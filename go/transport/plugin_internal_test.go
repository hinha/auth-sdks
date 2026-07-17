package transport

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/hinha/auth-sdk-go/logging"
)

func TestSafePathAndLatency(t *testing.T) {
	t.Parallel()
	if safePath(nil) != "" {
		t.Fatal("nil req")
	}
	if safePath(&http.Request{}) != "" {
		t.Fatal("nil url")
	}
	u, _ := url.Parse("http://x/v1/hello")
	req := &http.Request{URL: u}
	if safePath(req) != "/v1/hello" {
		t.Fatal(safePath(req))
	}
	u2, _ := url.Parse("http://x")
	u2.Path = ""
	if safePath(&http.Request{URL: u2}) != "/" {
		t.Fatal("empty path")
	}
	if latencyMS(context.Background()) != 0 {
		t.Fatal("no start")
	}
	ctx := context.WithValue(context.Background(), reqStartKey, time.Now().Add(-time.Millisecond))
	if latencyMS(ctx) < 0 {
		t.Fatal("latency")
	}
	p := NewRequestLogger(logging.NewNop(), "svc").(*requestLogger)
	req3, _ := http.NewRequest(http.MethodGet, "http://x/path", nil)
	p.OnRequestStart(req3)
	if latencyMS(req3.Context()) < 0 {
		t.Fatal("after start")
	}
}
