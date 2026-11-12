#!/usr/bin/env bash
# Every check of this module, the same locally and in CI (ci/Dockerfile, the verify build a
# platform runs for each pull request). Run it before a release tag. The scaffolded app's Docker
# build and image run need a container runtime and stay a local step (README).
set -euo pipefail
cd "$(dirname "$0")/.."
echo "== gofmt"; test -z "$(gofmt -l $(git ls-files '*.go'))" || { gofmt -l $(git ls-files '*.go'); exit 1; }
echo "== vet";   go vet ./...
echo "== tests (race; includes the scaffolded app's own vet and tests)"; go test -race ./...
echo "== govulncheck"; go run golang.org/x/vuln/cmd/govulncheck@latest ./...
echo "== no platform or personal names"
if git grep -niE 'jprinzbach|castillo|niiku|prinzbach|nikolas|philips|freiburg|baloise|primeo|sympany|waldorf|gmail\.com|\blina\b|\bkimi\b' -- . ':!frontendregistry/sources/template/pnpm-lock.yaml' ':!MIGRATING.md' ':!ci/check.sh'; then
  echo "platform or personal names in upstream code"; exit 1
fi
echo "all checks passed"
