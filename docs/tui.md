# Gextto terminal UI (TUI)

Interfaccia a terminale per un daemon Gextto già in esecuzione. È bilingue
(italiano/inglese), offre filtro e ordinamento dei download, dettagli, log SSE,
metriche live e notifiche non invasive; non legge mai direttamente i database.

> [!NOTE]
> La TUI usa l'API HTTP del daemon. Il daemon deve quindi essere avviato e
> raggiungibile dall'URL indicato prima di aprire la TUI.

## Avvio

```bash
/opt/gextto/gexttod tui                    # installazione standard
/opt/gextto/gexttod tui --url http://host:5000 --lang en
GEXTTO_URL=http://host:5000 GEXTTO_LANG=it /opt/gextto/gexttod tui
```

Con un'installazione diversa da quella standard, sostituisci `/opt/gextto` con
la directory che contiene `gexttod`.

La lingua viene scelta in quest'ordine: `--lang`, `GEXTTO_LANG`, la lingua
attiva del daemon (`/api/i18n/active`), altrimenti italiano.

## Schede disponibili

1. **Stato** — modalità (dry-run/attiva), torrent e download HTTP attivi,
   prossimo ciclo, ultimo ciclo, elementi visti nei feed, velocità aggregate,
   CPU/RAM e andamento del trasferimento.
2. **Download** — elenco unificato torrent + download HTTP dei fumetti con stato,
   progresso, byte, velocità e nome; filtro (`F`), ordinamento (`o`/`O`), dettagli
   torrent (`Invio`), pausa/ripresa e rimozione dei download HTTP.
3. **Log** — ultime righe, filtro (`/`), segui (`f`), stream SSE live.
4. **Salute** — CPU/RAM/disco, percorsi, dischi, RAM disk, ultimi errori.
5. **Archivio** — archivio delle release, filtro e accodamento diretto.
6. **Mancanti** — episodi mancanti della libreria monitorata.
7. **Blocklist** — release bloccate, con rimozione interattiva.
8. **Libreria** — viste compatte per Serie TV, Film e Fumetti monitorati, con
   stato attivo, metadati essenziali e filtro testuale.

Completamenti, errori, stalli e archiviazioni arrivano come messaggio temporaneo
nella barra inferiore. La TUI mostra anche gli stessi cambi di stato per i
download HTTP dei fumetti.

## Scorciatoie da tastiera

| Ambito | Tasti |
| --- | --- |
| Globali | `1`-`8`/`Tab` schede · `r` aggiorna · `?` aiuto · `q` esci |
| Aggiunta | `a` magnet/URL · `t` file `.torrent` · `c` ciclo · `s` cerca · `e` eventi |
| Download | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `Invio` dettagli torrent · `p` pausa/riprendi · `d`/`D` rimuovi (torrent con o senza file; HTTP dalla lista) · `X` pulisci completati · `k` verifica · `R` riannuncia · `n` senza-rinomina · `i`/`u` pin/unpin · `L` limiti · `o`/`O` ordina · `F` filtro |
| Dettagli | `1` generale · `2` tracker · `3` file · `4` peer · `↑↓` scorri · `Esc`/`Invio` indietro |
| Log | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `/` filtro · `f` segui/ferma |
| Salute | `x` svuota cestino (con conferma) |
| Archivio | `↑↓` seleziona · `Invio` accoda · `/` filtro |
| Mancanti | `↑↓` seleziona · `r` aggiorna |
| Blocklist | `↑↓` seleziona · `d` rimuovi |
| Libreria | `1` Serie TV · `2` Film · `3` Fumetti · `↑↓` seleziona · `s` filtra |

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
