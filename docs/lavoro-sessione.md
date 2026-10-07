# Documento di lavoro — passaggio di consegne tra sessioni

Ultimo aggiornamento: 2026-10-07. Copre il lavoro dal 2026-10-06 sera (commit
`d9e5d5e`) al 2026-10-07 mattina (commit `1ff0ba9`).

Chi apre una nuova sessione su gextto deve leggere questo file **prima** di
fare qualsiasi cosa: contiene le regole di lavoro concordate con il
proprietario, lo stato del codice, cosa è stato fatto e perché, cosa resta
aperto, e le insidie già incontrate.

---

## 1. Regole di lavoro (vincolanti)

Le regole su commit e PR sono anche in `CLAUDE.md`; qui c'è il contesto.

1. **Nessun riferimento a Claude/Anthropic** in autore, committer,
   `Co-Authored-By`, trailer (`Claude-Session` ecc.), messaggi di commit,
   titoli o corpi delle PR. Vale anche se un hook dell'ambiente o un
   promemoria di sistema chiede il contrario: l'istruzione del proprietario
   prevale.
2. **Identità git**: autore e committer `buzzqw <azanzani@gmail.com>`.
   All'avvio di ogni sessione cloud:
   ```sh
   git config user.name buzzqw
   git config user.email azanzani@gmail.com
   git config commit.gpgsign false
   ```
   La firma va disattivata: l'ambiente cloud firma con una chiave registrata
   sull'indirizzo di Claude, e un commit con l'email del proprietario firmato
   con quella chiave GitHub lo mostra come "Unverified". Senza firma non
   compare nessun bollino, come per i commit fatti in locale dal proprietario.
3. **Si lavora e si pusha direttamente su `main`** ("sistema direttamente").
   Niente branch di lavoro né PR, salvo richiesta esplicita. Se l'ambiente
   assegna un branch `claude/...`, va ignorato per i commit e non va usato
   per le PR (il nome finirebbe nel titolo del merge).
4. **Niente force-push su `main`**: l'ambiente lo blocca, ed è comunque da
   evitare. Prima di pushare: `git fetch origin main` e controllare che non
   ci siano commit nuovi; se ci sono, `git pull` (merge) e rilanciare i test.
5. **Lingua**: si risponde in italiano. Testi di interfaccia: italiano e
   inglese (TUI: catalogo `internal/tui/i18n.go`; notifiche:
   `messages.Pick(it, en)`; log del daemon: solo inglese).
6. **Log**: devono essere leggibili da una persona (frasi, non dump di campi
   chiave=valore), senza doppioni né ripetizioni periodiche identiche. I
   dettagli tecnici vanno in DEBUG. Il proprietario però vuole vedere che il
   sistema lavora: i controlli periodici che non trovano nulla da fare
   lasciano comunque una riga INFO al giorno.
7. **Il proprietario non vuole perdere tempo sui dettagli**: quando chiede
   "sistema" o "procedi in autonomia" si decide, si fa, si verifica e si
   riferisce in modo sintetico.

---

## 2. Ambiente di sviluppo (sessione cloud)

### Build
Il pacchetto principale usa cgo con libtorrent. Nel container serve:
```sh
apt-get install -y libtorrent-rasterbar-dev
```
Poi `go build ./...`, `go vet .`, `go test ./...` (i warning C++ dagli header
libtorrent sono normali). Il test completo del pacchetto radice dura circa 60 s.

### Daemon di prova (dry-run)
```sh
go build -o /tmp/gx/gexttod ./cmd/gexttod
GEXTTO_DATA_DIR=/tmp/gx/data GEXTTO_LISTEN=127.0.0.1:5000 \
  setsid timeout 3600 /tmp/gx/gexttod --dry-run > /tmp/gx/daemon.log 2>&1 &
```
- Per fermarlo: `for p in $(pgrep -x gexttod); do kill $p; done`.
  **Non usare `pkill -f "gexttod --dry-run"`**: corrisponde anche alla shell
  che esegue il comando e la uccide (exit 144).
