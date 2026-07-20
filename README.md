# Auth SDKs

Official multi-language consumer SDKs for [Auth Service](https://github.com/hinha/auth-service) `/v1/consumer-auth/*`.

## Repository layout

This is a **multi-language monorepo**. Each language owns its own folder and package path (unlike single-language repos such as [`redis/go-redis`](https://github.com/redis/go-redis), where the module sits at the repo root as `github.com/redis/go-redis/v9`).

```text
auth-sdks/
├── go/                 # module: github.com/hinha/auth-sdks/go
├── python/             # planned
├── typescript/         # planned
├── examples/
└── README.md
```

| Language | Path | Module / package | Status |
|---|---|---|---|
| **Go** | [`go/`](./go) | `github.com/hinha/auth-sdks/go` | Available |
| Python | `python/` | planned | Planned |
| TypeScript | `typescript/` | planned | Planned |

Go version tags for the subdirectory module use the `go/` prefix (Go toolchain rule): `go/v0.1.0` → consumers still write `@v0.1.0`.

---

## Go

Module: [`github.com/hinha/auth-sdks/go`](./go)

### Install

**Requires:** Go 1.22+ (module declares `go 1.25`).

#### From GitHub

```bash
go get github.com/hinha/auth-sdks/go@v0.1.0
# or
go get github.com/hinha/auth-sdks/go@latest
```

```go
import authsdk "github.com/hinha/auth-sdks/go"
```

#### From a local checkout

```go
// go.mod
require github.com/hinha/auth-sdks/go v0.0.0

replace github.com/hinha/auth-sdks/go => ../auth-sdks/go
```

#### Verify

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
- **First-login bootstrap**: `IsFirstLogin` / `FirstLogin` for operator temp passwords
- JWT verify via JWKS cache (no hardcoded keys)
- `AuthorizeAction` hybrid RBAC
- Lifecycle: register / verify-email / password flows
- Machine path: API key verify + authorize-endpoint
- **Route discovery → bulk import**: collect HTTP routes (stdlib registry / Echo / Gin) and `ImportEndpoints` into Auth Service

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

	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/hinha/auth-sdks/go/logging"
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
		if authsdk.IsFirstLogin(err) {
			// Operator-provisioned temp password: force change, then login again.
			if _, err := client.FirstLogin(ctx, authsdk.FirstLoginInput{
				Email:           "andi@acme.com",
				CurrentPassword: "P@ssw0rd!",
				NewPassword:     "N3wP@ss!",
			}); err != nil {
				log.Fatal(err)
			}
			session, err = client.Login(ctx, authsdk.LoginInput{
				Email: "andi@acme.com", Password: "N3wP@ss!",
			})
		}
		if err != nil {
			log.Fatal(err)
		}
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

Keep `AUTH_API_KEY` on your **backend/BFF** (not in the browser). When Auth Service policy `require_client_api_key` is enabled (default true), register/login/forgot/verify/reset/**first-login** are rejected without a key bound to the same `application_service`.

When Nuts creates an `app_user` with a temp password (`last_login` null), `Login` returns `FirstLoginError` (`IsFirstLogin`). Call `FirstLogin` then `Login` again — no JWT is issued until the password is changed.

### Sync routes → Auth Service endpoints

Register routes, then bulk-import them as `consumer_endpoints` (sa_*):

```go
import (
	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/hinha/auth-sdks/go/routes"
	echoadapter "github.com/hinha/auth-sdks/go/routes/echo" // optional
	ginadapter "github.com/hinha/auth-sdks/go/routes/gin"   // optional
)

// net/http (Go 1.22+ patterns) — ServeMux has no public route list, use Registry:
reg := routes.NewRegistry()
_ = reg.HandleFunc("GET /notes", notesList)
_ = reg.HandleFunc("GET /notes/{id}", notesGet)
http.ListenAndServe(":8080", reg)

discovered := routes.WithScopes(reg.Routes(), "GET", "/notes", "notes:read")
_, _ = client.ImportEndpoints(ctx, discovered) // conflict_policy=skip, sync_mode=additive
// After a complete collect (not partial boot):
// _, _ = client.ImportEndpoints(ctx, discovered, authsdk.WithConflictPolicy("update"), authsdk.WithSyncMode(authsdk.SyncModeMarkStale))
// Explicit prune (CI/CLI only — do not call on every process start):
// _, _ = client.ImportEndpoints(ctx, discovered, authsdk.WithSyncMode(authsdk.SyncModePrune))
// or: client.SyncHTTPRoutes(ctx, reg, authsdk.WithConflictPolicy("update"))

// Echo (separate module — only if you use Echo):
//   go get github.com/hinha/auth-sdks/go/routes/echo@latest
// _, _ = client.ImportEndpoints(ctx, echoadapter.Collect(e))

// Gin (separate module — only if you use Gin):
//   go get github.com/hinha/auth-sdks/go/routes/gin@latest
// _, _ = client.ImportEndpoints(ctx, ginadapter.Collect(engine))
```

`sync_mode` orphan handling (only `source=sdk` rows):

| Mode | Behavior |
|---|---|
| `additive` (default) | create/update only |
| `mark_stale` | missing SDK routes → `status=inactive` + `stale_at` |
| `prune` | missing SDK routes → soft-delete |

CMS-created endpoints stay `source=cms` and are never pruned by the SDK.

Auth Service endpoint: `POST /v1/consumer-auth/endpoints/import`.

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
| `AUTH_NEW_PASSWORD` | first-login smoke | New password when Login returns first-login gate |
