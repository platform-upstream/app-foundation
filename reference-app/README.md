# Foundation reference app

This local-only application is the Phase 1 integration proof for `app-foundation`. It is not a
production workload and has no deployment entry in the platform repositories. The committed
module pins `github.com/niiku-platform/app-foundation v0.8.0` exactly and vendors that resolved
graph so the container build needs no GitHub credential or module cache.

It proves the shared server lifecycle, standard JSON behavior, Cloudflare Access and loopback
local modes, SQLite migrations and snapshots, embedded React/Vite routing, PWA caching, the exact
shared Memory client, an app-owned seam over the shared `llm` gateway client, and the generated Docker/CI/Helm scaffold. The
database starts empty; verification writes only to isolated temporary files.

## Local run

```bash
cd web
pnpm install --frozen-lockfile
pnpm run typecheck
pnpm run build
cd ..

GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
GOWORK=off go build -trimpath -ldflags='-s -w' -o .local/foundation-reference ./cmd/app
APP_DB=.local/reference.sqlite ./.local/foundation-reference
```

The safe default is loopback-only local auth at `127.0.0.1:8099`. The app intentionally uses a
root-relative production asset base so direct SPA routes work; from this workstation the human
preview therefore uses `https://code.niiku.xyz/absproxy/8099/`.

Cloudflare mode requires all three external values and protects both documents and APIs while
leaving the shallow process health endpoint public:

```bash
APP_AUTH_MODE=cloudflare \
APP_ORIGIN=https://reference.example.invalid \
CF_TEAM_DOMAIN=team.cloudflareaccess.com \
CF_ACCESS_AUD=replace-me \
./.local/foundation-reference
```

`MEMORY_BASE_URL` plus `MEMORY_API_KEY` enable the shared Memory seam. `LLM_BASE_URL`,
`LLM_API_KEY` and `LLM_MODEL` (the app's gateway alias, required with a key) enable the LLM seam
over app-foundation `llm`; tests use fake HTTP servers (`llm/llmtest`) and never require live
inference.

## Reproducible verification

The data suite exercises fresh migration, one-version upgrade with row preservation, failed
migration rollback, live-WAL snapshot/restore, integrity checking, and immutable read-only access
that rejects writes. HTTP tests cover bounded strict JSON, the standard error envelope, origin
policy, explicit API 404s, SPA/missing-file boundaries, Access placement, fake integration seams,
and graceful shutdown with an in-flight request.

Build the credential-free runtime and render the chart with explicit non-secret placeholders:

```bash
GOWORK=off go mod vendor
podman build -t localhost/foundation-reference:test .
helm lint deploy \
  --set auth.origin=https://reference.example.invalid \
  --set auth.teamDomain=team.cloudflareaccess.com \
  --set auth.audience=reference-audience
helm template reference deploy \
  --set auth.origin=https://reference.example.invalid \
  --set auth.teamDomain=team.cloudflareaccess.com \
  --set auth.audience=reference-audience
```

Browser verification uses `playwright-cli` against the loopback server. It must cover an item
mutation, a deep link, service-worker activation, an offline navigation with an uncached API
request, one cache-version update/activation, and a fresh signed-out browser against Cloudflare
mode. Runtime measurement uses five cold starts and reads `Rss` and `Pss` from
`/proc/<pid>/smaps_rollup` after a fixed idle warm-up and again after the representative browser
and API sequence. The cold-start helper uses Go's monotonic clock from immediately before process
spawn to the first successful health response; the entire check is intentionally not a load test.
