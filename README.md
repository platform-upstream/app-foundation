# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for niiku first-party applications.
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification and loopback-only local mode.
- `httpx`: request IDs plus strict bounded JSON decoding and the existing `{"error":"..."}` shape.
- `server`: explicit HTTP server limits, health responses, and graceful context-driven shutdown.
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

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", server.Health(db.PingContext))
	mux.Handle("/", spa)

	srv := server.New("127.0.0.1:8080", httpx.RequestIDs(mux))
	if err := server.Run(ctx, srv); err != nil { log.Fatal(err) }
}
```

Applications construct their own route tree and choose their own authentication failure messages.
Compose middleware in this order: application security headers; public `/healthz`; separately
bearer-authenticated internal routes; Cloudflare/local authentication for everything else (including
static files); and same-origin plus body limits at the API layer. Mount `/api/` and `/internal/`
handlers before the SPA handler. See package documentation and tests for focused examples.

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
