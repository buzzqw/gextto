#!/usr/bin/env bash
# Lint the maintenance scripts so a scripting mistake fails the local test
# suite, not only a release or CI job. `bash -n` catches syntax errors; with
# ShellCheck installed it also catches SC2154 (a variable referenced under
# `set -u` but never assigned), the class of bug that once broke the release
# packaging only in CI. ShellCheck is not required: without it the syntax is
# still checked.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

scripts=()
while IFS= read -r path; do
  scripts+=("$path")
done < <(find "$ROOT/scripts" -maxdepth 1 -name '*.sh' -type f | sort)
while IFS= read -r path; do
  scripts+=("$path")
done < <(find "$ROOT" -maxdepth 1 -name '*.sh' -type f | sort)

if [[ ${#scripts[@]} -eq 0 ]]; then
  echo "check-scripts: no shell script found" >&2
  exit 1
fi

fail=0
for script in "${scripts[@]}"; do
  if ! bash -n "$script"; then
    echo "check-scripts: syntax error in ${script#"$ROOT"/}" >&2
    fail=1
  fi
done

if command -v shellcheck >/dev/null 2>&1; then
  if ! shellcheck -S warning -x "${scripts[@]}"; then
    fail=1
  fi
else
  echo "check-scripts: shellcheck not installed, syntax checked only"
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "check-scripts: ${#scripts[@]} scripts ok"
