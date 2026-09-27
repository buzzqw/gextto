SHELL := /bin/bash
BINARY := gexttod
CMD := ./cmd/gexttod
OUT := bin/$(BINARY)

.PHONY: all build fast test test-real test-anacrolix build-anacrolix vet fmt ui check-ui tidy package clean run

all: build

build:
	GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

fast:
	scripts/build-daemon.sh

test:
	CGO_ENABLED=1 go test ./...

test-real:
	CGO_ENABLED=1 go test -run 'LibtorrentLocalTransfer|LibtorrentMagnetTransfer' -v -timeout 300s ./...

# Optional native-Go backend (see docs/aggiunta-anacrolix.md). The default
# build and `make test` do not compile it; MPL-2.0 review gate still applies.
test-anacrolix:
	CGO_ENABLED=1 go test -tags anacrolix -timeout 600s ./...

build-anacrolix:
	GEXTTO_TAGS=anacrolix GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

vet:
	CGO_ENABLED=1 go vet ./...

fmt:
	gofmt -w .

# Regenerate the embedded web UI from ui/ (needs cargo-leptos + wasm target).
ui:
	scripts/build-ui.sh

# Fail when a UI setting is missing from the "Cerca impostazioni" index.
check-ui:
	scripts/check-ui-settings-index.sh

tidy:
	go mod tidy

package:
	scripts/package-linux.sh --binary $(OUT)

run: fast
	GEXTTO_DATA_DIR="$$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 $(OUT)

clean:
	rm -rf bin
