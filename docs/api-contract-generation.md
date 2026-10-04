# API contract generation decision

**Decision:** reject generated API/DTO source for Phase 1. Keep readable app-owned Go DTOs and
small TypeScript/Zod runtime contracts. Revisit only when two migrated consumers demonstrate that
one contract can replace more maintained code than it adds.

## Bounded experiment

The experiment modeled a Read-like `GET /api/items` plus `POST /api/items` slice with five schemas,
two query parameters, the standard error envelope, URL/length/range constraints, nullable fields,
and one domain response. It used pinned current releases:

- `oapi-codegen v2.8.0`, generating Go types only;
- `openapi-typescript 7.13.0`, generating TypeScript declarations only; and
- the repository's proven TypeScript 7.0.2 compiler for the declaration check.

The 90-line, 2,623-byte OpenAPI input produced 53 lines (1,605 bytes) of Go and 124 lines (3,166
bytes) of TypeScript. Two independent runs were byte-identical:

| Artifact | SHA-256 |
|---|---|
| OpenAPI input | `b31c668f7141c2d115c495a6ba7af5e4f97048037f752f4b32714e8e950f7839` |
| generated Go, both runs | `bb876cdaf0b96c87b498ac511620bdd6c358095a5c6426f1ba001b6db0dbbc52` |
| generated TypeScript, both runs | `11f54d065147acc081aeb04bf4c94b4322fcfded86bff81bda747871040bf89e` |

The Go output compiled and its focused test passed. The TypeScript declarations passed a strict
type check. Generation is therefore deterministic and the generated Go is readable enough; those
two acceptance criteria passed.

The decisive criterion failed: generation added more maintained contract surface than it removed.
The OpenAPI source plus generated files totaled 267 lines before routes, handler logic, app-owned
status semantics, or runtime validation. Read's current compact Zod contracts both infer frontend
types and validate untrusted runtime values. The Go migration still needs explicit parsing and
validation to preserve JavaScript/Zod behavior, including the already-proven WHATWG URL edge cases.

The constraint probe demonstrated the gap rather than relying on inspection:

- Go JSON decoding accepted `url: "not a URL"`, a 12,001-character value whose schema maximum was
  12,000, `limit: 0`, and `offset: -1`.
- TypeScript accepted the same invalid URL and oversized string because declarations cannot encode
  those runtime constraints.

Adopting the generated source would therefore introduce an OpenAPI schema and two generators while
retaining the Zod/runtime validators and parity tests that make the contract safe. Full generated
server/client layers would add routing/runtime dependencies and obscure the intentionally explicit
foundation composition without eliminating those validators.

## Commands and retained evidence

The ignored experiment is retained at `artifacts/fnd007/` in the workspace. It contains the input,
both generated runs, the Go constraint probe, and the strict TypeScript probe. The relevant commands
were:

```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 \
  -generate types -package contract -o run1/types.gen.go openapi.yaml
pnpm --package=openapi-typescript@7.13.0 dlx openapi-typescript \
  openapi.yaml -o run1/schema.d.ts
cmp run1/types.gen.go run2/types.gen.go
cmp run1/schema.d.ts run2/schema.d.ts
(cd run1 && go test ./...)
tsc --noEmit --strict --target ES2022 --module NodeNext --moduleResolution NodeNext \
  run1/typecheck.ts run1/schema.d.ts
```

This rejection does not prohibit app-owned OpenAPI documentation or a later consumer-specific
generator. It prevents an unproven generator from becoming a foundation dependency or a migration
gate.
