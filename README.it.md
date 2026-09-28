# Gextto

Copyright (c) 2026 buzzqw e collaboratori di Gextto.

**Gextto** è un demone self-hosted per l'acquisizione e l'archiviazione automatica
di serie TV, film e fumetti.

Un solo binario Go racchiude tutto: motore di scraping, archivio SQLite, web
UI/API (inclusa con `//go:embed`), TUI terminale e una sessione **libtorrent**
integrata. Non servono runtime né servizi esterni.

Monitora ciò che configuri, cerca su feed RSS/HTML, indexer Torznab
(Jackett/Prowlarr) e motori di ricerca pubblici, assegna un punteggio di qualità a
ogni release, scarica la migliore e la rinomina/archivia nella tua libreria (NAS
o disco locale).

> 🇬🇧 English: [`README.md`](README.md)
> 📖 Manuale completo: [`docs/MANUAL.it.md`](docs/MANUAL.it.md) ·
> [`docs/MANUAL.en.md`](docs/MANUAL.en.md)

[![Dona](https://img.shields.io/badge/❤️_Sostieni_Gextto-PayPal-00457C.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

---

## Cos'è Gextto

- **Un demone, nessun orchestratore esterno** — scraping, download, rinomina,
  archiviazione e UI vivono nello stesso processo.
- **Autonomo e auto-aggiornante** — un unico archivio di release (demone, UI web,
  libtorrent inclusa) che `gexttod --update` installa in modo atomico,
  preservando database e configurazione.
- **Sorgenti multiple** — feed RSS generici, listing HTML (con fallback
  FlareSolverr per Cloudflare), indexer Torznab (Jackett/Prowlarr) e motori web.
- **Punteggio qualità** — risoluzione, sorgente, codec, audio, HDR/Dolby Vision,
  gruppi e pesi configurabili, con simulatore integrato. Lo stesso punteggio
  viene usato per acquisizione, ricerche, upgrade, post-processing, archivio e
  rescore, includendo bonus dimensione e sottotitoli dei film.
- **Upgrade automatici** — sostituisce un file archiviato quando compare una
  release migliore (salto di risoluzione, HDTV→WEB-DL, HDR, repack) oltre una
  soglia di punteggio configurabile.
- **Serie e film** — metadati TMDB, locandine, monitoraggio per stagione, ricerca
  episodi mancanti, calendario, ricerca manuale.
- **Torrent** — libtorrent embedded: coda, limiti, tag, peer, tracker, file,
  spostamento storage, politica di seeding, fastresume, killswitch VPN e recupero
  dopo riavvio. I torrent **stalled** vengono messi realmente in pausa e fuori
  dagli slot attivi, poi riprovati automaticamente. I nuovi download sono
  **preallocati su disco** di default (interruttore nelle impostazioni e per
  singolo torrent). In aggiunta: pausa, sequenziale, salta-verifica, cima-coda,
  primo/ultimo pezzo, solo-metadati, preallocazione; priorità per-file, limiti
  per-torrent di connessioni/upload, upload/share mode, web seed, modifica
  tracker, scrape/DHT announce, super seeding ed export `.torrent`/magnet nel
  dettaglio torrent. Il `.torrent` di ogni torrent avviato può essere copiato in
  una cartella indicata.
- **Fumetti** — monitoraggio GetComics e weekly pack. Ogni fumetto aggiunto
  (weekly pack, titolo monitorato o *Download Now*) riceve il tag **`Comic`**,
  così la regola *Percorsi NAS per categoria (tag)* lo instrada nella cartella
  configurata.
- **Integrazioni** — Trakt, Simkl, Jellyfin, Plex, notifiche Telegram/e-mail/webhook.
- **UI web** — single-page responsive, tema chiaro/scuro, completamente in
  **italiano e inglese** (traduzione a runtime con import/export YAML), con log
  viewer, salute, grafici e manutenzione.
- **TUI terminale** — interfaccia interattiva via SSH/terminale per stato, torrent,
  log live, salute, archivio, mancanti e blocklist. Usa le stesse API del daemon
  senza accedere direttamente ai database.
- **Decisioni leggibili** — dai risultati di ricerca puoi vedere perché una
  release supera o non supera i controlli, con score, regole applicate e
  confronto read-only con l'archivio; la spiegazione non accoda né modifica dati.
- **Visti dai feed** — ogni release vista nelle sorgenti, raggruppata per titolo,
  consultabile anche per ciò che non è monitorato.
- **Regole di sanità automatiche** — sottotitoli hardcoded e dimensioni assurde
  (soglie per risoluzione derivate da un archivio reale) vengono rifiutati;
  nessun numero da configurare. Gli scarti sono eventi ordinari e vengono
  registrati a `DEBUG`, così il log `INFO` di produzione resta pulito.
- **Regolazione acquisizione** — delay prima del download con coda di attesa e
  interruttore per-titolo "Consenti aggiornamenti".
- **Ispezione reale dei file** — i dati `ffprobe` (HDR, codec, audio, lingue)
  sono salvati per file e **usati nei confronti di upgrade**, così le decisioni
  leggono il file archiviato vero, non solo il nome. I file nuovi sono
  analizzati al completamento; un backfill incrementale schedulato copre gli
  altri. È additivo: non declassa mai un file.
- **Robustezza** — backoff progressivo delle sorgenti con lista di reset e
  housekeeping/VACUUM programmato.
- **Automazione senza confusione** — hook eventi sotto *Integrazioni* e cartelle
  osservate sotto *Configurazione*. I file copiati nelle cartelle osservate
  vengono verificati come stabili e gli errori di importazione vengono ritentati
  con backoff, senza abbandonarli dopo un numero fisso di tentativi.
- **Libreria monotona** — Gextto non scarica mai un episodio più vecchio fuori
  dai buchi riconosciuti quando possiede già episodi successivi (un vero upgrade
  di qualità passa comunque); gap-fill e azioni manuali vincono sempre.
- **Backup** — manuali o programmati (locale, FTP, cartella cloud, Telegram).
  Salvano database e configurazione, non i media né lo stato torrent.
- **Leggero per davvero** — l'heap Go resta piccolo (tipicamente 4–20 MB) e le
  goroutine sono inattive fra le richieste; la CPU è spesa quasi interamente
  dalla sessione libtorrent durante un trasferimento. La cache disco e i buffer
  dei pezzi crescono durante il download e vengono restituiti al sistema al
  termine (il daemon chiama `malloc_trim`), così la memoria residente torna al
  valore di riposo invece di restare al picco.

## Installazione

### Installa su un server Linux

L'installer ufficiale supporta Debian, Ubuntu, Fedora, openSUSE e Arch Linux.
Va eseguito come root (o tramite `sudo`), su un host Linux 64 bit supportato con
systemd.
A ogni push su `main`, GitHub Actions pubblica un artefatto Linux `continuous`
testato. L'installer lo scarica, verifica il checksum SHA-256 e installa demone
e libtorrent inclusa senza compilare sul server. Crea anche l'utente di servizio,
il servizio systemd, le directory runtime e i database vuoti al primo avvio.
Non importa dati legacy.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer usa per default l'ultimo artefatto `continuous`. Puoi scegliere un
repository o una release diversi con `GEXTTO_REPO` e `GEXTTO_RELEASE`.

Esegui lo stesso comando una seconda volta per installare l'ultimo artefatto e
riavviare Gextto, oppure lascia che sia il demone installato ad
aggiornarsi (vedi *Aggiornamento di Gextto*). Database, configurazione, download,
archivi e log restano in `/var/lib/gextto`; programma e UI sono in `/opt/gextto`.

Variabili opzionali:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | \
  sudo env GEXTTO_DATA_DIR=/srv/gextto GEXTTO_PORT=5000 GEXTTO_RELEASE=continuous bash
```

Il servizio si chiama `gextto.service`:

```bash
sudo systemctl status gextto.service
sudo journalctl -u gextto.service -f
curl -fsS http://127.0.0.1:5000/api/health
```

Per un'installazione per utente senza root, compila da un checkout ed esegui
[`scripts/install-user-service.sh`](scripts/install-user-service.sh). Scrive e
avvia un'unità `systemctl --user`; per default ascolta sulla porta 5000 su tutte
le interfacce:

```bash
make build
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

Per mantenere il servizio attivo dopo il logout, abilita una volta il lingering
con `loginctl enable-linger "$USER"`. Se deve essere raggiungibile da un'altra
macchina, limita l'accesso con firewall o reverse proxy.

### Aggiornamento di Gextto

Ci sono due modi supportati per aggiornare. Entrambi installano lo stesso
payload e lasciano **intatti dati e configurazione**: tutto ciò che sta in
`/var/lib/gextto` (database, log, download, percorsi archivio) sopravvive
all'aggiornamento.

| Metodo | Comando | Note |
|---|---|---|
| Installer | riesegui il comando `install.sh` qui sopra | installa l'ultimo artefatto e aggiorna l'unità systemd |
| Demone | `sudo /opt/gextto/gexttod --update` | aggiorna solo il payload: `gexttod`, `lib/`, `run.sh` |

Il programma installato vive in `/opt/gextto`:

```
gexttod     il demone; la UI web è inclusa (rpath $ORIGIN/lib)
lib/        la libtorrent inclusa
run.sh      launcher (imposta LD_LIBRARY_PATH)
VERSION     il marker di release mostrato da --version
```

`/opt/gextto/gexttod --update` scarica `gextto-linux-<arch>.tar.gz`, verifica il `.sha256`
pubblicato quando la release lo fornisce e prepara il nuovo payload prima di
toccare l'installazione corrente. Se il download, il checksum o l'estrazione
falliscono, l'installazione in esecuzione resta invariata; se uno swap fallisce,
i file precedenti vengono ripristinati. Il servizio viene riavviato
automaticamente quando il comando gira come root, altrimenti viene stampato il
comando `systemctl` esatto.

```bash
/opt/gextto/gexttod --version                         # versione e build
sudo /opt/gextto/gexttod --update                    # ultima build continua
sudo /opt/gextto/gexttod --update --channel stable   # ultima release con tag
sudo /opt/gextto/gexttod --update --release v0.2.0   # un tag specifico
```

Se hai installato in un'altra directory, sostituisci `/opt/gextto` con quella
directory. L'updater riavvia automaticamente `gextto.service`; usa
`--no-restart` per un'installazione ferma o gestita manualmente. Usa
`--install-dir` solo quando il servizio o l'installazione di test risiede davvero
in quella directory.

Da un checkout sorgente, [`scripts/update.sh`](scripts/update.sh) ricompila il
demone (`make build`) e riavvia il servizio; aggiungi `--release` per installare
il payload pubblicato.

L'unità systemd **non** viene sovrascritta da `--update`: le personalizzazioni
locali (utente, porte, percorsi) restano. Per rigenerarla usa l'installer. Il
marker `VERSION` scritto accanto all'eseguibile è il nome della release
(`continuous`, un tag, oppure `source-main`); il numero di build numerico è
compilato nel binario e identifica la build esatta.

### Pacchetto Linux autonomo

Lo script di packaging produce un archivio autonomo
`gextto-linux-<arch>.tar.gz` (con il relativo `.sha256`) contenente:

```
gexttod     il demone; la UI web è inclusa (rpath $ORIGIN/lib)
lib/        la libreria libtorrent inclusa
run.sh      launcher (imposta LD_LIBRARY_PATH)
README.md   avvio rapido e prerequisiti
```

Si estrae e si esegue senza compilatore:

```bash
mkdir gextto && tar -xzf gextto-linux-x86_64.tar.gz -C gextto
cd gextto
./run.sh --version
GEXTTO_DATA_DIR="$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 ./run.sh
```

Poiché l'archivio include libtorrent e il demone ha la UI web già inclusa, non
serve alcuna libtorrent di sistema. L'archivio è prodotto da
[`scripts/package-linux.sh`](scripts/package-linux.sh) ed è quello che
`gexttod --update` installa. Richiede un Linux 64 bit recente (glibc, libstdc++,
OpenSSL 3, zlib, libzstd); `ffprobe` è opzionale. Il repository ufficiale
pubblica attualmente asset precompilati **solo per x86_64**. `aarch64` è
accettato come override per un repository che pubblichi l'asset corrispondente,
ma non è ancora disponibile nelle release ufficiali continuous/taggate. Per
un'installazione gestita come servizio usa l'installer descritto sopra.

### Compila da un checkout (sviluppo)

Per gli sviluppatori, installa Go ≥ 1.26, una toolchain C++17 e gli header di
sviluppo `libtorrent-rasterbar`, poi compila il demone:

```bash
make build            # -> bin/gexttod
# oppure
CGO_ENABLED=1 go build -o bin/gexttod ./cmd/gexttod
```

La UI web è inclusa nel binario con `//go:embed` da `webui/pkg`, quindi una build
normale non richiede passi UI. Il sorgente Leptos è in `ui/`; dopo averlo
modificato, rigenera il bundle con `make ui` (servono `cargo leptos` e il target
`wasm32-unknown-unknown`), che aggiorna `webui/pkg`. Un bundle su disco in
`GEXTTO_UI_DIR` (o `<exe>/ui`) ha comunque la precedenza per lo sviluppo.

Per una prova locale in modalità dry-run, senza download reali:

```bash
GEXTTO_DATA_DIR="$PWD/data" GEXTTO_ACTIVE=0 GEXTTO_DRY_RUN=1 ./bin/gexttod --dry-run
```

Per installare il servizio reale usa l'installer descritto sopra.

### Verifica

```bash
curl --fail http://127.0.0.1:5000/api/status
curl --fail http://127.0.0.1:5000/api/health
```

## Come si usa

Tutto si gestisce dalla UI web, all'indirizzo configurato (default
`http://<host>:5000`).

### Primo avvio

1. Apri la UI. Se non esiste ancora una data directory, completa il **setup
   iniziale**.
2. Lascia il daemon in **dry-run** finché non hai configurato le sorgenti: in
   dry-run i download non partono.
3. Quando sei pronto, abilita la **modalità attiva** in
   *Configurazione → Daemon*.

### Configura le sorgenti

In *Configurazione → Sorgenti* aggiungi:

- feed RSS / listing HTML (URL e pagine da seguire);
- indexer Torznab (Jackett/Prowlarr) con pulsante **Verifica**;
- motori di ricerca web e, se serve, l'URL di FlareSolverr;
- filtri contenuto e blacklist.

Nella stessa sezione regoli punteggi, rinomina, percorsi e libtorrent.

Per Jackett usa normalmente l'URL base, ad esempio `http://host:9117`, insieme
alla sua API key. Gextto interroga l'endpoint Torznab e usa `t=caps` nel controllo
salute, così verifica anche la chiave e la disponibilità degli indexer configurati.
Gli errori Torznab vengono riconosciuti anche con risposta HTTP 200; nei risultati
la sorgente può essere indicata come `jackett:NomeTracker`.

### Aggiungi serie e film

- Da **Esplora** (ricerca TMDB) con un clic, oppure
- da **Serie TV / Film → Aggiungi** in manuale.

Per ogni titolo scegli qualità minima, lingua, stagioni/anni, alias, esclusioni e
**percorso NAS**. I fumetti si gestiscono da **Fumetti**.

### Cicli e download

Gextto lavora a cicli: cerca, valuta, scarica, rinomina e archivia.

- Dalla **Dashboard** avvii un ciclo completo, di un solo dominio (Serie, Film,
  Fumetti) o un backup immediato.
- I cicli girano anche in automatico all'intervallo configurato.
- In **Scarico → Sessione torrent** vedi i torrent nel client (con badge **NAS** se
  già archiviati); sotto il nome trovi il **motivo** del download e la **fonte**
  (indexer/RSS/web). **Pulisci completati** rimuove i torrent che hanno raggiunto
  il limite di seed.
- In **Storico download** finiscono i torrent **usciti dalla sessione**, con esito
  (NAS o motivo dell'eventuale scarto).

### Leggere i log

Il log segue sempre il percorso dell'operazione, non mostra l'hash come unica
identità leggibile:

1. **CYCLE STARTED** — indica la modalità e il dominio (`full`, Serie, Film o
   Fumetti).
2. **Step 1/2** e **Step 2/2** — indicano quante sorgenti e quanti titoli vengono
   analizzati; eventuali sorgenti irraggiungibili riportano nome e motivo.
3. **Gap fill** — distingue ciò che è stato trovato nell'archivio da ciò
   che deve essere cercato online.
4. **Download started / Gap filled** — mostra titolo, episodi, sorgente e
   punteggio. Se un candidato viene saltato, il log indica motivo e decisione.
5. **CYCLE REPORT** e **CYCLE DOWNLOADS** — riassumono durata, release raccolte,
   download, upgrade, lacune ed errori.
6. Gli eventi torrent spiegano metadati ricevuti, spostamenti su NAS o dal RAM
   disk, completamento, rinomina, seeding, recovery e rimozione.

Le righe hanno formato `data ora LIVELLO [componente] messaggio · campo: valore`.
Nome o titolo sono sempre presenti nei messaggi torrent; l'hash resta soltanto un
campo tecnico per correlare un errore. `INFO` mostra il percorso normale, `WARN`/
`ERROR` spiegano cosa non è riuscito e quale risorsa è coinvolta, `DEBUG` aggiunge
i dettagli diagnostici quando è abilitato (anche gli scarti ordinari di filtro e
sanità vivono qui).

### Le sezioni della UI

| Sezione | A cosa serve |
|---|---|
| **Dashboard** | Ricerca manuale, avvio cicli, statistiche, rete, prossime uscite |
| **Scarico** | Sessione torrent, aggiunta magnet/.torrent, storico |
| **Serie TV / Film** | Libreria, dettagli, episodi mancanti, ricerca manuale |
| **Mancanti** | Episodi mancanti e riempimento |
| **Calendario** | Prossime uscite dalle serie monitorate |
| **Esplora** | Scoperta TMDB e ricerca release |
| **Archivio** | Release passate; *Visti dal feed* per film/serie |
| **Fumetti** | GetComics e weekly pack |
| **Configurazione** | Sorgenti, libtorrent, punteggi, rinomina, percorsi, notifiche |
| **Integrazioni** | Trakt, Simkl, Jellyfin, Plex, hook eventi |
| **Manutenzione** | Backup, duplicati, scoring, riavvio |
| **Salute / Log / Grafici** | Diagnostica e monitoraggio |

La guida dettagliata di ogni schermata è nel
[manuale](docs/MANUAL.it.md).

### Riga di comando

Il demone parte normalmente come servizio systemd. Eseguito direttamente,
`gexttod` accetta anche queste opzioni:

| Opzione | Cosa fa |
|---|---|
| `-h`, `--help` | mostra il riepilogo d'uso |
| `-V`, `--version` | mostra versione installata, build number e libtorrent inclusa |
| `--config <file>` | usa un file di configurazione specifico (default `gextto.json`) |
| `--dry-run` | avvia senza download reali |
| `tui` | apre la TUI verso un daemon già avviato |
| `--update` | scarica e installa l'ultimo payload (vedi *Aggiornamento di Gextto*) |

Opzioni di `--update`:

| Opzione | Cosa fa |
|---|---|
| `--repo <owner/name>` | repository GitHub da cui scaricare (default `buzzqw/gextto`) |
| `--channel <name>` | `continuous` (default) o `stable` |
| `--release <tag>` | installa un tag di release specifico |
| `--install-dir <dir>` | directory di installazione (default: quella del binario) |
| `--archive <file>` | installa da un archivio locale invece di scaricare |
| `--force` | reinstalla anche se la versione è invariata (ha senso solo con `--release`: i canali rolling `continuous` e `stable` scaricano sempre l'ultimo asset) |
| `--no-restart` | non riavviare `gextto.service` dopo l'installazione |

Esempi:

```bash
/opt/gextto/gexttod --version                         # cosa è installato ora
sudo /opt/gextto/gexttod --update                    # ultima build continua
sudo /opt/gextto/gexttod --update --channel stable   # ultima release con tag
sudo /opt/gextto/gexttod --update --release v0.2.0   # un tag specifico
sudo /opt/gextto/gexttod --update --install-dir /srv/gextto --no-restart
sudo /opt/gextto/gexttod --update --archive ./gextto-linux-x86_64.tar.gz   # offline
```

### TUI da terminale

La TUI è un sottocomando dello stesso binario, ma viene eseguita come processo
separato dal daemon e comunica con esso via HTTP/SSE. Avvia prima il daemon, poi:

```bash
/opt/gextto/gexttod tui                                  # daemon locale
/opt/gextto/gexttod tui --url http://host:5000 --lang it
GEXTTO_URL=http://host:5000 /opt/gextto/gexttod tui
```

Sono disponibili `--url`/`-u` e `--lang`/`-l`; la variabile d'ambiente per l'URL è
`GEXTTO_URL`. Le schede sono
Stato, Torrent, Log, Salute, Archivio, Mancanti e Blocklist. I tasti rapidi e i
prompt sono documentati in [`docs/tui.md`](docs/tui.md).

Per provare un aggiornamento senza toccare un'installazione reale, combina
`--install-dir` con una directory usa e getta e `--no-restart`; `--archive` evita
del tutto la rete.

`gexttod --version` stampa la versione del prodotto, il **numero di build
monotono** generato in fase di build (dai file `VERSION` e `build_number`) e la
libtorrent inclusa. Il `[marker]` opzionale è l'etichetta di release scritta
accanto all'eseguibile dall'installer o dall'archivio (`continuous`, un tag o
`source-main`).

### Dove trovare i dettagli

Il README è la panoramica pratica; il [manuale](docs/MANUAL.it.md) documenta ogni
schermata. Indice rapido:

| Argomento | README | Manuale |
|---|---|---|
| Installazione e servizio | *Installazione* | [1. Primo avvio](docs/MANUAL.it.md#1-primo-avvio) |
| Aggiornamento, versione, pacchetto | *Aggiornamento di Gextto*, *Riga di comando* | [1. Primo avvio](docs/MANUAL.it.md#1-primo-avvio) |
| Primo avvio e modalità | *Primo avvio* | [1. Primo avvio](docs/MANUAL.it.md#1-primo-avvio) |
| Dashboard, cicli, statistiche | *Cicli e download* | [2. Dashboard](docs/MANUAL.it.md#2-dashboard) |
| Torrent, stalled, storico | *Cicli e download* | [3. Scarico](docs/MANUAL.it.md#3-scarico) |
| Serie, episodi, gap | *Aggiungi serie e film* | [4. Serie TV](docs/MANUAL.it.md#4-serie-tv) |
| Film | *Aggiungi serie e film* | [5. Film](docs/MANUAL.it.md#5-film) |
| Esplora, Archivio, Fumetti | *Le sezioni della UI* | [6. Esplora, Archivio, Fumetti](docs/MANUAL.it.md#6-esplora-archivio-fumetti) |
| Sorgenti, punteggi, rinomina | *Configura le sorgenti* | [7. Configurazione](docs/MANUAL.it.md#7-configurazione) |
| Trakt, Jellyfin, hook | *Le sezioni della UI* | [8. Integrazioni](docs/MANUAL.it.md#8-integrazioni) |
| Backup, duplicati, DB | *Le sezioni della UI* | [9. Manutenzione](docs/MANUAL.it.md#9-manutenzione) |
| Salute, log, grafici | *Leggere i log* | [10. Salute, Log, Grafici](docs/MANUAL.it.md#10-salute-log-grafici) |
| Notifiche | *Le sezioni della UI* | [11. Notifiche](docs/MANUAL.it.md#11-notifiche) |
| Problemi comuni | — | [12. Risoluzione problemi](docs/MANUAL.it.md#12-risoluzione-problemi) |

### Dati e log

- Data directory di default: `data/` (modificabile con `GEXTTO_DATA_DIR`).
- Log in `data/gextto.log`, con rotazione a 5 MB (file attivo + 3 backup),
  consultabili in streaming dalla UI web e dalla TUI. I viewer permettono filtro
  e follow/pause.
- Database: `gextto_series.db`, `gextto_archive.db`, `gextto_config.db`,
  `gextto_comics.db`.
- Stato della sessione torrent in `data/gextto_torrents_state/` (fastresume).

### Variabili d'ambiente

| Variabile | Scopo |
|---|---|
| `GEXTTO_DATA_DIR` | Data directory (database, log, download) |
| `GEXTTO_LISTEN` | Indirizzo UI/API (default `127.0.0.1:5000`; il servizio installato usa `0.0.0.0:5000` per default) |
| `GEXTTO_ENGINE_LISTEN` | Canale interno del motore (default `127.0.0.1:8889`) |
| `GEXTTO_UI_DIR` | Directory della UI web compilata (installazioni pacchettizzate) |
| `GEXTTO_INSTALL_DIR` | Directory di installazione usata da `--update` |
| `GEXTTO_REPO` | Repository GitHub usato da installer e `--update` (default `buzzqw/gextto`) |
| `GEXTTO_RELEASE` | Artefatto installer (`continuous` per default o un tag) |
| `GEXTTO_ARCH` | Override architettura asset (`x86_64`; `aarch64` solo se il repository lo pubblica) |
| `GEXTTO_ACTIVE` | `1` abilita i cicli di acquisizione |
| `GEXTTO_DRY_RUN` | `1` disabilita i download reali |
| `GEXTTO_LOG` | Filtro log (`info`, oppure `debug` quando il debug è attivo) |
| `GEXTTO_CHANNEL` / `GEXTTO_VERSION` | Canale dell'updater (`continuous`, `stable`) o un tag di release specifico |
| `GEXTTO_PORT` / `GEXTTO_ENGINE_PORT` | Installer: porta UI/API (default `5000`) e porta motore (default `8889`) |
| `GEXTTO_USER` | Installer: utente di servizio da creare/usare (default `gextto`) |
| `GEXTTO_SKIP_PACKAGES` | Installer: `1` salta l'installazione dei pacchetti di sistema |

## Uso delle risorse

- La logica del daemon è trascurabile: con libtorrent disabilitato un'istanza
  senza sessione sta a **0% CPU e ~30 MB RSS** (misurato) e l'heap Go resta
  nell'ordine di pochi MB. Con libtorrent attivo, CPU e RAM sono spese quasi
  interamente dal motore quando ha torrent (DHT, announce ai tracker, peer e
  cache su disco), non dai loop del daemon.
- **RAM**: i consumatori principali sono la cache disco di libtorrent
  (`cache_size`, in blocchi da 16 KiB) e il budget di scritture in coda
  (`max_queued_disk_bytes`). Il daemon chiama `malloc_trim` dopo i
  completamenti, dopo ogni ciclo e ogni 15 minuti, così la memoria dell'arena
  liberata torna al sistema invece di restare al picco del download. RAM
  totale/libera in `/api/health`; `/api/system/lt_mem_suggest` suggerisce i
  valori per la macchina.
- **CPU**: la coda è dinamica (`libtorrent_dynamic_queue`), i torrent lenti o a
  0 B/s non occupano slot attivi (`dont_count_slow_torrents`) e i torrent
  stalled vengono messi in pausa e ritentati invece di girare a vuoto.
  *Configurazione → libtorrent → **Ottimizza*** (o `libtorrent_auto_optimize`)
  dimensiona cache, buffer e coda sull'hardware. Lo snapshot dei torrent è
  calcolato una volta per tick del worker (cache breve) invece di una query di
  stato per torrent a ogni loop.
- Parametri utili: `connections_limit`, `aio_threads`, `active_downloads` /
  `active_seeds`, `mixed_mode_algorithm`, `cache_size`,
  `max_queued_disk_bytes`. Meno torrent attivi o morti significano meno traffico
  DHT/tracker e CPU/RAM più basse.

## Affidabilità

- **Isolamento dei panic**: i worker in background girano sotto un watchdog che
  li registra e li riavvia se vanno in panic; gli handler HTTP restituiscono un
  `500` pulito invece di interrompere la connessione. Un difetto non può
  abbattere il daemon.
- **Controlli di integrità al completamento**: prima che un download finito sia
  rinominato e registrato come archiviato, il file viene validato (non vuoto,
  non riempito di zeri, contenitore video riconosciuto). Un file corrotto o
  troncato viene messo in quarantena e il torrent segnato come fallito, così non
  sostituisce mai una copia buona.
- **Nessuna azione prematura**: politica di seed, spostamento, pausa e rimozione
  si applicano solo a torrent realmente completati (byte verificati, non uno
  stato momentaneo). Uno spostamento che lascia un download incompleto in pausa
  lo riprende automaticamente.
- **Ricontrollo dei dati all'avvio**: un torrent che il fastresume ripristina
  come completo ma i cui file sono assenti o pieni di zeri viene ricontrollato
  forzatamente all'avvio, così un file spostato o troncato viene riscaricato
  invece di essere considerato valido.
- **Staging sicuro su RAM disk**: un download viene spostato dal RAM disk in base
  ai byte **ancora da scrivere**, non alla dimensione totale, così un torrent
  quasi completo non viene spostato a metà trasferimento.
- **Nessuna chiamata in uscita non richiesta**: gli endpoint di integrazione
  (Trakt, Simkl) restituiscono un errore chiaro prima di qualunque richiesta di
  rete quando non sono configurati.
- **Test BitTorrent reali**: `make test-real` avvia un seeder, un tracker e un
  leecher locali e verifica un trasferimento reale byte per byte, senza rete
  esterna. La suite completa è `make test`.

## Sviluppo

### Compila, prova ed esegui

```bash
make build       # build del demone per produzione (bin/gexttod)
make fast        # build rapida senza trimpath/ldflags
make vet         # go vet
make test        # CGO_ENABLED=1 go test ./...
make test-real   # trasferimenti locali ermetici seeder/tracker/leecher
make fmt         # gofmt -w .
```

`scripts/acceptance.sh` esegue il collaudo isolato descritto in
[`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md): usa una data directory temporanea e
porte dedicate in dry-run, quindi non tocca mai un'installazione reale.

L'integrazione continua (`.github/workflows/`) esegue `go vet` e l'intera suite
di test a ogni push e pull request, più l'analisi CodeQL; le release con tag e la
build continua vengono impacchettate per Linux x86_64 e pubblicate
automaticamente. In CI il numero di build è il numero di esecuzione del
workflow, così ogni payload pubblicato è distinto.

### Packaging

[`scripts/package-linux.sh`](scripts/package-linux.sh) produce l'archivio
autonomo usato dall'installer e da `gexttod --update`:

```bash
make build
scripts/package-linux.sh --binary bin/gexttod
# -> gextto-linux-<arch>.tar.gz + gextto-linux-<arch>.tar.gz.sha256
```

L'archivio contiene `gexttod`, la libtorrent in `lib/`, il launcher `run.sh` e un
breve README. La UI web è inclusa nel binario e il demone è linkato con rpath
`$ORIGIN/lib`, quindi parte direttamente dall'archivio estratto; `install.sh`
copia lo stesso payload in `/opt/gextto`.

Per provare l'updater in locale senza sostituire il binario del checkout,
puntalo a una directory usa e getta usando l'archivio appena creato:

```bash
scripts/package-linux.sh --output /tmp/gextto-linux-x86_64.tar.gz
mkdir -p /tmp/gextto-install
bin/gexttod --update --archive /tmp/gextto-linux-x86_64.tar.gz \
  --install-dir /tmp/gextto-install --no-restart
/tmp/gextto-install/gexttod --version
```

### Come sono fatte le parti

| Componente / risorsa | Ruolo | Funzioni che abilita |
|---|---|---|
| Go + `net/http` (`web.go`, `web_router.go`) | runtime del demone e server HTTP | cicli, API REST, stream SSE dei log, UI statica |
| UI web inclusa (`webui/`, `//go:embed`) | front-end single-page | dashboard, schermate libreria, impostazioni, UI bilingue |
| SQLite (`modernc.org/sqlite`, Go puro) | persistenza locale | serie/episodi, film, archivio, fumetti, config, statistiche cicli, metadati torrent |
| libtorrent (`libtorrent_bridge.cpp`, `libtorrent_cgo.go`, `libtorrent.go`) | motore BitTorrent integrato | coda e limiti, politica di seeding, tracker, file, peer, fastresume, killswitch VPN |
| `rss.go`, `websearch.go`, `httpx.go` | acquisizione sorgenti | feed RSS/HTML, indexer Torznab, motori web, fallback FlareSolverr |
| `tmdb.go`, `tvdb.go` | provider metadati | locandine, stagioni, date episodi, scoperta |
| `mediainfo.go` | ispezione reale dei file | dati codec/HDR/audio/lingue usati nei confronti di upgrade |
| `integrations.go` | integrazioni media server | Trakt, Simkl, Jellyfin, Plex |
| `notifier.go` | notifiche | avvisi di completamento/errore, webhook HMAC, hook eventi |
| `backup.go` | backup programmati | snapshot di database e configurazione (locale, FTP, cloud, Telegram) |
| `parser.go`, `decision.go`, `config.go`, `internal/rules` | logica di dominio | parsing release, punteggio qualità, controlli di sanità, upgrade, tracce decisionali |
| `cli.go`, `update.go` | operazioni | `--version`/`--help`, download release con checksum, swap atomico del payload e rollback |
| installer + packaging + systemd | operazioni | install da sorgente/release, unità di servizio, archivio autonomo |
| `importer.go` | CLI di migrazione | import una tantum dei database di un'installazione precedente |

Le build di sviluppo restano piccole e non crescono all'infinito:

- Le build sono incrementali e la cache dei moduli è condivisa, quindi `make fast`
  è rapido; `go clean -cache` riparte da zero quando serve.
- Gli script di supporto per lo sviluppo sono intenzionalmente locali e ignorati
  da Git; non servono all'installer né a un'installazione di produzione.

## Sicurezza

La porta web è un'interfaccia amministrativa senza autenticazione. Legala al
loopback oppure limita l'accesso con firewall/reverse proxy prima di esporla in
rete. Vedi [SECURITY.md](SECURITY.md) per il modello di rete, le garanzie del
daemon (nessuna shell, SQL parametrizzato, richieste limitate, aggiornamenti
atomici) e come segnalare una vulnerabilità.

## ❤️ Sostieni il progetto

Gextto è software libero e open-source, costruito interamente nel tempo libero.
Se ti fa risparmiare ore di configurazione, RAM o usura del disco, considera di
offrire un caffè all'autore.

Ogni donazione finanzia direttamente nuove funzionalità, correzioni di bug e la
sopravvivenza del progetto.

<div align="center">

[![Dona con PayPal](https://img.shields.io/badge/Dona-PayPal-00457C?style=for-the-badge&logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

*Grazie. Sul serio.*

</div>

## ⚖️ Uso lecito & responsabilità

Gextto è uno **strumento di automazione dei download**. Non ospita, non indicizza
e non distribuisce alcun contenuto protetto da copyright.

- Gextto si connette agli **indexer che configuri tu** (Jackett, Prowlarr, feed
  RSS pubblici). Non ha un indice integrato.
- Ciò che scarichi è **interamente sotto la tua responsabilità**. Usa Gextto solo
  per contenuti che hai il diritto di accedere — dominio pubblico, licenze
  Creative Commons, o media di tua proprietà.
- L'integrazione torrent (libtorrent) è una tecnologia neutrale. Gextto non
  incoraggia né facilita la pirateria.
- Questo progetto è rilasciato sotto licenza open-source **EUPL 1.2**.

> *"Con grande automazione viene grande responsabilità."*

## Licenza

Rilasciato sotto **European Union Public Licence v. 1.2** — vedi [`LICENSE`](LICENSE).
