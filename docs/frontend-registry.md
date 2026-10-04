# Frontend template and source registry

The registry installs reviewable source into an application repository. It is not a runtime package
and does not centralize application design. Consumers pin the exact `app-foundation` release used
to install or check source.

## Workflow

```bash
go run github.com/niiku-platform/app-foundation/cmd/frontend-registry@v0.7.0 list
go run github.com/niiku-platform/app-foundation/cmd/frontend-registry@v0.7.0 \
  install -target . -modules template,api
go run github.com/niiku-platform/app-foundation/cmd/frontend-registry@v0.7.0 \
  check -target . -modules template,api
```

`install` writes missing files but refuses to replace a changed target. `check` exits non-zero and
lists missing or changed registry-owned files. `sync` is the explicit replacement operation; review
its diff because it overwrites selected registry-owned source. Selecting `template` automatically
includes its `pwa` dependency. With no module list, every module is selected.

The template pins the React/Vite/TypeScript/TanStack Query/Zod versions already exercised by Read,
uses a relative Vite base for path-proxy previews, and includes a frozen pnpm lockfile. Rename the
package and replace the shell content, manifest metadata, and neutral stylesheet as part of app
creation. Those files are a starting point, not a shared theme.

## Accepted primitives

- `api` is a same-origin JSON client. It understands the standard `{ "error": string }` envelope,
  requires an injected runtime schema such as Zod, refuses paths outside its configured API base,
  exposes status through `APIError`, uses manual redirects so Cloudflare Access expiry is visible,
  and leaves the exact auth-expiry predicate/action and user-facing fallback copy injectable.
- `pwa` installs the update lifecycle proven by Read and a network-first service worker. It never
  handles or caches `/api/`, `/internal/`, or `/cdn-cgi/`; it does not cache redirects; and ordinary
  offline navigations may fall back to the cached shell. Cache names are isolated by service-worker
  scope. Push, share-target, notification, and background-sync behavior remains app-owned.
- `template` supplies only a clean React/Vite entry point, visible application-owned QueryClient
  policy, accessible document defaults, relative-base build configuration, PWA registration, and a
  minimal shell that tells adopters to create an application identity.

## Deliberate non-abstractions

- Query stale times, retries, invalidation, polling, and streaming are application policy. Read and
  Console already differ, so the registry pins the library but provides no shared query wrapper.
- Color values, typography choices, components, layout, navigation, empty states, and interaction
  design are not shared. Existing apps have distinct identities; there is no token package or design
  system.
- Cloudflare identity, authorization, signed-out pages, CSP, and security-header policy remain at
  the edge or in each application. The API source accepts only an expiry hook.
- Runtime API schemas stay app-owned following the FND-007 generation rejection.

The local acceptance fixture is retained under the workspace's ignored `artifacts/fnd008/`. A fresh
registry install passed registry check, frozen-lockfile install, strict type checking, and production
Vite build.
