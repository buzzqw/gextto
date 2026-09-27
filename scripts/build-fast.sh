#!/usr/bin/env bash
# Fast development build (no strip, incremental).
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p bin
CGO_ENABLED=1 go build -o bin/gexttod ./cmd/gexttod
echo "built bin/gexttod"
