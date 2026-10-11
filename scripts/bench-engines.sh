#!/usr/bin/env bash
# bench-engines.sh — reproducible RAM/throughput probe for gx-core (the
# gx-torrent engine). Run it by hand when a performance decision needs numbers;
# it is not part of CI.
#
# It reports:
#   1. loopback swarm throughput (the existing measure harness) and the peak RSS
#      of the run;
#   2. the disk write throughput of a target directory (point --dir at an NFS
#      mount to compare), which is what dominates piece writing on a NAS.
#
# Usage:
#   scripts/bench-engines.sh [--size MiB] [--dir DIR] [--leechers N]
#
#   --size      size of the disk probe file in MiB (default 256)
#   --dir       directory to probe (default $TMPDIR or /tmp)
#   --leechers  swarm size of the loopback measurement (default 3)

set -euo pipefail

size_mib=256
dir="${TMPDIR:-/tmp}"
leechers="${GX_MEASURE_LEECHERS:-3}"

while [ $# -gt 0 ]; do
  case "$1" in
    --size) size_mib="$2"; shift 2 ;;
    --dir) dir="$2"; shift 2 ;;
    --leechers) leechers="$2"; shift 2 ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

cd "$(dirname "$0")/.."

echo "gx-core benchmark — $(date -u +%FT%TZ)"
echo "host: $(uname -srm) · cpu: $(nproc) cores"
echo "disk target: $dir"
echo

echo "== 1/2 · loopback swarm throughput (gx-torrent) =="
# /usr/bin/time -v wraps the whole go test process, which runs the seeding
# daemons in-process: its peak RSS is a proxy for the engine's.
if command -v /usr/bin/time >/dev/null 2>&1; then
  GX_MEASURE=1 GX_MEASURE_LEECHERS="$leechers" /usr/bin/time -v go test ./cmd/gx-torrent/ -run MeasureSeeding -v -count=1 2>&1 \
    | grep -E "uploaded=|Maximum resident set size|Elapsed \(wall" || true
else
  GX_MEASURE=1 GX_MEASURE_LEECHERS="$leechers" go test ./cmd/gx-torrent/ -run MeasureSeeding -v -count=1
fi
echo

echo "== 2/2 · disk write throughput ($size_mib MiB, 1 MiB blocks, fsync) =="
probe="$dir/gextto-bench-$$.bin"
trap 'rm -f "$probe"' EXIT
start=$(date +%s%N)
dd if=/dev/zero of="$probe" bs=1M count="$size_mib" conv=fsync status=none
end=$(date +%s%N)
ms=$(( (end - start) / 1000000 ))
[ "$ms" -gt 0 ] || ms=1
echo "wrote ${size_mib} MiB in ${ms} ms (~$(( size_mib * 1000 / ms )) MiB/s)"
rm -f "$probe"
trap - EXIT
