#!/usr/bin/env bash
# End-to-end UI tests. On a developer machine the spec files are split into
# independent groups, each with its own daemon, data directory and ports, and
# run concurrently: the suite used to be serial because the specs shared one
# dry-run data directory (and therefore the selected language).
#
# Set GEXTTO_TEST_UI_SERIAL=1 (or run in CI) to run the old single process.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

scripts/build-daemon.sh

if [[ ! -d "$ROOT/uiweb/end2end/node_modules" ]]; then
    (cd "$ROOT/uiweb/end2end" && npm ci --no-audit --no-fund)
fi

if [[ "${CI:-}" == "true" || "${GEXTTO_TEST_UI_SERIAL:-0}" == "1" ]]; then
    cd "$ROOT/uiweb/end2end"
    exec npx playwright test
fi

run_dir="$(mktemp -d "${TMPDIR:-/tmp}/gextto-test-ui.XXXXXX")"
cleanup() {
    # Stop the groups when the run is interrupted, then the managed gx-torrent:
    # it survives its gexttod by design (it is a systemd scope on the real
    # service) and a test run must not leave one listening for fifteen minutes.
    for pid in "${pids[@]:-}"; do
        [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
    done
    pkill -f "$run_dir" 2>/dev/null || true
    rm -rf "$run_dir"
}
trap cleanup EXIT

# name|spec files|UI port|engine port|gx-torrent port
groups=(
    "settings|tests/settings.spec.ts tests/i18n.spec.ts tests/example.spec.ts tests/security.spec.ts tests/jobs.spec.ts|15003|18892|18893"
    "pages|tests/accessibility.spec.ts tests/pages.spec.ts|15013|18902|18903"
)

pids=()
names=()
for group in "${groups[@]}"; do
    IFS='|' read -r name specs port engine gxtorrent <<< "$group"
    (
        cd "$ROOT/uiweb/end2end"
        # shellcheck disable=SC2086 # the spec list must expand to separate arguments
        GEXTTO_TEST_PORT="$port" \
        GEXTTO_TEST_ENGINE_PORT="$engine" \
        GEXTTO_TEST_GXTORRENT_PORT="$gxtorrent" \
        GEXTTO_TEST_DATA_DIR="$run_dir/data-$name" \
        GEXTTO_TEST_OUTPUT_DIR="$run_dir/results-$name" \
        GEXTTO_TEST_HTML_DIR="$run_dir/report-$name" \
        GEXTTO_TEST_REUSE=0 \
            npx playwright test $specs --reporter=line
    ) >"$run_dir/$name.log" 2>&1 &
    pids+=("$!")
    names+=("$name")
    printf 'UI group %s started on port %s\n' "$name" "$port"
done

status=0
for index in "${!pids[@]}"; do
    if wait "${pids[$index]}"; then
        printf 'UI group %s passed\n' "${names[$index]}"
        tail -n 2 "$run_dir/${names[$index]}.log"
    else
        printf 'UI group %s FAILED\n' "${names[$index]}"
        status=1
    fi
done

if [[ "$status" -ne 0 ]]; then
    for log in "$run_dir"/*.log; do
        printf '\n=== %s ===\n' "$(basename "$log")"
        cat "$log"
    done
    exit 1
fi
