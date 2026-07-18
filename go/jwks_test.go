package authsdk

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func rsaJWK(t *testing.T, key *rsa.PrivateKey, kid string) map[string]string {
	t.Helper()
	return map[string]string{
		"kid": kid,
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyAccessToken_JWKS(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "kid-test-1"
	signed := signRS256(t, key, kid, jwt.MapClaims{
		"sub": "42",
		"uid": float64(42),
		"sid": "sess-xyz",
		"iss": "auth-service",
		"aud": []string{"memoo", "consumer"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{rsaJWK(t, key, kid)},
		})
	})

	client, _ := newTestClient(t, mux, WithJWKSCacheTTL(time.Minute))
	claims, err := client.VerifyAccessToken(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 || claims.SessionID != "sess-xyz" || claims.Subject != "42" {
		t.Fatalf("claims=%+v", claims)
	}
	if claims.Issuer != "auth-service" || len(claims.Audience) == 0 {
		t.Fatalf("claims=%+v", claims)
	}

	// Cache hit path.
	if _, err := client.VerifyAccessToken(context.Background(), signed); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAccessToken_UserIDFromSubFallback(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "kid-sub-only"
	signed := signRS256(t, key, kid, jwt.MapClaims{
		"sub": "7",
		"sid": "sess-sub",
		"iss": "auth-service",
		"aud": []string{"memoo", "consumer"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		requireAPIKey(t, r)
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{rsaJWK(t, key, kid)},
		})
	})

	client, _ := newTestClient(t, mux, WithJWKSCacheTTL(time.Minute))
	claims, err := client.VerifyAccessToken(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 7 {
		t.Fatalf("expected UserID from sub, got %+v", claims)
	}
}

func TestVerifyAccessToken_EmptyAndInvalid(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{"keys": []any{}})
	})
	client, _ := newTestClient(t, mux)

	if _, err := client.VerifyAccessToken(context.Background(), "  "); !IsUnauthorized(err) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := client.VerifyAccessToken(context.Background(), "not-a-jwt"); !IsUnauthorized(err) {
		t.Fatalf("invalid: %v", err)
	}
}

func TestVerifyAccessToken_KidRefreshAndNoKid(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		calls++
		kid := "old-kid"
		if calls >= 2 {
			kid = "new-kid"
		}
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{rsaJWK(t, key, kid)},
		})
	})

	client, _ := newTestClient(t, mux, WithJWKSCacheTTL(time.Hour))
	// Prime cache with old kid.
	if _, err := client.GetJWKS(context.Background()); err != nil {
		t.Fatal(err)
	}

	signed := signRS256(t, key, "new-kid", jwt.MapClaims{
		"sub": "1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client.VerifyAccessToken(context.Background(), signed); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatalf("expected refresh fetch, calls=%d", calls)
	}

	// No kid + single key.
	client2, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{rsaJWK(t, key, "")},
		})
	}), WithJWKSCacheTTL(time.Minute))
	signedNoKid := signRS256(t, key, "", jwt.MapClaims{
		"sub": "1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client2.VerifyAccessToken(context.Background(), signedNoKid); err != nil {
		t.Fatal(err)
	}
}

func TestJWKS_LookupMultiKeyRequiresKid(t *testing.T) {
	t.Parallel()
	key1, _ := rsa.GenerateKey(rand.Reader, 2048)
	key2, _ := rsa.GenerateKey(rand.Reader, 2048)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{
				rsaJWK(t, key1, "k1"),
				rsaJWK(t, key2, "k2"),
			},
		})
	})
	client, _ := newTestClient(t, mux)
	signed := signRS256(t, key1, "", jwt.MapClaims{
		"sub": "1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client.VerifyAccessToken(context.Background(), signed); !IsUnauthorized(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestJWKS_SkipsNonRSAAndInvalid(t *testing.T) {
	t.Parallel()

	t.Run("no rsa keys", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(w, http.StatusOK, map[string]any{
				"keys": []map[string]string{{"kty": "EC", "kid": "ec1"}},
			})
		})
		client, _ := newTestClient(t, mux)
		if _, err := client.VerifyAccessToken(context.Background(), "x.y.z"); !IsUnauthorized(err) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("bad n", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(w, http.StatusOK, map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA", "kid": "bad", "n": "!!!", "e": "AQAB",
				}},
			})
		})
		client, _ := newTestClient(t, mux)
		if _, err := client.VerifyAccessToken(context.Background(), "x.y.z"); !IsUnauthorized(err) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("bad e", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(w, http.StatusOK, map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA",
					"kid": "bad-e",
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   "!!!",
				}},
			})
		})
		client, _ := newTestClient(t, mux)
		if _, err := client.VerifyAccessToken(context.Background(), "x.y.z"); !IsUnauthorized(err) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("zero exponent", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 2048)
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(w, http.StatusOK, map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA",
					"kid": "zero-e",
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString([]byte{0}),
				}},
			})
		})
		client, _ := newTestClient(t, mux)
		if _, err := client.VerifyAccessToken(context.Background(), "x.y.z"); !IsUnauthorized(err) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestClaimHelpers(t *testing.T) {
	t.Parallel()
	mc := jwt.MapClaims{
		"sid":     "s1",
		"uid":     json.Number("12"),
		"user_id": int64(99),
		"other":   true,
	}
	if got := claimString(mc, "missing", "sid"); got != "s1" {
		t.Fatalf("str=%q", got)
	}
	if got := claimString(mc, "other"); got != "" {
		t.Fatalf("str other=%q", got)
	}
	if got := claimUint(mc, "uid"); got != 12 {
		t.Fatalf("uid=%d", got)
	}
	mc2 := jwt.MapClaims{"user_id": 5}
	if got := claimUint(mc2, "user_id"); got != 5 {
		t.Fatalf("int=%d", got)
	}
	mc3 := jwt.MapClaims{"user_id": int64(8)}
	if got := claimUint(mc3, "user_id"); got != 8 {
		t.Fatalf("int64=%d", got)
	}
	if got := claimUint(jwt.MapClaims{"user_id": "nope"}, "user_id"); got != 0 {
		t.Fatalf("bad=%d", got)
	}
}

func TestJWKS_LookupKidMissAndFreshRefresh(t *testing.T) {
	t.Parallel()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, map[string]any{
			"keys": []map[string]string{rsaJWK(t, key, "only-kid")},
		})
	})
	client, _ := newTestClient(t, mux, WithJWKSCacheTTL(time.Hour))

	// Prime cache.
	if _, err := client.GetJWKS(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second refresh while still fresh (double-check path).
	if err := client.jwks.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	signed := signRS256(t, key, "missing-kid", jwt.MapClaims{
		"sub": "1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client.VerifyAccessToken(context.Background(), signed); !IsUnauthorized(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestJWKS_EnsureFreshError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/consumer-auth/jwks/memoo", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusInternalServerError, "500", "down")
	})
	client, _ := newTestClient(t, mux)
	signed := signRS256(t, mustRSA(t), "k", jwt.MapClaims{
		"sub": "1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := client.VerifyAccessToken(context.Background(), signed); !IsUnauthorized(err) {
		t.Fatalf("err=%v", err)
	}
}

func mustRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
