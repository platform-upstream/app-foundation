#!/bin/sh
# Syncs the registry-owned frontend files (app-foundation's UI modules) after an app-foundation
# update: Renovate runs it as a postUpgradeTask (renovate.json), a human after `go get` and
# `go mod vendor`. The targets and modules are the ones the Dockerfile checks, so check and sync
# never drift apart. Offline, from vendor/.
set -eu
cd "$(dirname "$0")/.."
grep -o 'frontend-registry check -target [^ ]* -modules [^ "]*' Dockerfile | sort -u |
  while read -r line; do
    # shellcheck disable=SC2086
    GOFLAGS=-mod=vendor GOPROXY=off go tool $(echo "$line" | sed 's/ check / sync /')
  done
