# App scaffold

`app-scaffold` renders a complete, deployable application into an empty repository: Go backend
(Cloudflare Access, SQLite with migrations, security headers, public `/pwa/` files), the React/Vite
frontend from the registry (`template` with `pwa`, `pwa-shell`, `touch` and `motion`, plus
`api`, `sheet`, `toast` and `phone-audit`), a starting letter icon in the app colour, Dockerfile,
`.ci/pipeline.yaml`, Helm chart and AGENTS.md. A notes feature runs through every layer as a
working example. It renders once and refuses to replace anything; every file then belongs to the
app. Generation rejects symlink parents and uses root-confined atomic writes.

```bash
go run github.com/platform-upstream/app-foundation/cmd/app-scaffold@vX.Y.Z \
  -platform ../platform-config/platform.json -target ./notes -name notes -title "Notes"
cd notes && go mod tidy
```

## Platform profile

No platform's names live in this module. Each platform keeps one profile, e.g. `platform.json`
in its config repository:

```json
{ "org": "acme-apps", "domain": "apps.example.org",
  "registry": "registry.example.org", "accessTeam": "acme.cloudflareaccess.com" }
```

It yields the module path `github.com/<org>/<name>`, the hostname `<name>.<domain>` and the image
`<registry>/<org>/<name>`. The Access team appears only in the generated AGENTS.md example: the
chart's `cfTeamDomain` is empty and required, set by the deployment wrapper. `-org`, `-domain`, `-registry` and
`-access-team` override single values; `-host`, `-module` and `-image` override the derived ones.

## What the generated app follows

- Dockerfile: Node 26 + pnpm from npm (`packageManager`), Go stage with `frontend-registry check`,
  gofmt, vet and tests, distroless static non-root runtime. Modules come from `go mod download`
  (public proxy, checked against `go.sum`); module and build cache mounts end in
  `-${CACHE_SCOPE}` (CI contract).
- Chart: chart-owned image, RollingUpdate with a native `preStop` sleep (Kubernetes 1.30+), PVC
  with `helm.sh/resource-policy: keep`, read-only root filesystem, UID 65532, readiness on
  `/readyz` (database ping), liveness on `/healthz`, `APP_VERSION`/`APP_COMMIT` from the chart for `/version`, Access values (`cfTeamDomain`, `cfAccessAud`,
  `cfAccessServiceTokens`).
- Routes: security headers → `/healthz`, `/readyz`, `/version` (`server.Probes`) → `web.PublicFiles` (`/pwa/` without a session) →
  Access middleware → `/api/` (same-origin mutations) and the SPA.
- Migrations are additive, because old and new pod overlap during a release.
- Phone-ready UI (handbook ADR 0005): `touch.css`, `motion.css`, `sheet.css` and `toast.css` are
  imported after `shell.css` and before the app's CSS, `no-pan-x` sits on `html`, `body` and the
  shell's `<main>`, nav items are 44px, a `ToastRegion` is mounted and the notes feature reports
  deletes with `toast()`. `pnpm build && pnpm test:phone` runs the phone audit (375x667 and
  390x844, then reduced motion) against `vite preview` of the build on port `-port` minus 2800,
  with the API answered from fixtures in `web/e2e/phone.spec.ts`; `CHROME=<chromium binary>` uses
  a browser other than Playwright's own. The Dockerfile checks `api`, `pwa`, `pwa-shell`, `touch`,
  `motion`, `sheet`, `toast` and `phone-audit`; it does not run the audit (no browser in the
  image).

`TestGeneratedAppBuildsAndPassesItsTests` generates an app, points it at the checkout and runs its
`go vet` and `go test`, so the templates cannot rot unnoticed.
