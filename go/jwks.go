package authsdk

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hinha/auth-sdks/go/internal/api"
	"github.com/hinha/auth-sdks/go/logging"
)

type jwkSet struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksCache struct {
	client *Client
	ttl    time.Duration

	mu        sync.RWMutex
	fetchedAt time.Time
	keys      map[string]*rsa.PublicKey
	inflight  sync.Mutex
}

func newJWKSCache(c *Client, ttl time.Duration) *jwksCache {
	return &jwksCache{
		client: c,
		ttl:    ttl,
		keys:   make(map[string]*rsa.PublicKey),
	}
}

// GetJWKS fetches (or returns cached) JWKS for the application service.
func (c *Client) GetJWKS(ctx context.Context) (*jwkSet, error) {
	var set jwkSet
	path := c.path("/jwks/" + c.cfg.ApplicationService)
	err := c.api.DoJSON(ctx, http.MethodGet, path, nil, &set, c.withClientKey(api.WithRawBody())...)
	if err != nil {
		return nil, err
	}
	return &set, nil
}

// VerifyAccessToken validates JWT signature via JWKS and returns claims.
// Permission checks must still go through AuthorizeAction.
func (c *Client) VerifyAccessToken(ctx context.Context, accessToken string) (*Claims, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, &UnauthorizedError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "access token required",
		}}
	}

	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}))
	token, err := parser.Parse(accessToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		key, err := c.jwks.lookup(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	})
	if err != nil {
		// One forced refresh on kid miss / stale cache, then retry once.
		if refreshErr := c.jwks.refresh(ctx); refreshErr == nil {
			token, err = parser.Parse(accessToken, func(t *jwt.Token) (any, error) {
				kid, _ := t.Header["kid"].(string)
				return c.jwks.lookup(ctx, kid)
			})
		}
	}
	if err != nil {
		logging.Warn(ctx, c.log, "jwt_verify_failed", logging.Err(err))
		return nil, &UnauthorizedError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "invalid access token",
		}}
	}
	if !token.Valid {
		return nil, &UnauthorizedError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "invalid access token",
		}}
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &UnauthorizedError{APIError: &APIError{
			StatusCode: http.StatusUnauthorized,
			Code:       "401",
			Message:    "invalid token claims",
		}}
	}
	return mapToClaims(mapClaims), nil
}

func (c *jwksCache) lookup(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if err := c.ensureFresh(ctx); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if kid != "" {
		if key, ok := c.keys[kid]; ok {
			return key, nil
		}
		return nil, fmt.Errorf("jwks: kid %q not found", kid)
	}
	// No kid: return the sole key if exactly one exists.
	if len(c.keys) == 1 {
		for _, key := range c.keys {
			return key, nil
		}
	}
	return nil, fmt.Errorf("jwks: kid required when multiple keys present")
}

func (c *jwksCache) ensureFresh(ctx context.Context) error {
	c.mu.RLock()
	fresh := time.Since(c.fetchedAt) < c.ttl && len(c.keys) > 0
	c.mu.RUnlock()
	if fresh {
		return nil
	}
	return c.refresh(ctx)
}

func (c *jwksCache) refresh(ctx context.Context) error {
	c.inflight.Lock()
	defer c.inflight.Unlock()

	// Double-check after lock.
	c.mu.RLock()
	fresh := time.Since(c.fetchedAt) < c.ttl && len(c.keys) > 0
	c.mu.RUnlock()
	if fresh {
		return nil
	}

	set, err := c.client.GetJWKS(ctx)
	if err != nil {
		return err
	}
	parsed := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if !strings.EqualFold(k.Kty, "RSA") {
			continue
		}
		pub, err := jwkToRSA(k)
		if err != nil {
			return err
		}
		kid := k.Kid
		if kid == "" {
			kid = "default"
		}
		parsed[kid] = pub
	}
	if len(parsed) == 0 {
		return fmt.Errorf("jwks: no RSA keys in set")
	}

	c.mu.Lock()
	c.keys = parsed
	c.fetchedAt = time.Now()
	c.mu.Unlock()

	logging.Debug(ctx, c.client.log, "jwks_refreshed", logging.Int("keys", len(parsed)))
	return nil
}

func jwkToRSA(k jwkKey) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("jwks: decode n: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("jwks: decode e: %w", err)
	}
	var eInt int
	for _, b := range eb {
		eInt = eInt<<8 + int(b)
	}
	if eInt == 0 {
		return nil, fmt.Errorf("jwks: invalid exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: eInt,
	}, nil
}

func mapToClaims(mc jwt.MapClaims) *Claims {
	out := &Claims{Raw: map[string]any(mc)}
	if sub, err := mc.GetSubject(); err == nil {
		out.Subject = sub
	}
	if iss, err := mc.GetIssuer(); err == nil {
		out.Issuer = iss
	}
	if aud, err := mc.GetAudience(); err == nil {
		out.Audience = []string(aud)
	}
	if exp, err := mc.GetExpirationTime(); err == nil && exp != nil {
		out.ExpiresAt = exp.Time
	}
	if iat, err := mc.GetIssuedAt(); err == nil && iat != nil {
		out.IssuedAt = iat.Time
	}
	out.SessionID = claimString(mc, "sid", "session_id")
	out.UserID = claimUint(mc, "uid", "user_id")
	if out.UserID == 0 && out.Subject != "" {
		// Consumer access tokens historically only set sub (user id as string).
		if n, err := strconv.ParseUint(out.Subject, 10, 64); err == nil {
			out.UserID = uint(n)
		}
	}
	return out
}

func claimString(mc jwt.MapClaims, keys ...string) string {
	for _, k := range keys {
		if v, ok := mc[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func claimUint(mc jwt.MapClaims, keys ...string) uint {
	for _, k := range keys {
		v, ok := mc[k]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return uint(n)
		case json.Number:
			i, _ := n.Int64()
			return uint(i)
		case int:
			return uint(n)
		case int64:
			return uint(n)
		case string:
			i, err := strconv.ParseUint(n, 10, 64)
			if err == nil {
				return uint(i)
			}
		}
	}
	return 0
}
