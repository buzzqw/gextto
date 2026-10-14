SHELL := /bin/bash
BINARY := gexttod
CMD := ./cmd/gexttod
OUT := bin/$(BINARY)

.PHONY: all build fast gx-torrent test test-race test-engine test-dht test-ui fuzz-smoke container vet fmt check-ui installer-test tidy package clean run measure-seeding measure-pipeline measure-realswarm measure-choking measure-dht measure-merge3 cache-refresh

all: build

# Default build: pure Go. gx-torrent (the engine) is a pure-Go daemon.
build:
	GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

fast:
	scripts/build-daemon.sh

# Only the gx-torrent daemon (pure Go). Bumps the daemon's
# own build number, independent from Gextto's.
gx-torrent:
	GX_BUILD="$$(GEXTTO_BUMP_BUILD=1 scripts/next-gx-build-number.sh)"; \
	CGO_ENABLED=0 go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=$$GX_BUILD -X github.com/buzzqw/gextto/internal/constants.Commit=$$(git rev-parse HEAD 2>/dev/null) -X github.com/buzzqw/gextto/internal/constants.BuiltAt=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-o bin/gx-torrent ./cmd/gx-torrent; \
	printf 'built bin/gx-torrent (build %s)\n' "$$GX_BUILD"

# Build the standalone container image (needs docker). The daemon's build
# number is taken from gx-torrent.build_number when it exists.
container:
	docker build --build-arg GX_BUILD="$$(cat gx-torrent.build_number 2>/dev/null || echo 0)" -t gx-torrent .

test: check-ui installer-test test-dht
	CGO_ENABLED=0 go test ./...

# Tests of the gx-core engine. It lives in internal/gxcore as part of the main
# module, so the packages resolve through the main go.sum.
test-engine:
	go test \
		github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth \
		github.com/buzzqw/gextto/internal/gxcore/internal/bitfield \
		github.com/buzzqw/gextto/internal/gxcore/internal/blocklist \
		github.com/buzzqw/gextto/internal/gxcore/internal/mse \
		github.com/buzzqw/gextto/internal/gxcore/internal/peerconn \
		github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerwriter \
		github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol \
		github.com/buzzqw/gextto/internal/gxcore/internal/piececache \
		github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker \
		github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage \
		github.com/buzzqw/gextto/internal/gxcore/internal/unchoker \
		github.com/buzzqw/gextto/internal/gxcore/torrent

# Tests of the DHT library (internal/dht is a separate Go module,
# so it is not covered by the root `go test ./...`).
test-dht:
	cd internal/dht && CGO_ENABLED=0 go test ./...

# Short fuzzing pass over the network-facing parsers. `make test` runs only the
# seed corpus; this actually fuzzes, so a panic on hostile input surfaces before
# a release (the pass of 2026-10-13 found a .torrent scanner panic in minutes).
# Not part of `make test`; run it by hand or in a nightly job. Override the
# per-target time with FUZZ_TIME=2m. See docs/evoluzione.md §10.
FUZZ_TIME ?= 30s
# Each target runs once and, if it fails, once more. The Go fuzzing engine
# occasionally ends a target with a spurious "context deadline exceeded" when
# -fuzztime elapses on a loaded machine (a shutdown race, not a finding); a real
# crash reproduces on the retry, so the retry does not hide one.
fuzz-smoke:
	@set -e; \
	fuzzrun() { \
		name="$$1"; shift; \
		echo "fuzz $$name"; \
		"$$@" || { echo "retry $$name (transient fuzzer shutdown?)"; "$$@"; }; \
	}; \
	for t in FuzzDecodeBencode FuzzReadResponse FuzzValidBencode FuzzParseNodesString; do \
		(cd internal/dht && fuzzrun "dht/$$t" go test -run '^$$' -fuzz "^$$t$$" -fuzztime $(FUZZ_TIME) .); \
	done; \
	for spec in \
	  "internal/utils:FuzzTorrentBytes FuzzMagnetParsing" \
	  "internal/rss:FuzzParse" \
	  "internal/torznab:FuzzParse" \
	  "internal/gxcore/internal/metainfo:FuzzMetaInfoNew FuzzNewV2Info FuzzParseFileTree FuzzParseInfo" \
	  "internal/gxcore/internal/magnet:FuzzNew" \
	  "internal/gxcore/internal/peerprotocol:FuzzExtensionMessageUnmarshal FuzzHolepunchUnmarshal FuzzHashRequestHeader" \
	  "internal/gxcore/internal/tracker:FuzzDecodePeersCompact" \
	  "internal/gxcore/internal/tracker/httptracker:FuzzParsePeersDictionary" \
	  "internal/gxcore/internal/tracker/wstracker:FuzzWSReadMessage" \
	  "internal/gxcore/internal/btconn:FuzzReadHandshake" \
	  "internal/gxcore/internal/mse:FuzzHandshakeIncoming" \
	  "internal/utp/transport:FuzzHeaderUnmarshal"; do \
		dir="$${spec%%:*}"; names="$${spec##*:}"; \
		for t in $$names; do \
			fuzzrun "$$dir/$$t" go test -run '^$$' -fuzz "^$$t$$" -fuzztime $(FUZZ_TIME) ./$$dir/; \
		done; \
	done; \
	fuzzrun ". / FuzzParseRelease" go test -run '^$$' -fuzz '^FuzzParseRelease$$' -fuzztime $(FUZZ_TIME) .

