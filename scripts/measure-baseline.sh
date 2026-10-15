#!/usr/bin/env bash
# measure-baseline.sh — one reproducible baseline of the cheap harnesses of the
# gx-core engine, so a change can be compared against it (docs/evoluzione.md §8, and the numbers go into docs/measurements.md,
# WP0/WP1). Local by default; the network harnesses run only when their
# environment is set, because they need a public torrent or the live DHT.
#
#   scripts/measure-baseline.sh [outfile]
#   GX_MEASURE_REAL=<public.torrent> GX_MEASURE_DHT=3m \
#       GX_MEASURE_DHT_INFOHASH=<40-hex> scripts/measure-baseline.sh out.txt
#
# The output (a Markdown-ish log) is what gets pasted into measurements.md.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
out=${1:-/tmp/gx-baseline-$(date +%Y%m%d-%H%M%S).txt}
exec > >(tee "$out") 2>&1

echo "# gx-torrent baseline"
echo "- date: $(date -Iseconds)"
echo "- commit: $(git rev-parse --short HEAD 2>/dev/null || echo '-')"
echo "- host: $(uname -srm), $(nproc) cpus"
echo

echo "## benchmarks (go test -bench, -benchmem)"
go test -run '^$' -bench . -benchmem ./internal/gxcore/... ./internal/utp/... 2>/dev/null \
	| grep -E '^(goos|goarch|cpu|Benchmark|ok|FAIL|PASS)'
(cd internal/dht && go test -run '^$' -bench . -benchmem . 2>/dev/null \
	| grep -E '^(goos|goarch|cpu|Benchmark|ok|FAIL|PASS)')
echo

echo "## local swarm — measure-seeding"
GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureSeeding -v -count=1 2>&1 \
	| grep -E 'measure_(test|pipeline).*\.go:[0-9]+: ' | sed 's/^ *//'
echo

echo "## pipelining — measure-pipeline"
GX_MEASURE_PIPELINE=1 go test ./cmd/gx-torrent/ -run 'MeasurePipelin' -v -count=1 2>&1 \
	| grep -E 'measure_(test|pipeline).*\.go:[0-9]+: ' | sed 's/^ *//'
echo

if [[ -n "${GX_MEASURE_REAL:-}" ]]; then
	echo "## real swarm — measure-realswarm ($GX_MEASURE_REAL)"
	GX_MEASURE_REAL="$GX_MEASURE_REAL" GX_MEASURE_REAL_SECONDS="${GX_MEASURE_REAL_SECONDS:-120}" \
		go test ./cmd/gx-torrent/ -run MeasureRealSwarm -v -count=1 -timeout 20m 2>&1 \
		| grep -E 'measure_realswarm.*\.go:[0-9]+: ' | sed 's/^ *//'
	echo
fi

if [[ -n "${GX_MEASURE_DHT:-}" ]]; then
	echo "## DHT — measure-dht (${GX_MEASURE_DHT})"
	(cd internal/dht && GX_MEASURE_DHT="$GX_MEASURE_DHT" \
		GX_MEASURE_DHT_INFOHASH="${GX_MEASURE_DHT_INFOHASH:-}" \
		GX_MEASURE_DHT_SUBNET_CAP="${GX_MEASURE_DHT_SUBNET_CAP:-}" \
		go test -run MeasureDHT -v -count=1 -timeout 30m 2>&1 \
		| grep -E 'measure_test.*\.go:[0-9]+: ' | sed 's/^ *//')
	echo
fi

echo "# end"
