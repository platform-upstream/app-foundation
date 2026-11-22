# Migrating to v1.21.1

The module is public now: no `GOPRIVATE`, no git credential, no `vendor/`. Compatible for the code;
the scaffold's `Dockerfile`, `.ci/pipeline.yaml` and `scripts/foundation-sync.sh` changed, and an
app follows them once:

- `go get github.com/platform-upstream/app-foundation@vX.Y.Z && go mod tidy`, then `git rm -r
  vendor` (Go uses `vendor/` only while it exists).
- Dockerfile Go stage: `COPY vendor/ ./vendor/` becomes `ARG CACHE_SCOPE=local` and
  `RUN --mount=type=cache,id=gomod-${CACHE_SCOPE},target=/go/pkg/mod go mod download`; every later
  `RUN` that runs `go` mounts the same cache; drop `-mod=vendor`, `GOPROXY=off` and
  `GOFLAGS=-mod=vendor`. Do not set `GOPRIVATE` or `GONOSUMDB` for this module: `go.sum` and
  sum.golang.org check what the proxy serves.
- `.ci/pipeline.yaml`: drop `vendor/**`; `scripts/foundation-sync.sh`: plain `go tool`.
- A workstation: `go env -u GOPRIVATE` if it was set for platform-upstream.

# Migrating to v1.21.0

Compatible; no registry file changes. New in `agentkit`: `Tool` (name, class, hints),
`AddTool[In, Out]` and `Handler`, the MCP plumbing Paper, Finance, Omni and Memory each carried.
New in `googleauth`: `ErrAccount`, which `Exchange` wraps when it cannot name a verified account.

