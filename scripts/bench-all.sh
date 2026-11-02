#!/usr/bin/env bash
# bench-all.sh — every Go benchmark of Gextto, gx-torrent and gx-core in one
# reproducible pass. Run it on an idle machine and paste the result into
# docs/measurements.md; it is not part of CI.
#
#   scripts/bench-all.sh                    # on this host
#   scripts/bench-all.sh --docker           # in a pinned Go image, pinned CPUs
#   scripts/bench-all.sh --compare OLD NEW  # benchstat between two raw files
#
# Environment:
#   GX_BENCH_COUNT  runs per benchmark (default 6)
#   GX_BENCH_PROCS  GOMAXPROCS of the run (default 2)
#   GX_BENCH_OUT    output folder (default gextto-data/bench)
#   GX_BENCH_PKGS   package patterns (default ./...)
#   GX_BENCH_DHT    also run the separate internal/dht module (default 1)
#   GX_BENCH_CPUSET CPUs of the container (default: all of them)
#   GX_BENCH_IMAGE  container image (default golang:<Go series>)
#
# The output is two files with the same stamp: the raw `go test` log and its
# benchstat table, plus `latest.txt` copies of both for the next comparison.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

count=${GX_BENCH_COUNT:-6}
procs=${GX_BENCH_PROCS:-2}
outdir=${GX_BENCH_OUT:-gextto-data/bench}
pkgs=${GX_BENCH_PKGS:-./...}
dht=${GX_BENCH_DHT:-1}
mode=host
read -r -a pkgargs <<<"$pkgs"

# benchstat is a go install away and is not always on PATH.
find_benchstat() {
  if command -v benchstat >/dev/null 2>&1; then
    command -v benchstat
    return 0
  fi
  local gopath
  gopath=$(go env GOPATH 2>/dev/null || true)
  if [ -n "$gopath" ] && [ -x "$gopath/bin/benchstat" ]; then
    printf '%s\n' "$gopath/bin/benchstat"
    return 0
  fi
  return 1
}

case "${1:-}" in
  --compare)
    shift
    if [ "$#" -ne 2 ]; then
      echo "usage: $0 --compare OLD NEW" >&2
      exit 2
    fi
    bs=$(find_benchstat) || {
      echo "benchstat is not installed (go install golang.org/x/perf/cmd/benchstat@latest)" >&2
      exit 1
    }
    exec "$bs" "$1" "$2"
    ;;
  --docker) mode=docker ;;
  --host) mode=host ;;
  -h|--help) awk 'NR>1 && /^#/ {sub(/^# ?/, ""); print; next} NR>1 {exit}' "$0"; exit 0 ;;
  "") ;;
  *) echo "unknown argument: $1" >&2; exit 2 ;;
esac

if [ "$mode" = docker ]; then
  if ! command -v docker >/dev/null 2>&1; then
    echo "docker is not installed" >&2
    exit 1
  fi
  case "$outdir" in
    /*) echo "GX_BENCH_OUT must be relative in docker mode (the checkout is the mount)" >&2; exit 2 ;;
  esac
  image=${GX_BENCH_IMAGE:-golang:$(go env GOVERSION | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/')}
  cpuset=${GX_BENCH_CPUSET:-$(seq -s, 0 $(( $(nproc) - 1 )))}
  extra=()
  if bs=$(find_benchstat); then
    extra+=(-v "$bs:/usr/local/bin/benchstat:ro")
  fi
  echo "bench-all in $image on cpus $cpuset (GOMAXPROCS=$procs, count=$count)" >&2
  set +e
  docker run --rm \
    --cpuset-cpus "$cpuset" \
    -e GX_BENCH_COUNT="$count" \
    -e GX_BENCH_PROCS="$procs" \
    -e GX_BENCH_OUT="$outdir" \
    -e GX_BENCH_PKGS="$pkgs" \
    -e GX_BENCH_DHT="$dht" \
    -v "$root:/src" \
    -w /src \
    -v gextto-bench-gomod:/go/pkg/mod \
    -v gextto-bench-gocache:/root/.cache/go-build \
    "${extra[@]}" \
    "$image" bash scripts/bench-all.sh --host
  status=$?
  set -e
  # The container runs as root; give the report back to the caller.
  case "$outdir" in
    /*) report_dir=$outdir ;;
    *) report_dir="$root/$outdir" ;;
  esac
  if [ -d "$report_dir" ]; then
    chown -R "$(id -u):$(id -g)" "$report_dir" 2>/dev/null || true
  fi
  exit "$status"
fi

if [ -n "$(git status --porcelain)" ]; then
  echo "warning: the checkout has uncommitted changes; the stamp names the commit only" >&2
fi
if [ "$(awk '{print int($1)}' /proc/loadavg)" -gt 1 ]; then
  echo "warning: load average is $(cut -d' ' -f1-3 /proc/loadavg); a busy host is not a measurement" >&2
fi

mkdir -p "$outdir"
stamp=$(date +%Y%m%d-%H%M%S)
raw="$outdir/raw-$stamp.txt"
table="$outdir/benchstat-$stamp.txt"
model=$(awk -F: '/model name/ {gsub(/^[ \t]+/, "", $2); print $2; exit}' /proc/cpuinfo)

{
  echo "# bench-all"
  echo "# date $(date -Iseconds)"
  echo "# commit $(git rev-parse --short HEAD 2>/dev/null || echo '-')"
  echo "# host $(uname -srm), $(nproc) cpus, $model"
  echo "# go $(go version | awk '{print $3}'), GOMAXPROCS=$procs, count=$count"
  echo
  GOMAXPROCS="$procs" go test -run '^$' -bench . -benchmem -count "$count" -timeout 60m "${pkgargs[@]}"
  if [ "$dht" = 1 ]; then
    (
      cd internal/dht
      GOMAXPROCS="$procs" go test -run '^$' -bench . -benchmem -count "$count" -timeout 60m .
    )
  fi
} 2>&1 | tee "$raw"

benchstat_bin=$(find_benchstat) || {
  echo "benchstat is not installed: the raw log is here, install it for the table" >&2
  benchstat_bin=""
}
if [ -n "$benchstat_bin" ]; then
  "$benchstat_bin" "$raw" >"$table"
  cp "$table" "$outdir/latest-benchstat.txt"
fi
cp "$raw" "$outdir/latest.txt"
echo
echo "raw:       $raw"
if [ -n "$benchstat_bin" ]; then
  echo "benchstat: $table"
  echo "compare:   scripts/bench-all.sh --compare <old raw> $raw"
else
  echo "benchstat: not produced"
fi
