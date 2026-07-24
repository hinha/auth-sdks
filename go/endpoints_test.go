package authsdk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/hinha/auth-sdks/go/routes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/endpoints/import", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "sa_test_key", r.Header.Get("X-API-Key"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "memoo", body["application_service"])
		assert.Equal(t, "skip", body["conflict_policy"])
		assert.Equal(t, "mark_stale", body["sync_mode"])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "ok",
			"code":    "200",
			"data": map[string]any{
				"created":      1,
				"updated":      0,
				"skipped":      0,
				"failed":       0,
				"marked_stale": 1,
				"pruned":       0,
				"items": []map[string]any{
					{"method": "GET", "path": "/notes", "status": "created", "id": 9},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := authsdk.New(srv.URL, "memoo",
		authsdk.Credentials("sa_test_key"),
		authsdk.WithHTTPDoer(srv.Client()),
	)
	require.NoError(t, err)

	out, err := client.ImportEndpoints(context.Background(), []routes.Route{
		{Method: "GET", Path: "/notes", Name: "list_notes"},
	}, authsdk.WithSyncMode(authsdk.SyncModeMarkStale))
	require.NoError(t, err)
	assert.Equal(t, 1, out.Created)
	assert.Equal(t, 1, out.MarkedStale)
}

func TestSyncHTTPRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/endpoints/import", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "ok", "code": "200",
			"data": map[string]any{"created": 2, "updated": 0, "skipped": 0, "failed": 0, "items": []any{}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := authsdk.New(srv.URL, "memoo",
		authsdk.Credentials("sa_test_key"),
		authsdk.WithHTTPDoer(srv.Client()),
	)
	require.NoError(t, err)

	reg := routes.NewRegistry()
	require.NoError(t, reg.HandleFunc("GET /a", func(w http.ResponseWriter, r *http.Request) {}))
	require.NoError(t, reg.HandleFunc("POST /b", func(w http.ResponseWriter, r *http.Request) {}))

	out, err := client.SyncHTTPRoutes(context.Background(), reg, authsdk.WithConflictPolicy("update"))
	require.NoError(t, err)
	assert.Equal(t, 2, out.Created)
}
