package obs

import (
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"time"
)

const userAgent = "auth-sdks-obs/1"

const (
	pathRemoteWrite = "/api/v1/prom/remote/write"
	pathOTLPTraces  = "/v1/traces"
	pathIngest      = "/ingest"
)

func joinAPI(base, suffix string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	suffix = "/" + strings.TrimPrefix(suffix, "/")
	if base == "" {
		return suffix
	}
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return base + suffix
}

func originURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, pathIngest)
	return strings.TrimRight(base, "/")
}

func basicAuthHeader(user, pass string) string {
	token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return "Basic " + token
}

func applyAuth(req *http.Request, cfg Config) {
	req.Header.Set("User-Agent", userAgent)
	if cfg.Username != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
}

func httpClient(cfg Config) *http.Client {
	if cfg.HTTPClient != nil {
		return cfg.HTTPClient
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: newIdleCappedTransport()}
}

func newIdleCappedTransport() *http.Transport {
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		tr := dt.Clone()
		tr.MaxIdleConns = 32
		tr.MaxIdleConnsPerHost = 4
		tr.IdleConnTimeout = 30 * time.Second
		return tr
	}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
