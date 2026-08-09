package redisstore_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hinha/auth-sdks/go/ratelimit"
	redisstore "github.com/hinha/auth-sdks/go/ratelimit/redis"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestNewStore_NilClient(t *testing.T) {
	t.Parallel()
	_, err := redisstore.NewStore(nil)
	require.Error(t, err)
}

func TestNewStoreFromAddr_Empty(t *testing.T) {
	t.Parallel()
	_, _, err := redisstore.NewStoreFromAddr("")
	require.Error(t, err)
}

func TestRedisStore_WithLimiter(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store, err := redisstore.NewStore(client, redisstore.Options{Prefix: "test-rl"})
	require.NoError(t, err)

	lim, err := ratelimit.New(ratelimit.Config{
		Enabled:  true,
		Profiles: map[string]string{"default": "1-M"},
		Store:    store,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.9.8.7:1"

	res1, _, err := lim.Allow(req.Context(), req)
	require.NoError(t, err)
	require.True(t, res1.Allowed)

	res2, _, err := lim.Allow(req.Context(), req)
	require.NoError(t, err)
	require.False(t, res2.Allowed)
}

func TestNewStoreFromAddr_OK(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	store, client, err := redisstore.NewStoreFromAddr(mr.Addr(), redisstore.Options{Prefix: "addr-rl"})
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, client)
	t.Cleanup(func() { _ = client.Close() })
}
