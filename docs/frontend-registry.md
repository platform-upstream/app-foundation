# Frontend template and source registry

The registry installs reviewable source into an application repository. It is not a runtime package
and does not centralize application design. Consumers pin the exact `app-foundation` release used
to install or check source.

## Workflow

The tool is pinned with the rest of the foundation in the consumer's `go.mod` and vendored, so
it runs offline and checks against the pinned release:

```bash
go get -tool github.com/platform-upstream/app-foundation/cmd/frontend-registry@v0.11.0
go mod vendor
go tool frontend-registry list
go tool frontend-registry install -target . -modules template,api
go tool frontend-registry check -target . -modules pwa,pwa-shell
```

Consumers run `check` for the modules they installed in their Dockerfile's Go stage (the build
the update train verifies), with the registry targets copied into that stage. A pin bump that
changes registry files fails it until `sync` is run and its diff reviewed.

`install` writes missing files but refuses to replace a changed target. `check` exits non-zero and
lists missing or changed registry-owned files. `sync` is the explicit replacement operation; review
its diff because it overwrites selected registry-owned source. Selecting `template` automatically
includes its `pwa` dependency. With no module list, every module is selected.
All reads and writes are anchored to the requested real directory, reject symlink parents, and use
root-confined atomic replacement, so a consumer-tree symlink cannot redirect output elsewhere.

The template pins the React/Vite/TypeScript/TanStack Query/Zod versions already exercised by Read,
uses a root-relative production base so direct nested SPA routes load the same assets, and includes
a frozen pnpm lockfile. First-party apps are hosted at the root of their own origin; local previews
that preserve this contract use the workstation's `absproxy` route. Rename the package and replace
the shell content, manifest metadata, and neutral stylesheet as part of app creation. Those files
are a starting point, not a shared theme.

## Accepted primitives

- `api` is a same-origin JSON client. It understands the standard `{ "error": string }` envelope,
  requires an injected runtime schema such as Zod, refuses paths outside its configured API base,
  resolves the default API base from the origin rather than the current SPA route, exposes status
  through `APIError`, uses manual redirects so Cloudflare Access expiry is visible, and leaves the
  exact auth-expiry predicate/action and user-facing fallback copy injectable.
- `pwa` installs the update lifecycle proven by Read. `template` installs the service worker
  (`public/sw.js`). It is template-owned and `check` does not cover it: after a release that changes
  it, copy the new file over; the app's own worker code (push, notifications, share target, extra
  cache rules) lives in `public/sw-app.js`, which the worker loads with `importScripts` when it
  exists. A fetch listener there that answers a request calls `event.stopImmediatePropagation()`.
  The worker caches only navigations (one shell entry, network first, the shell only when the
  network fails) and `/assets/` files (cache first, at most 64); it never handles `/api/`,
  `/internal/`, `/cdn-cgi/` or `/pwa/`, never stores redirects or `private` responses, and stores
  `no-store` responses only as the shell (web.Handler serves documents `no-store`).
  `registerServiceWorker({ name })` registers `sw.js?name=<app>`, which names the cache. A page that stays open, such as an installed
  app that is resumed rather than relaunched, checks for a newer build on load and whenever it
  becomes visible, and reloads into it. `onUpdate(apply)` hands that decision to the application:
  ask the user, or wait until nothing would be interrupted (Listen never reloads during playback). Cache names are isolated by
  service-worker scope. Push, share-target, notification, and background-sync behavior remains app-owned.
