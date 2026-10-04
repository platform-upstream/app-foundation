# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for niiku first-party applications.
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification and loopback-only local mode.
- `httpx`: opt-in request IDs, strict bounded JSON decoding, an explicit JSON API 404, and the
  standard `{"error":"..."}` shape with application-owned copy and status semantics.
- `memory`: the proven shared Memory service client, including the identical Cook/Watch SQLite
  cache, durable outbox, and conflict behavior.
- `server`: explicit HTTP server limits, shallow process health, and graceful context-driven
  shutdown with a bounded deadline.
- `sqlite`: bounded pure-Go SQLite setup, explicit read-only operation, consistent snapshots, and
  ordered embedded transactional migrations.
- `web`: embedded SPA/static serving with safe fallback and cache rules.

There is intentionally no app object, router, ORM, repository layer, generic worker, deployment
runtime, generated API contract layer, or shared visual system. The bounded Read-like generation
experiment and rejection are recorded in [docs/api-contract-generation.md](docs/api-contract-generation.md).

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

Cloudflare mode verifies one bounded `Cf-Access-Jwt-Assertion`, RS256, issuer, audience, expiry,
issued-at, and subject against a bounded/cached JWKS fetch. It deliberately returns only success or
an error: current consumers use Access as an owner gate and do not share an identity model. Local
mode still requires the literal `127.0.0.1` listener and checks the actual request peer. Mutation
origin checks accept the configured external origins or the direct request origin, reject ambiguous
duplicate browser headers, and continue to accept missing `Origin` unless `Sec-Fetch-Site` says
`cross-site`, matching the proven Read and Cook behavior. Authorization policy, internal-service
credentials, rejection headers/copy, and CSP remain application-owned.

SQLite defaults to WAL, foreign keys, a five-second busy timeout, a 4 MiB page cache, and one
open/idle connection. `OpenReadOnly` accepts an ordinary existing path, adds `mode=ro`, and enforces
`query_only`; `Immutable` is reserved for isolated self-contained snapshots with no live WAL.
Migration files use `<version>_<lowercase-name>.sql`; applied files are forward-only and may not be
removed or renamed. Each pending migration and its state row commit in one transaction. `Snapshot`
uses SQLite's online backup API to capture committed live/WAL content consistently without changing
implicit row IDs, verifies integrity, syncs the result, and atomically publishes only to a
destination that does not exist. Callers own
snapshot retention and must supply a context deadline. Schemas, migration SQL, portable exports,
restore policy, and destructive-migration approval remain application-owned.

`web.Handler` serves a Vite-style embedded filesystem with Read's verified cache contract:
documents and ordinary files are `no-store`, `sw.js` and `manifest.webmanifest` revalidate, and
`/assets/` files are immutable for one year. Extensionless missing paths fall back to `index.html`;
missing assets, file-like paths, `/api/*`, and `/internal/*` return non-cacheable 404s. The API mux
still owns the JSON error response. Filesystem failures return 500 rather than silently booting the
SPA. Console's broader `no-cache` document policy is not generalized until another consumer needs
it, and CSP/security headers remain app-owned.

Authentication must wrap the web handler so a signed-out document reaches Cloudflare Access rather
than receiving the embedded shell. The frontend service worker must register network-first
navigations before its precache route, use the cached shell only for ordinary offline navigation,
never fall back for a reauthentication navigation, and never cache `/api/`, `/internal/`, or
`/cdn-cgi/` responses. Those browser behaviors are installed as source by the frontend registry;
they are not hidden inside this Go handler.

The `memory` package extracts the byte-identical client generated into Cook and Watch: bounded
authenticated calls, local cache, durable pending writes, conflict preservation, and lost-response
reconciliation. The app still owns its Memory scopes, note content, sync schedule, user-facing
conflict flow, and availability policy. The existing table names are retained so later adoption can
reuse app data without conversion.

No shared local-LLM client is included. Cook and Watch both call the stable `local-llm` alias, but
their payloads, deadlines, queueing, caching, structured-output checks, and fallbacks are materially
different. The reference app uses an injected app-local seam and a fake server; extraction waits
until two consumers demonstrate identical transport behavior. Prompts and output validation always
remain app-owned.

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
