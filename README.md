# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for the first-party applications of the platforms built on platform-upstream (castillo, niiku).
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification, loopback-only local mode, and the bearer-key
  guards for in-cluster internal routes (`Internal`) and for remote callers' bypassed paths (`Remote`).
- `agentkit`: what an app's MCP tools at `/mcp` share (the `mcp-tools` skill): caller keys with
  class ceilings behind `access.Internal`, a second door for agents outside the cluster
  (`GuardRemote`, ceiling at most prepare), references `<app>:<noun>/<id>`, idle-bounded runs, cut
  results and tool errors. Each app keeps its tools, write log and undo.
- `appscaffold`: a one-time repository-local Docker/CI/Helm scaffold generator.
- `httpx`: opt-in request IDs, strict bounded JSON decoding, an explicit JSON API 404, and the
  standard `{"error":"..."}` shape with application-owned copy and status semantics.
- `frontendregistry`: a checked source installer for the React/Vite template, the iOS-safe mobile shell (`pwa-shell`), same-origin JSON client,
  and PWA lifecycle; it shares no theme or layout.
- `llm`: the portable LLM gateway client (completion, JSON-schema output, streaming with tools) and its `llmtest` fake.
- `push`: Web Push (VAPID) delivery to the subscriptions an app stores, with the endpoint
  allowlist, subscription bound and the Apple `mailto:` fix that Omni and Console each carried.
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
The source registry workflow and its deliberate non-abstractions are documented in
[docs/frontend-registry.md](docs/frontend-registry.md).
The credential-free delivery starting point and app-owned customization boundaries are documented
in [docs/app-scaffold.md](docs/app-scaffold.md).
The scaffold test generates a complete application and runs its own vet and tests on every run;
it replaced the former reference application.

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
issued-at, and subject against a bounded/cached JWKS fetch. Access is an owner gate, so there is no
shared identity model: `Middleware` only puts a `Caller` into the request context (`CallerFrom`),
which says whether a person signed in (`Person()`), an accepted service token called, or a local
request came in, for routes that must refuse service tokens (Finance's bank routes). Local
mode still requires the literal `127.0.0.1` listener and checks the actual request peer. Mutation
origin checks accept the configured external origins or the direct request origin, reject ambiguous
duplicate browser headers, and continue to accept missing `Origin` unless `Sec-Fetch-Site` says
`cross-site`, matching the proven Read and Cook behavior. `Internal` guards the routes an app
serves outside Access for its sibling apps in the cluster (Omni's mail bridge, Paper's intake): one
bearer key per caller, compared in constant time, the matched caller name in the context
(`InternalCallerFrom`), and any request carrying a header Traefik, cloudflared or Access adds
(`X-Forwarded-*`, `Forwarded`, `X-Real-Ip`, `Cf-Connecting-Ip`, `Cf-Ray`, the Access assertion) is
refused, so the public hostname never reaches them. A key shorter than 32 bytes, an unset one
included, closes the route. Authorization policy, which callers exist, their NetworkPolicies,
rejection headers/copy, and CSP remain application-owned.

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

`llm` calls the LLM gateway's chat completions API with one portable request shape, so an app
works whichever model the gateway serves under its alias (the local Gufo model or Claude). Every
request names the app's alias (no `local-llm` fallback: a key without an alias fails in `New`),
an explicit `MaxTokens` ceiling for thinking and answer together, and a reasoning `Effort`
(`none` when unset, because backends differ in what an omitted effort means). Structured output is
a strict JSON schema through `CompleteJSON`; `Temperature` is a best-effort hint that Claude drops.
It never sends a backend's own switches (`chat_template_kwargs`, `thinking_budget_tokens`, `top_k`,
`repeat_penalty`, `json_object`). The one non-portable piece is the `File`
part for documents: through the gateway Claude reads PDF and text files, the local runtime rejects it
with a 400, so an app sends extracted text where it can. A client without an API key is disabled and returns
`ErrDisabled`, so an app keeps running without inference. Deadlines come from the caller's context;
there are no retries, and bodies are never logged. `llm/llmtest` is a recording fake gateway for
tests. `Stream` sends the same request as a stream and reports text, reasoning and tool-call
events as they arrive; it assembles fragmented tool calls (at most 64, 1 MiB of arguments each),
moves `<think>` text to the reasoning, reads a plain JSON reply as one, and returns the partial
reply with `ErrIncomplete` when the stream ends without `[DONE]`. `Usage` includes cached prompt
tokens; the local runtime's `timings` are passed through raw. Tool loops and tool execution stay
in the app. Prompts, schemas, ceilings, concurrency, caches and validation remain app-owned, and each app
keeps its own small interface in front of the client. Design and rollout:
`docs/llm/shared-llm-client-proposal.md` in the workspace repository.

## Private-module consumption

Developers and release automation that resolve the private module need GitHub authentication and:

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
go get github.com/platform-upstream/app-foundation@v0.1.0
```

Consumers should pin an exact release. For container builds, resolve the module outside the image
build, then commit the resulting `vendor/` tree and build with `go build -mod=vendor`. This keeps
GitHub credentials out of build arguments and image layers. `go mod vendor` records the source;
`go.mod` and `go.sum` retain the exact module version.

## Version and release policy

SemVer from v1.0.0 (see [MIGRATING.md](MIGRATING.md)). Two platforms consume it (castillo, niiku),
so: compatible additions are a minor, fixes a patch, and a breaking change is a new major that
both owners agree on first. Consumers pin an exact tag; Renovate proposes bumps. Before tagging,
run the checks in WSL or Linux (Windows file locking breaks some tests):

```bash
go test ./... && go vet ./... && go test -race ./...
git tag -a vX.Y.Z -m "app-foundation vX.Y.Z" && git push origin vX.Y.Z
gh release create vX.Y.Z --verify-tag --generate-notes
```

Nothing platform-specific goes in here: no org, domain, team, registry or secret path. Those come
from the consumer (environment, chart values, the platform profile of the scaffold).
