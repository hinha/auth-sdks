package ginadapter

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hinha/auth-sdks/go/routes"
)

// Collect returns registered Gin routes as auth-sdks Route values.
func Collect(engine *gin.Engine) []routes.Route {
	if engine == nil {
		return nil
	}
	raw := engine.Routes()
	out := make([]routes.Route, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, gr := range raw {
		method := strings.ToUpper(strings.TrimSpace(gr.Method))
		path := strings.TrimSpace(gr.Path)
		if method == "" || path == "" {
			continue
		}
		key := method + " " + path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		// Gin uses :id — already Auth-compatible.
		r := routes.Route{
			Method: method,
			Path:   path,
		}.Normalize()
		out = append(out, r)
	}
	return out
}
