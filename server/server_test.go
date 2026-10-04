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
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 20*time.Second || srv.WriteTimeout != 20*time.Second || srv.IdleTimeout != 60*time.Second || srv.MaxHeaderBytes != 32<<10 {
		t.Fatalf("unexpected server defaults: %#v", srv)
	}
}

func TestRunRejectsNilServer(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("Run accepted nil server")
	}
}

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		check  func(context.Context) error
		status int
		body   string
	}{
		{"healthy", func(context.Context) error { return nil }, 200, `{"ok":true}` + "\n"},
		{"unhealthy", func(context.Context) error { return errors.New("private detail") }, 503, `{"ok":false}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			Health(tc.check).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if w.Code != tc.status || w.Body.String() != tc.body || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("health = %d %q %#v", w.Code, w.Body.String(), w.Header())
			}
		})
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
