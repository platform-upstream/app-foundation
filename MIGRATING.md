# Migrating to v1.1.0

v1.1.0 is backwards compatible: bump the pin, `go mod tidy && go mod vendor`, then
`go tool frontend-registry sync -target web -modules api,pwa,pwa-shell` and review the diff.

Service worker (template-owned, not covered by `check`): copy the new
`frontendregistry/sources/pwa/sw.js` over `web/public/sw.js` and keep your additions in
`web/public/sw-app.js`, which the worker loads when it exists (a fetch listener there that answers
a request calls `event.stopImmediatePropagation()`). The new worker caches only the shell and
`/assets/`, skips `/pwa/`, and drops the older caches of its scope on activation. Pass the app name
to `registerServiceWorker({ name: '<app>' })` in `src/main.tsx` to name the cache.

Behaviour change: `sqlite.Open` now begins transactions `IMMEDIATE`. Two processes on one database
no longer fail with `SQLITE_BUSY_SNAPSHOT`; a second writer waits up to the busy timeout instead.
Read-only transactions that should not take the write lock use `sql.TxOptions{ReadOnly: true}`.
`sqlite.Snapshot` also works on filesystems without hard links.

Optional adoptions (nothing breaks without them):

- Readiness: mount `GET /readyz` with `server.Ready(dbPing)` next to `/healthz` and point the
  chart's `readinessProbe` at `/readyz` (liveness stays on `/healthz`). Drop any checks passed to
  `server.Health`; they were always ignored.
- Security headers: replace the app's own header middleware with
  `web.Headers(web.HeaderPolicy{CSP: <the app's CSP>})`. It adds COOP, HSTS and Permissions-Policy;
  check the app against them (an app that embeds itself in a frame sets `FrameOptions: "SAMEORIGIN"`).
- Chart: the scaffold no longer bakes the Access team into `values.yaml`; `cfTeamDomain` is empty
  and `required`. Existing apps may do the same once their wrapper sets `cfTeamDomain`.
- Authentication config: `cfg, err := access.ConfigFromEnv(addr)` then `access.New(cfg)` replaces
  the hand-written `access.Config` from `AUTH_MODE`/`CF_*` (same variables, same separators).

# Migrating to v1.0.0 (platform-upstream)

v1.0.0 is the first release under the neutral module path. It contains everything of
`castillo-platform/app-foundation` v0.21.0 and `niiku-platform/app-foundation` up to 8eb1954
(`agentkit.GuardRemote`, `access.Remote`). The API is unchanged apart from `appscaffold`.

In an app:

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
grep -rl 'github.com/\(castillo\|niiku\)-platform/app-foundation' --include='*.go' --include=go.mod . \
  | xargs sed -i 's#github.com/\(castillo\|niiku\)-platform/app-foundation#github.com/platform-upstream/app-foundation#g'
go mod edit -require=github.com/platform-upstream/app-foundation@v1.0.0   # before tidy: the old tag does not exist under the new path
go mod tidy && go mod vendor && gofmt -w cmd internal
go tool frontend-registry check -target web -modules api,pwa,pwa-shell   # sync + review if it differs
```

Then move manifest and icons to `web/public/pwa/` (links in `index.html`), let `web.Public` (or
`web.PublicFiles`) pass them without a session, and add the host to `pwa_hosts` in the platform
OpenTofu. Color did exactly this in castillo-platform/color fb0bd75.

Windows: Go fetches the private module through git; give it gh as the credential helper:

```bash
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=credential.helper GIT_CONFIG_VALUE_0= \
  GIT_CONFIG_KEY_1=credential.helper GIT_CONFIG_VALUE_1='!gh auth git-credential'
```

`appscaffold.Options` needs a `Platform` (org, domain, registry, Access team); the CLI reads it
from `-platform platform.json`. `reference-app` is gone; the scaffold test covers its role.
