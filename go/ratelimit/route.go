package ratelimit

import (
	"net/http"
	"strings"
)

// RouteRule maps a method + path pattern to a rate-limit profile.
//
// Pattern syntax:
//   - exact: "/api/v1/search"
//   - named param (one segment): "/api/v1/jobs/:id" or "/api/v1/jobs/{id}"
//   - trailing wildcard: "/api/v1/files/*" (one or more remaining segments)
//
// Put more specific rules before broader ones (first match wins).
type RouteRule struct {
	Method  string // empty = any method
	Pattern string
	Profile string
}

// RouteTable is an ordered list of rules; first match wins.
type RouteTable []RouteRule

// Pick implements PickProfileFunc. Returns "" when nothing matches
// (Limiter then falls back to DefaultProfile).
func (t RouteTable) Pick(r *http.Request) string {
	if r == nil {
		return ""
	}
	path := r.URL.Path
	for _, rule := range t {
		if rule.Profile == "" || rule.Pattern == "" {
			continue
		}
		if rule.Method != "" && !strings.EqualFold(rule.Method, r.Method) {
			continue
		}
		if MatchPath(rule.Pattern, path) {
			return rule.Profile
		}
	}
	return ""
}

// MatchPath reports whether path matches pattern.
// Supports :name / {name} for a single segment, and a trailing /* for the rest.
func MatchPath(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if pattern == path {
		return true
	}

	pSegs := splitPath(pattern)
	aSegs := splitPath(path)

	pi, ai := 0, 0
	for pi < len(pSegs) && ai < len(aSegs) {
		ps := pSegs[pi]
		if ps == "*" {
			if pi != len(pSegs)-1 {
				return false
			}
			// require at least one remaining path segment
			return true
		}
		if isParam(ps) {
			pi++
			ai++
			continue
		}
		if ps != aSegs[ai] {
			return false
		}
		pi++
		ai++
	}

	if pi == len(pSegs) && ai == len(aSegs) {
		return true
	}
	// pattern still has trailing * but path ended at parent → no match
	if pi == len(pSegs)-1 && pSegs[pi] == "*" {
		return ai < len(aSegs)
	}
	return false
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isParam(seg string) bool {
	if seg == "" {
		return false
	}
	if seg[0] == ':' && len(seg) > 1 {
		return true
	}
	return len(seg) > 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}
