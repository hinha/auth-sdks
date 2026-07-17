package main

import (
	"context"
	"fmt"
	"log"
	"os"

	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/hinha/auth-sdks/go/logging"
)

// Smoke example — set AUTH_BASE_URL, APPLICATION_SERVICE, AUTH_API_KEY, AUTH_EMAIL, AUTH_PASSWORD.
func main() {
	baseURL := envOr("AUTH_BASE_URL", "http://localhost:8080")
	service := envOr("APPLICATION_SERVICE", "memoo")
	apiKey := os.Getenv("AUTH_API_KEY")
	if apiKey == "" {
		log.Fatal("AUTH_API_KEY is required (sa_* client key)")
	}

	client, err := authsdk.New(baseURL, service,
		authsdk.Credentials(apiKey),
		authsdk.WithLogger(logging.NewSlog(nil)),
		authsdk.WithUserAgent("auth-sdk-go-smoke/0.1"),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	session, err := client.Login(ctx, authsdk.LoginInput{
		Email:    os.Getenv("AUTH_EMAIL"),
		Password: os.Getenv("AUTH_PASSWORD"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("session_id=", session.SessionID)

	if _, err := client.VerifyAccessToken(ctx, session.AccessToken); err != nil {
		log.Fatal("jwks verify: ", err)
	}

	perm := envOr("AUTH_PERMISSION", "demo:read")
	ok, err := client.Allow(ctx, session.AccessToken, perm)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("permission", perm, "allowed=", ok)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
