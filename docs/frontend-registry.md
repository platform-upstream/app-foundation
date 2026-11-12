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
- `touch` (`src/foundation/touch.css`, `@layer foundation`, imported after `shell.css` and before
  the app's CSS) is the touch floor proven by Console and Paper: on `(pointer: coarse)` fields at
  `max(16px, 1em)` (iOS zooms into a smaller field and stays zoomed) and buttons, selects and
  fields at least 44px high (checkboxes and radios take the finger through their label); on
  controls no tap highlight and `touch-action: manipulation` (no double-tap delay, pinch zoom
  stays); `text-size-adjust: 100%`; `.hit-target`, an invisible `::after` that grows a small chip
  to 44 x 44 around its centre (the element must not use `::after` itself); and `.no-pan-x`
  (`overflow-x: clip; overscroll-behavior-x: none`), which the app puts on `html`, `body` and the
  scroller (`mainProps={{ className: 'no-pan-x' }}` on `AppShell`): without it on all three a
  diagonal flick pans the page or swipes back in history. Rule that goes with it: never autofocus a
  field on touch; the keyboard opens, `pwa-shell` takes it for typing and drops the nav (focus on
  a tap, or only on `(pointer: fine)`). Unlayered app rules win over the layer, so a component
  that sets a 14px field or a 32px button overrides the floor; `phone-audit` reports it. Press
  feedback, sizes above the floor and every look value stay app-owned.
- `motion` has no tokens: durations, easings, keyframes and distances are the app's look.
  `motion.css` holds the reduced-motion policy (its header is the rule set: every movement under
  `prefers-reduced-motion: no-preference`; never a global `!important` kill switch, because an
  `!important` in an earlier cascade layer beats every later and unlayered `!important`, so a
  kill switch in one layer silently defeats the app's reduced-motion fade in another; entrances
  with fill-mode `backwards`; no `view-transition-name` on the scrolling page frame) and stops
  view transitions under `reduce`. `motion.ts`: `prefersReducedMotion()`, `useReducedMotion()`
  (follows the setting live), `useEntrance(ref, ready)` → `entering` for `data-entering` on a
  list (true from the render in which the data first arrives until the animations under `ref`
  finish, never again for that component: no entrance on a refetch or a filter),
  `viewTransition(update)` (same-document view transition with the update flushed synchronously;
  without the API, under `reduce` or on a hidden page it just runs the update; resolves when
  done), `useSlidingIndicator(containerRef, selector, key, axis?)` → `{ style, instant }` (measures
  the selected item, follows resizes and font loads; `instant` on the first placement, so the app
  writes its transition as `.indicator:not([data-instant])`), plus `animationsSettled`,
  `indicatorStyle`, `sameBox`. Never put `view-transition-name` on `.app-shell-main` or the page
  column: it becomes a stacking context while named, and fixed overlays inside land under the bars
  (Paper, Finance). Which way a page enters, FLIP lists and the keyframes stay app-owned.
- `phone-audit` (`e2e/foundation/phone-audit.ts`, `e2e/foundation/phone-audit-tests.ts`) is the
  phone check from Paper's overflow and pan scripts and Workbench's mobile spec, for Playwright.
  It opens each route at 375x667 and 390x844 (touch, `isMobile`, `serviceWorkers: 'block'`,
  `de-CH`) and reports structured findings (`check`, `selector`, `detail`, `text`):
  `page-overflow`, `wide-element`, `sideways-pan` (real CDP touch swipes at three heights, both
  ways: scroll, visual viewport, scroller, zoom or a history swipe), `text-overflow` (text outside
  its control; ellipsis and line clamp pass), `number-split`, `tap-target` (44px, counting an
  invisible `::before`/`::after`, inline links in text exempt), `input-font` (16px), `small-text`
  (14px), `autofocus`, `layout-shift` (Layout Instability API in a refetch window), `axe`
  (wcag2a/aa, 2.1 a/aa, 2.2 aa) and, in a pass with `reducedMotion: 'reduce'`,
  `running-animation`. `sideways-pan` and `layout-shift` need Chromium; elsewhere they report
  nothing. The app adds `@playwright/test` and `@axe-core/playwright` as devDependencies (or sets
  `axe: false`), a Playwright config with its `baseURL` and server, and one spec it owns:

  ```ts
  // e2e/phone.spec.ts
  import { forbidComputedStyle } from './foundation/phone-audit.ts';
  import { phoneAudit } from './foundation/phone-audit-tests.ts';

  phoneAudit({
    setup: (page) => page.route('**/api/**', serveFixtures), // the app's own test data
    routes: [
      { path: '/', ready: (page) => page.getByRole('list').waitFor() },
      { path: '/settings', options: { skip: ['layout-shift'] } },
    ],
    refetch: (page) => page.evaluate(() => window.dispatchEvent(new Event('focus'))),
    platformRules: [forbidComputedStyle('no-glass', 'backdrop-filter')],
  });
  ```

  Options: `skip` (check names), `ignore` (a selector whose subtree is not reported), `controls`
  (the app's own control classes, judged like buttons), `scroller` (default `.app-shell-main`),
  `minTarget`/`minInputFont`/`minText`, `axe: { tags, disableRules } | false`, `refetch` and
  `refetchWindowMs` (default: a `visibilitychange` and a `focus` event, which refetch only stale
  TanStack queries; serve changed data or invalidate for a real refetch), `maxLayoutShift`,
  `viewports`, `locale`, `reducedMotion: false`. `platformRules` take a platform's own rules
  (`{ name, run(page) → Finding[] }`, or `forbidComputedStyle(name, property, allowed?)`), so a
  platform enforces its look decisions without putting them in the shared code. Apps with their
  own specs use `auditPage`, `auditReducedMotion`, `installLayoutShiftObserver` (before
  `page.goto`), `openRoute`, `phoneContextOptions` and `formatFindings` directly. The e2e files
  import with `.ts` extensions, so an `e2e/tsconfig.json` that type-checks them sets
  `allowImportingTsExtensions`. What Chromium cannot show (status bar, `lvh`, the keyboard,
  pull-to-refresh on iOS) stays a short check on an iPhone.
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
