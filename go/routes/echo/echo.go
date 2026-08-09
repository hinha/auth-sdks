package echoadapter

import (
	"strings"

	"github.com/hinha/auth-sdks/go/routes"
	"github.com/labstack/echo/v4"
)

// Collect returns registered Echo routes as auth-sdks Route values.
// Skips Echo's internal / and HEAD shadows where useful; keeps explicit registrations.
func Collect(e *echo.Echo) []routes.Route {
	if e == nil {
		return nil
	}
	raw := e.Routes()
	out := make([]routes.Route, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, er := range raw {
		if er == nil {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(er.Method))
		path := strings.TrimSpace(er.Path)
		if method == "" || path == "" {
			continue
		}
		// Deduplicate method+path (Echo may list the same route more than once).
		key := method + " " + path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		r := routes.Route{
			Name:   strings.TrimSpace(er.Name),
			Method: method,
			Path:   path,
		}.Normalize()
		out = append(out, r)
	}
	return out
}
