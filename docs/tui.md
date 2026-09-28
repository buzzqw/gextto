# Gextto TUI

Interfaccia a terminale per un daemon Gextto in esecuzione. Replica la vecchia
TUI Python di Rextto e la supera: bilingue (italiano/inglese), filtro e
ordinamento dei torrent, dettagli con sotto-viste, stream dei log via SSE e
nessuna dipendenza oltre a `golang.org/x/sys`.

## Avvio

```bash
gexttod tui                        # daemon su http://127.0.0.1:5000
gexttod tui --url http://host:5000 --token <token> --lang en
GEXTTO_URL=http://host:5000 GEXTTO_API_TOKEN=<token> GEXTTO_LANG=it gexttod tui
```

La lingua viene scelta in quest'ordine: `--lang`, `GEXTTO_LANG`, la lingua
attiva del daemon (`/api/i18n/active`), altrimenti italiano.

## Schede

1. **Stato** — modalità (dry-run/attiva), torrent, prossimo ciclo, ultimo ciclo,
   elementi visti nei feed.
2. **Torrent** — elenco con hash, stato, progresso, scaricato, velocità, nome;
   filtro (`F`), ordinamento (`o`/`O`), dettagli (`Invio`).
3. **Log** — ultime righe, filtro (`/`), segui (`f`), stream SSE live.
4. **Salute** — CPU/RAM/disco, percorsi, dischi, RAM disk, ultimi errori.
5. **Archivio** — archivio delle release, filtro e accodamento diretto.
6. **Mancanti** — episodi mancanti della libreria monitorata.
7. **Blocklist** — release bloccate, con rimozione interattiva.

## Tasti

| Ambito | Tasti |
| --- | --- |
| Globali | `1`-`7`/`Tab` schede · `r` aggiorna · `?` aiuto · `q` esci |
| Aggiunta | `a` magnet/URL · `t` file `.torrent` · `c` ciclo · `s` cerca · `e` eventi |
| Torrent | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `Invio` dettagli · `p` pausa/riprendi · `b` riavvia · `d`/`D` rimuovi (con o senza file) · `X` pulisci completati · `k` verifica · `R` riannuncia · `n` senza-rinomina · `i`/`u` pin/unpin · `L` limiti · `o`/`O` ordina · `F` filtro |
| Dettagli | `1` generale · `2` tracker · `3` file · `4` peer · `↑↓` scorri · `Esc`/`Invio` indietro |
| Log | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `/` filtro · `f` segui/ferma |
| Salute | `x` svuota cestino (con conferma) |
| Archivio | `↑↓` seleziona · `Invio` accoda · `/` filtro |
| Mancanti | `↑↓` seleziona · `r` aggiorna |
| Blocklist | `↑↓` seleziona · `d` rimuovi |

## Architettura

`internal/tui`:

- `i18n.go` — catalogo bilingue IT/EN, `Translator`, risoluzione della lingua.
- `client.go` — client HTTP tipizzato di tutte le API del daemon.
- `format.go` — formattazione di byte, durate, numeri opzionali.
- `model.go` / `update.go` / `render.go` — stato puro, transizioni da tasti e
  rendering in righe con stile semantico (testabili senza terminale).
- `keys.go` — decodifica del flusso di byte in tasti (sequenze ANSI, UTF-8).
- `term.go` — raw mode e schermo alternato (`golang.org/x/sys/unix`).
- `run.go` — ciclo principale: polling, stream SSE, esecuzione azioni.

La TUI parla **solo** con l'API HTTP del daemon e non tocca mai i database.

Nei prompt di testo sono disponibili anche `←→`, `Home`/`End`, `Delete`,
`Ctrl-A`/`Ctrl-E` (inizio/fine), `Ctrl-U` (svuota), `Ctrl-K` (cancella fino
alla fine) e `Ctrl-W` (cancella la parola precedente).

## Test

```bash
go test ./internal/tui/...
go test -race ./internal/tui/...
```

Coprono il catalogo i18n, la formattazione, il client (con un server HTTP di
test), la decodifica dei tasti e tutte le transizioni del modello nei due
linguaggi.
