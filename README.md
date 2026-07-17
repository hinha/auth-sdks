# Auth SDKs

Official multi-language consumer SDKs for [Auth Service](https://github.com/hinha/auth-service) `/v1/consumer-auth/*`.

| Language | Path | Status |
|---|---|---|
| **Go** | [`go/`](./go) | Available |
| Python | — | Planned |
| TypeScript | — | Planned |

---

## Go

Module: [`github.com/hinha/auth-sdk-go`](./go)

### Install

**Requires:** Go 1.22+ (module declares `go 1.25`).

#### From a published module / GitHub

In your consumer project:

```bash
go get github.com/hinha/auth-sdk-go@latest
```

Or pin a version (after a release tag exists, e.g. `v0.1.0`):

```bash
go get github.com/hinha/auth-sdk-go@v0.1.0
```

Then import:

```go
import authsdk "github.com/hinha/auth-sdk-go"
```

#### From a local monorepo (not pushed / private)

If the SDK still lives on disk (`…/auth-sdks/go`), add a `replace` in your app `go.mod`:

```bash
go get github.com/hinha/auth-sdk-go@v0.0.0
```

```go
// go.mod
require github.com/hinha/auth-sdk-go v0.0.0

replace github.com/hinha/auth-sdk-go => ../auth-sdks/go
```

Or an absolute path:

```go
replace github.com/hinha/auth-sdk-go => /Users/hinha/Projects/hinha/auth-sdks/go
```

#### Verify the install

```bash
cd go
go test ./...
go test ./... -cover
```

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

Keep `AUTH_API_KEY` on your **backend/BFF** (not in the browser). When Auth Service policy `require_client_api_key` is enabled (default true), register/login/forgot/verify/reset are rejected without a key bound to the same `application_service`.

### Example smoke

```bash
export AUTH_BASE_URL=http://localhost:8080
export APPLICATION_SERVICE=memoo
export AUTH_API_KEY=sa_...
export AUTH_EMAIL=andi@acme.com
export AUTH_PASSWORD='P@ssw0rd!'

cd examples/go-smoke
go run .
```

### Env vars

| Variable | Required | Description |
|---|---|---|
| `AUTH_BASE_URL` | yes | Auth Service origin |
| `APPLICATION_SERVICE` | yes | Technical service name (max 32) |
| `AUTH_API_KEY` | yes | Client key `sa_*` |
| `AUTH_EMAIL` / `AUTH_PASSWORD` | for login demos | End-user credentials |
