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

A pin bump resolves the private module, on any machine: `go env -w
GOPRIVATE=github.com/platform-upstream/*` and a git credential helper for github.com (for example
`git config --global credential.https://github.com.helper '!gh auth git-credential'`). GOPRIVATE
implies `GONOSUMDB`, but an environment that sets `GONOPROXY` or `GONOSUMDB` itself replaces that
default and asks sum.golang.org, where a private tag (or a fresh one) is missing (`verifying
module: … not found`): set `GONOSUMDB=github.com/platform-upstream/*` as well. Then `go get -tool github.com/platform-upstream/app-foundation/cmd/frontend-registry@vX.Y.Z
&& go mod vendor`, `sync` for the installed modules and review the diff.

`install` writes missing files but refuses to replace a changed target. `check` exits non-zero and
lists missing or changed registry-owned files. `sync` is the explicit replacement operation; review
its diff because it overwrites selected registry-owned source. Selecting `template` automatically
includes its dependencies `pwa`, `pwa-shell`, `touch` and `motion`. With no module list, every module is selected.
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
  to 44 x 44 around its centre (the element must not use `::after` itself; a `.hit-target` gets no
  44px `min-height`, so it keeps its own look); and `.no-pan-x`
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
  (follows the setting live), `useEntrance(ref, ready, { once?, maxMs? })` → `entering` for
  `data-entering` on a list (true from the render in which the data first arrives until the
  animations under `ref` finish, never again for that component: no entrance on a refetch or a
  filter; `once: 'session'` plays only in components that mount with the session's first view,
  until the page is next idle, so later navigations show their lists at once, as in Finance),
  `viewTransition(update)` (same-document view transition with the update flushed synchronously;
  without the API, under `reduce` or on a hidden page it just runs the update; resolves when
  done), `useSlidingIndicator(containerRef, selector, key, axis?)` → `{ style, instant }` (measures
  the selected item, follows resizes and font loads; `instant` on the first placement, so the app
  writes its transition as `.indicator:not([data-instant])`), plus `animationsSettled`,
  `indicatorStyle`, `sameBox`. Never put `view-transition-name` on `.app-shell-main` or the page
  column: it becomes a stacking context while named, and fixed overlays inside land under the bars
  (Paper, Finance). Which way a page enters, FLIP lists and the keyframes stay app-owned.
  Moving an existing app onto the policy is mostly mechanical: every `animation*` and
  `transition*` declaration goes into one `@media (prefers-reduced-motion: no-preference)` block,
  keeping its selector and at-rule ancestors; `@keyframes` and rules already under a
  `prefers-reduced-motion` query stay. Paper did it with a one-off PostCSS script; the registry
  ships none (it would add a Node dependency to a Go tool). Moving declarations to the end changes
  their cascade order: diff the result and check that no later rule meant to override them.
- `phone-audit` (`e2e/foundation/phone-audit.ts`, `e2e/foundation/phone-audit-tests.ts`) is the
  phone check from Paper's overflow and pan scripts and Workbench's mobile spec, for Playwright.
  It opens each route at 375x667 and 390x844 (touch, `isMobile`, `serviceWorkers: 'block'`,
  `de-CH`) and reports structured findings (`check`, `selector`, `detail`, `text`):
  `page-overflow`, `wide-element`, `sideways-pan` (real CDP touch swipes at three heights, both
  ways: scroll, visual viewport, scroller, zoom or a history swipe), `text-overflow` (text outside
  its control; ellipsis and line clamp pass; visually hidden text such as `.sr-only` and
  absolutely positioned children and pseudo-elements, a `NavBadge` or the `::after` of
  `.hit-target`, are not measured), `number-split`, `tap-target` (44px, counting an invisible
  `::before`/`::after` against its containing block, so a stretched link's `::after` with
  `inset: 0` counts as its card; inline links in text exempt), `input-font` (16px), `small-text`
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
  It requires `touch` and `motion` and passes `phone-audit` as installed: `main.tsx` imports
  `shell.css`, `touch.css`, `motion.css`, then `app.css`; `no-pan-x` sits on `html`, `body` and
  `mainProps.className`; nav items are 44px targets. Its `package.json` carries `@playwright/test`
  and `@axe-core/playwright` (in the frozen lockfile) and a `test:phone` script, so installing
  `phone-audit` only needs a Playwright config and `e2e/phone.spec.ts` (`app-scaffold` writes both).
