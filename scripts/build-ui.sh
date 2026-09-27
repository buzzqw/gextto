#!/usr/bin/env bash
# Build the Leptos web UI and refresh the bundle embedded by the daemon.
#
# The daemon serves its own index.html and embeds webui/pkg; this script only
# regenerates the compiled assets (ui.js, ui.wasm, ui.css).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
UI="$ROOT/ui"
[[ -d "$UI" ]] || { echo "ui/ source not found" >&2; exit 1; }

if ! command -v cargo-leptos >/dev/null; then
    echo "cargo-leptos is required: cargo install cargo-leptos --locked" >&2
    exit 1
fi
rustup target add wasm32-unknown-unknown >/dev/null 2>&1 || true

( cd "$UI" && cargo leptos build --release --frontend-only )

SITE="$UI/target/site/pkg"
for asset in ui.js ui.wasm ui.css; do
    [[ -f "$SITE/$asset" ]] || { echo "build did not produce $SITE/$asset" >&2; exit 1; }
done

mkdir -p "$ROOT/webui/pkg"
cp -a "$SITE/." "$ROOT/webui/pkg/"
echo "web UI updated in webui/pkg"
