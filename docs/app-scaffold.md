# Application delivery scaffold

`app-scaffold` generates the repository-local Docker, CI, and Helm starting point for a stateful Go
application with an embedded React/Vite frontend. It renders once and refuses to replace any target;
the resulting files belong to the application and are expected to diverge where its behavior needs
different resources, secrets, networking, or release checks.
Generation rejects symlink parents and uses root-confined atomic writes so repository contents
cannot redirect output outside the requested target.

```bash
go run github.com/castillo-platform/app-foundation/cmd/app-scaffold@v0.8.0 \
  -target . \
  -name example-app \
  -image zot.int.jprinzbach.de/castillo-platform/example-app
```

The name must be a DNS label. The image argument is a repository without a tag. Generate into a
repository that already follows the standard `cmd/`, `internal/`, `migrations/`, and `web/` layout;
then review every generated file before committing it.

The Dockerfile checks the registry-owned frontend files (`pwa`, `pwa-shell`); add the tool to
`go.mod` first (`go get -tool github.com/castillo-platform/app-foundation/cmd/frontend-registry`,
then `go mod vendor`).

The generated `AGENTS.md` is a stub: fill in the first paragraph and the app-specific look, keep
the pointers to the shared UI rules, the architecture specification and the `review` skill.

## Docker and private modules

The Dockerfile builds the frozen pnpm frontend first, then copies it into the Go embed directory.
The Go stage format-checks, vets, tests, and builds with `-mod=vendor`; it performs no module fetch
and accepts no credential argument or secret mount. Resolve an exact private foundation release on
the trusted workstation and vendor it before building:

```bash
go env -w GOPRIVATE=github.com/castillo-platform/*
go get github.com/castillo-platform/app-foundation@v0.8.0
go mod tidy
go mod vendor
podman build -t example-app:local .
```

The runtime is the established distroless static non-root image and contains only `/app`. The chart
provides writable `/tmp` and optional `/data` mounts while keeping the root filesystem read-only.
The default build assumes one main package beneath `cmd/`; an application with multiple commands
must make the output choice explicit in its owned Dockerfile.

## Repository-local CI

`.ci/pipeline.yaml` follows the current direct-main `ci-deliver` contract: pnpm verifies the
frontend, the Docker builder gates Go formatting/vet/tests, the repository owns one image, and the
repository's `deploy/` chart is released with it. This scaffold does not change the shared runner,
router, app-of-apps wrapper, or any production workload.

## Helm defaults

- The chart owns `image.repository` and `image.tag`; a production wrapper must not shadow the
  released chart image.
- One replica, `Recreate`, and a `ReadWriteOnce` PVC avoid concurrent SQLite writers and RWO rollout
  deadlocks.
- The initial profile is 5m CPU / 16 MiB memory requested, 500m / 64 MiB limited, and
  `GOMEMLIMIT=48MiB`. These are starting values that an app replaces with measured values.
- The pod listens on all its interfaces (`APP_ADDR=:8080`); the namespace's NetworkPolicies decide
  who may connect.
- Service-account tokens and service links are disabled. Pod/container security contexts require
  non-root UID/GID 65532, RuntimeDefault seccomp, no capabilities or privilege escalation, and a
  read-only root filesystem.
- Readiness and liveness are shallow `GET /healthz` probes. The PVC is optional for genuinely
  stateless apps. Ingress is disabled until a hostname is supplied.

ExternalSecrets, egress/ingress NetworkPolicies, RBAC, background schedules, backup annotations,
and application environment variables are deliberately not generated: their least-privilege shape
depends on the app. Add them in the consumer chart from demonstrated requirements.

## Local acceptance evidence

The ignored fixture at `artifacts/fnd009/` was generated with this command, composed with the
frontend registry, and pinned to private `app-foundation v0.7.0` before vendoring. Validation passed:

- Go vendored test, frontend frozen install/typecheck/build, and credential-free `podman build`;
- distroless container execution as `65532:65532` with `--read-only`, writable tmpfs mounts, healthy
  `/healthz`, and an embedded shell;
- `helm lint`, disabled-ingress render, enabled-ingress render, and assertions for the chart-owned
  image, probes, PVC, resource defaults, listen address, and security contexts; and
- a source/image-history credential-pattern scan.

No image was pushed and no chart was installed.
