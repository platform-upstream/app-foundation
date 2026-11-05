# Migrating to v1.8.0

Compatible. New package `googleauth` for an app that connects a person's own Google account
(`go doc googleauth`, the example shows connect, callback and a Google call):

- Config: the OAuth client (Web application) with the app's callback as a redirect URI, the
  scopes the app needs (`openid email` are added) and `OptionalScopes` it can do without. A
  client shared with other apps keeps `IncludeGrantedScopes` off.
- Store: the app's own tables. A state from `NewState` with the person, for `StateTTL`, taken
  once in the callback; per person the three `Sealed` values, the account e-mail (`Grant.Email`,
  the additional data `Open` needs) and the granted scopes. A token key of 32 random bytes in
  base64 (`openssl rand -base64 32`) from a secret, read with `ParseKey` at startup.
- Use: `Client.AccessToken(ctx, refresh)` before each Google call; the Client caches access tokens
  per refresh token, so keep one Client per process. `ErrInvalidGrant` means the grant is gone:
  forget the token and offer to connect again. Disconnecting forgets the token and never revokes it.

An app with its own Google code (Omni, Paper) keeps its tables and key: `Key.Open(Sealed{ciphertext,
iv, tag}, user_email)` opens the stored rows and `Seal` writes the same format, so nobody consents
again. Its HMAC-signed state, token cache and refresh code go; its required scopes move to
`Config.Scopes` (an optional one such as Paper's Gmail to `OptionalScopes`). An app whose client
is shared with others drops its revoke on disconnect.

# Migrating to v1.7.0

Compatible. The `profile` module gets a menu; `ProfileCard` and `SettingsSheet` stay.

- `ProfileMenu` replaces card plus sheet: `<ProfileMenu sections={…} />` bottom left of the
  sidebar, `<ProfileMenu compact sections={…} />` in the top bar on phones (a bottom sheet there).
  Language (and appearance) apply at once; there is no Save button.
- App entries are data (`MenuSection[]`): `toggle` and `choice` store `settings.extra[key]`, read
  them with `useSetting(key, fallback)`; `action`, `link` and `info` for the rest.
- `<ProfileProvider themes>` for apps with a dark look: the menu offers Auto/Light/Dark
  (`settings.extra.theme`) and sets `<html data-theme="light|dark">`; style
  `:root[data-theme="dark"]`. Without `themes` nothing changes.
- Other apps: `profile.New(…).Apps(client)` serves `{prefix}/apps` from directory (>= 1.10,
  `identity.Client.Apps`); without it, or with an older directory, the list is empty and hidden.
- The menu renders into `<body>`; an app's own `button` reset may still need a restore rule, check
  the menu in a screenshot.

# Migrating to v1.6.0

Compatible. `profile.Handler.FromAccess()` and `.Lookup(resolver)` serve `/api/me` to apps that
do not run `identity.Middleware` yet: the person is looked up by the Access e-mail for display only
(photo, name), and a person directory does not know still gets a profile named after the e-mail.
The app's own gate stays in front; the lookup admits nobody.

# Migrating to v1.5.0

Compatible (bump, `go mod tidy && go mod vendor`). New:

- `identity.Person.Spaces` and `Via` (directory >= 1.9): where the person's data lives in this
  app, their own space first, then each family space they share here. Key shared data by
  `identity.Space{Kind: "family", ID: …}`; `InSpace` checks a space a request names,
  `FamilySpaces` lists them. An older directory gives the own space only.
- `profile`: `profile.New(db, client, "/api/me")` mounted at `/api/me` behind
  `identity.Middleware` serves the person, their photo from directory and their settings in this
  app (`person_settings`, created on first use; language and the app's own keys).
- Registry module `profile` (`frontend-registry sync --modules profile`): `ProfileProvider` with
  the app's `Messages` (de, en; the browser's language by default), `useT()`, `ProfileCard`
  bottom left of the sidebar (compact in the top bar on phones) and `SettingsSheet` (language plus
  the app's own fields). Import `foundation/profile.css` after `shell.css`.

# Migrating to v1.4.0

Compatible (bump, `go mod tidy && go mod vendor`). New: periodic backup snapshots of every
writable SQLite database, off unless `APP_SNAPSHOT_DIR` is set. app-chart (0.7+) sets
`APP_SNAPSHOT_DIR=<persistence.mountPath>/.snapshots` for apps with persistence; nothing to change
in code. A wrong `APP_SNAPSHOT_INTERVAL` (not a duration, under 1m) or `APP_SNAPSHOT_KEEP` makes
`sqlite.Open` fail. Explicit control: `sqlite.StartSnapshots(ctx, db, path, opts)`.
Restore: scale the app to 0, copy the newest `<db>-<hash>-<time>.sqlite` over the database file,
delete its `-wal` and `-shm`, scale up.

# Migrating to v1.3.0

Compatible: `identity.Client.People(ctx)` lists everyone directory grants the app (kid profiles
included), for services whose callers name people (memory) and for pickers;
`identitytest.Directory.People` does the same in tests.

# Migrating to v1.2.0

v1.2.0 is backwards compatible (bump, `go mod tidy && go mod vendor`). New:

- `identity`: the directory person behind a request. Adopt it per app, when the app moves to
  directory people (principles: tenancy = person within a platform):
  1. directory: add the app to `callers` (key `caller-<app>-key`) and its namespace to
     `networkPolicy.callerNamespaces`; grant the people in the directory UI.
  2. App chart: `DIRECTORY_URL` (directory's in-cluster URL) and `DIRECTORY_KEY` (the same key,
     from the app's own secret). The scaffold's chart shows it (`directory.url`,
     `directory.existingSecret`, key `directory-key`).
  3. Code: `identity.ConfigFromEnv` → `identity.NewClient` → `identity.NewCache`, then
     `identity.Middleware(cache, opts)` inside `auth.Middleware` on the API. Local mode passes a
     nil resolver and `Options.Local`. Service tokens that act for someone go in
     `Options.ServiceTokens`.
  4. Data: key it by `identity.From(ctx).ID`; migrate old user ids to directory ids once.
  Tests use `identitytest.New(people...)` or `identity.NewContext`.
- `access.NewContext` puts a Caller into a context (tests of handlers behind access).
- Scaffold: notes are owned by the calling person, viewers read only, the chart requires the
  directory values, and AGENTS.md has the Tenancy section.

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
