# Documento di lavoro — passaggio di consegne tra sessioni

Ultimo aggiornamento: 2026-10-07. Copre il lavoro dal 2026-10-06 sera (commit
`d9e5d5e`) al 2026-10-07 (commit `432ce98` e il commit `docs:` successivo):
log e notifiche, TUI, web su telefono e le migliorie della sezione 3.6.

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
5. **Lingua**: si risponde in italiano. Testi di interfaccia: la UI web segue i
   cataloghi `internal_translations*.yml` (en, de, fr, es, pl; chiave = testo
   italiano; un test verifica che i cinque cataloghi abbiano le stesse chiavi):
   **ogni testo nuovo va tradotto in tutte e cinque le lingue**. Fuori dai
   template si traduce con `uiText(s, "testo italiano")`. TUI: catalogo
   `internal/tui/i18n.go` (it/en); notifiche: `messages.Pick(it, en)`; log del
   daemon: solo inglese.
8. **Pubblico italiano**: Gextto nasce per il pubblico italiano, dove le serie
   arrivano doppiate mesi o anni dopo la messa in onda originale. Non dare mai
   per scontato che un episodio sia disponibile quando va in onda: le date TMDB
   vanno presentate come "messa in onda originale".
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
4. `npx playwright test -c <config>`. Stato attuale: **36 test, tutti verdi**
   (verificato anche dopo le migliorie 3.6).

### Insidie già incontrate
- Modificare `internal/tui/i18n.go` con sostituzioni testuali: `gofmt`
  riallinea gli spazi delle mappe, quindi una sostituzione esatta può
  fallire alla seconda volta. Usare regex sulla chiave o rileggere il file.
- `go test` può rispondere `(cached)`: va bene solo se il pacchetto non è
  cambiato dall'ultima esecuzione.
- `git push origin --delete <branch>` e il force-push su `main` sono
  bloccati dall'ambiente; vanno fatti dal proprietario.
