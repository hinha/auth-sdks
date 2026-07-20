package ginadapter_test

import (
	"net/http"
	"testing"

	ginadapter "github.com/hinha/auth-sdks/go/routes/gin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/notes", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/notes/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	got := ginadapter.Collect(r)
	require.NotEmpty(t, got)
	var paths []string
	for _, route := range got {
		if route.Method == http.MethodGet {
			paths = append(paths, route.Path)
		}
	}
	assert.Contains(t, paths, "/notes")
	assert.Contains(t, paths, "/notes/:id")
}
