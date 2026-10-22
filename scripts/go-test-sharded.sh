#!/usr/bin/env bash
# Runs the Go tests with the root package split across processes.
#
# The root package holds most of the tests (over a thousand) and they share
# package-level state, so they cannot simply be marked t.Parallel. Each shard
# is a separate process of the same test binary with its own globals: the
# shards run concurrently with no interference. The other packages run with
# the usual `go test` parallelism at the same time.
#
# GEXTTO_TEST_SHARDS sets the number of root shards (default: the CPU count,
# at most 4). Extra arguments are passed to every `go test`/test binary run
# (for example -count=1 or -race is not supported here: use go test directly).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export CGO_ENABLED="${CGO_ENABLED:-0}"

cpus="$(nproc 2>/dev/null || echo 2)"
shards="${GEXTTO_TEST_SHARDS:-$(( cpus < 4 ? cpus : 4 ))}"
(( shards >= 1 )) || shards=1

work="$(mktemp -d "${TMPDIR:-/tmp}/gextto-gotest.XXXXXX")"
pids=()
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() {
    for pid in "${pids[@]:-}"; do
        [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
    done
    rm -rf "$work"
}
trap cleanup EXIT

module="$(go list -m)"
others=()
while IFS= read -r pkg; do
    [[ "$pkg" == "$module" ]] || others+=("$pkg")
done < <(go list ./...)

# The other packages: one ordinary go test run in the background.
go test "$@" "${others[@]}" >"$work/others.log" 2>&1 &
pids+=("$!")

# The root package: build the test binary once, list its tests, deal them out
# round-robin (neighbouring tests are often slow together) and run the shards.
go test -c -o "$work/root.test" . >"$work/build.log" 2>&1 || { cat "$work/build.log"; exit 1; }
mapfile -t tests < <("$work/root.test" -test.list '.*' | grep -E '^(Test|Example|Fuzz)')
declare -a patterns
for index in "${!tests[@]}"; do
    shard=$(( index % shards ))
    patterns[shard]+="${patterns[shard]:+|}${tests[$index]}"
done
for shard in $(seq 0 $(( shards - 1 ))); do
    [[ -n "${patterns[$shard]:-}" ]] || continue
    # The test binary runs in the package directory, as go test does.
    "$work/root.test" -test.run "^(${patterns[$shard]})\$" -test.timeout 10m >"$work/root-$shard.log" 2>&1 &
    pids+=("$!")
done

status=0
for pid in "${pids[@]}"; do
    wait "$pid" || status=1
done
pids=()

cat "$work/others.log"
for shard in $(seq 0 $(( shards - 1 ))); do
    log="$work/root-$shard.log"
    [[ -f "$log" ]] || continue
    if grep -q '^PASS$' "$log" && ! grep -q -- '--- FAIL' "$log"; then
        printf 'ok  \t%s\t(shard %d/%d)\n' "$module" "$(( shard + 1 ))" "$shards"
    else
        printf 'FAIL\t%s\t(shard %d/%d)\n' "$module" "$(( shard + 1 ))" "$shards"
        cat "$log"
        status=1
    fi
done
exit "$status"
