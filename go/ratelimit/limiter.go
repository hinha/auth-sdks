package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ulule/limiter/v3"
)

// Result is the outcome of a rate-limit check.
type Result struct {
	Allowed   bool
	Limit     int64
	Remaining int64
	Reset     int64 // unix seconds
}

// RetryAfterSeconds returns seconds until reset (min 1 when exhausted).
func (r Result) RetryAfterSeconds() int64 {
	now := time.Now().Unix()
	if r.Reset <= now {
		return 1
	}
	return r.Reset - now
}

// Store abstracts the limiter backend (memory by default; Redis via
// github.com/hinha/auth-sdks/go/ratelimit/redis).
type Store interface {
	limiter.Store
}

// KeyFunc builds a rate-limit key for the request and profile.
type KeyFunc func(r *http.Request, profile string) string

// SkipFunc reports whether the request should bypass rate limiting.
type SkipFunc func(r *http.Request) bool

// PickProfileFunc selects which profile rate applies.
type PickProfileFunc func(r *http.Request) string

// Config configures a multi-profile Limiter.
type Config struct {
	// Enabled when false makes Middleware a no-op.
	Enabled bool
	// Profiles maps profile name → ulule rate string (e.g. "30-M").
	Profiles map[string]string
	// DefaultProfile used when PickProfile returns empty (default "default").
	DefaultProfile string
	// Store backend; nil uses an in-memory store.
	Store Store
	// KeyFunc defaults to DefaultKeyFunc.
	KeyFunc KeyFunc
	// SkipFunc optional bypass.
	SkipFunc SkipFunc
	// PickProfile defaults to always DefaultProfile.
	PickProfile PickProfileFunc
	// FailOpen when true allows the request if the store errors (default true).
	FailOpen *bool
	// CredentialHeader used by DefaultKeyFunc (e.g. "X-API-Key"); empty skips.
	CredentialHeader string
	// OnLimit optional callback when a request is rejected.
	OnLimit func(r *http.Request, profile string, result Result)
}

func (c Config) failOpen() bool {
	if c.FailOpen == nil {
		return true
	}
	return *c.FailOpen
}

// Enabled reports whether rate limiting is active.
func (l *Limiter) Enabled() bool {
	return l != nil && l.cfg.Enabled
}

// FailOpen reports store-error policy (default true).
func (l *Limiter) FailOpen() bool {
	if l == nil {
		return true
	}
	return l.cfg.failOpen()
}

// NotifyLimit invokes OnLimit if configured.
func (l *Limiter) NotifyLimit(r *http.Request, profile string, res Result) {
	if l != nil && l.cfg.OnLimit != nil {
		l.cfg.OnLimit(r, profile, res)
	}
}

// Limiter evaluates rate limits across named profiles.
type Limiter struct {
	cfg      Config
	limiters map[string]*limiter.Limiter
}

// New builds a Limiter from Config.
func New(cfg Config) (*Limiter, error) {
	if !cfg.Enabled {
		return &Limiter{cfg: cfg, limiters: map[string]*limiter.Limiter{}}, nil
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("ratelimit: Profiles required when Enabled")
	}
	store := cfg.Store
	if store == nil {
		store = NewMemoryStore()
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = DefaultKeyFunc(cfg.CredentialHeader)
	}
	if cfg.PickProfile == nil {
		def := cfg.DefaultProfile
		cfg.PickProfile = func(*http.Request) string { return def }
	}

	out := make(map[string]*limiter.Limiter, len(cfg.Profiles))
	for name, rateStr := range cfg.Profiles {
		rate, err := limiter.NewRateFromFormatted(rateStr)
		if err != nil {
			return nil, fmt.Errorf("ratelimit: parse profile %q rate %q: %w", name, rateStr, err)
		}
		out[name] = limiter.New(store, rate)
	}
	return &Limiter{cfg: cfg, limiters: out}, nil
}

// Allow checks the limit for r under the picked profile.
func (l *Limiter) Allow(ctx context.Context, r *http.Request) (Result, string, error) {
	if l == nil || !l.cfg.Enabled {
		return Result{Allowed: true}, "", nil
	}
	if l.cfg.SkipFunc != nil && l.cfg.SkipFunc(r) {
		return Result{Allowed: true}, "", nil
	}
	profile := strings.TrimSpace(l.cfg.PickProfile(r))
	if profile == "" {
		profile = l.cfg.DefaultProfile
	}
	lim, ok := l.limiters[profile]
	if !ok {
		return Result{}, profile, fmt.Errorf("ratelimit: unknown profile %q", profile)
	}
	key := l.cfg.KeyFunc(r, profile)
	ctxRes, err := lim.Get(ctx, key)
	if err != nil {
		return Result{}, profile, err
	}
	res := Result{
		Allowed:   !ctxRes.Reached,
		Limit:     ctxRes.Limit,
		Remaining: ctxRes.Remaining,
		Reset:     ctxRes.Reset,
	}
	return res, profile, nil
}

// DefaultKeyFunc hashes credential header or client IP, prefixed by profile.
func DefaultKeyFunc(credentialHeader string) KeyFunc {
	header := strings.TrimSpace(credentialHeader)
	return func(r *http.Request, profile string) string {
		id := ""
		if header != "" {
			id = strings.TrimSpace(r.Header.Get(header))
		}
		if id == "" {
			id = ClientIP(r)
		}
		sum := sha256.Sum256([]byte(id))
		return profile + ":" + hex.EncodeToString(sum[:])
	}
}

// ClientIP extracts the host from RemoteAddr.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// SkipPrefixes returns a SkipFunc that skips OPTIONS and path prefixes.
func SkipPrefixes(prefixes ...string) SkipFunc {
	cleaned := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return func(r *http.Request) bool {
		if r.Method == http.MethodOptions {
			return true
		}
		path := r.URL.Path
		for _, prefix := range cleaned {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
		}
		return false
	}
}

// BoolPtr returns a *bool for FailOpen config.
func BoolPtr(v bool) *bool { return &v }