- **Esito dei test**: `go test ... | grep ... && git commit` committa anche con
  test rossi (conta l'uscita di `grep`/`head`). Salvare l'output su file e
  controllare `$?` di `go test` prima di committare.
- Il registro dei download HTTP dei fumetti è globale al processo: nei test non
  dare per scontato che sia vuoto.
- Nei manuali una riga che inizia con `#` (es. «#41» andato a capo) diventa un
  titolo Markdown: lo scopre il test e2e del manuale.
- Il manuale è incorporato nel binario: dopo averlo modificato ricompilare il
  daemon prima dei test e2e.
- Test e2e: 36 test, tutti verdi a fine sessione.

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

### 3.6 Migliorie (07/10) — dettagli in `docs/proposte-migliorie.md`
| Commit | Cosa |
|---|---|
| `20b28eb` | Hardlink in libreria dei file in seed (`hardlink.go`, `hardlink_seeding` attivo di default), ripiego sulla copia. |
| `1bfaad4` | Jellyfin/Plex aggiornano solo la cartella cambiata (`media_refresh.go`), mappature percorsi per Docker. |
| `95f6f97` | Soglia `upgrade_until_score` (`upgrade_until.go`). |
| `7dae752` | Login facoltativo con rete locale esente (`auth.go`), chiave API, `GEXTTO_AUTH_DISABLE=1`. |
| `e78bcc2`, `a919fcd` | Calendario `/feed/calendar.ics` (arrivi, messa in onda originale, uscite italiane dei film); Dashboard etichettata. |
| `6a7f739` | `ComicInfo.xml` nei CBZ scaricati direttamente (`comicinfo.go`). |
| `f4a9f8f` | Anime con numerazione assoluta (`anime.go`, casella nella scheda serie web/TUI; aggiunta anche «Niente upgrade» al web). |
| `e815180` | Gli episodi già in download non sono più mancanti. |
| `893bc94`, `42db4b2` | Badge dei download in corso su "Scarico", anche sul telefono. |
| `698efbf` | Login, calendario e descrizioni delle nuove impostazioni tradotti nelle 6 lingue. |
| `432ce98` + `docs:` | Manuali, README, SECURITY, ADVANCED, API, ARCHITECTURE, UI_V2, tui aggiornati. |

### 3.7 gx-torrent completato (07/10) — dettagli in `docs/gx-torrent.md`
Il commit `a2778b5` (altra sessione) era uno scheletro: gx-torrent non era mai
selezionabile (`TorrentBackendName` lo riportava a `embedded`), le sue
impostazioni non si salvavano (mancava `gxtorrent_` nella whitelist), la lista
non dava progresso/percorso, niente eventi, rimozione che poteva cancellare i
dati, coda che riprendeva i torrent messi in pausa dall'utente.
Riscritti il demone (`cmd/gx-torrent/`) e l'adapter (`gxtorrent_engine.go`,
`gxtorrent_runtime.go`):
- **coda autogestita nel demone**:
  - slot download/seed e tetto totale;
  - i torrent lenti non contano;
  - rotazione dei torrent fermi con raffreddamento di 30 minuti;
  - pin e probe fuori coda;
  - coda dinamica portata dal bridge C++;
- **stalled**:
  - `MarkStalled`/`Restart`/`ClearStalled` diventano park/probe/unpark;
  - il parcheggio è persistente;
  - i torrent ruotati restano `stalled` così l'orologio di `MonitorStalled`
    non si azzera;
- **dati al sicuro**:
  - symlink per torrent;
  - `delete_files` cancella solo il contenuto del torrent;
  - spostamento con rename o copia;
- **sicurezza**: token, solo loopback senza token, RPC di rain spento;
- **gestione**: gextto avvia e sorveglia il binario; dopo 3 crash in 10
  minuti torna a libtorrent;
- **integrazione**: build/pacchetto/install/`--update`, impostazioni
  (token, eseguibile; tolti categoria/tag), matrice capacità onesta;
- **test**: test demone (anche con sessione rain reale) e adapter.

Smoke test eseguito qui:
- gextto avvia gx-torrent da sé;
- il torrent locale viene verificato e va in seed;
- pausa e ripresa da gextto funzionano (la pausa resta al 100%: bug di rain
  corretto);
- spostamento da gextto con symlink ripuntato;
- dopo un kill -9 il demone viene riavviato da gextto;
- allo spegnimento di gextto anche il demone si ferma con SIGTERM.

Big Buck Bunny resta in `downloading_metadata`: il sandbox non raggiunge i
peer (solo HTTPS).

### 3.8 gx-torrent: rete, selezione file, v2 (07/10) — dettagli in `docs/gx-torrent.md`
rain è ora una copia modificata in `third_party/rain` (MIT, `replace` in
`go.mod`); le modifiche sono descritte in `third_party/rain/GEXTTO.md`.

- **Porta unica** come libtorrent (prima libera di `libtorrent_port_min/max`
  o di `libtorrent_listen_interfaces`), DHT sulla stessa porta UDP. Apertura
  automatica sul router con UPnP/NAT-PMP, rinnovata ogni 20 minuti.
- **Selezione dei file** (priorità 0 = escluso): i file esclusi restano in
  `DATA/parts`; progresso e completamento contano solo i file scelti.
- **Rete**:
  - interfaccia in ascolto e interfaccia uscente (killswitch);
  - proxy SOCKS5/HTTP (nuova impostazione `gxtorrent_proxy`);
  - cifratura 0/1/2;
  - filtro IP (P2P, eMule, CIDR, intervalli), con il pulsante di
    aggiornamento di gextto che vale anche per gx-torrent.
- **v2-only**: rifiutati in modo chiaro; gextto mette la release in blocklist
  e usa la successiva.
- **Statistiche**: `/api/libtorrent/session-stats` funziona anche con
  gx-torrent (contatori di rain).
- **Bug di rain corretti**:
  - IP dei peer mai più contattati dopo un completamento;
  - race sul logger globale.
- **Test** con trasferimenti reali tra due demoni:
  - porta unica, anche con cifratura forzata;
  - selezione dei file;
  - proxy SOCKS5 e HTTP;
  - RAM disk spostato a metà download;
  - filtro IP.
- **Smoke test**:
  - gextto avvia il demone con i flag di rete;
  - porta 6881 scelta;
  - torrent preesistenti migrati sulla porta unica;
  - messaggio chiaro senza router UPnP.

### 3.9 uTP, LSD, statistiche in Salute (07/10)
- **uTP**: socket UDP sulla porta unica condiviso con il DHT (anche
  `nictuku/dht` ora è in `third_party/dht`); in uscita uTP e TCP in parallelo.
- **LSD**: multicast BEP 14 nel demone.
- **Pagina Salute**: pannello "Motore torrent" per gx-torrent, libtorrent e
  qBittorrent.
- **Test reali** passati:
  - trasferimento solo uTP con il DHT sullo stesso socket;
  - scoperta via LSD con multicast vero.
- **Bug di chiusura risolto**: il DHT restava bloccato sul socket uTP
  condiviso.

### 3.10 Dettaglio torrent con gx-torrent e qBittorrent (07/10)
- **Scheda dettagli** (web):
  - mostra solo i comandi che il motore supporta (super seeding, web seed,
    limiti di velocità e connessioni per torrent), con una nota che spiega
    cosa manca;
  - per gx-torrent i file sono solo "Salta / Scarica";
  - gli errori del motore compaiono nelle schede invece di liste vuote;
  - nuove righe: sciame (seed/peer) e motore; nuova colonna "Connessione"
    sui peer (uTP/TCP, cifrata, entrata/uscita).
- **qBittorrent**:
  - l'avanzamento dei peer era mostrato 100 volte più piccolo (0-1 invece
    di percentuale);
  - aggiunti tipo di connessione e flag dei peer;
  - il super seeding ora funziona (era bloccato da un controllo solo
    libtorrent);
  - "Salva tracker" sostituisce l'elenco invece di aggiungere soltanto.