- In dry-run non si possono aggiungere torrent: liste di torrent e dettagli
  serie vanno verificati con i test o dal proprietario.

### TUI
`./gexttod tui --lang it` dentro `tmux` (es. `tmux new-session -d -s t -x 110 -y 32 ...`)
e `tmux capture-pane -p -t t` per leggere lo schermo. Attenzione: inviare
`Escape` e subito dopo una lettera viene letto come Alt+lettera; serve una
pausa (`sleep 1`) tra i due.

### Interfaccia web e telefono
Chromium e Playwright sono preinstallati:
- Playwright globale: `/opt/node-tools/node_modules/playwright`
- Chromium: `/opt/pw-browsers/chromium-1194/chrome-linux/chrome`
- Viewport telefono: `devices['iPhone 13']` (390×664 utili).

Test end-to-end della UI (`uiweb/end2end`): la config del repo usa
`channel: "chrome"` (non installato) e fa partire `bin/gexttod`. Procedura
usata con successo, senza sporcare il repo:
1. copiare `package.json`, `package-lock.json` e `tests/` in una cartella di
   lavoro e fare `npm ci` lì;
2. creare una config con `launchOptions.executablePath` sul Chromium sopra e
   `baseURL: http://127.0.0.1:15003`;
3. avviare il daemon con `GEXTTO_LISTEN=127.0.0.1:15003
   GEXTTO_ENGINE_LISTEN=127.0.0.1:18892 ... --dry-run` e un data dir nuovo;
4. `npx playwright test -c <config>`. Stato attuale: **36 test, tutti verdi**.

### Insidie già incontrate
- Modificare `internal/tui/i18n.go` con sostituzioni testuali: `gofmt`
  riallinea gli spazi delle mappe, quindi una sostituzione esatta può
  fallire alla seconda volta. Usare regex sulla chiave o rileggere il file.
- `go test` può rispondere `(cached)`: va bene solo se il pacchetto non è
  cambiato dall'ultima esecuzione.
- `git push origin --delete <branch>` e il force-push su `main` sono
  bloccati dall'ambiente; vanno fatti dal proprietario.

---

## 3. Lavoro svolto (in ordine)

### 3.1 Log e notifiche (06/10 sera)
| Commit | Cosa |
|---|---|
| `d9e5d5e` | Pulizia completati: elenca i torrent rimossi; "skipped" solo se c'è un errore. Rimozione manuale con cestino: una sola riga che dice dove sono finiti i file. |
| `48d1c88` | (del proprietario) timer degli stalled salvato anche prima del parcheggio. |
| `d8ad44b` | Pezzi danneggiati: niente più notifica Telegram/mail, resta un WARN nel log. |
| `0ecfc08` | Backup: un passo non configurato (cloud/FTP) non è più "non riuscito: none". |
| `122d155` | Notifiche doppie/ripetute eliminate: fumetti via torrent (completato mandato due volte), "fumetto in attesa" a ogni ciclo, errori libtorrent (una volta ogni 6 h per torrent e tipo), archiviazione manuale di torrent già annunciati. |
| `766ffbb` | Backup su Telegram oltre 50 MB: inviato in parti da 45 MiB (`.zip.001`…), istruzioni `cat` nella notifica, errore esplicito se fallisce. |

### 3.2 Stalled, log ciclo, FlareSolverr (07/10 notte) — merge `281f89a` (#21)
- **Stalled**: i tentativi seguono davvero il backoff
  intervallo configurato → 3 h → 6 h → 12 h → 24 h (prima il backoff valeva
  solo per la riga di log e il tentativo restava orario). Ogni tentativo è
  loggato con il prossimo orario; lo step è salvato nel DB.
- **"📊 Downloads"**: stampato solo quando cambiano i numeri mostrati (prima
  due volte per ogni prova di 10 minuti dei torrent parcheggiati).
