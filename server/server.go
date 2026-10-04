// Package server supplies conservative net/http defaults and graceful shutdown.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const ShutdownTimeout = 5 * time.Second

// New returns an HTTP server with the limits already used by the small Go applications.
func New(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}

// Health returns a private-data-free health endpoint. Every check must pass; no failing check or
// error text is disclosed in the response.
func Health(checks ...func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		for _, check := range checks {
			if check != nil && check(r.Context()) != nil {
				status = http.StatusServiceUnavailable
				break
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": status == http.StatusOK})
	})
}

// Run listens at srv.Addr until the context is cancelled or the server fails.
func Run(ctx context.Context, srv *http.Server) error {
	if srv == nil {
		return errors.New("server is nil")
	}
	return run(ctx, srv, srv.ListenAndServe)
}

// Serve is Run with a caller-provided listener, useful when the port is assigned externally.
func Serve(ctx context.Context, srv *http.Server, listener net.Listener) error {
	if srv == nil {
		return errors.New("server is nil")
	}
	return run(ctx, srv, func() error { return srv.Serve(listener) })
}

func run(ctx context.Context, srv *http.Server, serve func() error) error {
	if srv == nil {
		return errors.New("server is nil")
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
		shutdownContext, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownContext); err != nil {
			closeErr := srv.Close()
			return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr)
		}
		err := <-result
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