- **gx-torrent**:
  - avanzamento e seed dei peer dal bitfield;
  - tracker corrente;
  - seed connessi contati correttamente.

### 3.11 Cache automatica gx-torrent e log dei v2 (07/10)
- **Cache disco gx-torrent**:
  - automatica dalla RAM (lettura 1/32, scrittura 1/16, entro limiti);
  - oppure da `libtorrent_cache_size`;
  - scadenza e preallocazione (`fallocate`) applicate;
  - "Ottimizza impostazioni" funziona anche con gx-torrent;
  - riga "Cache disco" nel pannello Salute.
- **Log dei torrent solo v2**:
  - «🔁 … was a BitTorrent v2-only torrent …; «…» (v1) is downloaded
    instead» quando parte un'alternativa v1 o ibrida;
  - «⏳ … no v1 or hybrid release is available yet» a fine ciclo se non c'è,
    poi un promemoria al giorno;
  - «⌛ … no longer tracked» dopo 30 giorni.
  - La memoria è in RAM: un riavvio di gextto la azzera, ma la release v2
    resta comunque in blocklist.

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

### Da verificare sul server reale (gx-torrent, 3.7 e 3.8)
- Nel log del demone deve comparire «router port N opened with UPNP/NATPMP».
  Altrimenti va aperta la porta a mano (TCP e UDP).
- Con VPN: impostare l'interfaccia uscente (es. `wg0`); spegnendo la VPN il
  traffico deve fermarsi.

1. `make build`, poi in Configurazione → Motore torrent scegliere `gx-torrent`
   e riavviare. Il log deve dire «gx-torrent avviato da Gextto» e
   «Torrent engine: gx-torrent».
2. Scaricare un torrent libero (es. Big Buck Bunny) e controllare:
   - progresso;
   - completamento e rinomina/import;
   - log del demone in `DATA_DIR/gx-torrent/gx-torrent.log`.
3. Con più torrent della soglia `active_downloads`, controllare:
   - che quelli fermi liberino lo slot (`/api/v1/stats`: `slow`, `queued`);
   - che le pause manuali restino tali.

### Da verificare sul server reale (migliorie 3.6)
1. **Hardlink**: con download e libreria sullo stesso filesystem il log deve
   dire «linked into the library (hardlink…)»; altrimenti compare una volta
   «Hardlink not possible (…)». Controllare con `ls -li` che i due nomi abbiano
   lo stesso inode e contatore 2.
2. **Jellyfin/Plex**: dopo un import, nei log di Jellyfin/Plex deve comparire la
   scansione della sola cartella; se servono, compilare le mappature percorsi.
3. **Login da fuori**: provare dietro il reverse proxy reale che
   `X-Forwarded-For` arrivi (altrimenti tutto risulta locale).
4. **Anime**: marcare una serie anime vera e controllare nei log del ciclo che
   le release «Titolo - NNN» vengano riconosciute.
5. **Calendario**: iscriversi da Thunderbird/telefono (serve la chiave TMDB).

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
| Hardlink | `hardlink.go` (`linkOrCopyFile`, `hardlinkAtomically`), chiamato da `tev_completeEpisodeFolderWithArchive` e `StagePackFile` |
| Jellyfin/Plex | `media_refresh.go`, richieste raggruppate in `web_background.go` (`requestMediaLibraryRefresh`) |
| Soglia upgrade | `upgrade_until.go` (`upgradeReasonUntil`), `database.go` (approvazioni), `decision.go` (spiegazione) |
| Accesso | `auth.go` (`AuthMiddleware`, montato in `web_serve.go`), hash password in `saveConfigSetting` (`web.go`) |
| Calendario | `calendar_ics.go`, `tmdb.go` (`MovieReleaseDate`) |
| Fumetti | `comicinfo.go` (`tagDownloadedComic`, chiamato in `handleHTTPOutcome` e dopo Mega) |
| Anime | `parser.go` (`parseAbsoluteEpisode`), `anime.go` (`resolveAnimeReleases`, ricerche assolute), `engine.go` (`SearchSeriesEpisode`) |
| Mancanti | `database.go` (`ArchiveGaps`, `activeDownloadEpisodes`) |
| Badge Scarico | `uiweb_shell.go` (`ActiveDownloads`, `uiNavCounts`), `v2-core.js` (`updateDownloadsBadge`) |
| Traduzioni | `internal_translations*.yml`, `uiText` in `auth.go` |

---

## 6. Come riprendere

1. Leggere questo file, `CLAUDE.md` e `docs/proposte-migliorie.md` (stato delle
   migliorie).
2. Configurare git come al punto 1.2 e installare `libtorrent-rasterbar-dev`.
3. `git pull origin main`, `go test ./...` per partire da uno stato verde.
4. Chiedere al proprietario quali punti della sezione 4 affrontare, oppure
   procedere con quello che indica.
5. A fine lavoro aggiornare questo documento (sezioni 3 e 4) nello stesso
   commit o in uno dedicato.