- `api` also installs `online.ts`: `useOnline()` (`navigator.onLine` and its events through
  `useSyncExternalStore`) and `isOfflineError(err)`, true for a request that never reached a
  server (offline, or the fetch `TypeError` of Chromium, Firefox and Safari) and false for any
  `APIError`, which is an answer (also the 401 of an expired session). What the app shows offline,
  and whether its queries pause, stays app policy; there is no query-library coupling.
- `profile`: the compact profile button (`ProfileMenu compact`, `ProfileCard compact`, top bar on
  phones) is a 44px target, and its accessible name is "menu: name, initials" while the initials
  are what it shows (`profileButtonLabel`); the full card is named by its visible text.
  `profile` also installs `public/profile-theme.js`, for an app with a dark look
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
- `palette` is a command palette's logic, no component: `rank(items, query, tieBreak?)` orders
  `{ label, detail?, aliases? }` by exact label, label prefix, word start, every word of a
  multi-word query starting a word, alias, substring, every word anywhere, detail and last a fuzzy
  subsequence ("crts" → Certificates); `normalize` ignores case and accents and folds ß to ss and
  ae/oe/ue to a/o/u, so "uber", "ueber" and "Über" meet. Within a tier a shorter label wins, then
  the given order; `tieBreak(a, b)` (a comparator) decides between equal matches before the label
  length, e.g. active contracts first. `readRecents(key)` and
  `pushRecent(key, id)` keep the last picks (5) in localStorage under the app's key.
  `shortcuts.ts` has `isTypingTarget(el)` and `createShortcutHandler({ keys, go, primary })`: single
  keys by the typed character, `g` then a letter within 1.5 s, and `primary` combinations with ⌘
  (Apple) or Ctrl. It leaves alone keys in text fields (except `primary`), inside or behind an open
  modal dialog, during IME composition, and any key with Alt held, so AltGr (Ctrl+Alt on Windows;
  AltGr+2 types @ on a Swiss keyboard) never fires a shortcut. `primaryInDialog: true` lets the
  `primary` combinations through from inside a dialog, so ⌘K/Ctrl+K in the open palette closes it
  (single keys and go sequences stay off there; an action that does not apply returns false). The
  palette's look, commands and shortcut list stay in the app.
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

## Sheets, toasts, pull to refresh and busy cues

Look-neutral UI mechanics (handbook ADR 0005): behaviour, accessibility and iOS quirks, never a
look value. Each module has a core without React (`sheet.ts`, `toast.ts`, `pull.ts`, `busy.ts`) and a thin
React layer; CSS sits in `@layer foundation`, imported after `shell.css` and before the app's CSS,
so the app's rules win. Their logic runs under `go test` (Node with type stripping, 22.18+);
`frontendregistry/testdata/mechanics_e2e.mjs` drives an installed fixture in Chromium at 390×844
with touch (how: its header). Chromium cannot show iOS scroll chaining, safe areas or the
keyboard; an iPhone confirms those.

### `sheet`

A bottom sheet on a native `<dialog>`. `showModal()` puts it in the top layer, so it renders above
everything wherever it sits in the DOM (no portal; a `transform` or `view-transition-name` on the
page frame cannot trap it), makes the page behind inert, keeps Tab inside, and sends Escape only to
the topmost modal.

```tsx
import { useSheet, useSheetDrag } from './foundation/useSheet.js';
import './foundation/sheet.css';

const ref = useRef<HTMLDialogElement>(null);
useSheet(ref, open, () => setOpen(false));
useSheetDrag(ref, { onClose: () => setOpen(false), below: 700 });

<dialog ref={ref} className="my-sheet" aria-labelledby="sheet-title">
  <header data-sheet-handle><span className="grabber" /><h2 id="sheet-title">…</h2></header>
  …
</dialog>
```

