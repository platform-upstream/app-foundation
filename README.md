# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for the first-party applications of the platforms built on platform-upstream (castillo, niiku).
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification, loopback-only local mode, `ConfigFromEnv`, and the bearer-key
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
- `server`: explicit HTTP server limits, shallow liveness (`Health`), bounded readiness (`Ready`),
  and graceful context-driven shutdown with a bounded deadline.
- `sqlite`: bounded pure-Go SQLite setup, explicit read-only operation, consistent snapshots, and
  ordered embedded transactional migrations.
- `web`: embedded SPA/static serving with safe fallback and cache rules, public `/pwa/` files, and
  security headers (`Headers`).

There is intentionally no app object, router, ORM, repository layer, generic worker, deployment
runtime, generated API contract layer, or shared visual system. The bounded Read-like generation
experiment and rejection are recorded in [docs/api-contract-generation.md](docs/api-contract-generation.md).
The source registry workflow and its deliberate non-abstractions are documented in
[docs/frontend-registry.md](docs/frontend-registry.md).
The credential-free delivery starting point and app-owned customization boundaries are documented
in [docs/app-scaffold.md](docs/app-scaffold.md).
The scaffold test generates a complete application and runs its own vet and tests on every run;
it replaced the former reference application.

## Use

API detail and runnable usage live in the packages: `go doc <package>` shows each package's example
(`example_test.go` in `access`, `server`, `sqlite`, `web`). This README keeps the design decisions.

## Design decisions

**Composition.** Applications build their own route tree and choose their own status semantics,
validation and failure copy. Outermost first: `web.Headers`; optional `httpx.RequestIDs`; the public
probes `/healthz` and `/readyz`; bearer-authenticated internal routes (`access.Internal`);
`web.PublicFiles(spa, gated)`; Cloudflare/local authentication for everything else, static files
included, so a signed-out document reaches Access instead of the embedded shell; same-origin checks
and bounded JSON decoding at the API, with an `httpx.NotFound` `/api/` boundary before the SPA.
Request IDs are opt-in because an added response header is observable.

**`web.PublicFiles(spa, gated)` is the only safe way to serve `/pwa/` without a session.** Never
use `web.Public(r)` to decide that the whole mux (or any handler but the static file handler) runs
unauthenticated: every `GET /pwa/<anything>.<ext>` would then reach app routes without a session.

**Probes.** `Health` stays shallow (liveness: a restart cannot fix a dependency). Dependency checks
go to `Ready`, which never names the failing check in its response. Keep readiness local (a
database ping): a shared dependency that fails would take every pod out of the service at once.
Checks passed to `Health` are ignored (v0.1 call shape, deprecated).

**Access.** It is an owner gate, so there is no shared identity model: the middleware only puts a
`Caller` into the context, for the few routes that must refuse service tokens. Everything fails
closed: missing configuration is a startup error, and local mode requires the literal `127.0.0.1`
listener and a loopback peer per request. Internal routes refuse anything that passed the ingress,
tunnel or Access, so the public hostname never reaches them. Mutation origin checks accept a missing
`Origin` unless `Sec-Fetch-Site` says `cross-site`, matching the proven Read and Cook behaviour.
Authorization policy, the callers that exist, their NetworkPolicies and rejection copy stay in apps.

**SQLite.** One open connection, WAL, a busy timeout, and `BEGIN IMMEDIATE` transactions: old and new
pod share the file during a rolling update, and a deferred transaction that reads, sees the other
process commit, then writes fails with `SQLITE_BUSY_SNAPSHOT`, which no busy timeout cures.
Immediate transactions take the write lock up front and wait instead; read-only transactions
(`sql.TxOptions{ReadOnly: true}`) stay deferred. Migrations are forward-only and transactional, and
should be additive because both pods run against the new schema for a moment. `Snapshot` uses the
online backup API (consistent, row IDs kept) and never replaces an existing file. Schemas, exports,
restore policy and retention stay in apps.

**Web.** `web.Handler` keeps Read's verified cache contract: documents `no-store`, `sw.js` and the
`/pwa/` files revalidate, `/assets/` immutable, missing files and `/api/`, `/internal/` 404 instead
of the shell, filesystem errors 500. `web.Headers` sets the standard security headers; the CSP is
the app's (`HeaderPolicy.CSP`), and an empty one means the strict `DefaultCSP` (no inline script or
style, no third-party origin), so an app adopting it without a CSP checks its pages first. The
service worker's browser behaviour (shell and `/assets/` only, no `/api/`, `/internal/`,
`/cdn-cgi/`, `/pwa/`, no `no-store`/`private` responses) is installed as source by the frontend
registry, not hidden in the Go handler.

**Clients.** `memory` is the byte-identical Cook/Watch client (cache, durable outbox, conflict
preservation; table names kept so adoption reuses app data). `llm` sends one portable request shape
so an app works with whichever model the gateway serves under its alias: an explicit alias, token
ceiling and reasoning effort on every request, no backend-specific switches, no retries, bodies
never logged, and a disabled client (`ErrDisabled`) without a key so the app runs without
inference. Prompts, schemas, tool loops, ceilings and caches stay in apps. Design and rollout:
`docs/llm/shared-llm-client-proposal.md` in the workspace repository.

## Private-module consumption

Developers and release automation that resolve the private module need GitHub authentication and:

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
go get github.com/platform-upstream/app-foundation@v1.1.0
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
