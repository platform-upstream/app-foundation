package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewUsesBoundedDefaults(t *testing.T) {
	srv := New("127.0.0.1:8080", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != DefaultReadHeaderTimeout || srv.ReadTimeout != DefaultReadTimeout || srv.WriteTimeout != DefaultWriteTimeout || srv.IdleTimeout != DefaultIdleTimeout || srv.MaxHeaderBytes != DefaultMaxHeaderBytes {
		t.Fatalf("unexpected server defaults: %#v", srv)
	}
}

func TestRunRejectsNilServer(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("Run accepted nil server")
	}
}

func TestServeRejectsNilListener(t *testing.T) {
	if err := Serve(context.Background(), New("127.0.0.1:0", http.NotFoundHandler()), nil); err == nil {
		t.Fatal("Serve accepted nil listener")
	}
}

func TestHealth(t *testing.T) {
	for _, handler := range []http.Handler{
		Health(),
		Health(func(context.Context) error { return errors.New("legacy dependency failure") }),
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}`+"\n" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("health = %d %q %#v", w.Code, w.Body.String(), w.Header())
		}
	}
}

func TestServeGracefullyFinishesActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := New(listener.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "done")
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, listener) }()

	response := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			defer resp.Body.Close()
			_, err = io.ReadAll(resp.Body)
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Serve returned before active request completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-response; err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestServeForcesCloseAtShutdownDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := New(listener.Addr().String(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeWithShutdownTimeout(ctx, srv, listener, 30*time.Millisecond) }()

	response := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			defer resp.Body.Close()
			_, err = io.ReadAll(resp.Body)
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter handler")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ServeWithShutdownTimeout error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(started); elapsed < 20*time.Millisecond || elapsed > 500*time.Millisecond {
			t.Fatalf("shutdown elapsed = %v, want bounded near configured deadline", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeWithShutdownTimeout exceeded bounded deadline")
	}
	close(release)
	select {
	case <-response:
	case <-time.After(time.Second):
		t.Fatal("client did not return after forced close")
	}
}

func TestServeRejectsInvalidShutdownTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	err = ServeWithShutdownTimeout(context.Background(), New(listener.Addr().String(), http.NotFoundHandler()), listener, 0)
	if err == nil {
		t.Fatal("ServeWithShutdownTimeout accepted zero timeout")
	}
}