- `useSheet(ref, open, onClose, { scroller?, backdropClose?, initialFocus? })` /
  `showSheet(dialog, { onClose, scroller, backdropClose, initialFocus }) → close`: `onClose` is the
  person's request (Escape, backdrop tap, a form with `method=dialog`); the app sets its state, and
  the hook closes the dialog. Focus returns to the opener. On a touch screen the dialog itself
  takes the focus unless an element inside has `autofocus`: a focused field brings up the
  keyboard, and `pwa-shell` then hides the nav. On a fine pointer `initialFocus` (an element or a
  selector inside the dialog) or else the first `[data-autofocus]` inside takes the focus. Use
  `data-autofocus`, not React's `autoFocus`: React focuses when the element mounts, before the
  dialog is shown, and that focus is lost.
- `lockScroll(scroller?) → release`: ref-counted, on `.app-shell-main` (the document scroller
  without the shell). Never `body`: with `pwa-shell` the document does not scroll, so a lock on
  `body` leaves the page behind the sheet scrolling on iOS. `showSheet` takes it; use it directly
  for other overlays.
- `useSheetDrag(ref, { onClose, threshold = 80, below? })` / `attachSheetDrag`: a drag from an
  element marked `data-sheet-handle` (grabber, header), touch and pen only, closes past `threshold`
  px or on a flick (over 0.5 px/ms in the last 100 ms); buttons, links and fields inside a handle
  stay pressable. The offset is `--sheet-drag`, applied as `translate`, so the app's own transforms
  and keyframes are untouched; `data-dragging` is set during the drag. Without a drag offset
  `translate` is `none`: any other value makes the sheet the containing block of its
  `position: fixed` children (a menu, a popover), which then scroll and clip with it.
- `sheet.css` (`dialog[data-sheet]`, set by `showSheet`): at the bottom edge, full width, never
  under the status bar, scrolls itself with `overscroll-behavior: contain`, and an `::after` spacer
  of `env(safe-area-inset-bottom)` so the last content clears the home indicator with or without a
  footer. Background, radius, shadow, padding, the grabber and the animations in and out are the
  app's; a centred dialog from a breakpoint is an app rule (`inset: 0; margin: auto; width: …`).
  Settling back after a drag is a `translate` transition the app may retime.

### `toast`

A store outside React, so data code can speak without a component; the look and every word are the
app's.

```tsx
import { toast, configureToasts } from './foundation/toast.js';
import { ToastRegion } from './foundation/ToastRegion.js';
import './foundation/toast.css';

configureToasts({ undoLabel: 'Rückgängig' });            // once; also max, ms, actionMs, errorMs
const dismiss = toast('Gelöscht', { undo: restore });     // or { action: { label, run }, tone, detail, ms }
<ToastRegion className="toasts" toastClassName="toast" label="Meldungen" />   // once, anywhere
```

- `toast(text, options) → dismiss`: at most `max` (3) at once, the oldest dropped; 4 s plain, 7 s
  with an action or undo and for errors, `ms: Infinity` until dismissed. An action dismisses the
  toast before it runs. `role="alert"` for `tone: 'error'`, `role="status"` otherwise.
- Timers pause while a mouse is over the toasts, focus is inside, or the page is hidden.
- `useToasts()` for a custom renderer; `<ToastRegion>` renders each toast as an element with the
  role and `data-tone`, the default content being text, detail and the action button, or the app's
  `children(toast)`.
- Placement: a manual popover in the top layer at `bottom: max(var(--shell-nav-height),
  env(safe-area-inset-bottom))`. The nav height already holds the home-indicator inset; do not add
  it again. While the keyboard is open (`pwa-shell`) the toasts move to the top. While a modal
  dialog is open the region moves into the topmost one: a modal makes everything outside it inert,
  a popover in the top layer included, so an Undo there could not be pressed.
- Apps without React: `subscribeToasts`/`getToasts` to render, `toastRegion(element)` to wire the
  element (popover, pause, following modals).

### `pull-refresh`

A Home Screen app has no pull to refresh of its own; a browser tab has. On only when the app runs
standalone (`display-mode` or iOS `navigator.standalone`) on a coarse pointer.

```tsx
import { usePullToRefresh } from './foundation/usePullToRefresh.js';

const main = useRef<HTMLElement>(null);
const { pull, ready, busy } = usePullToRefresh(main, () => queryClient.refetchQueries({ type: 'active' }));

<AppShell mainProps={{ ref: main }} …>
  {(pull > 0 || busy) && (
    <div className="ptr" role="status" style={{ height: pull }} data-ready={ready || undefined}>
      {busy ? <Spinner label="Aktualisiere" /> : <ArrowDown style={{ rotate: `${pull * 2}deg` }} />}
    </div>
  )}
  …
</AppShell>
```