- An app's own `addTool` becomes `agentkit.AddTool(agentkit.Tools{Server, Callers, Fail, Links},
  agentkit.Tool{...}, description, enums, run)`. `Fail` maps a run's error to what the caller
  reads (the app logs internals there); `Links` adds resource links. The schema, enum paths
  (dotted, into list items and nested objects) and hints are what the apps built: read-only for
  read, never open world, destructive only above read, idempotent as set.
- `mcp.NewStreamableHTTPHandler` + `GuardRemote` + a no-store wrapper becomes
  `agentkit.Handler(keys, remote, callers, idle, pick)`.
- Check: the app's `tools/list` answer is byte-identical before and after.

# Migrating to v1.20.0

Compatible for the code. Registry file change in `phone-audit`: `go tool frontend-registry sync`
after the pin bump. Two new checks may now fail an app's `pnpm test:phone`; fix what they find:

- `covered-target`: a visible control whose centre and corners a tap cannot reach (an overlay, a
  sticky bar, the nav over the last row, a neighbour's hit area). It scrolls the page, or the
  shell's `<main>`, in up to 8 steps and reports a control only when it was covered at every step
  it was on screen. A finding names the control and what the tap lands on. `ignore` also takes
  out a cover; `coverSteps` changes the steps.
- Error gate in `phoneAudit`: an uncaught exception, a console error (also a fixture's 404 for an
  image or a request) or a failed request while a route is open fails its test. Serve the request
  in the fixtures, or accept documented noise with `allowErrors: (error) => boolean` (per config or
  route); `skip: ['console-error']` and the like switch a part off. `watchErrors(page, allow)` is
  the same gate for an app's own specs.

The audit now also runs in CI: add the `phone` stage of the scaffold's `Dockerfile`
(`docs/frontend-registry.md`, phone-audit) and let the runtime stage copy its marker; the
Playwright config must then serve the built UI without building again. The image version follows
`@playwright/test` (1.65.0).
# Migrating to v1.19.1

Compatible. Registry file change in `kids` (`Kids.tsx`, `kids.css`): `go tool frontend-registry sync`
after the pin bump. A tile takes the game's own `className` and `style` (Garage's colour per
player, Color's ring in the kid's colour); on a phone the takeover's choice wraps under the name
instead of being cut.

# Migrating to v1.19.0

Compatible. New Go package `kids` and registry module `kids` (`src/foundation/kids.ts`, `Kids.tsx`,
`kids.css`), extracted from the four games that each carried their own `internal/directory` and
kid UI.

- Go: `kids.New(identity.Config, app)` replaces an app's own directory client for kid profiles;
  `kids.NewRoster(client).Run(ctx, time.Minute, link, after)` replaces the minute sync. Refusals are
  `*kids.Error` (`kids.Status(err)`, `errors.Is(err, kids.ErrConflict)` …); a 5xx or network error
  is a plain error. `Roster` kids carry `Trusted`: an owner acts for them and no family of theirs
  is run by a self-registered adult (the only kids that may take over an older kid by name).
- Frontend: `go tool frontend-registry install -modules kids`, add `kids` to the Dockerfile's
  `check`, import `foundation/kids.css` after `shell.css`, wrap the kid places in `<KidsProvider
  lang button primary danger link onSignIn>` and paint the `kids-*` classes in the app's CSS.
  The components render content only: put `KidForm` and `Adopt` into the app's own sheet.

# Migrating to v1.18.0

Compatible. Registry file change in `pwa-shell` (`AppShell.tsx`, `shell.css`):
`go tool frontend-registry sync` after the pin bump. New optional `footer` slot of `AppShell`: a bar
between the scrolling `<main>` and the nav that stays while the soft keyboard is open (the nav
goes). Put a composer there instead of `position: sticky` inside `<main>`: an input inside the
scroller makes the browser scroll the page on every keystroke.

# Migrating to v1.17.4

Registry file change in `pwa-shell` (`viewport.ts`): `go tool frontend-registry sync` after the pin
bump. The rubber-band guard also covers open `<dialog>`s (sheets, menus in the top layer), which
are outside the shell: a vertical drag on their non-scrolling parts no longer moves the page.

# Migrating to v1.17.3

Registry file change in `pwa-shell` (`viewport.ts`): `go tool frontend-registry sync` after the pin
bump. `canScrollY` drops its unused second parameter, so the file compiles under
`noUnusedParameters` (Console).

# Migrating to v1.17.2

Registry file change in `pwa-shell` (`viewport.ts`): `go tool frontend-registry sync` after the pin
bump. The rubber-band guard of v1.17.1 now compiles under `noUncheckedIndexedAccess`.

# Migrating to v1.17.1

Registry file change in `pwa-shell` (`AppShell.tsx`, `viewport.ts`): `go tool frontend-registry sync`
after the pin bump (Renovate's `foundation-sync.sh` does it). Nothing else to change.

- The shell no longer rubber-bands on iOS: a vertical drag that starts where nothing can scroll (the
  nav, the top bar, a page shorter than the screen) is cancelled by `useNoShellRubberBand`, which
  `AppShell` calls. Drags inside a scroller, horizontal drags, pinch zoom and overlays outside the
  shell stay native.

# Migrating to v1.17.0

Compatible; no registry file changes. New in `server`: `Version()` answers `GET /version` with
`{"version","commit","foundation","go"}` (from `APP_VERSION` and `APP_COMMIT`, else the binary's
build information), and `Probes(mux, readyChecks...)` registers `/healthz`, `/readyz` and
`/version` in one call.

- Replace `mux.Handle("GET /healthz", server.Health())` and `mux.Handle("GET /readyz",
  server.Ready(checks...))` with `server.Probes(mux, checks...)`, in the same place (before Access).
  An app without readiness checks passes none. An app with its own `/version` route drops it.
- The chart sets `APP_VERSION` (the released chart version) and `APP_COMMIT` (the chart's
  `appVersion`, the commit): platform-charts' `app-chart` from 0.8.0 does it; an app with its own
  Deployment template adds both (as the scaffold's `deploy/templates/deployment.yaml` does).
- Check after the release: `GET /version` shows the release and its commit.

# Migrating to v1.16.0

Compatible; no registry file changes for installed modules, so `check` stays green after the pin
bump. New module `busy` (`docs/frontend-registry.md`): busy cues without a look.

- `go tool frontend-registry install -target <web dir> -modules busy`, add `busy` to the
  Dockerfile's `check` list, import `./foundation/busy.css` after `shell.css` and before the app's
  CSS.
- An app's own delayed-busy hook, top-bar fetch line or recompute blur (Console's `FetchLine`,
  Finance's `Pending`) becomes `useDelayedBusy`, `<BusyBar>` and `<PendingScope>`. The look moves
  into the app's CSS as values of `--busy-bar-duration`, `--busy-bar-opacity`, `--busy-fade`,
  `--busy-pending-opacity`, `--busy-pending-filter` and the bar's own class (place, height,
  `color`). Mark result elements `data-result`; cards dimmed by class keep an app rule over the
  same properties.

# Migrating to v1.15.0

Compatible for the code. Registry files changed in `phone-audit`, `touch`, `palette`, `sheet`,
`pull-refresh`, `motion` and `profile`: after the pin bump `check` fails for each of these an app
installed until `go tool frontend-registry sync -target web -modules <yours>` is run and its diff
reviewed. No new files. Bumping the pin: `GOPRIVATE=github.com/platform-upstream/*` and a git
credential helper on the machine (`docs/frontend-registry.md`, Workflow); where `GONOPROXY` or
`GONOSUMDB` is set explicitly, add `GONOSUMDB=github.com/platform-upstream/*`.

- `phone-audit`: fewer false positives. Visually hidden text (`.sr-only`) is not judged;
  `text-overflow` no longer counts absolutely positioned children and pseudo-elements (`NavBadge`,
  `.hit-target`'s `::after`); `tap-target` measures a stretched link's `::after` (`inset: 0`)
  against its positioned card. Drop `ignore` entries and `skip`s added for these.
- `touch`: a `.hit-target` button, select or field no longer gets the 44px `min-height` on touch
  screens; an app that worked around it (`min-height: 0` on its chips) can drop that.
- `palette`: `createShortcutHandler({ …, primaryInDialog: true })` lets ⌘K/Ctrl+K through inside a
  dialog, so the open palette closes with the key that opened it (an app's own handler for that
  can go). `rank(items, query, tieBreak)` takes a comparator for equal matches (before the
  shorter-label rule).
- `sheet`: on a desktop the first `[data-autofocus]` in the sheet (or `initialFocus`, an element or
  selector, in `useSheet`/`showSheet` options) takes the focus; replace `autoFocus` and any
  focus-after-open effect with `data-autofocus`. `sheet.css` sets `translate` only while a drag
  offset is set, so `position: fixed` children of a sheet (menus, popovers) are placed against the
  viewport again; an app that compensated for the sheet as containing block drops that.
- `pull-refresh`: an open Radix dialog (`[role=dialog][data-state=open]`) stops the pull like a
  modal `<dialog>`.
- `motion`: `useEntrance(ref, ready, { once: 'session' })` plays only with the session's first
  view; an app with its own once-per-session gate (Finance's `lib/entrance.ts`) can switch.
  `useEntrance(ref, ready, maxMs)` still works.
- `profile`: the compact profile button is 44px (6px padding around the 32px photo, was 4px) and
  its accessible name contains the initials it shows; the full card is named by its text. An app
  that styled `.pf-compact` padding itself keeps its own.

# Migrating to v1.14.0

Compatible for the code. New apps from `app-scaffold` start phone-ready (`docs/app-scaffold.md`).

- `template` now requires `touch` and `motion`: `install`/`sync -modules template` also brings
  `src/foundation/touch.css`, `motion.css` and `motion.ts`. Add `touch,motion` to the Dockerfile's
  `check` list.
- Apps on `template` that sync its files (all app-owned, so review the diff and keep your own
  look): `src/main.tsx` imports `./foundation/touch.css` and `./foundation/motion.css` after
  `shell.css`, before `app.css`; `index.html` has `class="no-pan-x"` on `<html>` and `<body>`;
  `App.tsx` passes `className: 'no-pan-x'` in `mainProps`; `app.css` makes `.bottom-nav a` a 44px
  flex target (nav padding top 0.25rem instead of 0.75rem) and drops its own 16px field rule
  (`touch.css` has it on touch screens); `package.json` adds `@playwright/test` 1.65.0,
  `@axe-core/playwright` 4.14.0 and `"test:phone": "playwright test"`, with `pnpm-lock.yaml` to
  match. An app with its own nav makes its items 44px itself; the phone audit shows which.
- `toast`: `<ToastRegion label="…">` renders `role="region"` with the label (an `aria-label`
  without a role fails axe's `aria-prohibited-attr`, which the phone audit reports). `check` fails
  for `toast` until `sync -modules toast` is run.
- To give an existing scaffolded app the scaffold's phone audit: `install -modules phone-audit`
  (plus `sheet,toast` if wanted), copy `web/playwright.config.ts` (set the port) and
  `web/e2e/phone.spec.ts` from a fresh scaffold, adapt the fixtures and routes, add the modules to
  the `check` list and `web/test-results/`, `web/playwright-report/` to `.gitignore`.

# Migrating to v1.13.0

Compatible for the code; the registry gains files, so the pin bump fails `check` for `api`,
`profile` and `pwa-shell` until `go tool frontend-registry sync -target . -modules <yours>` is run
and its diff reviewed (new files `src/foundation/online.ts`, `public/profile-theme.js`,
`src/foundation/NavBadge.tsx`; `shell.css` gains the nav badge rules, nothing else in it changes).
Nothing behaves differently until the app uses them (`docs/frontend-registry.md`):

- New modules `push`, `palette`, `scroll-edges`: `go tool frontend-registry install -target .
  -modules push` (and so on), then add them to the Dockerfile's `check`. Each replaces an app's own
  copy: delete it and import from `src/foundation/`.
  - `push`: the app's `public/sw-app.js` drops its `push` and `notificationclick` listeners for
    `importScripts('sw-push.js')` and `FoundationPush.handlePush(event, { fallbackTitle, icon })` /
    `FoundationPush.handleClick(event)`. Payloads keep their shape (`id` still works as the tag;
    add `tag` and `badge` where useful). A worker that called `navigate()` before `focus()` or
    opened any URL it was sent is fixed by the switch. The settings switch uses `usePush()` over
    the existing `/api/push/{key,subscribe,unsubscribe}` routes, `setAppBadge(n)` replaces the
    app's own badge call.
  - `palette`: `rank`, `normalize`, `readRecents(key)`/`pushRecent(key, id)` (the key is the
    app's existing localStorage key, so recents survive), `isTypingTarget` and
    `createShortcutHandler({ keys, go, primary })` replace the app's ranking, folding and shortcut
    guard. Shortcuts with Alt held (AltGr) and during IME composition no longer fire.
  - `scroll-edges`: `useScrollEdges(ref)` plus `className="edge-fade"` replace an app's own edge
    detection and mask rules; set `--edge-fade-start`/`--edge-fade-end` for other fade lengths.
- `api`: `useOnline()` and `isOfflineError(err)` from `src/foundation/online.ts` replace the app's
  own online/offline wiring.
- `profile`: an app with `themes` replaces its own theme bootstrap with
  `<script src="/profile-theme.js"></script>` in `<head>` before the stylesheets (an older storage
  key: `data-legacy-key="acme-theme"` on the tag) and moves its theme colours onto the meta:
  `<meta name="theme-color" content="#ffffff" data-light="#ffffff" data-dark="#16181d">`, before
  the script. A light-only app does not load it.
- `pwa-shell`: a nav count becomes `<span className="nav-badge-anchor"><Icon />
  <NavBadge count={n} label={…} /></span>`; the app keeps the badge's background, colour, radius
  and ring on `.nav-badge` (and `[data-tone]`), and drops its own positioning rules.
- New modules `sheet`, `toast`, `pull-refresh`: `go tool frontend-registry install -target .
  -modules sheet,toast,pull-refresh`, then add them to the Dockerfile's `check`. Details and
  examples: `docs/frontend-registry.md`.
  - `sheet`: import `src/foundation/sheet.css` after `shell.css`. Move the app's sheet onto a native
    `<dialog>` with `useSheet(ref, open, onClose)`; mark grabber and header `data-sheet-handle` and add
    `useSheetDrag(ref, { onClose, below })`. Delete the app's own portal, scrim element (style
    `::backdrop` instead), Escape listener, focus trap and focus restore, and every scroll lock on
    `body` (`lockScroll()` for other overlays). Keep the look; drop bottom padding that only cleared
    the home indicator (the spacer does it now) and inline drag transforms (`--sheet-drag` is applied
    as `translate`).
  - `toast`: import `toast.css`, render `<ToastRegion>` once and call `toast(text, { undo, tone })`
    where the app's own store was; `configureToasts({ undoLabel })` for the app's copy. Delete the
    app's store and region and any bottom offset that adds `env(safe-area-inset-bottom)` to
    `--shell-nav-height`.
  - `pull-refresh`: `usePullToRefresh(mainRef, refresh)` on `AppShell`'s `mainProps.ref` and the
    app's indicator from `{ pull, ready, busy }`; delete the app's own touch handling.
- New modules `touch`, `motion`, `phone-audit` (details in `docs/frontend-registry.md`):

  - `touch`: `go tool frontend-registry install -target . -modules touch`, then
    `import './foundation/touch.css'` after `shell.css`, before the app's CSS. Put `no-pan-x` on
    `html`, `body` and `AppShell`'s `mainProps.className`. Delete the app's own copy of the same
    rules (tap highlight, 16px fields, 44px floors, `text-size-adjust`, `overflow-x: clip` on the
    frame); keep its own larger sizes and press feedback. Remove `autoFocus` from fields on touch.
  - `motion`: install, `import './foundation/motion.css'` next to `touch.css`, and replace the app's
    `reduced()`/`prefersReducedMotion`, entrance hook, `viewTransition` and indicator hook with the
    ones from `./foundation/motion.js`. Move every animation and moving transition under
    `@media (prefers-reduced-motion: no-preference)` and delete a global reduced-motion kill switch
    (`* { animation: none !important }`, `.01ms`). Changed shapes: `useEntrance(ref, ready)` returns
    a boolean for `data-entering` instead of a class name; `useSlidingIndicator` returns
    `{ style, instant }` (Console's `box` + `indicatorStyle`, Finance's `useIndicator` with
    `data-ind` become a selector such as `[aria-current="page"]` and a key); `viewTransition` no
    longer carries a hero element (Paper keeps its own `doc-hero` naming around it).
  - `phone-audit`: install, `pnpm add -D @playwright/test @axe-core/playwright`, a Playwright config
    with the app's `baseURL` and server, and `e2e/phone.spec.ts` calling `phoneAudit({ routes, setup })`
    (example in the docs). Add `phone-audit` to the `check` list like the other modules. Paper's
    `e2e/phone-audit/overflow.mjs` and `pan.mjs` are covered by it, except the sibling-overlap,
    labels-touch, word-split and badge checks, which stay Paper's.

  Verified in a scratch app: `install -modules template,touch,motion,phone-audit`, frozen install,
  `tsc`, `vite build`, the audit at 390x844 clean on the template page (after app-owned 44px nav
  links and `no-pan-x`) and failing a planted 28px button with its selector.

# Migrating to v1.10.0

Compatible; nothing changes without the new configuration. `push` can hand the owner's messages
to a platform's hub, an app that tells the owner for every app, instead of sending its own web
push. To opt in:

- Only for the owner: the hub tells one person. Set `Hub` only on a `Sender` whose subscriptions
  are the owner's; an app with one `Sender` per person sets it on the owner's and on no other.
- Config: `push.Config.Hub` with `URL` and `Key`, from `PUSH_HUB_URL` and `PUSH_HUB_KEY`. The URL
  is the hub's in-cluster notify route, the key this app's bearer key there (at least 32
  characters, the hub's `access.Internal` caller key). Both unset: no hub. Keep the VAPID keys
  while the hub runs: they are the fallback.
- Code: where the app tells the owner, `sender.Notify(ctx, push.Message{Title, Body, Ref, Urgency,
  ID}, payload)` instead of `sender.Send(ctx, payload)`. `payload` is the app's existing
  service-worker message; it goes out when there is no hub or the hub fails. `Ref` is an app
  reference (`acme:invoice/42`), not a URL. `UrgencyLater` waits for the hub's digest; the zero
  value is `UrgencyNow`. The same `ID` replaces an earlier message at the hub.
- Errors: `ErrInvalidMessage` (a title of 1 to 120 characters, a body up to 500, the ref's shape,
  or the hub answered 400) sent nothing, on every platform. `ErrHubFailed` means the web push went
  instead; `Delivery.Devices` says whether it reached a device (after the hub's 429, too many
  messages, nothing went). Log both.
- An app with the hub and no VAPID keys gates on `Enabled() || HubEnabled()`, not `Enabled()`.

`Send` and `SendTo` are unchanged and never reach the hub: messages for other people stay on
`SendTo` or on a `Sender` without `Hub`, and a test push for the app's own devices on `Send`.

# Migrating to v1.9.0

Compatible; nothing changes without the new variable. `APP_SNAPSHOT_AT=HH:MM` (24h) takes the
SQLite snapshots once a day at that local time instead of every `APP_SNAPSHOT_INTERVAL`: the first
at its next occurrence after the start, then every day, so restarts no longer push it back. Set it
a little before the nightly volume backup; `APP_SNAPSHOT_KEEP` (2) then keeps today's and
yesterday's copy. Local time is `TZ` (without it usually UTC in a container); distroless static
carries the zone data, an image without tzdata needs it installed or `import _ "time/tzdata"`.
`sqlite.Open` fails on a time that is not strictly `HH:MM` (`2:15`, `24:00`), on
`APP_SNAPSHOT_AT` together with `APP_SNAPSHOT_INTERVAL`, and, with `APP_SNAPSHOT_AT` set, on a `TZ` the binary cannot load.
Explicit control: `SnapshotOptions.At`. A daily snapshot may take up to an hour; a failed one is
logged and tried again the next day, the earlier snapshots stay.

# Migrating to v1.8.0

Compatible. New package `googleauth` for an app that connects a person's own Google account
(`go doc googleauth`, the example shows connect, callback and a Google call):

- Config: the OAuth client (Web application) with the app's callback as a redirect URI, the
  scopes the app needs (`openid email` are added) and `OptionalScopes` it can do without. A
  client shared with other apps keeps `IncludeGrantedScopes` off.
- Store: the app's own tables. A state from `NewState` with the person and the PKCE verifier
  `AuthCodeURL` returns, for `StateTTL`, taken once in the callback and passed to `Exchange`; per person the three `Sealed` values, the account e-mail (`Grant.Email`,
  the additional data `Open` needs) and the granted scopes. A token key of 32 random bytes in
  base64 (`openssl rand -base64 32`) from a secret, read with `ParseKey` at startup.
- Use: `Client.AccessToken(ctx, refresh)` before each Google call; the Client caches access tokens
  per refresh token, so keep one Client per process. `ErrInvalidGrant` means the grant is gone:
  forget the token and offer to connect again. Disconnecting forgets the token and never revokes it.

An app with its own Google code (Omni, Paper) keeps its tables and key: `Key.Open(Sealed{ciphertext,
iv, tag}, user_email)` opens the stored rows and `Seal` writes the same format, so nobody consents
again. Its HMAC-signed state becomes a stored state with its verifier; its token cache and refresh code go; its required scopes move to
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
