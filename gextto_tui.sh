#!/usr/bin/env bash
#
# Lancia la TUI di Gextto (interfaccia a terminale) verso un daemon già avviato.
# Pensata per una shell remota (SSH): non richiede display grafico.
#
# Uso:
#   ./gextto_tui.sh                          # http://127.0.0.1:5000
#   ./gextto_tui.sh --url http://host:5000
#   ./gextto_tui.sh --lang en
#
# Variabili d'ambiente (se non passi --url):
#   GEXTTO_URL        URL del daemon (default http://127.0.0.1:5000)
#   GEXTTO_BINARY     percorso alternativo al binario gexttod
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BIN="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"

if [[ ! -x "$BIN" ]]; then
	echo "gexttod non trovato o non eseguibile: $BIN" >&2
	echo "Compilalo con 'make fast' (o 'make build') oppure imposta GEXTTO_BINARY." >&2
	exit 1
fi

if [[ ! -t 0 || ! -t 1 ]]; then
	echo "Attenzione: la TUI richiede un terminale interattivo (avviala da una shell/SSH)." >&2
fi

exec "$BIN" tui "$@"