- A one-finger pull from `scrollTop` 0: the first 10 px decide (more vertical than sideways and
  down, or never: a carousel, tabs or a diagonal swipe stay what they are), damping 0.5, `ready`
  from 72 px of indicator travel (144 px of finger), capped at 1.6× that; `busy` lasts until
  `refresh` settles (a rejection too) and at least `minSpin` (500 ms). All options:
  `{ trigger, max, slop, damping, minSpin, enabled }`.
- Off while a modal dialog, `[aria-modal="true"]` or an open Radix dialog
  (`[role=dialog][data-state=open]`, also `alertdialog`) is open, and for new pulls while busy.
- Passive listeners: nothing is cancelled and the page itself never moves, so the scroller's own
  bounce stays as it is. The indicator, its position and its motion are the app's.
- Apps without React: `attachPullToRefresh(scroller, refresh, onState, options) → detach`.

### `busy`

Cues for work in progress that keep the page as it is: a thin bar while data refetches and a
blur over results while they recompute. A gate decides when they show; the look, the timings and
the words are the app's.

```tsx
import { BusyBar, PendingScope, useDelayedBusy } from './foundation/Busy.js';
import './foundation/busy.css';

<header className="topbar">
  …
  <BusyBar busy={useIsFetching() > 0} delay={350} minVisible={400} className="topbar-busy" status={refreshing ? 'Updating' : undefined} />
</header>

<PendingScope busy={query.isFetching} delay={150} minVisible={0}>
  {(on) => <>
    <section className="card" data-controls>…sliders…</section>
    <section className="card" data-result>…figures…</section>
    {on && <div className="pending-pill">Calculating</div>}
  </>}
</PendingScope>
```

```css
.topbar-busy { position: absolute; inset: auto 0 -1px; height: 2px; color: var(--accent);
  --busy-bar-duration: 1.2s; --busy-bar-opacity: 0.8; --busy-fade: 0.25s; }
.busy-scope { --busy-pending-opacity: 0.5; --busy-pending-filter: blur(1.5px); --busy-fade: 0.2s; }
```

- `busyGate({ delay, minVisible }, onChange, clock?)` (`busy.ts`, no React) turns a busy flag into
  shown or hidden: shown after `delay` ms of busy, so a fast answer never blinks; busy ending
  before that shows nothing. Once shown it stays at least `minVisible` ms; busy again meanwhile
  keeps it without a new delay. `useDelayedBusy(busy, { delay, minVisible })` is the hook. Both
  take a plain boolean (`useIsFetching() > 0`, `query.isFetching`, `mutation.isPending`); there is
  no query wrapper.
- `<BusyBar busy delay minVisible className? status?>`: always mounted, `data-busy` while shown,
  so it never shifts the layout; the bar works at every width (one transform moves the segment
  across, `--busy-bar-segment` wide, 30% by default) and is `aria-hidden`. Beside it a visually
  hidden `role="status"` region, also always mounted, says `status` while the bar shows: give it
  for a wait the person started (a manual refresh), leave it out for background polls.
- `<PendingScope busy delay minVisible className?>` sets `data-pending` and `aria-busy` on its
  wrapper (`.busy-scope`); descendants marked `data-result` take `--busy-pending-opacity` and
  `--busy-pending-filter`. Everything else stays sharp and usable, the controls that caused the
  recompute above all. `children` may be a function of `on` to render the app's own label. To dim
  more than `[data-result]`, an app rule uses the same properties, e.g.
  `.busy-scope[data-pending] .card:not([data-controls]) { opacity: var(--busy-pending-opacity);
  filter: var(--busy-pending-filter); }`.
- `busy.css` has no look default: the bar's place, height, radius and colour (the segment is
  `currentColor`) come from the app's class; `--busy-bar-duration` (required for the sweep),
  `--busy-bar-easing` (ease-in-out), `--busy-bar-opacity` (1), `--busy-fade` (fade in and out, none
  without it), `--busy-pending-opacity` (1) and `--busy-pending-filter` (none). Sweep and fades
  run only under `prefers-reduced-motion: no-preference`; under `reduce` the bar is still and full
  width while busy.
