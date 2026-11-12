# app-foundation

`app-foundation` is the deliberately small Go backend toolkit for the first-party applications of the platforms built on platform-upstream.
It packages behavior already needed by Read and proven in Console, Cook, Watch, and Memory while
leaving application routes, schema, copy, policy, and domain code in each app.

The initial packages are:

- `access`: bounded Cloudflare Access JWT verification, loopback-only local mode, `ConfigFromEnv`, and the bearer-key
  guards for in-cluster internal routes (`Internal`) and for remote callers' bypassed paths (`Remote`).
- `identity`: who a signed-in caller is: the directory person (id, name, photo, role in this app,
  the kid profiles they act for) from directory's in-cluster identity API, cached with
  stale-if-error, as `Middleware` after access that fails closed; `identitytest` fakes directory.
- `agentkit`: what an app's MCP tools at `/mcp` share (the `mcp-tools` skill): caller keys with
  class ceilings behind `access.Internal`, a second door for agents outside the cluster
  (`GuardRemote`, ceiling at most prepare), references `<app>:<noun>/<id>`, idle-bounded runs, cut
  results and tool errors. Each app keeps its tools, write log and undo.
- `appscaffold`: a one-time repository-local Docker/CI/Helm scaffold generator.
- `httpx`: opt-in request IDs, strict bounded JSON decoding, an explicit JSON API 404, and the
  standard `{"error":"..."}` shape with application-owned copy and status semantics.
- `googleauth`: a person's own Google grant for an app: the consent URL, the code exchange that
  names the account and checks the granted scopes, the refresh token sealed (AES-256-GCM) in the
  format Omni and Paper store, and access tokens cached in memory with one refresh at a time.
- `frontendregistry`: a checked source installer for the React/Vite template, the iOS-safe mobile shell (`pwa-shell`), same-origin JSON client,
  and PWA lifecycle; it shares no theme or layout.
- `llm`: the portable LLM gateway client (completion, JSON-schema output, streaming with tools) and its `llmtest` fake.
- `push`: Web Push (VAPID) delivery to the subscriptions an app stores, with the endpoint
  allowlist, subscription bound and the Apple `mailto:` fix that Omni and Console each carried;
  `Notify` hands the owner's messages to a platform's hub instead, when one is configured.
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
(`example_test.go` in `access`, `push`, `server`, `sqlite`, `web`). This README keeps the design decisions.

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

**Access.** It proves the login and nothing more: the middleware puts a `Caller` (subject, e-mail
or service token) into the context; who that is and what they may do is `identity`'s job. Everything fails
closed: missing configuration is a startup error, and local mode requires the literal `127.0.0.1`
listener and a loopback peer per request. Internal routes refuse anything that passed the ingress,
tunnel or Access, so the public hostname never reaches them. Mutation origin checks accept a missing
`Origin` unless `Sec-Fetch-Site` says `cross-site`, matching the proven Read and Cook behaviour.
Authorization policy, the callers that exist, their NetworkPolicies and rejection copy stay in apps.

**Identity.** Tenancy is a person within a platform, and who a person is comes from directory,
never from an app's own user list. `identity.Middleware` runs after access on the API: a person
signs in through Access (e-mail), service tokens act for a person named in `Options.ServiceTokens`,
local mode acts as `Options.Local`; everyone else, anyone directory does not grant this app, and
any request while directory cannot answer is refused. Apps key data by `Person.ID` and filter every
query by `identity.From`. Kids have no login: an adult sends `X-Act-For: <kid id>`, accepted only
for ids in `Person.ActsFor` that directory reports as kids; `From` is then the kid, `ActorFrom`
the adult. `Cache` keeps a person fresh for a minute (a removed grant takes effect within it),
serves a known person up to an hour longer while directory is unavailable (never an unknown one),
and shares concurrent lookups. Roles beyond owner/member/viewer and what they allow stay in apps.

**Backups.** A file copy of a live SQLite database (Velero's file system backup) can mix pages
from different moments. With `APP_SNAPSHOT_DIR` set (app-chart sets it to `<data>/.snapshots` for
apps with persistence), every database opened writable also writes an integrity-checked online
copy there every `APP_SNAPSHOT_INTERVAL` (1h) and keeps `APP_SNAPSHOT_KEEP` (2): the backup of the
volume always holds a copy that opens cleanly (`sqlite/snapshots.go`, restore steps there). An
interval counts from the pod's start, so a long one never fires in a pod that restarts more often;
`APP_SNAPSHOT_AT=HH:MM` instead takes one a day at that local time (`TZ`), the first at its next
occurrence: set it shortly before the volume backup. Setting both fails `Open`.

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

**Push hub.** A platform may have one app that tells the owner for every app, so the owner hears
one voice. `push.Config.Hub` (its in-cluster notify route and the app's key there, sent the way
`access.Internal` reads it) is off while empty, and only `Notify` uses it: a structured message
(title, body, a reference `<app>:<noun>/<id>`, urgency now or later, a stable id) goes to the
hub, and the app's own web push with its own payload goes out only without a hub or when the hub
fails (unreachable, slower than five seconds, 401/403/5xx). A message the hub calls invalid (400) is the
app's bug and is returned, not sent another way, and neither is one over the hub's bound (429); `Notify` checks the hub's rules on every
platform, so it fails the same without a hub. The hub tells one person: `Hub` goes only on a
`Sender` whose subscriptions are the owner's (an app with one `Sender` per person sets it on the
owner's alone); `Notify` has no audience, and `Send` and `SendTo` never reach the hub. Copy, payload
shapes and what is urgent stay in apps.

**Google grants.** `googleauth` is the consent flow and token handling Omni and Paper each carried,
on `golang.org/x/oauth2` (Google's endpoint from `endpoints`, not the `google` package, which would
pull the Cloud metadata client into every consumer's vendor tree). The account comes from OpenID
Connect userinfo, as in both apps, and must have a verified e-mail. The sealed format (ciphertext,
nonce and tag in separate base64 values, the account's lower-case e-mail as additional data) is
theirs, so they move over without a new consent; a test opens rows sealed by Omni's code and
writes the same bytes. Several apps may share one OAuth client, hence: no revoke helper
(revocation removes every scope the person granted to the Cloud project, for all its clients, so
disconnecting only forgets the app's token), `include_granted_scopes` off by default (it would
hand an app the others' scopes), and one refresh token per app and person (Google invalidates the
oldest beyond its limit). Tokens and OAuth state stay in the app's own tables; scopes, copy and
the Google REST calls stay in apps.

## Private-module consumption

Developers and release automation that resolve the private module need GitHub authentication and:

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
go get github.com/platform-upstream/app-foundation@v1.2.0
```

Any machine that bumps the pin needs a git credential helper for github.com (for example
`gh auth git-credential`). An environment that sets `GONOPROXY` or `GONOSUMDB` itself loses
GOPRIVATE's checksum-database exemption; add `GONOSUMDB=github.com/platform-upstream/*` then, or a
fresh tag fails with `verifying module: … not found`.

Consumers should pin an exact release. For container builds, resolve the module outside the image
build, then commit the resulting `vendor/` tree and build with `go build -mod=vendor`. This keeps
GitHub credentials out of build arguments and image layers. `go mod vendor` records the source;
`go.mod` and `go.sum` retain the exact module version.

## Version and release policy

SemVer from v1.0.0 (see [MIGRATING.md](MIGRATING.md)). Two platforms consume it,
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
