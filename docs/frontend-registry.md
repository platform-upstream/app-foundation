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

## Sheets, toasts and pull to refresh

Look-neutral UI mechanics (handbook ADR 0005): behaviour, accessibility and iOS quirks, never a
look value. Each module has a core without React (`sheet.ts`, `toast.ts`, `pull.ts`) and a thin
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

- `useSheet(ref, open, onClose, { scroller?, backdropClose? })` / `showSheet(dialog, { onClose,
  scroller, backdropClose }) → close`: `onClose` is the person's request (Escape, backdrop tap, a
  form with `method=dialog`); the app sets its state, and the hook closes the dialog. Focus returns
  to the opener. On a touch screen the dialog itself takes the focus unless an element inside has
  `autofocus`: a focused field brings up the keyboard, and `pwa-shell` then hides the nav.
- `lockScroll(scroller?) → release`: ref-counted, on `.app-shell-main` (the document scroller
  without the shell). Never `body`: with `pwa-shell` the document does not scroll, so a lock on
  `body` leaves the page behind the sheet scrolling on iOS. `showSheet` takes it; use it directly
  for other overlays.
- `useSheetDrag(ref, { onClose, threshold = 80, below? })` / `attachSheetDrag`: a drag from an
  element marked `data-sheet-handle` (grabber, header), touch and pen only, closes past `threshold`
  px or on a flick (over 0.5 px/ms in the last 100 ms); buttons, links and fields inside a handle
  stay pressable. The offset is `--sheet-drag`, applied as `translate`, so the app's own transforms
  and keyframes are untouched; `data-dragging` is set during the drag.
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
- Off while a modal dialog or `[aria-modal="true"]` is open, and for new pulls while busy.
- Passive listeners: nothing is cancelled and the page itself never moves, so the scroller's own
  bounce stays as it is. The indicator, its position and its motion are the app's.
- Apps without React: `attachPullToRefresh(scroller, refresh, onState, options) → detach`.
