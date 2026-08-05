// Package callback helps owner services listen for task-hub TaskCallback/Dispatch.
package callback

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"

	callbackv1 "github.com/hinha/auth-sdks/go/taskhub/callback/v1"
)

// Listener wraps a gRPC server serving TaskCallback.
type Listener struct {
	server *grpc.Server
	addr   string
	logger *slog.Logger
}

// Option configures Listen.
type Option func(*listenConfig)

type listenConfig struct {
	logger     *slog.Logger
	grpcServer *grpc.Server
}

// WithLogger sets the logger used for start/stop messages.
func WithLogger(l *slog.Logger) Option {
	return func(c *listenConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithGRPCServer uses an existing grpc.Server (e.g. shared with other services).
func WithGRPCServer(s *grpc.Server) Option {
	return func(c *listenConfig) {
		c.grpcServer = s
	}
}

// Listen binds TCP :port and serves TaskCallback. Returns nil,nil if port <= 0 or svc is nil.
func Listen(port int, svc callbackv1.TaskCallbackServer, opts ...Option) (*Listener, error) {
	if port <= 0 || svc == nil {
		return nil, nil
	}
	cfg := listenConfig{logger: slog.Default()}
	for _, opt := range opts {
		opt(&cfg)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen taskhub callback :%d: %w", port, err)
	}

	gs := cfg.grpcServer
	if gs == nil {
		gs = grpc.NewServer()
	}
	callbackv1.RegisterTaskCallbackServer(gs, svc)

	l := &Listener{server: gs, addr: lis.Addr().String(), logger: cfg.logger}
	go func() {
		cfg.logger.Info("starting task-hub callback gRPC", "addr", l.addr)
		if err := gs.Serve(lis); err != nil {
			cfg.logger.Error("task-hub callback gRPC stopped", "error", err)
		}
	}()
	return l, nil
}

// Addr returns the bound address (e.g. "[::]:9090").
func (l *Listener) Addr() string {
	if l == nil {
		return ""
	}
	return l.addr
}

// GracefulStop stops the callback gRPC server.
func (l *Listener) GracefulStop() {
	if l == nil || l.server == nil {
		return
	}
	l.logger.Info("stopping task-hub callback gRPC", "addr", l.addr)
	l.server.GracefulStop()
}