# Race detector run (technical review, phase 0). Kept separate from `test` so it
# can stay non-blocking until the shared state has been audited.
test-race:
	CGO_ENABLED=1 go test -race ./...

# End-to-end UI tests (Playwright + Node, in uiweb/end2end). They build the
# daemon, start it in dry-run on their own port and drive the real UI in a
# browser; they cover the pages, the mobile layout, the i18n and the axe
# accessibility checks. Kept out of `make test` because they need Node and a
# browser. Run `npm ci` automatically when node_modules is missing.
test-ui:
	scripts/build-daemon.sh
	cd uiweb/end2end && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npx playwright test

# Seeding/choking measurements. The deterministic unchoker harness
# runs with `make test-engine`; this runs the opt-in local-swarm harness. Add
# GX_MEASURE_SUPERSEED=1 to compare against super-seeding, GX_MEASURE_LEECHERS=N
# for the swarm size. See docs/evoluzione.md (section 6).
measure-seeding:
	GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureSeeding -v -count=1

# Pipelining (adaptive request queue) harness: runs the local swarm at a shallow
# and at deeper queue depths. On loopback the RTT is ~0, so the depth barely
# matters: it is a regression/sanity harness, not a WAN demonstration. Add
# GX_MEASURE_LEECHERS=N. See docs/evoluzione.md (section 6).
measure-pipeline:
	GX_MEASURE_PIPELINE=1 go test ./cmd/gx-torrent/ -run MeasurePipelining -v -count=1

# Real-swarm uTP/RTO validation (opt-in, outside CI): downloads a public torrent
# with the DHT, the trackers and uTP enabled, sampling the uTP socket counters
# while peers connect (healthy control paths must keep the resend counters low).
# GX_MEASURE_REAL=<torrent file> is required; GX_MEASURE_REAL_SECONDS sets the
# window (default 120). See docs/evoluzione.md (section 6).
measure-realswarm:
	GX_MEASURE_REAL=$${GX_MEASURE_REAL:?set GX_MEASURE_REAL=<public torrent file>} \
	  GX_MEASURE_REAL_SECONDS=$${GX_MEASURE_REAL_SECONDS:-120} \
	  go test ./cmd/gx-torrent/ -run MeasureRealSwarm -v -count=1 -timeout 20m

# Choking/seeding campaign (opt-in, outside CI): the controlled half serves a
# real libtorrent leecher on loopback (needs python3-libtorrent); the real-swarm
# half seeds a public torrent and needs a reachable inbound port.
measure-choking:
	GX_MEASURE_CHOKING_LT=1 go test ./cmd/gx-torrent/ -run MeasureChokingLibtorrent -v -count=1 -timeout 10m

# Swarm merge Phase 3 on real data (opt-in, outside CI): public ISOs fetched
# over BitTorrent and two v2 torrents of each with different piece lengths
# (scripts/phase3-env.sh), then the bytes the destination takes from the
# network. GX_PHASE3_DIR sets the folder, GX_PHASE3_SET picks debian or arch.
# See docs/evoluzione.md (section 10.1).
measure-merge3:
	scripts/phase3-env.sh $${GX_PHASE3_DIR:-$$HOME/gx-phase3}
	GX_MEASURE_MERGE3=$${GX_PHASE3_DIR:-$$HOME/gx-phase3}/$${GX_PHASE3_SET:-debian} go test ./cmd/gx-torrent/ -run MeasureMergePhase3 -v -count=1 -timeout 40m

# DHT real-swarm measurement (opt-in, outside CI): routing-table fill and query
# success over time, the numbers behind the anti-Sybil/multi-path decisions.
# GX_MEASURE_DHT=5m sets the duration; GX_MEASURE_DHT_INTERVAL=10s the sampling.
measure-dht:
	cd internal/dht && GX_MEASURE_DHT=$${GX_MEASURE_DHT:-3m} go test -run MeasureDHT -v -count=1 -timeout 30m

vet:
	CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -w .

# Validate the server-rendered UI settings index.
check-ui:
	scripts/check-ui-settings-index.sh

# Side-effect-free checks for the bash installers (also run by `make test`).
installer-test:
	scripts/installer-selftest.sh

# Keep the Go build cache from growing without bound: when it passes the
# threshold (5 GiB by default) wipe it and rebuild + test so a clean, warm cache
# is left behind. Install a weekly systemd user timer with
# `scripts/refresh-build-cache.sh --install`. See docs/DEVELOPERS.md.
cache-refresh:
	scripts/refresh-build-cache.sh

tidy:
	go mod tidy

package:
	scripts/package-linux.sh --binary $(OUT)

run: fast
	GEXTTO_DATA_DIR="$$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 $(OUT)

clean:
	rm -rf bin
