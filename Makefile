SHELL := /bin/bash
BINARY := gexttod
CMD := ./cmd/gexttod
OUT := bin/$(BINARY)

.PHONY: all build fast test vet fmt tidy package clean run

all: build

build:
	GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

fast:
	scripts/build-daemon.sh

test:
	CGO_ENABLED=1 go test ./...

test-real:
	CGO_ENABLED=1 go test -run 'LibtorrentLocalTransfer|LibtorrentMagnetTransfer' -v -timeout 300s ./...

vet:
	CGO_ENABLED=1 go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

package:
	scripts/package-linux.sh --binary $(OUT)

run: fast
	GEXTTO_DATA_DIR="$$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 $(OUT)

clean:
	rm -rf bin