- `pwa-shell` is the mobile shell proven by Omni in iOS standalone PWAs: `shell.css` (in
  `@layer foundation`; import it before the app's CSS) keeps the shell in normal flow at `100dvh`,
  or `100lvh` in a standalone app when `index.html` sets the `black-translucent` status bar (only
  then does the web view cover the status bar; with the default bar, or in a browser tab where `lvh`
  is the height without the URL bar, `lvh` pushes the nav off the screen), and follows the visual viewport while the keyboard is open; `viewport.ts` (`useVisualViewport`)
  mirrors `visualViewport` into `--app-height`/`--vv-offset-top` and toggles `keyboard-open`;
  `AppShell.tsx` renders shell > sidebar + column (top bar, scrolling `<main>`, nav as the last
  flex child), drops the nav while the keyboard is open and publishes `--shell-nav-height`.
  The application owns the top bar (it pads `env(safe-area-inset-top)`), the nav (its background
  and bottom padding `var(--shell-safe-bottom)`, the trimmed home-indicator inset, never `position: fixed`, hidden where a sidebar takes
  over), the desktop layout via `className`, and overlays outside `<AppShell>`. Do not override
  `.app-shell` height or position; floating elements use `--shell-nav-height`, not a hard-coded
  clearance. Only a real iPhone confirms the shell; desktop Chromium cannot reproduce the
  status bar and `lvh`/`dvh` behaviour.
- `template` supplies only a clean React/Vite entry point, visible application-owned QueryClient
  policy, accessible document defaults, root-origin build configuration, PWA registration, and a
  minimal `AppShell` with a top bar and nav that tells adopters to create an application identity.
- `api` also installs `online.ts`: `useOnline()` (`navigator.onLine` and its events through
  `useSyncExternalStore`) and `isOfflineError(err)`, true for a request that never reached a
  server (offline, or the fetch `TypeError` of Chromium, Firefox and Safari) and false for any
  `APIError`, which is an answer (also the 401 of an expired session). What the app shows offline,
  and whether its queries pause, stays app policy; there is no query-library coupling.
- `profile` also installs `public/profile-theme.js`, for an app with a dark look
  (`ProfileProvider themes`). `index.html` loads it in `<head>` before any stylesheet, as a file
  because the CSP blocks inline scripts: `<script src="/profile-theme.js"></script>`. It sets
  `<html data-theme>` and `color-scheme` from the stored appearance (`profile.theme`; auto follows
  the system) before the first paint, so a dark app never flashes light; `data-legacy-key` on the
  script tag names an app's older key, read while `profile.theme` is unset. Each
  `<meta name="theme-color">` placed before the script with `data-light` and `data-dark` (the app's
  colours) follows `data-theme` from then on; its `media` attribute is dropped. The template
  worker does not cache the file: an offline launch paints with the CSS default until the profile
  module runs.
- `pwa-shell` also has a nav badge: `NavBadge({ count, label, dot?, tone? })` inside an element
  with class `nav-badge-anchor` around the icon. Nothing at 0, `99+` above 99, `dot` for a dot
  without the number; `label` is the accessible text in the app's words. `shell.css` places it
  out of flow over the icon's top end corner at a fixed height, so it never moves the icon or the
  label; size and position via `--nav-badge-size`, `--nav-badge-top`, `--nav-badge-overlap`,
  `--nav-badge-font-size`, `--nav-badge-dot`. Background, colour, radius, ring and tones
  (`[data-tone]`) are the app's, as are nav height and the active item.
- `push` is Web Push without the app's words. `public/sw-push.js` is loaded by the app's
  `public/sw-app.js` and wired there:

  ```js
  importScripts('sw-push.js');
  self.addEventListener('push', (event) => FoundationPush.handlePush(event, { fallbackTitle: 'Acme', icon: '/pwa/icon-192.png' }));
  self.addEventListener('notificationclick', (event) => FoundationPush.handleClick(event));
  ```

  `handlePush` always shows a notification (iOS ends the subscription of a worker that stays
  silent): the payload's `title` (at most 120 characters, else `fallbackTitle`), `body` (500),
  `url`, `tag` (or `id`, 120; a push with the same tag replaces the earlier one, so send one tag
  per subject) and `badge` (the app icon's count, 0 clears it; feature-detected, a refusal is no
  error). A Declarative Web Push message (`{"web_push": 8030, "notification": {title, body,
  navigate, tag, app_badge}}`) is read the same way. Only a URL of the app's own scope is kept;
  anything else opens the start page. `handleClick` focuses an open window of the app first (while
  the click still counts as a gesture) and then navigates it, or opens a new one. On the page,
  `usePush({ endpoint = '/api/push', vapidKey? })` returns `{ state, busy, error, subscribe,
  unsubscribe }` over the routes Console, Paper and Finance share (`GET key` → `{publicKey}`,
  `POST subscribe` with the subscription JSON that Go `push.Subscription` reads, `POST unsubscribe`
  with `{endpoint}`); `state` is `unknown`, `unsupported`, `needs-install` (iOS in a browser tab),
  `denied`, `off` or `on`. Call `subscribe` from a tap. `setAppBadge(n)` sets or clears the icon
  count from the page. What is sent, counted and said stays in the app.
- `palette` is a command palette's logic, no component: `rank(items, query)` orders
  `{ label, detail?, aliases? }` by exact label, label prefix, word start, every word of a
  multi-word query starting a word, alias, substring, every word anywhere, detail and last a fuzzy
  subsequence ("crts" → Certificates); `normalize` ignores case and accents and folds ß to ss and
  ae/oe/ue to a/o/u, so "uber", "ueber" and "Über" meet. `readRecents(key)` and
  `pushRecent(key, id)` keep the last picks (5) in localStorage under the app's key.
  `shortcuts.ts` has `isTypingTarget(el)` and `createShortcutHandler({ keys, go, primary })`: single
  keys by the typed character, `g` then a letter within 1.5 s, and `primary` combinations with ⌘
  (Apple) or Ctrl. It leaves alone keys in text fields (except `primary`), inside or behind an open
  modal dialog, during IME composition, and any key with Alt held, so AltGr (Ctrl+Alt on Windows;
  AltGr+2 types @ on a Swiss keyboard) never fires a shortcut. The palette's look, commands and
  shortcut list stay in the app.
- `scroll-edges`: `useScrollEdges(ref)` sets `data-more-start` / `data-more-end` on a sideways
  scroller while there is more that way (scroll, resize of the scroller or its items, items added),
  without re-rendering; `scroll-edges.css` adds `.edge-fade`, a mask only (lengths
  `--edge-fade-start`, `--edge-fade-end`), so it fades over any background. Snapping, arrows and
  the strip's look stay in the app.

## Deliberate non-abstractions

- Query stale times, retries, invalidation, polling, and streaming are application policy. Read and
  Console already differ, so the registry pins the library but provides no shared query wrapper.
- Shell mechanics are shared (`pwa-shell`, ADR-021); color values, typography, components, page
  layout, the top bar, navigation items and their look, empty states, and interaction design are
  not. Existing apps have distinct identities; there is no token package or design system.
- Cloudflare identity, authorization, signed-out pages, CSP, and security-header policy remain at
  the edge or in each application. The API source accepts only an expiry hook.
- Runtime API schemas stay app-owned following the FND-007 generation rejection.

The local acceptance fixture is retained under the workspace's ignored `artifacts/fnd008/`. A fresh
registry install passed registry check, frozen-lockfile install, strict type checking, and production
Vite build.

The `template` module's `index.html` and `public/pwa/manifest.webmanifest` point at
`/pwa/icon.svg` and `/pwa/icon-{180,192,512}.png`; the registry ships no icons. `app-scaffold` draws
a starting set; an app that installs `template` on its own adds these four files itself.
