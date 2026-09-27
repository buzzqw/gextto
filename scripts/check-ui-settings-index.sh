#!/usr/bin/env bash
# Ensures every rendered setting_key is present in the "Cerca impostazioni"
# index (SETTINGS_INDEX) in ui/app/src/lib.rs, so the search always finds new
# options. Cheap grep/python check: no Rust toolchain needed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/ui/app/src/lib.rs"
[[ -f "$SRC" ]] || { echo "ui source not found: $SRC" >&2; exit 1; }

python3 - "$SRC" <<'PY'
import re, sys

src = open(sys.argv[1]).read()
rendered = sorted(set(re.findall(r'setting_key="([a-z0-9_]+)"', src)))
block = re.search(r'const SETTINGS_INDEX:.*?= &\[(.*?)\n\];', src, re.S)
if not block:
    print("SETTINGS_INDEX not found in ui/app/src/lib.rs", file=sys.stderr)
    sys.exit(1)
tuples = re.findall(r'\(\s*"([^"]*)"\s*,\s*"([^"]*)"\s*,\s*"([^"]*)"\s*\)', block.group(1))
indexed = {key for (_, _, key) in tuples if key}
missing = [key for key in rendered if key not in indexed]
if missing:
    print("setting_key rendered in the UI but missing from SETTINGS_INDEX:")
    for key in missing:
        print("  -", key)
    sys.exit(1)
print(f"OK: {len(rendered)} setting_key covered by SETTINGS_INDEX")
PY
