package echoadapter_test

import (
	"net/http"
	"testing"

	"github.com/hinha/auth-sdks/go/routes"
	echoadapter "github.com/hinha/auth-sdks/go/routes/echo"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect(t *testing.T) {
	e := echo.New()
	e.GET("/notes", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	e.GET("/notes/:id", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	got := echoadapter.Collect(e)
	require.NotEmpty(t, got)
	var paths []string
	for _, r := range got {
		if r.Method == http.MethodGet {
			paths = append(paths, r.Path)
		}
	}
	assert.Contains(t, paths, "/notes")
	assert.Contains(t, paths, "/notes/:id")
	for _, r := range got {
		if r.Path == "/notes/:id" {
			assert.Equal(t, "pattern", r.PathMatchType)
		}
	}
	_ = routes.NormalizeAll(got)
}
