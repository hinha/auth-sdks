package obs

import (
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPClient_CustomAndJoinEmpty(t *testing.T) {
	t.Parallel()
	c := &http.Client{Timeout: time.Second}
	require.Equal(t, c, httpClient(Config{HTTPClient: c}))
	require.Equal(t, "/v1/traces", joinAPI("", "/v1/traces"))
	require.Equal(t, "", originURL("   "))
}

func TestJoinAPI_NoDoubleSuffix(t *testing.T) {
	t.Parallel()
	base := "https://logs.example.com"
	require.Equal(t, "https://logs.example.com/api/v1/prom/remote/write", joinAPI(base, "/api/v1/prom/remote/write"))
	require.Equal(t, "https://logs.example.com/api/v1/prom/remote/write", joinAPI(base+"/api/v1/prom/remote/write", "/api/v1/prom/remote/write"))
	require.Equal(t, "https://logs.example.com/v1/traces", joinAPI(base+"/", "/v1/traces"))
}

func TestOriginURL_StripsIngest(t *testing.T) {
	t.Parallel()
	require.Equal(t, "https://logs.example.com", originURL("https://logs.example.com/ingest"))
	require.Equal(t, "https://logs.example.com", originURL("https://logs.example.com/"))
}

func TestApplyAuth_SetsBasicAndUserAgent(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	applyAuth(req, Config{Username: "u", Password: "p"})
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p"))
	require.Equal(t, want, req.Header.Get("Authorization"))
	require.Equal(t, userAgent, req.Header.Get("User-Agent"))
}

func TestHTTPClient_DefaultTimeout(t *testing.T) {
	t.Parallel()
	c := httpClient(Config{})
	require.Equal(t, 5*time.Second, c.Timeout)
}
