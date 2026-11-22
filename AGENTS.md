# app-foundation

Small Go primitives for the platforms' first-party apps; a toolkit, not a framework. Usage: `go doc <pkg>` (examples in `example_test.go`).

- `access`: Cloudflare Access JWT, fail-closed local mode, `ConfigFromEnv`, bearer guards `Internal`/`Remote`.
- `identity`: Access caller → directory person (role, actsFor, spaces), cached with stale-if-error, `Middleware` after access, fail closed; fake `identitytest`.
- `kids`: directory's kid profiles for games (`Client`: families, kid create/rename/delete/photo for a family admin, `SelfRegistered`, `AdminFamily`; `Roster`: kids granted the app, synced) and registry module `kids` (tiles, kid form, guest note, takeover, account actions; content only, no look, de/en).
- `profile`: `/api/me` for the frontend module `profile` (person, photo, settings, language, other apps; the menu).
- `agentkit`: shared parts of an app's MCP tools (caller ceilings, references, runs, cut results).
- `httpx`: request IDs, bounded JSON decoding, `{"error":"..."}` envelope, API 404.
- `server`: bounded `http.Server`, `Health` (/healthz), `Ready` (/readyz), `Version` (/version from `APP_VERSION`/`APP_COMMIT`), `Probes` (all three), graceful shutdown.
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
Also `push`, `palette`, `scroll-edges`: look-neutral mechanics only (handbook 0005), never colours,
radii, shadows or fonts; their Node tests (`frontendregistry/testdata/*_test.mjs`) need Node 22.18+.
`public/sw-push.js` and `public/profile-theme.js` are module files (checked), unlike `public/sw.js`.
UI mechanics `sheet`, `toast`, `pull-refresh`, `busy` (handbook ADR 0005): behaviour only, cores without React, CSS in
`@layer foundation` with no look values (a test checks); TS logic tests need Node 22.18+, the browser run is
`frontendregistry/testdata/mechanics_e2e.mjs`.
UI mechanics modules (`touch`, `motion`, `phone-audit`) carry behaviour only, never look values (colours, radii,
shadows, fonts, durations, nav look); CSS in `@layer foundation`, no `!important`. Their TS/JS tests run on Node with
type stripping; `PHONE_AUDIT_PLAYWRIGHT=<path to the playwright package>` (and `CHROME`) runs the phone audit.

Change: tests for changed behaviour; in WSL `go vet ./... && go test -race ./...` (Node runs the sw.js test);
commit to `main` without attribution; CI green (go, minimum-go, scaffold). Release: SemVer (breaking = new
major agreed by both platform owners), MIGRATING.md entry, and a `Release: none|patch|minor|major` commit
trailer; after green CI the platform CI's release step tags the next version (docs-only releases nothing, a major becomes a
draft). Never tag by hand. The repo is private: never put a GitHub credential in a build or config.
