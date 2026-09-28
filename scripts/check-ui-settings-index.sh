#!/usr/bin/env bash
# Ensures the "Cerca impostazioni" index (SETTINGS_INDEX in ui/app/src/lib.rs)
# stays in sync with the keys the UI actually exposes:
#   1. every setting_key rendered in the classic UI must be indexed;
#   2. every indexed key must exist in the server-rendered settings index
#      (uiweb_settings.go) or in the structured-settings map of uiweb_pages.go,
#      and vice versa, so new/changed fields cannot drift apart.
# Cheap grep/python check: no Rust toolchain needed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/ui/app/src/lib.rs"
GO_SRC="$ROOT/uiweb_settings.go"
GO_PAGES="$ROOT/uiweb_pages.go"
[[ -f "$SRC" ]] || { echo "ui source not found: $SRC" >&2; exit 1; }
[[ -f "$GO_SRC" ]] || { echo "server UI settings source not found: $GO_SRC" >&2; exit 1; }
[[ -f "$GO_PAGES" ]] || { echo "server UI pages source not found: $GO_PAGES" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }

python3 - "$SRC" "$GO_SRC" "$GO_PAGES" <<'PY'
import re, sys

src = open(sys.argv[1]).read()
go_src = open(sys.argv[2]).read()
go_pages = open(sys.argv[3]).read()

# Keys advertised by the classic UI search box.
rendered = sorted(set(re.findall(r'setting_key="([a-z0-9_]+)"', src)))

block = re.search(r'const SETTINGS_INDEX:.*?= &\[(.*?)\n\];', src, re.S)
if not block:
    print("SETTINGS_INDEX not found in ui/app/src/lib.rs", file=sys.stderr)
    sys.exit(1)
tuples = re.findall(r'\(\s*"([^"]*)"\s*,\s*"([^"]*)"\s*,\s*"([^"]*)"\s*\)', block.group(1))
# Search entries use a few display ids (e.g. "rename-format"); normalize them to
# the real setting key so the index is compared on equal footing.
indexed = {key.replace("-", "_") for (_, _, key) in tuples if key}

missing = [key for key in rendered if key not in indexed]
if missing:
    print("setting_key rendered in the UI but missing from SETTINGS_INDEX:")
    for key in missing:
        print("  -", key)
    sys.exit(1)

# Keys exposed by the server-rendered settings page: plain rows (which must all
# be indexed) plus structured settings edited with dedicated forms (intentionally
# not part of the plain search index).
plain_go = set(re.findall(r'\{Key:\s*"([a-z0-9_]+)"', go_src))
structured_go = set()
structured = re.search(r'structured := map\[string\]struct\{\}\{(.*?)\n\t\}', go_pages, re.S)
if structured:
    structured_go = set(re.findall(r'"([a-z0-9_]+)"', structured.group(1)))
implemented_go = plain_go | structured_go

unimplemented = sorted(indexed - implemented_go)
unindexed = sorted(plain_go - indexed)
if unimplemented or unindexed:
    if unimplemented:
        print("settings in SETTINGS_INDEX but missing from the server UI (no row and no structured editor):")
        for key in unimplemented:
            print("  -", key)
    if unindexed:
        print("settings rendered by the server UI but missing from SETTINGS_INDEX:")
        for key in unindexed:
            print("  -", key)
    sys.exit(1)

print(f"OK: {len(rendered)} setting_key covered by SETTINGS_INDEX and server UI index is synchronized")
PY
