# app-foundation

Small Go primitives for the platforms' first-party apps; a toolkit, not a framework. Usage: `go doc <pkg>` (examples in `example_test.go`).

- `access`: Cloudflare Access JWT, fail-closed local mode, `ConfigFromEnv`, bearer guards `Internal`/`Remote`.
- `identity`: Access caller → directory person (role, actsFor, spaces), cached with stale-if-error, `Middleware` after access, fail closed; fake `identitytest`.
- `profile`: `/api/me` for the frontend module `profile` (person, photo, settings, language, other apps; the menu).
- `agentkit`: shared parts of an app's MCP tools (caller ceilings, references, runs, cut results).
- `httpx`: request IDs, bounded JSON decoding, `{"error":"..."}` envelope, API 404.
- `server`: bounded `http.Server`, `Health` (/healthz), `Ready` (/readyz), graceful shutdown.
- `sqlite`: bounded pure-Go SQLite (WAL, BEGIN IMMEDIATE), read-only, snapshots, forward-only migrations.
- `web`: embedded SPA with cache rules, `PublicFiles` for /pwa/, security `Headers`.
- `googleauth`: a person's Google grant (consent URL, code exchange, refresh token sealed in Omni's format, cached access tokens); storage stays in apps, never revoke.
- `llm`, `memory`, `push`: clients; `frontendregistry`: frontend source installer; `appscaffold`: app generator.

Composition, outermost first: `web.Headers` → probes → `access.Internal` routes → `web.PublicFiles(spa, gated)` → Access → `identity` (on `/api/`) → `/api/`, SPA.

Rules: no platform names (org, domain, Access team, registry, secret path; examples use example.org,
acme); extract only what a consumer proved; no app object, router, ORM, DI; schemas, CSP, policy and copy
stay in apps; local auth only on literal `127.0.0.1` with a loopback peer; never `web.Public` alone as a gate.

Managed in apps: registry modules `api`, `pwa`, `pwa-shell`, `profile` (`check`/`sync`); `public/sw.js` is a template
copy (apps extend `sw-app.js`); everything else the scaffold writes belongs to the app.

Change: tests for changed behaviour; in WSL `go vet ./... && go test -race ./...` (Node runs the sw.js test);
commit to `main` without attribution; CI green (go, minimum-go, scaffold). Release: SemVer (breaking = new
major agreed by both platform owners), MIGRATING.md entry, `git tag -a vX.Y.Z`, `gh release create vX.Y.Z
--verify-tag --generate-notes`. The repo is private: never put a GitHub credential in a build or config.
