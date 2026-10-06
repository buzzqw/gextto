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

1. **Stato** — cruscotto: daemon (versione, modalità, uptime, PID, CPU/RAM),
   sistema (CPU, carico, RAM, disco, cestino), ciclo (conto alla rovescia,
   ultimo avvio e contatori), feed, download per stato, traffico con
   andamento, consumi, una riga **Attenzione** con ciò che richiede un
   intervento (torrent in errore, percorsi non scrivibili, disco quasi pieno,
   ultimo errore), i trasferimenti in corso con barra e ETA e, ancorate in
   fondo, le **ultime righe di log**. Se la finestra è bassa la parte alta
   scorre con `↑↓`/`PgUp`/`PgDn`.
2. **Download** — elenco unificato torrent + download HTTP dei fumetti con stato,
   progresso, byte, velocità e nome; filtro (`F`), ordinamento (`o`/`O`), dettagli
   torrent (`Invio`), pausa/ripresa e rimozione dei download HTTP.
   L'intestazione mostra i limiti di banda in vigore (base, programmati o
   temporanei con i minuti rimasti). `T` imposta un limite temporaneo come nella
   web: `DL UL [minuti]` in KiB/s, dove `0` è illimitato e senza minuti (o con
   `0`) il limite resta finché non lo rimuovi; `off` lo rimuove e ripristina i
   limiti normali. La durata massima è 1440 minuti.
3. **Log** — ultime righe, filtro (`/`), segui (`f`), stream SSE live.
4. **Salute** — CPU/RAM/disco, percorsi, dischi, RAM disk, ultimi errori.
5. **Archivio** — archivio delle release, filtro e accodamento diretto.
6. **Mancanti** — episodi mancanti della libreria monitorata: `Invio` apre la
   serie sull'episodio, `s` lo cerca sugli indexer, `i` lo ignora.
7. **Blocklist** — release bloccate, con rimozione interattiva.
8. **Libreria** — Serie TV, Film e Fumetti monitorati. Serie e film si
   gestiscono da qui: aggiunta cercando su TMDB, modifica dei requisiti con un
   modulo a campi, pausa ed eliminazione. Il dettaglio di una serie mostra una
   stagione alla volta con lo stato di ogni episodio (✓ presente, ✗ mancante,
   ↓ in download, `·` in uscita, `-` ignorato); da lì si attivano o
   disattivano le stagioni, si cercano e accodano le release, si ignorano o
   riscaricano episodi, si aggiornano i metadati e si rinominano i file. Il
   dettaglio di un film mostra trama, storico e release già in archivio.
   L'elenco si aggiorna ogni 10 secondi; le azioni ricaricano subito ciò che
   cambiano. I fumetti restano in sola lettura.

Completamenti, errori, stalli e archiviazioni arrivano come messaggio temporaneo
nella barra inferiore. La TUI mostra anche gli stessi cambi di stato per i
download HTTP dei fumetti.

## Scorciatoie da tastiera