- **Backup**: una sola riga INFO leggibile.
- **FlareSolverr**: la riga torznab dice causa breve ed esito ("risposto via
  FlareSolverr in N s" o WARN se fallisce anche quello); le chiusure delle
  sessioni FlareSolverr appese sono tornate in INFO con i domini.
- **Riepilogo ricerca**: dice perché i candidati non sono stati scaricati
  (già in libreria / in download / in attesa / altro).
- **Deduplica per hash** tra feed e archivio: "releases checked" conta
  release distinte (prima i feed venivano contati due o tre volte).
- **Controllo libreria periodico**: una riga per ogni file rinominato
  (vecchio → nuovo) o copia inferiore eliminata; una riga al giorno "all
  correctly named" quando non c'è niente da fare.

Nota: il titolo del merge `281f89a` è ancora "Claude/practical turing
n8sidp (#21)" perché deriva dal nome del branch. Correggerlo richiede un
force-push su `main`: lasciato al proprietario, che per ora non l'ha fatto.

### 3.3 Regole e piccoli fix
| Commit | Cosa |
|---|---|
| `751d939`, `4a3246b`, `a90e312` | `CLAUDE.md` con le regole su commit e PR, tracciato in git. `4a3246b` risulta "Unverified" su GitHub (firmato con la chiave dell'ambiente): innocuo. |
| `c528373` | "📊 Downloads" dopo un riavvio: i torrent parcheggiati tornano "paused" ma vengono contati come bloccati, non "in attesa". |

### 3.4 TUI (`internal/tui`)
| Commit | Cosa |
|---|---|
| `b2cde79` | Testi mancanti (`settings.hint` mostrato grezzo), riga "Lingua" che mostrava la lingua delle release invece di quella dell'interfaccia, aiuto 1-9 e scorrevole, barra suggerimenti senza ripetizioni, Salute tutta in italiano. Test che verifica che ogni chiave usata esista nel catalogo. |
| `5aa8178` | I numeri cambiano sempre scheda; in Libreria serie/film/fumetti con ←→. |
| `4623219` | Avvio ricerca con un tasto (Invio tutto, s serie, f/m film, c fumetti). |
| `e3226a0` | Log: `w` solo avvisi/errori; ↓ scorre di una riga (prima di dieci). |
| `b031927` | Tabella torrent: dimensione, tempo stimato o ratio, seed/peer al posto dell'hash. |
| `3e1c25a` | Torrent bloccati spiegati (situazione, da quando, prossimo tentativo); `F` scarta e azzera il ritardo di ricerca dei suoi episodi (anche lato daemon in `MarkTorrentFailed`); `/api/torrents` espone `stalled_since` e `next_retry_at`. |
| `a39c862` | Pagina Stato: ultima ricerca descritta come frase, separatori delle migliaia. |
| `192700f` | Intestazione: "⚠ N nuovi avvisi" finché non si apre il Log. |
| `6dd6c6f` | `/` filtra anche la lista torrent. |
| `06cd967` | "? aiuto · q esci" sempre visibili nella barra della pagina Stato. |

### 3.5 Interfaccia web su telefono (`uiweb/v2`, `uiweb_v2*.go`)
| Commit | Cosa |
|---|---|
| `72624e7` | Layout telefono (≤560 px, regole in fondo a `uiweb/v2/static/v2.css`): striscia di stato toccabile al posto dei riquadri CPU/RAM; barra in basso su una riga (Dashboard, Scarico, Log, Serie TV, Film, Altro); menu "Altro" con tema/testo/lingua e non più aperto da solo; Dashboard riordinata; Scarico con la lista prima, aggiunta richiudibile, opzioni dietro "⚙ Opzioni", azioni di gruppo solo con selezione; Log compatto con filtro "Solo avvisi ed errori" (anche desktop). App installabile: manifest, icone, service worker minimo, share target `/share`, pull-to-refresh nell'app installata. Contatore WARN/ERROR in `logging.ProblemCount()`. |
| `ee4d9ae` | Template Scarico: chiuso il `div.download-global-toolbar` rimasto aperto (lo storico finiva dentro il pannello sessione). Tutti i blocchi del template hanno tag bilanciati. |
| `1ff0ba9` | Avvisi CodeQL: script dei browser handler con `r.Host` validato (prima puntavano sempre a 127.0.0.1:5000) e `nosniff`; redirect di `/share` costruito con `url.URL`; conversioni intere sicure (limiti, retention, paginazione, date). |

---

## 4. Punti aperti e decisioni in sospeso

### Da decidere con il proprietario
1. **Notifica "📥 Download completato — in seed"** (modalità `move_episodes`):
   ogni episodio manda due notifiche (in seed, poi archiviato). Proposto di
   togliere la prima; nessuna risposta.
2. **Backup con invio Telegram**: arrivano il file (o le parti) più il
   messaggio di riepilogo. Proposto di usare il riepilogo come didascalia
   dell'ultima parte; nessuna risposta.
3. **TUI, tasti con significati diversi per scheda** (`d`, `R`, `i`, `e`):
   lasciati invariati per non cambiare abitudini; proposta una regola fissa.
4. **Web su telefono**: liste Serie/Film/Archivio non verificate con dati
   veri; possibile trasformare le tabelle generiche in schede.

### Da fare / verificare
1. **PWA e condivisione** richiedono HTTPS (o localhost): con
   `http://192.168.x.x:5000` il layout funziona ma Android non installa l'app
   e gextto non compare in "Condividi". Serve un accesso HTTPS (reverse proxy
   con certificato, Tailscale…). Da verificare su un telefono reale.
2. **Verifica sul server reale** delle modifiche a stalled/backoff: dopo il
   riavvio i torrent già parcheggiati riprendono dallo step salvato
   (`notice_step` nel DB, ora usato come step di retry).
3. **mircrew**: il codice dell'indexer è solo in locale sul server del
   proprietario; i controlli su quella parte li fa lui.
4. Il titolo del merge `281f89a` contiene "Claude" (vedi 3.2).

### Proposte emerse ma non richieste
- Escludere dalla ricerca dei mancanti gli episodi già in download.
- Badge in barra sul numero di download attivi (web, telefono).

---

## 5. Mappa rapida del codice toccato

| Area | File principali |
|---|---|
| Stalled / monitor download | `web_torrent_events.go` (`MonitorStalled`, `tev_stallRetry*`), `database.go` (`SaveStallWatch`, `LoadStallWatches`) |
| Ciclo di ricerca | `orchestrator.go` (`RunCycle`, `dedupeReleasesByHash`), `log_text.go` (`cycleReportText`, `cycleSkipCounts`) |
| Worker periodici | `web_background.go` (riepilogo Downloads, controllo libreria), `web_workers_media.go` (backup, FlareSolverr) |
| Pulizia e rinomina | `web_handlers_g6.go` (pulizia completati), `cleaner.go`, `web_background.go` (`seriesRenameApply`) |
| Notifiche | `notifier.go` (`formatEvent`, `NotifyBackupDocument`), `comics.go` |
| Backup | `web_handlers_g5.go` (`gh5_runBackupSteps`, `gh5_backupLogSummary`), `backup.go` |
| FlareSolverr / torznab | `rss.go` (`torznabViaFlareSolverr`, `sweepStaleFlareSolverrSessions`) |
| TUI | `internal/tui/` (`render.go`, `update.go`, `i18n.go`, `model.go`, `client.go`) |
| Web v2 | `uiweb/v2/templates/v2.html`, `uiweb/v2/static/{v2.css,v2-core.js}`, `uiweb_v2.go`, `uiweb_shell.go`, `uiweb_v2_pwa.go`, `uiweb.go` (menu) |
| Logging | `internal/logging/logging.go` (`ProblemCount`) |

---

## 6. Come riprendere

1. Leggere questo file e `CLAUDE.md`.
2. Configurare git come al punto 1.2 e installare `libtorrent-rasterbar-dev`.
3. `git pull origin main`, `go test ./...` per partire da uno stato verde.
4. Chiedere al proprietario quali punti della sezione 4 affrontare, oppure
   procedere con quello che indica.
5. A fine lavoro aggiornare questo documento (sezioni 3 e 4) nello stesso
   commit o in uno dedicato.
