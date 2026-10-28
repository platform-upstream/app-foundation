// Package server supplies conservative net/http defaults and graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 20 * time.Second
	DefaultWriteTimeout      = 20 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
	DefaultMaxHeaderBytes    = 32 << 10
	ShutdownTimeout          = 5 * time.Second
)

// New returns an HTTP server with the limits already used by the small Go applications.
func New(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		ReadTimeout:       DefaultReadTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		MaxHeaderBytes:    DefaultMaxHeaderBytes,
	}
}

// Health returns a shallow, private-data-free process health endpoint. The variadic argument keeps
// v0.1 source compatibility but checks are intentionally ignored. Dependency readiness is
// application-owned and should only be added when a demonstrated consumer needs it.
func Health(_ ...func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"ok\":true}\n"))
	})
}

// Run listens at srv.Addr until the context is cancelled or the server fails.
func Run(ctx context.Context, srv *http.Server) error {
	return RunWithShutdownTimeout(ctx, srv, ShutdownTimeout)
}

// RunWithShutdownTimeout is Run with an application-selected positive graceful-shutdown deadline.
func RunWithShutdownTimeout(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error {
	if srv == nil {
		return errors.New("server is nil")
	}
	return run(ctx, srv, srv.ListenAndServe, shutdownTimeout)
}

// Serve is Run with a caller-provided listener, useful when the port is assigned externally.
func Serve(ctx context.Context, srv *http.Server, listener net.Listener) error {
	return ServeWithShutdownTimeout(ctx, srv, listener, ShutdownTimeout)
}

// ServeWithShutdownTimeout is Serve with an application-selected positive graceful-shutdown
// deadline.
func ServeWithShutdownTimeout(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	if srv == nil {
		return errors.New("server is nil")
	}
	if listener == nil {
		return errors.New("listener is nil")
	}
	return run(ctx, srv, func() error { return srv.Serve(listener) }, shutdownTimeout)
}

func run(ctx context.Context, srv *http.Server, serve func() error, shutdownTimeout time.Duration) error {
	if srv == nil {
		return errors.New("server is nil")
	}
	if ctx == nil {
		return errors.New("context is nil")
	}
	if shutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	result := make(chan error, 1)
	go func() { result <- serve() }()

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownContext); err != nil {
			closeErr := srv.Close()
			serveErr := <-result
			if errors.Is(serveErr, http.ErrServerClosed) {
				serveErr = nil
			}
			return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr, serveErr)
		}
		err := <-result
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
