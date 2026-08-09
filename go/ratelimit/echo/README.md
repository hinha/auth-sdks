# ratelimit/echo

Module: `github.com/hinha/auth-sdks/go/ratelimit/echo`

Echo middleware for [`ratelimit`](../).

## Install

```bash
go get github.com/hinha/auth-sdks/go/ratelimit/echo@latest
```

## Usage

```go
import (
	"github.com/hinha/auth-sdks/go/ratelimit"
	echoadapter "github.com/hinha/auth-sdks/go/ratelimit/echo"
	"github.com/labstack/echo/v4"
)

lim, err := ratelimit.New(ratelimit.Config{
	Enabled:  true,
	Profiles: map[string]string{"default": "30-M"},
})

e := echo.New()
e.Use(echoadapter.Middleware(lim))
```

Honors the same headers / fail-open policy as the `net/http` middleware.

## Test

```bash
go test ./...
```
