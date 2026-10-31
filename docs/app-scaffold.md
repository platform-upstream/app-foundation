# App scaffold

`app-scaffold` renders a complete, deployable application into an empty repository: Go backend
(Cloudflare Access, SQLite with migrations, security headers, public `/pwa/` files), the React/Vite
frontend from the registry (`template`, `api`), a starting letter icon in the app colour, Dockerfile,
`.ci/pipeline.yaml`, Helm chart and AGENTS.md. A notes feature runs through every layer as a
working example. It renders once and refuses to replace anything; every file then belongs to the
app. Generation rejects symlink parents and uses root-confined atomic writes.

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
go run github.com/platform-upstream/app-foundation/cmd/app-scaffold@v1.1.0 \
  -platform ../platform-config/platform.json -target ./notes -name notes -title "Notes"
cd notes && go mod tidy && go mod vendor
```

## Platform profile

No platform's names live in this module. Each platform keeps one profile, e.g. `platform.json`
in its config repository:

```json
{ "org": "castillo-platform", "domain": "jprinzbach.de",
  "registry": "zot.int.jprinzbach.de", "accessTeam": "prinzbach.cloudflareaccess.com" }
```

It yields the module path `github.com/<org>/<name>`, the hostname `<name>.<domain>` and the image
`<registry>/<org>/<name>`. The Access team appears only in the generated AGENTS.md example: the
chart's `cfTeamDomain` is empty and required, set by the deployment wrapper. `-org`, `-domain`, `-registry` and
`-access-team` override single values; `-host`, `-module` and `-image` override the derived ones.

## What the generated app follows

- Dockerfile: Node 26 + pnpm from npm (`packageManager`), Go stage with `frontend-registry check`,
  gofmt, vet and tests against `vendor/`, distroless static non-root runtime. Cache mounts end in
  `-${CACHE_SCOPE}` (CI contract).
- Chart: chart-owned image, RollingUpdate with a native `preStop` sleep (Kubernetes 1.30+), PVC
  with `helm.sh/resource-policy: keep`, read-only root filesystem, UID 65532, readiness on
  `/readyz` (database ping), liveness on `/healthz`, Access values (`cfTeamDomain`, `cfAccessAud`,
  `cfAccessServiceTokens`).
- Routes: security headers → `/healthz`, `/readyz` → `web.PublicFiles` (`/pwa/` without a session) →
  Access middleware → `/api/` (same-origin mutations) and the SPA.
- Migrations are additive, because old and new pod overlap during a release.

`TestGeneratedAppBuildsAndPassesItsTests` generates an app, points it at the checkout and runs its
`go vet` and `go test`, so the templates cannot rot unnoticed.
