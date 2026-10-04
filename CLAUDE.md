# app-foundation

Small, versioned Go primitives shared by niiku first-party applications. This repository is a
toolkit, not an application framework.

- Keep packages independent and usable with `net/http` and `database/sql` directly.
- Extract only behavior demonstrated by a consumer. Do not add domain types, route registries,
  repositories, dependency injection, an ORM, job DSLs, prompts, or app-owned copy.
- Application handlers, schemas, migrations, authorization policy, CSP, user-facing messages, and
  frontend design remain in each application.
- Cloudflare Access local mode must fail closed unless the listener is literal `127.0.0.1` and the
  actual request peer is loopback.
- SQLite defaults to one open/idle connection. Migrations are ordered, embedded, forward-only, and
  transactional.
- Tests must cover changed behavior. Run `go test ./...`, `go vet ./...`, and `go test -race ./...`.
- Commit directly to `main`. Releases use reviewed SemVer `v0.x.y` tags; consumers pin an exact tag.
- The repository is private. Consumers may vendor it for secret-free container builds; never put a
  GitHub credential in a Dockerfile, image layer, module URL, or committed configuration.
