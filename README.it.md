# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** è un demone self-hosted che
cerca, seleziona, scarica, verifica, rinomina e archivia serie TV, film e
fumetti. L'interfaccia web è il piano di controllo; il demone continua a
funzionare come servizio.

> **English:** [README.md](README.md) · **Manuale:**
> [italiano](docs/MANUAL.it.md) · **Segnalazioni e proposte:**
> [GitHub Issues](https://github.com/buzzqw/gextto/issues) ·
> **Contribuire:** [guida sviluppatori](docs/DEVELOPERS.md)

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Gextto Media Orchestrator                       │
│  Decision Engine · Parse & Scoring · Multi-Indexer · Post-Processing   │
│  UI Server-Side (HTMX + SSR) · TUI Terminale · Integrazioni Media      │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Contratto engine (torrent_engine.go)
┌───────────────────────────────────▼────────────────────────────────────┐
│                  gx-torrent (Demone standalone/gestito)                │
│  Web UI dedicata · qBittorrent Web API v2 (Sonarr/Radarr) · Feed RSS   │
│  Servizio Linux (systemd) e Windows nativo (NTFS Junctions)            │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│                    gx-core Engine (internal/gxcore)                    │
│  BitTorrent v1 / v2 (BEP 52 Merkle) · uTP (BEP 29) · Holepunch (BEP 55)│
│  DHT Dual-stack IPv6 (BEP 32/42/44) · Streaming Readahead · MSE In-Place│
│  Zero CGO · Memory footprint: ~25 MB idle / ~100 MB picco              │
└────────────────────────────────────────────────────────────────────────┘
```

## Cosa offre Gextto

### 🎯 Automazione intelligente e Decision Engine
- **Valutazione meticolosa delle release:** assegna un punteggio deterministico a ogni candidato valutando qualità video, sorgente, codec, audio, HDR/DV, lingua e dimensione. Protegge i file migliori già archiviati e registra nei log il motivo esatto di ogni scarto. Una soglia configurabile ferma gli upgrade quando il file raggiunge lo standard desiderato; `ffprobe` può analizzare i file su disco per affinare le decisioni.
- **Fumetti:** per i fumetti CBZ genera automaticamente metadati `ComicInfo.xml` compatibili con Komga, Kavita e i reader standard.
- **Recupero dai download bloccati (Anti-Stall):** un torrent fermo da tempo (nessun byte) o un magnet in attesa prolungata dei metadati viene affiancato da un'alternativa valida dopo una finestra temporale definita (*Configurazione → Qualità e upgrade → Download bloccati*), mentre il trasferimento originale continua a tentare.

### 🛡️ Integrità della Libreria e Storage Safety
- **Hardlink immediati durante il seed:** quando download e libreria risiedono sullo stesso filesystem, il file entra istantaneamente in archivio tramite hardlink, rendendolo subito fruibile senza duplicare lo spazio occupato su disco.
- **Protezione mount point e NAS:** rileva tempestivamente la disconnessione di una condivisione di rete (NAS smontato), evitando di scambiare l'assenza temporanea per file eliminati; gestisce retry automatici trasparenti su timeout o dischi pieni e riprende da solo i trasferimenti interrotti al riavvio.
- **Cestino di sicurezza:** le versioni sostituite da un upgrade reale vengono spostate nel cestino configurato prima dell'eliminazione definitiva.

### 🔌 Fonti, Integrazioni e Controllo Operativo
- **Sorgenti flessibili:** feed RSS, indexer Torznab (Jackett, Prowlarr), motori web dedicati e supporto opzionale a FlareSolverr.
- **Notifiche e Media Server:** notifica gli eventi via Telegram, e-mail o webhook (Discord, Slack, ntfy, Gotify, Pushover), inclusi riavvii di Gextto e anomalie di gx-torrent; invia refresh mirati a Jellyfin e Plex toccando solo la cartella modificata ed esporta un calendario iCal con arrivi e uscite.
- **Tracciabilità completa dei download:** ogni operazione ha un **ID di acquisizione** univoco tracciato in tutti i log (ricerca, selezione, download, post-processing e archivio); la dashboard **Da controllare** raccoglie i casi anomali, e gli avvisi a soglia segnalano tempestivamente se una sorgente fallisce tre cicli consecutivi o rallenta.
- **Interfaccia Web SSR + HTMX e TUI da terminale:** UI responsive fruibile su desktop e smartphone (PWA), navigabile interamente da tastiera e verificata con axe-core per l'accessibilità; TUI nativa completa per l'amministrazione via SSH. Disponibile in 6 lingue (IT, EN, DE, FR, ES, PL).

La UI è disponibile su `http://<host>:5000/`. Il [manuale italiano](docs/MANUAL.it.md) e l'[English manual](docs/MANUAL.en.md) spiegano la configurazione e tutte le sezioni dell'interfaccia.

## Motori torrent e l'Engine gx-core

Gextto conserva il piano di controllo (coda, punteggi, post-processing, archivio) e delega il trasferimento dati a un motore selezionabile in *Configurazione → Motore torrent*. Un solo motore gira alla volta: il passaggio da uno all'altro è una migrazione pulita e coordinata, senza conflitti di I/O o processi concorrenti sugli stessi file.

### gx-core & gx-torrent (Predefinito e consigliato)

**gx-torrent** è il motore predefinito ed è compilato in **Go puro** (`CGO_ENABLED=0`), senza alcuna dipendenza C/C++. Gira in un processo separato sorvegliato da Gextto, sopravvive ai riavvii dell'orchestratore, dispone di una propria interfaccia web autonoma ed è dove arrivano per prime le innovazioni di protocollo.

Il suo nucleo, **`gx-core`** (`internal/gxcore`), è un motore BitTorrent ad altissime prestazioni mantenuto direttamente in questo repository (con porzioni derivate con licenza MIT/BSD; vedi `internal/gxcore/LICENSE`). Offre un supporto di protocollo completo e all'avanguardia con prestazioni elevate:

- **BitTorrent v1, v2 e Ibridi (BEP 52):** supporto completo per v1, v2-only e ibridi sia da file `.torrent` che da magnet (`urn:btmh:`). Utilizza un modello a pezzi per-file (nessun padding block tra file diversi), verifica crittografica basata su alberi Merkle SHA-256 e fetch dinamico dei `piece layers` direttamente dai peer (`hash request`, `hashes`, `hash reject`).
- **Trasporto uTP (BEP 29) e Single-Port Listening:** motore dual-transport con esecuzione parallela di TCP e uTP e fallback automatico. Un unico socket UDP gestisce simultaneamente il trasporto uTP e il traffico DHT sulla medesima porta, con multiplexing delle connessioni e instradamento dell'handshake in base all'info-hash così che tutti gli swarm annuncino la stessa porta.
- **NAT Traversal con Holepunching (BEP 55):** implementazione completa dell'estensione `ut_holepunch` per stabilire connessioni punto-punto dirette su uTP attraverso NAT simmetrici e firewall senza necessità di aprire porte sul router, con supporto sia come client attivo sia come nodo di relè per altri peer.
- **Port Mapping e Integrazione di Rete:** apertura automatica delle porte sul router via UPnP e NAT-PMP, supporto proxy SOCKS5 e HTTP CONNECT, e killswitch vincolato all'interfaccia (`outgoing-interface`) per confinare tutto il traffico BitTorrent ai soli tunnel VPN.
- **Super-seeding (BEP 16) e Choking Deterministico:** harness di simulazione deterministico dell'unchoker per ottimizzare throughput ed equità; supporto super-seeding per distribuire nuovi torrent annunciando un pezzo alla volta ed evitando upload ridondanti nel seed iniziale.
- **Streaming Multimediale nel Browser e Priorità Graduata:** prioritizzazione in tempo reale per la riproduzione video: il piece picker distingue tra la finestra immediata del player (`SetStreamWindow`) e il margine di avanzamento (`SetReadaheadWindow`), privilegiandoli rispetto alle strategie rarest-first e servendo file video MKV/MP4 direttamente al browser o a VLC via richieste HTTP Range.
- **Crittografia MSE/PE Zero-Alloc:** stream cipher RC4 eseguito direttamente in-place sul buffer di memoria chiamante (`inPlaceStreamWriter`), eliminando le allocazioni per blocco durante l'upload.
- **Accelerazione Hardware e Coda Adattiva:** conteggio del bitfield accelerato con istruzioni native della CPU (`math/bits.OnesCount64`); profondità della coda di richieste per peer (`RequestQueue`) modulata dinamicamente in base alla velocità di consegna dei blocchi e azzerata su choke/snub.
- **Architettura di Memoria e Disco ad Alte Prestazioni:** cache di lettura dinamica in RAM con sweeper a scadenza lazy, limitatore del buffer di scrittura, allocazione file sparsi e preallocazione con `fallocate` (con protezione su tmpfs), e supporto per RAM disk per download temporanei.
- **Filtro IP a Ricerca Binaria Ottimizzato:** supporta formati CIDR, intervalli, P2P ed eMule `.dat` fusi in intervalli ordinati e non sovrapposti in memoria, eseguendo lookup a ricerca binaria senza allocazioni con footprint trascurabile (~byte per regola).
- **Modulo DHT Avanzato (`internal/dht`):** modulo isolato con supporto dual-stack IPv6 (BEP 32), Security Extension anti-spoofing con CRC32C Castagnoli (BEP 42), K/V store immutabile e mutabile firmato ed25519 (BEP 44), modalità read-only per nodi di transito (BEP 43) e tracker scrape (BEP 48 HTTP/UDP).
- **Protezione Swarm, Diagnostica e Resilienza:** conformità rigorosa a BEP 27 per torrent privati (disattiva automaticamente DHT, PEX in/out e LSD); esclusione temporanea (30 min) di sessione per peer che trasmettono blocchi corrotti; WebSeed (BEP 19); diagnostica per-pezzo; resume persistente (BoltDB); diagnosi granulare degli errori di storage; e bus eventi live Server-Sent Events (`/ui/events`).
- **Multipiattaforma e Client Standalone:** eseguibile sia come motore interno di Gextto sia come client autonomo (`gx-torrent`) dotato di propria Web UI, API compatibile qBittorrent (`/api/v2`), regole RSS con smart episode tracking, demone systemd su Linux e servizio nativo Windows Service su Windows (amd64/arm64).

### Motori alternativi

- **qBittorrent-nox:** l'alternativa per chi lo usa già o preferisce la sua interfaccia nativa: Gextto lo pilota via Web API.
- **libtorrent (build opzionale):** disponibile **solo se compili Gextto con libtorrent** (`make build-libtorrent`, `GEXTTO_LIBTORRENT=1`): gira all'interno del processo di Gextto ed è anche il fallback automatico se gx-torrent non riesce a restare attivo. Un crash del motore coinvolge anche Gextto.

| Motore | Dove gira | Quando sceglierlo |
|---|---|---|
| gx-torrent (predefinito) | Processo Go separato e sorvegliato | Il predefinito: Go puro, autonomo, sviluppo attivo. BitTorrent v1, ibridi e solo-v2 (BEP 52, `.torrent` e magnet), DHT (IPv6, security BEP 42, read-only, scrape) e cifratura MSE/PE; usabile anche da solo su Linux e Windows (vedi *gx-torrent come client autonomo*); niente WebTorrent/WebRTC |
| qBittorrent-nox | Demone esterno, pilotato via Web API | Hai già qBittorrent-nox o preferisci la sua UI. Servono le mappature percorsi e un processo in più |
| libtorrent (build opzionale) | Nel processo di Gextto; **solo se l'hai compilato** (`make build-libtorrent`) | Ti servono i controlli avanzati o la compatibilità di libtorrent; è anche il fallback automatico. Un crash coinvolge anche Gextto |

Come includere libtorrent: vedi *Installazione dal sorgente* più sotto.

Le statistiche del motore attivo — torrent per stato, velocità e slot,
porta/router, DHT/uTP/LSD, cifratura, proxy, filtro IP, cache, **connessioni in
entrata** e **I/O disco** (job in coda, letture/scritture totali) con i totali di
sessione — sono nel pannello **Salute → Motore torrent** (`/?view=health`); gli
stessi contatori sono in `GET /api/v1/stats` del demone e in
`GET /api/libtorrent/session-stats`.

## Uso delle risorse

Gextto è progettato per restare leggero quando è inattivo: è un solo demone (più
il piccolo processo `gx-torrent` sorvegliato quando quel motore è attivo), limita
la concorrenza delle sorgenti e delle attività in background e usa il backoff per
evitare tentativi continui verso provider in errore. Il consumo effettivo di CPU
e RAM dipende da titoli monitorati, sorgenti, torrent attivi e scansioni
dell'archivio; le misurazioni su una singola macchina sono indicative, non una
garanzia.

**Memoria del motore torrent, misurata.** Con 50 torrent reali e ~100 MB/s di
download su una macchina da 16 GB, l'impronta del motore cambia molto:

| Motore | Idle (torrent caricati) | Picco durante il trasferimento |
|---|---:|---:|
| gx-torrent | ~25 MB | ~100 MB |
| qBittorrent-nox | ~40 MB | ~5 GB |
| libtorrent (nel processo di Gextto, build opzionale) | ~0,5 GB | 3–5 GB |

libtorrent e qBittorrent tengono una grande cache disco in-process (GB di
memoria anonima). gx-torrent **non ha una grande cache write-back in-process, per
scelta**: scrive i pezzi in streaming e lascia alla page cache del kernel
(reclamabile) il compito di fondere le scritture, quindi la sua impronta resta
nelle decine/centinaia di MB anche sotto carico; non tiene dati in RAM solo per
occuparla. I valori variano con torrent, peer e storage; su HDD/NFS Gextto dà a
gx-torrent un buffer più grande, usato solo quando le scritture restano
indietro rispetto al download.

## Installazione Linux

L'installer ufficiale è destinato a server Linux 64 bit (x86_64 o aarch64) con
systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE Leap 15.6+/Tumbleweed e
Arch Linux. Eseguilo come root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa il programma in `/opt/gextto`, conserva i dati del servizio in
`/var/lib/gextto` ed espone la UI sulla porta 5000. Il motore torrent
predefinito, `gx-torrent` (Go puro), viene installato accanto a `gexttod` e
avviato in modalità gestita. Il pacchetto di release è **Go puro**: non serve
installare altro. In alternativa si può usare un qBittorrent-nox esterno; il
motore libtorrent richiede una build propria (`make build-libtorrent`, vedi più
sotto).

Prima di toccare il sistema l'installer avvia una volta il programma scaricato:
se mancano librerie o la glibc è troppo vecchia lo dice subito, senza lasciare un
servizio che non parte. Il servizio gira per impostazione predefinita con
**l'utente locale che esegue l'installer**, così può creare cartelle nella tua
libreria senza permessi aggiuntivi; `--user gextto` usa invece l'account di
sistema isolato e senza login. Se la libreria è su un NAS di proprietà di un
altro utente, `--media-group <gruppo>` aggiunge l'utente del servizio a quel
gruppo.

> [!IMPORTANT]
> Gextto è pensato per una rete fidata: per impostazione predefinita la UI web e
> l'API sono aperte. Se lo raggiungi da fuori casa attiva il login facoltativo
> (*Configurazione → Accesso e servizi*, la rete locale resta libera) e usa comunque HTTPS
> tramite reverse proxy o VPN. Leggi prima la
> [politica di sicurezza](docs/SECURITY.md).

### Installazione dal sorgente

Serve Go 1.26.8+; la build predefinita è in **Go puro**. Solo per includere il
motore opzionale **libtorrent** servono anche un compilatore C++17 e gli header
di sviluppo libtorrent-rasterbar.
La build normale include la UI web: non è richiesto un build frontend separato.
Consulta il [manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build                 # Go puro (predefinita), con gx-torrent
make build-libtorrent      # include anche il motore opzionale libtorrent
```

`make build` incrementa il numero di build **committato** (`build_number`, la
stessa sorgente condivisa con la CI: checkout e versione installata mostrano lo
stesso `1.1.<n>`) e scrive il demone
versionato in `bin/gexttod`, insieme al motore `gx-torrent` in puro Go. Per
ricompilare senza incrementare il numero usa `make fast` (o
`make fast-libtorrent` per la variante con libtorrent). Per compilare **solo**
il demone `gx-torrent` usa `make gx-torrent`. Verifica binario, versione del
prodotto, numero di build e, se collegata, la versione di libtorrent con:

```bash
./bin/gexttod --version
```

Il demone `gx-torrent` ha un **proprio** contatore di build, separato da quello
di Gextto: `make build` e `make gx-torrent` fanno avanzare
`gx-torrent.build_number` (tramite `scripts/next-gx-build-number.sh`) solo
quando il suo codice cambia davvero. È il numero che `./bin/gx-torrent --version`
e l'endpoint `/api/v1/health` del demone riportano come `1.1.<n>`.

## gx-torrent come client autonomo

gx-torrent è anche un client BitTorrent completo a sé, utilizzabile senza Gextto
esattamente come `qbittorrent-nox` o `transmission-daemon`. È **lo stesso
binario** che Gextto pilota come motore, non un prodotto separato:

- **Compatibilità Drop-in con l'ecosistema `*arr`:** implementa l'**API Web
  v2 di qBittorrent** (`/api/v2/...`), integrandosi nativamente e senza bridge
  con Sonarr, Radarr, Lidarr, Readarr e con le app mobili per la gestione torrent.
- **API JSON nativa e login:** espone una propria API JSON (`/api/v1/...`), la
  stessa con cui Gextto lo pilota, più una pagina di accesso (`/ui/login`) quando
  è attiva l'autenticazione standalone.
- **Interfaccia Web Indipendente e Wizard di Setup:** include una UI web completa
  con grafici e diagnostica in tempo reale, gestione categorie e tag, download
  automatico da feed RSS con regole e tracking smart degli episodi, e ricerca
  diretta sugli indexer (Jackett, Prowlarr, MIRCrew).
- **Architettura Multipiattaforma:**
  - **Linux (x86_64, aarch64):** pacchetto standalone `tar.gz`, unit systemd inclusa
    (utente o sistema) e container Docker non privilegiato (`Dockerfile`).
  - **Windows nativo (amd64, arm64):** gira come **servizio Windows nativo**
    gestito da script PowerShell (`install-service.ps1` / `uninstall-service.ps1`).
    Sfrutta le API Win32 per memoria, tipologia disco e gateway di default,
    e gestisce i percorsi tramite **NTFS Junctions** (nessun bisogno di permessi
    amministratore per i symlink né di Modalità sviluppatore).

Ogni release pubblica pacchetti dedicati accanto a quelli di Gextto:

| Piattaforma | Pacchetto | Come gira |
|---|---|---|
| Linux (x86_64, aarch64) | `gx-torrent-linux-<arch>.tar.gz` | `run.sh` o la unit systemd inclusa |
| Windows (amd64, arm64) | `gx-torrent-windows-<arch>.zip` | Servizio Windows nativo, installato da `install-service.ps1` |
| Container | `Dockerfile` nella radice del repository | Utente non privilegiato, dati in `/data`, UI sulla porta 8080 |

**Windows.** Estrai lo zip ed esegui `install-service.ps1` da un PowerShell
**con privilegi di amministratore**: registra e avvia il servizio `gx-torrent`
(avvio automatico), con dati e log in `%ProgramData%\gx-torrent`.
`uninstall-service.ps1` lo rimuove; il `README.txt` incluso descrive anche
l'alternativa `sc create`. Su Windows gx-torrent usa le API native per memoria,
tipo di disco, spazio libero e gateway di default (UPnP/NAT-PMP) e collega ogni
torrent con una **junction**, quindi non servono né il privilegio dei link
simbolici né la Modalità sviluppatore. Su Windows gira solo gx-torrent; Gextto
resta solo Linux.

**Primo avvio.** Apri `http://<host>:8890/`: il wizard guida lingua, cartelle
di download e temporanea (con un **selettore di cartelle** che sfoglia il
server), porta peer con test di raggiungibilità, limiti di banda globali, utente
e password, accesso dalla LAN e indexer facoltativi. Scegliendo Prowlarr o
Jackett l'URL dell'indexer viene riempito con il suo solito indirizzo locale e
porta (comunque modificabile); ogni altro servizio Torznab si aggiunge come tipo
personalizzato. La pagina collega a **RSS e indexer**, dove si gestiscono senza
modificare il JSON a mano la lista *Cercati* (i titoli da cercare, ricercati
negli indexer e nei feed configurati, con gli episodi mancanti cercati di nuovo)
e i feed, le regole e gli indexer — e a
**Impostazioni**, il pannello del client: cartelle, rete e porte, protocolli
(cifratura, DHT/uTP/LSD…), coda e cache, banda e scheduler giornaliero, lingua e
notifiche. In
una sessione desktop il browser si apre da solo sul wizard; su un server senza
grafica o come servizio Windows il log stampa l'indirizzo da aprire.

Il riferimento completo (flag, `settings.json`, API, note di piattaforma) è il
[documento tecnico di gx-torrent](docs/gx-torrent.md).

## Primo avvio sicuro

1. Apri `http://<server>:5000`: al primo avvio parte la **configurazione
   guidata** (password, cartelle con verifica dei permessi, indexer e TMDB,
   primo titolo, attivazione). Puoi saltarla e riaprirla da `/?view=setup`.
2. Configura percorsi, una sorgente e, se necessarie, credenziali TMDB/TVDB.
3. Mantieni il **dry-run**, aggiungi un titolo di prova ed esegui una ricerca o
   un ciclo.
4. Controlla **Salute** e **Log**, inclusi permessi filesystem e risultati della
   sorgente.
5. Abilita la modalità attiva solo quando l'esito è corretto.

Checklist, configurazione NAS e diagnostica sono nel
[manuale utente](docs/MANUAL.it.md).

## Aggiornamento e disinstallazione

Con l'installazione ufficiale, quando esce una nuova versione compare il
pulsante **Aggiornamento disponibile** sopra **Dona**: in *Manutenzione →
Aggiornamenti* vedi la versione installata, le novità (i commit) e il pulsante
**Aggiorna ora**. Prima dell'aggiornamento viene fatto un backup dei database;
se la nuova versione non parte, torna da sola la precedente. Il controllo
(una piccola richiesta a GitHub ogni 6 ore) si disattiva in *Configurazione →
Sistema → Aggiornamenti*.

In alternativa ripeti l'installer. Se disponibile verifica il
checksum della release, sostituisce il binario in modo atomico, riavvia il
servizio e, se la nuova versione non parte, torna alla precedente. Dati e
configurazione restano intatti.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer accetta anche queste opzioni:

```bash
sudo bash install.sh --help       # tutte le opzioni
sudo bash install.sh --dry-run    # mostra cosa farebbe, senza modificare nulla
sudo bash install.sh --uninstall  # ferma e rimuove il programma (i dati restano)
sudo bash install.sh --uninstall --purge   # rimuove anche la directory dati
```

Le variabili d'ambiente (`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, …)
restano supportate. `GEXTTO_LOCAL_ARCHIVE=/percorso/gextto-linux-x86_64.tar.gz`
installa da un pacchetto locale (offline o CI).

Da un checkout sorgente:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

Lo script di aggiornamento esegue la build versionata, riavvia il servizio
rilevato e lascia intatti dati e configurazione. Usa
`./scripts/update.sh --no-restart` se vuoi riavviare manualmente.

## Accessibilità

La UI web include navigazione da tastiera, nomi accessibili per i controlli,
gestione del focus nei dialoghi, tabelle ordinabili semantiche, annunci live per
gli aggiornamenti dinamici, reflow responsive e contrasto migliorato nel tema
chiaro. Le regressioni vengono controllate con axe-core e Playwright:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

La suite automatica attuale copre le sezioni principali della UI con 13 test di
accessibilità. Questo non costituisce da solo una certificazione normativa:
servono ancora test manuali con screen reader, tastiera e tecnologie assistive.
Consulta l'[analisi di accessibilità](docs/evoluzione.md#13-accessibilità) per ambito e
limitazioni note.

## Documentazione

| Esigenza | Documento |
|---|---|
| Usare il servizio | [Manuale italiano](docs/MANUAL.it.md) · [English manual](docs/MANUAL.en.md) |
| NAS, reverse proxy, ripristino | [Guida avanzata](docs/ADVANCED.it.md) |
| Integrazioni HTTP | [Riferimento API](docs/API.md) |
| Migrare un'installazione | [Guida migrazione](docs/MIGRATION.md) |
| Client da terminale | [Riferimento TUI](docs/tui.md) |
| Motore gx-torrent e client autonomo | [Documento tecnico di gx-torrent](docs/gx-torrent.md) |
| Compilare o contribuire | [Manuale sviluppatori](docs/DEVELOPERS.md) |
| Rete e protezione dati | [Politica di sicurezza](docs/SECURITY.md) |
| Ambito e test accessibilità | [Analisi accessibilità](docs/evoluzione.md#13-accessibilità) |

Per l'intera struttura documentale parti dall'[indice della documentazione](docs/README.md).

## Supporta il progetto

Se Gextto ti è utile e vuoi sostenere il suo sviluppo, puoi fare una donazione
tramite PayPal. Grazie! Un pulsante **Dona** è anche sempre visibile nella UI,
in basso a destra.

[![Dona con PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## Uso corretto e fair use

Gextto è uno strumento di automazione dei download: non ospita, indicizza né
distribuisce contenuti protetti da copyright.

- Si collega alle sorgenti configurate dall'utente (per esempio Jackett,
  Prowlarr o feed RSS pubblici); non include un indice incorporato.
- L'utente è responsabile dei contenuti scaricati. Usa Gextto solo per contenuti
  a cui hai diritto di accedere, come opere di pubblico dominio, con licenza
  Creative Commons o media di tua proprietà.
- BitTorrent, e i motori che Gextto pilota, sono una tecnologia neutrale; il progetto non
  incoraggia né facilita la pirateria.
- Il progetto è distribuito con licenza open source **EUPL 1.2**.

## Licenza

Gextto è distribuito con [EUPL-1.2](LICENSE). I componenti di terze parti sono
indicati in [NOTICE](NOTICE).
