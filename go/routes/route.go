// Package routes discovers HTTP routes for bulk import into Auth Service
// consumer_endpoints.
package routes

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// Route is one discovered HTTP route ready for Auth Service import.
type Route struct {
	Name             string   `json:"name,omitempty"`
	Method           string   `json:"method"`
	Path             string   `json:"path"`
	PathMatchType    string   `json:"path_match_type,omitempty"`
	RequiredScopes   []string `json:"required_scopes,omitempty"`
	AllowedPlanCodes []string `json:"allowed_plan_codes,omitempty"`
	IsPublic         bool     `json:"is_public,omitempty"`
	Status           string   `json:"status,omitempty"`
}

// Normalize uppercases method, trims path, and infers path_match_type when empty.
func (r Route) Normalize() Route {
	out := r
	out.Method = strings.ToUpper(strings.TrimSpace(out.Method))
	out.Path = strings.TrimSpace(out.Path)
	out.Name = strings.TrimSpace(out.Name)
	out.PathMatchType = strings.TrimSpace(out.PathMatchType)
	out.Status = strings.TrimSpace(out.Status)
	if out.PathMatchType == "" {
		out.PathMatchType = InferPathMatchType(out.Path)
	}
	if out.Status == "" {
		out.Status = "active"
	}
	if out.Name == "" {
		out.Name = DefaultName(out.Method, out.Path)
	}
	return out
}

// InferPathMatchType returns pattern when the path has :param or *, else exact.
func InferPathMatchType(path string) string {
	if strings.Contains(path, ":") || strings.Contains(path, "*") {
		return "pattern"
	}
	return "exact"
}

// DefaultName builds a stable endpoint name from method + path.
func DefaultName(method, path string) string {
	cleaned := strings.ToLower(strings.TrimSpace(method) + "_" + strings.Trim(path, "/"))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = "endpoint"
	}
	if len(name) < 3 {
		name += "_ep"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// ParsePattern splits a Go 1.22-style ServeMux pattern ("GET /notes/{id}") into
// method + Auth-style path ("/notes/:id"). Method may be empty for path-only patterns.
func ParsePattern(pattern string) (method, path string, err error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", fmt.Errorf("empty pattern")
	}
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		path = method
		method = ""
	} else {
		method = strings.ToUpper(strings.TrimSpace(method))
		path = strings.TrimSpace(path)
	}
	if path == "" {
		return "", "", fmt.Errorf("pattern %q missing path", pattern)
	}
	path = convertBraceParams(path)
	return method, path, nil
}

// convertBraceParams turns /notes/{id} into /notes/:id (Echo/Gin style).
func convertBraceParams(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			b.WriteByte(path[i])
			continue
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			b.WriteString(path[i:])
			break
		}
		name := path[i+1 : i+j]
		if name == "" || name == "$" {
			b.WriteByte('*')
		} else {
			b.WriteByte(':')
			b.WriteString(name)
		}
		i += j
	}
	return b.String()
}

// NormalizeAll returns a copy of routes with Normalize applied.
func NormalizeAll(in []Route) []Route {
	out := make([]Route, 0, len(in))
	for _, r := range in {
		n := r.Normalize()
		if n.Method == "" || n.Path == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

// Registry records routes while registering them on an http.ServeMux.
// Prefer this over raw ServeMux when you need discovery (ServeMux has no public route list).
type Registry struct {
	mux    *http.ServeMux
	routes []Route
}

// NewRegistry creates a Registry backed by a new ServeMux.
func NewRegistry() *Registry {
	return &Registry{mux: http.NewServeMux()}
}

// NewRegistryOn wraps an existing ServeMux.
func NewRegistryOn(mux *http.ServeMux) *Registry {
	if mux == nil {
		mux = http.NewServeMux()
	}
	return &Registry{mux: mux}
}

// Handle registers pattern on the mux and records it for ImportEndpoints.
// pattern should be Go 1.22 style, e.g. "GET /notes/{id}".
func (r *Registry) Handle(pattern string, handler http.Handler) error {
	method, path, err := ParsePattern(pattern)
	if err != nil {
		return err
	}
	if method == "" {
		return fmt.Errorf("pattern %q must include an HTTP method", pattern)
	}
	r.mux.Handle(pattern, handler)
	r.routes = append(r.routes, Route{Method: method, Path: path}.Normalize())
	return nil
}

// HandleFunc is Handle for http.HandlerFunc.
func (r *Registry) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) error {
	return r.Handle(pattern, http.HandlerFunc(handler))
}

// ServeMux returns the underlying mux.
func (r *Registry) ServeMux() *http.ServeMux { return r.mux }

// Handler implements http.Handler.
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// Routes returns a copy of recorded routes.
func (r *Registry) Routes() []Route {
	return append([]Route(nil), r.routes...)
}

// WithScopes annotates matching method+path routes (after normalize).
func WithScopes(list []Route, method, path string, scopes ...string) []Route {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	out := make([]Route, len(list))
	copy(out, list)
	for i := range out {
		n := out[i].Normalize()
		if n.Method == method && n.Path == path {
			n.RequiredScopes = append([]string(nil), scopes...)
			out[i] = n
		} else {
			out[i] = n
		}
	}
	return out
}
