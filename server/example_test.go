package server_test

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/platform-upstream/app-foundation/server"
)

// A main: the public probes (shallow liveness on /healthz, readiness with a database ping on
// /readyz, the release on /version), the bounded server, and graceful shutdown on SIGTERM
// (Kubernetes) or Ctrl-C.
func Example() {
	var db *sql.DB // sqlite.Open(...)
	app := http.NewServeMux()

	mux := http.NewServeMux()
	server.Probes(mux, db.PingContext)
	mux.Handle("/", app)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, server.New(":8080", mux)); err != nil {
		log.Fatal(err)
	}
}
