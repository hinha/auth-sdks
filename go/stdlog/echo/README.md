# stdlog/echo

Module: `github.com/hinha/auth-sdks/go/stdlog/echo`

Echo middleware for [`stdlog`](../) access logging (strict field schema).

## Install

```bash
go get github.com/hinha/auth-sdks/go/stdlog/echo@latest
```

## Usage

```go
import (
	"github.com/hinha/auth-sdks/go/stdlog"
	echoadapter "github.com/hinha/auth-sdks/go/stdlog/echo"
	"github.com/labstack/echo/v4"
)

e := echo.New()
e.Use(echoadapter.Middleware(log, stdlog.AccessLogConfig{}))
```

Sets / propagates `X-Request-Id`, injects logger + request id into context, logs with route from `c.Path()`.

## Test

```bash
go test ./...
```
