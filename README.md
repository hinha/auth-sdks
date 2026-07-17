# Auth SDKs

Official multi-language consumer SDKs for [Auth Service](https://github.com/hinha/auth-service) `/v1/consumer-auth/*`.

## Go (`go/`)

Module: `github.com/hinha/auth-sdk-go`

### Features

- HTTP via [gojek/heimdall](https://github.com/gojek/heimdall) (retry + backoff)
- Structured logging Strategy (`Zap` / `slog` / `Nop`) + Heimdall request plugin
- **Client API key gate** via `Credentials(sa_*)` (required on `New`)
- User session: login / refresh / logout / introspect
- JWT verify via JWKS cache (no hardcoded keys)
- `AuthorizeAction` hybrid RBAC
- Lifecycle: register / verify-email / password flows
- Machine path: API key verify + authorize-endpoint

### Design patterns

| Pattern | Where |
|---|---|
| Facade | `authsdk.Client` |
| Strategy | `logging.Logger` |
| Adapter + Factory | `transport.NewHeimdall` |
| Decorator / Plugin | Heimdall request logger |
| Functional Options | `Credentials`, `WithLogger`, `WithTimeout`, … |
| Gateway | `internal/api.Requester` |

### Quick start

```go
package main

import (
	"context"
	"log"
	"os"

	authsdk "github.com/hinha/auth-sdk-go"
	"github.com/hinha/auth-sdk-go/logging"
	"go.uber.org/zap"
)

func main() {
	zl, _ := zap.NewProduction()
	client, err := authsdk.New("https://auth.example.com", "memoo",
		authsdk.Credentials(os.Getenv("AUTH_API_KEY")), // sa_* from Nuts → Application services → API keys
		authsdk.WithLogger(logging.NewZap(zl)),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	session, err := client.Login(ctx, authsdk.LoginInput{
		Email:    "andi@acme.com",
		Password: "P@ssw0rd!",
	})
	if err != nil {
		log.Fatal(err)
	}

	claims, err := client.VerifyAccessToken(ctx, session.AccessToken)
	if err != nil {
		log.Fatal(err)
	}
	_ = claims

	ok, err := client.Allow(ctx, session.AccessToken, "reports:read")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("allowed=%v", ok)
}
```

Store `AUTH_API_KEY` on your **backend/BFF** (not in the public SDK repo). Auth Service policy `require_client_api_key` (default true) rejects register/login/forgot/verify/reset without a key bound to the same `application_service`.

### Install

```bash
cd go && go test ./...
```

Python / TypeScript packages: planned (see Notion plan).
