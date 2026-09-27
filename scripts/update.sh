#!/usr/bin/env bash
# Update gextto and restart the service.
#
# Two update paths are supported, mirroring `gexttod --update`:
#
#   scripts/update.sh                 # rebuild from this checkout and restart
#   scripts/update.sh --release       # install the published payload
#   scripts/update.sh --channel stable
#   scripts/update.sh --archive FILE --install-dir DIR
#
# Options:
#   --release              download the latest payload instead of building
#   --channel NAME         continuous (default) or stable
#   --release <tag>        install a specific release tag (with --release)
#   --repo OWNER/NAME      GitHub repository used to download
#   --install-dir DIR      destination of the payload
#   --archive FILE         install from a local archive (offline)
#   --force                reinstall even if the version is unchanged
#   --no-restart           do not restart the service afterwards
#   -h, --help             show this help
#
# Data and configuration in GEXTTO_DATA_DIR are never touched.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"
MODE="build"
RESTART=1
UPDATE_ARGS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --release)
            # `--release` alone selects the published payload; `--release <tag>`
            # is only valid together with the downloaded payload, so accept a
            # following non-flag value as the tag.
            MODE="release"
            if [[ $# -ge 2 && "$2" != -* ]]; then
                UPDATE_ARGS+=(--release "$2")
                shift
            fi
            ;;
        --channel | --repo | --install-dir | --archive)
            [[ $# -ge 2 ]] || { echo "$1 requires a value" >&2; exit 2; }
            MODE="release"
            UPDATE_ARGS+=("$1" "$2")
            shift
            ;;
        --force)
            MODE="release"
            UPDATE_ARGS+=(--force)
            ;;
        --no-restart)
            RESTART=0
            ;;
        -h | --help)
            # Print the leading comment block (after the shebang) verbatim.
            awk 'NR==1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"
            exit 0
            ;;
        *)
            echo "unknown option: $1" >&2
            exit 2
            ;;
    esac
    shift
done

if [[ "$MODE" == "build" ]]; then
    echo "==> building from $ROOT"
    ( cd "$ROOT" && make build )
else
    [[ -x "$BINARY" ]] || { echo "gexttod not found at $BINARY" >&2; exit 1; }
    echo "==> installing published payload"
    "$BINARY" --update --no-restart ${UPDATE_ARGS[@]+"${UPDATE_ARGS[@]}"}
fi

if [[ "$RESTART" == "1" ]]; then
    if [[ -x "$ROOT/scripts/gextto-restart" ]]; then
        echo "==> restarting gextto"
        "$ROOT/scripts/gextto-restart" restart
    elif [[ -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/gextto.service" ]]; then
        echo "==> restarting user service"
        systemctl --no-block --user restart gextto.service
    else
        echo "==> restart the service manually:"
        echo "    sudo systemctl restart gextto.service"
    fi
fi

echo "==> done"
