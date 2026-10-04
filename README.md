# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for niiku first-party applications.
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification and loopback-only local mode.
- `httpx`: opt-in request IDs, strict bounded JSON decoding, an explicit JSON API 404, and the
  standard `{"error":"..."}` shape with application-owned copy and status semantics.
- `server`: explicit HTTP server limits, shallow process health, and graceful context-driven
  shutdown with a bounded deadline.
- `sqlite`: bounded pure-Go SQLite setup and ordered embedded transactional migrations.
- `web`: embedded SPA/static serving with safe fallback and cache rules.

There is intentionally no app object, router, ORM, repository layer, worker, LLM client, Memory
client, deployment scaffold, or frontend registry in this first release.

## Minimal use

```go
//go:embed all:dist
var frontend embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sqlite.Open(ctx, ".local/app.sqlite", sqlite.Options{})
	if err != nil { log.Fatal(err) }
	defer db.Close()

	assets, err := fs.Sub(frontend, "dist")
	if err != nil { log.Fatal(err) }
	spa, err := web.Handler(assets)
	if err != nil { log.Fatal(err) }

	protected := http.NewServeMux()
	protected.Handle("GET /api/items", itemsHandler(db))
	protected.Handle("/api/", httpx.NotFound("Not found."))
	protected.Handle("/", spa)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", server.Health())
	mux.Handle("/", protected)

	srv := server.New("127.0.0.1:8080", httpx.RequestIDs(mux))
	if err := server.Run(ctx, srv); err != nil { log.Fatal(err) }
}
```

Applications construct their own route tree and choose their own status semantics, validation and
authentication failure messages. Compose middleware in this order: application security headers;
optional request IDs; public shallow `/healthz`; separately bearer-authenticated internal routes;
Cloudflare/local authentication for everything else (including static files); and same-origin plus
bounded JSON decoding at the API layer. Mount an `httpx.NotFound` `/api/` boundary after registered
API routes and before the SPA handler. The ordinary JSON body default is 32 KiB; upload/import
limits remain app-owned. Request IDs remain opt-in because adding a response header is observable
and the verified Read contract does not currently expose one. See package tests for focused
examples.

The default server limits are 5 seconds for headers, 20 seconds for reads and writes, 60 seconds
idle, 32 KiB for headers, and 5 seconds for graceful shutdown. Use
`RunWithShutdownTimeout`/`ServeWithShutdownTimeout` only when a demonstrated application lifecycle
needs a different positive shutdown deadline. Dependency checks do not belong in the shared health
handler; applications may expose a separate readiness endpoint when a real consumer requires one.
The legacy `Health(check)` call shape still compiles for `v0.1.0` consumers, but checks are ignored;
use an application-owned readiness route if dependency state genuinely needs to affect routing.

## Private-module consumption

Developers and release automation that resolve the private module need GitHub authentication and:

```bash
go env -w GOPRIVATE=github.com/niiku-platform/*
go get github.com/niiku-platform/app-foundation@v0.1.0
```

Consumers should pin an exact release. For container builds, resolve the module outside the image
build, then commit the resulting `vendor/` tree and build with `go build -mod=vendor`. This keeps
GitHub credentials out of build arguments and image layers. `go mod vendor` records the source;
`go.mod` and `go.sum` retain the exact module version.

## Version and release policy

The module follows SemVer and remains on `v0.x.y` while the first consumers prove the interfaces.
Breaking pre-1.0 changes increment the minor version; compatible fixes increment the patch version.
The initial `v0.1.0` may be cut from reviewed, green `main` once the Read inventory and independent
foundation review confirm its interface. Later releases require an affected consumer to verify the
changed interface before tagging:

```bash
go test ./...
go vet ./...
go test -race ./...
git tag -a v0.1.0 -m "app-foundation v0.1.0"
git push origin v0.1.0
gh release create v0.1.0 --verify-tag --generate-notes
```

No release tag is created as part of the initial skeleton commit; the coordinator owns the review
gate and release decision.
