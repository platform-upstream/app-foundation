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

// Health returns a shallow, private-data-free process health endpoint for liveness (/healthz):
// it answers 200 while the process serves HTTP. The variadic argument only keeps v0.1 source
// compatibility; checks passed to it are ignored, and passing any is deprecated. Use Ready for
// dependency checks.
func Health(_ ...func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"ok\":true}\n"))
	})
}

// ReadyCheckTimeout bounds each readiness check.
const ReadyCheckTimeout = 2 * time.Second

// Ready returns a readiness endpoint (/readyz) that runs every check concurrently, each with its
// own ReadyCheckTimeout deadline derived from the request. It answers 200 {"ok":true} when all
// pass and 503 {"ok":false} otherwise; a check that panics or overruns its deadline fails. The
// response never names the failing check or its error: log inside the check if you need the
// reason. Keep checks cheap and local (a database ping), since the kubelet calls it every few
// seconds and a failing dependency elsewhere would take every pod out of the service at once.
func Ready(checks ...func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := true
		results := make(chan error, len(checks))
		for _, check := range checks {
			go func() { results <- runCheck(r.Context(), check) }()
		}
		for range checks {
			if err := <-results; err != nil {
				ok = false
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"ok\":false}\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"ok\":true}\n"))
	})
}

// runCheck runs one check under its deadline and stops waiting when the deadline passes, even if
// the check ignores its context.
func runCheck(parent context.Context, check func(context.Context) error) error {
	if check == nil {
		return errors.New("readiness check is nil")
	}
	ctx, cancel := context.WithTimeout(parent, ReadyCheckTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- errors.New("readiness check panicked")
			}
		}()
		done <- check(ctx)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
