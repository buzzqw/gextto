#!/usr/bin/env bash
# Check the curated "Cerca impostazioni" index used by the server-rendered UI.
# The index must contain unique keys, known tabs and non-empty labels.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_SRC="$ROOT/uiweb_settings.go"
[[ -f "$GO_SRC" ]] || { echo "server UI settings source not found: $GO_SRC" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }

python3 - "$GO_SRC" <<'PY'
import re, sys

go_src = open(sys.argv[1]).read()
tabs = set(re.findall(r'\{ID:\s*"([a-z0-9_]+)"', go_src))
# The optional Group names the panel inside the tab; the curated index must
# always set it, the score weights (uiScoreSettingDefs) are grouped by prefix.
raw_entries = re.findall(
    r'\{Key:\s*"([a-z0-9_.-]+)",\s*Label:\s*"([^"]+)",\s*Tab:\s*"([a-z0-9_]+)"(?:,\s*Group:\s*"([^"]*)")?\}',
    go_src,
)
entries = [(key, label, tab) for key, label, tab, _ in raw_entries]
ungrouped = sorted(key for key, _, tab, group in raw_entries if tab != "scores" and not group)
if not entries:
    print("uiSettingsIndex not found or empty", file=sys.stderr)
    sys.exit(1)

keys = [key for key, _, _ in entries]
duplicates = sorted({key for key in keys if keys.count(key) > 1})
bad_tabs = sorted({tab for _, _, tab in entries if tab not in tabs})
if duplicates or bad_tabs or ungrouped:
    if duplicates:
        print("duplicate settings keys:", ", ".join(duplicates), file=sys.stderr)
    if bad_tabs:
        print("unknown settings tabs:", ", ".join(bad_tabs), file=sys.stderr)
    if ungrouped:
        print("settings without a panel group:", ", ".join(ungrouped), file=sys.stderr)
    sys.exit(1)

print(f"OK: {len(entries)} settings indexed across {len(tabs)} tabs")
PY
