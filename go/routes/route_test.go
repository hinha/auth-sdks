package routes_test

import (
	"net/http"
	"testing"

	"github.com/hinha/auth-sdks/go/routes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RecordsGo122Patterns(t *testing.T) {
	reg := routes.NewRegistry()
	require.NoError(t, reg.HandleFunc("GET /notes", func(w http.ResponseWriter, r *http.Request) {}))
	require.NoError(t, reg.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {}))

	got := reg.Routes()
	require.Len(t, got, 2)
	assert.Equal(t, "GET", got[0].Method)
	assert.Equal(t, "/notes", got[0].Path)
	assert.Equal(t, "exact", got[0].PathMatchType)
	assert.Equal(t, "/notes/:id", got[1].Path)
	assert.Equal(t, "pattern", got[1].PathMatchType)
}

func TestParsePatternAndNormalize(t *testing.T) {
	method, path, err := routes.ParsePattern("POST /orgs/{slug}/members")
	require.NoError(t, err)
	assert.Equal(t, "POST", method)
	assert.Equal(t, "/orgs/:slug/members", path)

	r := routes.Route{Method: "get", Path: "/x"}.Normalize()
	assert.Equal(t, "GET", r.Method)
	assert.Equal(t, "active", r.Status)
	assert.NotEmpty(t, r.Name)
}

func TestWithScopes(t *testing.T) {
	list := []routes.Route{{Method: "GET", Path: "/notes"}}
	list = routes.WithScopes(list, "GET", "/notes", "notes:read")
	require.Equal(t, []string{"notes:read"}, list[0].RequiredScopes)
}
