#!/bin/sh
# Syncs the registry-owned frontend files (app-foundation's UI modules) after an app-foundation
# update: Renovate runs it as a postUpgradeTask (renovate.json), a human after `go get`. The
# targets and modules are the ones the Dockerfile checks, so check and sync never drift apart.
set -eu
cd "$(dirname "$0")/.."
grep -o 'frontend-registry check -target [^ ]* -modules [^ "]*' Dockerfile | sort -u |
  while read -r line; do
    # shellcheck disable=SC2086
    go tool $(echo "$line" | sed 's/ check / sync /')
  done