| Ambito | Tasti |
| --- | --- |
| Globali | `1`-`8`/`Tab` schede · `l` Libreria · `r` aggiorna · `?` aiuto · `q` esci |
| Aggiunta | `a` magnet/URL · `t` file `.torrent` · `c` ciclo · `s` cerca · `e` eventi |
| Download | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `Invio` dettagli torrent · `p` pausa/riprendi · `d`/`D` rimuovi (torrent con o senza file; HTTP dalla lista) · `X` pulisci completati · `k` verifica · `R` riannuncia · `n` senza-rinomina · `i`/`u` pin/unpin · `L` limiti globali · `T` limite temporaneo · `o`/`O` ordina · `F` filtro |
| Dettagli | `1` generale · `2` tracker · `3` file · `4` peer · `↑↓` scorri · `Esc`/`Invio` indietro |
| Log | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `/` filtro · `f` segui/ferma |
| Stato | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` scorri |
| Salute | `↑↓`/`PgUp`/`PgDn` scorri · `x` svuota cestino (con conferma) |
| Terminale | `Ctrl-L` ridisegna tutto · incolla un magnet/URL per aprire "aggiungi" già compilato |
| Archivio | `↑↓` seleziona · `Invio` accoda · `/` filtro |
| Mancanti | `↑↓` seleziona · `Invio` apri la serie · `s` cerca l'episodio · `i` ignora · `r` aggiorna |
| Blocklist | `↑↓` seleziona · `d` rimuovi |
| Libreria | `1` Serie TV · `2` Film · `3` Fumetti · `↑↓` seleziona · `s`/`/` filtra · `Invio` dettagli · `a` aggiungi da TMDB · `e` modifica · `p` pausa/riprendi · `d` elimina · `m` cerca (mancanti della serie / il film) |
| Serie | `←→` stagione · `Spazio` attiva/disattiva la stagione · `↑↓` episodio · `Invio` sorgenti in archivio · `s` cerca online · `i` ignora · `R` riscarica · `y` copia magnet · `m` cerca mancanti · `M` metadati TMDB · `n` rinomina (anteprima + conferma) · `e` modifica · `p` pausa · `d` elimina · `Esc` indietro |
| Film | `↑↓` release in archivio · `Invio` accoda · `s` cerca online · `y` copia magnet · `e` modifica · `R` riscarica · `p` pausa · `d` elimina · `Esc` indietro |
| Moduli | `↑↓` campo · `Invio` modifica (sì/no: `Invio` o `Spazio`) · `s` salva · `Esc` annulla; `*` segna i campi cambiati |

Le conferme (rimozioni, cestino, pulizia) accettano solo `s`/`y`: `Invio` annulla e il
testo incollato viene ignorato, così un `Invio` ripetuto su un link lento non può
cancellare un torrent con i suoi file.

## Uso via SSH

La TUI è pensata per una sessione SSH:

- **Banda.** Ogni frame invia solo le righe cambiate, e di una riga solo la parte
  finale che cambia; quando il log scorre è il terminale a far scorrere la
  regione e arrivano solo le righe nuove. I frame sono aggiornamenti
  sincronizzati (niente sfarfallio). Salute e statistiche si interrogano ogni
  10 secondi invece che a ogni aggiornamento. In basso a destra la TUI mostra la
  banda che sta usando, per esempio `TUI ↓7.2KB/s ↑230B/s · 1.5 req/s`:
  ↓ è ciò che riceve (risposte del daemon e tasti), ↑ ciò che invia (schermo e
  richieste), `req/s` le richieste al daemon al secondo.
- **A capo automatico.** Log, dettagli, salute e cruscotto vanno a capo invece di
  essere tagliati; negli elenchi la riga selezionata mostra il titolo intero.
  Le larghezze sono calcolate in colonne, quindi titoli con ideogrammi o emoji
  non rompono l'impaginazione.
- **Testo sicuro.** Sequenze di escape e caratteri di controllo presenti in nomi
  di torrent, titoli dei feed o log vengono rimossi prima di arrivare al
  terminale.
- **Incolla.** Il bracketed paste è attivo: un magnet incollato arriva intero nel
  campo, e fuori da un campo apre "aggiungi" invece di eseguire comandi.
- **Copia (`y`).** Usa OSC52, che copia negli appunti del computer da cui ti
  colleghi. In tmux serve `set -g set-clipboard on` (o `allow-passthrough on`);
  la TUI invia la sequenza anche nella forma passthrough di tmux e GNU screen.
- **Locale.** Con una locale non UTF-8 (es. `LANG=C`, frequente se SSH non
  inoltra `LC_*`) la TUI disegna solo in ASCII. Si forza con
  `GEXTTO_TUI_ASCII=1` o `GEXTTO_TUI_ASCII=0`.
- **Link lenti.** Se le frecce diventano lettere, alza l'attesa per le sequenze
  di escape: `GEXTTO_TUI_ESCDELAY=250` (millisecondi, default 100).
- **Disconnessione.** Se la connessione cade (SIGHUP o terminale chiuso) la TUI
  esce da sola. Un ridimensionamento della finestra ridisegna subito.
- Senza terminale interattivo la TUI si ferma con un messaggio: da remoto usa
  `ssh -t host /opt/gextto/gexttod tui`.

Altre variabili: `NO_COLOR` (o `TERM=dumb`) disattiva i colori,
`GEXTTO_TUI_THEME=high-contrast` attiva il tema ad alto contrasto.

## Architettura

`internal/tui`:

- `i18n.go` — catalogo bilingue IT/EN, `Translator`, risoluzione della lingua.
- `client.go` — client HTTP tipizzato di tutte le API del daemon.
- `format.go` — formattazione di byte, durate, numeri opzionali.
- `text.go` — larghezza in colonne, pulizia dei caratteri di controllo, a capo,
  fallback ASCII.
- `bandwidth.go` — misura della banda usata dalla TUI (piè di pagina).
- `model.go` / `update.go` / `render.go` — stato puro, transizioni da tasti e
  rendering in righe con stile semantico (testabili senza terminale).
- `keys.go` — decodifica del flusso di byte in tasti (sequenze CSI complete di
  modificatori, bracketed paste, UTF-8).
- `term.go` — raw mode, schermo alternato e renderer incrementale.
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
