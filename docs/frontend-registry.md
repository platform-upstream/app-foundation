# Frontend template and source registry

The registry installs reviewable source into an application repository. It is not a runtime package
and does not centralize application design. Consumers pin the exact `app-foundation` release used
to install or check source.

## Workflow

The tool is pinned with the rest of the foundation in the consumer's `go.mod` and vendored, so
it runs offline and checks against the pinned release:

```bash
go get -tool github.com/castillo-platform/app-foundation/cmd/frontend-registry@v0.11.0
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
- `pwa` installs the update lifecycle proven by Read. `template` installs a starting network-first
  service worker (`public/sw.js`) that the application owns from then on (every app adapts it:
  asset allowlists, push, cache names), so `check` does not cover it. The starting worker never
  handles or caches `/api/`, `/internal/`, or `/cdn-cgi/`; it does not cache redirects; and ordinary
  offline navigations may fall back to the cached shell. A page that stays open, such as an installed
  app that is resumed rather than relaunched, checks for a newer build on load and whenever it
  becomes visible, and reloads into it. `onUpdate(apply)` hands that decision to the application:
  ask the user, or wait until nothing would be interrupted (Listen never reloads during playback). Cache names are isolated by service-worker
  scope. Push, share-target, notification, and background-sync behavior remains app-owned.
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
