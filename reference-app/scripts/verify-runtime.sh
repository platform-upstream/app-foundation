#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="${1:-$root/.local/foundation-reference}"
runtime_dir="$(mktemp -d "$root/.local/runtime.XXXXXX")"
active_pid=""

stop_server() {
  if [[ -n "$active_pid" ]] && kill -0 "$active_pid" 2>/dev/null; then
    kill -TERM "$active_pid"
    wait "$active_pid"
  fi
  active_pid=""
}
trap stop_server EXIT

wait_for_health() {
  local url="$1"
  local deadline=$((SECONDS + 5))
  until curl --fail --silent "$url/healthz" >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      return 1
    fi
    sleep 0.01
  done
}

mkdir -p "$root/.local"
if [[ ! -x "$binary" ]]; then
  echo "runtime binary is missing: $binary" >&2
  exit 1
fi

cold_ms=()
for run in 1 2 3 4 5; do
  port=$((18100 + run))
  started_ns="$(date +%s%N)"
  APP_ADDR="127.0.0.1:$port" \
  APP_DB="$runtime_dir/cold-$run.sqlite" \
    "$binary" >"$runtime_dir/cold-$run.log" 2>&1 &
  active_pid=$!
  wait_for_health "http://127.0.0.1:$port"
  ready_ns="$(date +%s%N)"
  cold_ms+=("$(((ready_ns - started_ns) / 1000000))")
  stop_server
done

port=18120
APP_ADDR="127.0.0.1:$port" \
APP_DB="$runtime_dir/measured.sqlite" \
  "$binary" >"$runtime_dir/measured.log" 2>&1 &
active_pid=$!
wait_for_health "http://127.0.0.1:$port"
sleep 2

read -r idle_rss idle_pss < <(awk '/^Rss:/ {rss=$2} /^Pss:/ {pss=$2} END {print rss, pss}' "/proc/$active_pid/smaps_rollup")
base="http://127.0.0.1:$port"
curl --fail --silent --show-error "$base/healthz" >/dev/null
curl --fail --silent --show-error "$base/" >/dev/null
curl --fail --silent --show-error "$base/deep-link" >/dev/null
curl --fail --silent --show-error "$base/api/state" >/dev/null
curl --fail --silent --show-error -X POST \
  -H 'Content-Type: application/json' \
  -H "Origin: $base" \
  --data '{"name":"runtime proof"}' \
  "$base/api/items" >/dev/null
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' "$base/api/missing")" == "404" ]]
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' "$base/api/memory/people")" == "503" ]]
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H "Origin: $base" \
  --data '{"prompt":"runtime proof"}' \
  "$base/api/complete")" == "503" ]]
sleep 1
read -r exercised_rss exercised_pss < <(awk '/^Rss:/ {rss=$2} /^Pss:/ {pss=$2} END {print rss, pss}' "/proc/$active_pid/smaps_rollup")
stop_server

printf 'cold_start_ms=%s\n' "$(IFS=,; echo "${cold_ms[*]}")"
mapfile -t sorted_cold_ms < <(printf '%s\n' "${cold_ms[@]}" | sort -n)
printf 'cold_start_min_ms=%s cold_start_median_ms=%s cold_start_max_ms=%s\n' \
  "${sorted_cold_ms[0]}" "${sorted_cold_ms[2]}" "${sorted_cold_ms[4]}"
printf 'idle_warmup_seconds=2\n'
printf 'idle_rss_kib=%s idle_pss_kib=%s\n' "$idle_rss" "$idle_pss"
printf 'exercised_rss_kib=%s exercised_pss_kib=%s\n' "$exercised_rss" "$exercised_pss"
