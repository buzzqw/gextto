# Gextto — EXpert Torrent Transfer Orchestrator

**Di' a Gextto cosa vuoi vedere. Trova la release migliore, la scarica, la
controlla, le dà il nome giusto e la archivia nella tua libreria — poi continua
a cercarne una copia migliore.** Un solo demone self-hosted, un'interfaccia web
veloce e un motore BitTorrent proprio, scritto da zero in Go puro.

[![Licenza: EUPL-1.2](https://img.shields.io/badge/licenza-EUPL--1.2-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-puro%2C%20zero%20CGO-00ADD8?logo=go)
![Piattaforme](https://img.shields.io/badge/piattaforme-Linux%20%C2%B7%20Windows%20(gx--torrent)-informational)
![BEP](https://img.shields.io/badge/BEP%20BitTorrent-34%20implementati-success)
![Lingue](https://img.shields.io/badge/UI-IT%20%C2%B7%20EN%20%C2%B7%20DE%20%C2%B7%20FR%20%C2%B7%20ES%20%C2%B7%20PL-lightgrey)

> **English:** [README.md](README.md) · **Manuale:**
> [italiano](docs/MANUAL.it.md) · **Segnalazioni e proposte:**
> [GitHub Issues](https://github.com/buzzqw/gextto/issues) ·
> **Contribuire:** [guida sviluppatori](docs/DEVELOPERS.md)

## Perché Gextto

- **Decide come un esperto, e ti dice perché.** Ogni release candidata riceve un
  punteggio su risoluzione, sorgente, codec, audio, HDR/Dolby Vision, lingua e
  dimensione; vince la migliore, e ogni scartata conserva nel log il motivo
  esatto. Gli upgrade si fermano quando il file è abbastanza buono.
- **Non perde mai la tua libreria.** Hardlink invece di copie durante il seed,
  una guardia che distingue un NAS smontato da un file cancellato, un cestino
  per i file sostituiti, spostamenti che riprendono dopo un riavvio.
- **Ha un motore suo.** `gx-core` è un'implementazione BitTorrent costruita per
  questo progetto: BitTorrent v2, uTP, holepunching NAT, DHT IPv6, streaming —
  in un processo con un'impronta di memoria molto inferiore a qBittorrent
  (misurata in *Uso delle risorse* più sotto).
- **Funziona anche senza Gextto.** Lo stesso motore esiste come
  **[gx-torrent](https://github.com/buzzqw/gextto/releases)**,
  un client autonomo per Linux e Windows con UI web completa, API compatibile
  con qBittorrent per Sonarr/Radarr e un gestore RSS all'altezza dei migliori.
- **È software onesto.** Go puro, nessuna dipendenza C, un binario per
  componente, circa 1.600 funzioni di test, accessibilità verificata con
  axe-core, sei lingue nell'interfaccia.

### In breve

| | |
|---|---|
| **Memoria del motore** | ~25 MB a riposo, ~100 MB a 100 MB/s con 50 torrent (qBittorrent-nox: fino a ~5 GB) |
| **Protocollo** | BitTorrent v1, v2 e ibridi (BEP 52) · 34 BEP implementati e testati ([elenco](docs/bep.md)) |
| **Rete** | TCP + uTP su una porta · UPnP, NAT-PMP, PCP · holepunching BEP 55 · proxy SOCKS5/HTTP · killswitch VPN |
| **Automazione** | Serie TV, film, fumetti · Torznab (Jackett, Prowlarr) · RSS · ricerca web · sostituzione dei download bloccati |
| **Integrazioni** | Jellyfin, Plex, Telegram, e-mail, Discord/Slack/ntfy/Gotify/Pushover, iCal |
| **Interfacce** | UI web (SSR + HTMX, PWA installabile) · TUI da terminale via SSH · API JSON |
| **Build** | Go puro (`CGO_ENABLED=0`) · motore libtorrent opzionale · Linux x86_64/aarch64 · gx-torrent anche su Windows |

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Gextto Media Orchestrator                       │
│  Decision Engine · Parse & Scoring · Multi-Indexer · Post-Processing   │
│  Server-Side UI (HTMX + SSR) · Terminal TUI · Media Integrations       │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Engine contract (torrent_engine.go)
┌───────────────────────────────────▼────────────────────────────────────┐
│                  gx-torrent (Managed / Standalone Daemon)              │
│  Web UI · qBittorrent Web API v2 · RSS rules, archive, wanted titles   │
│  Linux Service (systemd) and Native Windows Service (NTFS Junctions)   │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│                    gx-core Engine (internal/gxcore)                    │
│  BitTorrent v1 / v2 (BEP 52 Merkle) · uTP (BEP 29) · Holepunch (BEP 55)│
│  Dual-Stack IPv6 DHT (BEP 32/42/44) · Streaming · MSE In-Place · Zero CGO│
└────────────────────────────────────────────────────────────────────────┘
```

## Cosa offre Gextto

### 🎯 Automazione intelligente e Decision Engine
- **Valutazione meticolosa delle release:** assegna un punteggio deterministico a ogni candidato valutando qualità video, sorgente, codec, audio, HDR/DV, lingua e dimensione. Protegge i file migliori già archiviati e registra nei log il motivo esatto di ogni scarto. Una soglia configurabile ferma gli upgrade quando il file raggiunge lo standard desiderato; `ffprobe` può analizzare i file su disco per affinare le decisioni.
- **Fumetti:** per i fumetti CBZ genera automaticamente metadati `ComicInfo.xml` compatibili con Komga, Kavita e i reader standard.
- **Recupero dai download bloccati (Anti-Stall):** un torrent fermo da tempo (nessun byte) o un magnet in attesa prolungata dei metadati viene affiancato da un'alternativa valida dopo una finestra temporale definita (*Configurazione → Qualità e upgrade → Download bloccati*), mentre il trasferimento originale continua a tentare. Una release a cui Gextto rinuncia va in blocklist, così la ricerca successiva sceglie un'altra versione invece della stessa release morta.

### 🛡️ Integrità della Libreria e Storage Safety
- **Hardlink immediati durante il seed:** quando download e libreria risiedono sullo stesso filesystem, il file entra istantaneamente in archivio tramite hardlink, rendendolo subito fruibile senza duplicare lo spazio occupato su disco.
- **Protezione mount point e NAS:** rileva tempestivamente la disconnessione di una condivisione di rete (NAS smontato), evitando di scambiare l'assenza temporanea per file eliminati; gestisce retry automatici trasparenti su timeout o dischi pieni e riprende da solo i trasferimenti interrotti al riavvio.
- **Cestino di sicurezza:** le versioni sostituite da un upgrade reale, e i file di un torrent rimosso "con i dati", vengono spostati nel cestino configurato prima dell'eliminazione definitiva. Il torrent esce prima dal motore (nessun file tenuto aperto su NFS) e la copia tra filesystem diversi gira in background, così la pagina risponde subito.

### 🔌 Fonti, Integrazioni e Controllo Operativo
- **Sorgenti flessibili:** feed RSS, elenchi HTML (ext.to, TorrentGalaxy, Corsaro), indexer Torznab (Jackett, Prowlarr), motori web dedicati e supporto opzionale a FlareSolverr.
- **Notifiche e Media Server:** notifica gli eventi via Telegram, e-mail o webhook (Discord, Slack, ntfy, Gotify, Pushover), inclusi riavvii di Gextto e anomalie di gx-torrent; invia refresh mirati a Jellyfin e Plex toccando solo la cartella modificata ed esporta un calendario iCal con arrivi e uscite.
- **Tracciabilità completa dei download:** ogni operazione ha un **ID di acquisizione** univoco tracciato in tutti i log (ricerca, selezione, download, post-processing e archivio); la dashboard **Da controllare** raccoglie i casi anomali, e gli avvisi a soglia segnalano tempestivamente se una sorgente fallisce tre cicli consecutivi o rallenta.
- **Interfaccia Web SSR + HTMX e TUI da terminale:** UI responsive fruibile su desktop e smartphone (PWA), navigabile interamente da tastiera e verificata con axe-core per l'accessibilità; tabelle che stanno nella finestra a qualsiasi dimensione del testo; un unico selettore **Tema** con oltre 20 temi (scuro, chiaro, crema e i classici Nord, Dracula, Solarized, Gruvbox, Catppuccin, Tokyo Night, GitHub, Alto contrasto…) tutti verificati per il contrasto, più **temi personali** costruiti da quattro colori (sfondo, pannelli, testo, accento) e salvati con un nome sul server, quindi disponibili su ogni dispositivo; TUI nativa completa per l'amministrazione via SSH. Disponibile in 6 lingue (IT, EN, DE, FR, ES, PL).

La UI è disponibile su `http://<host>:5000/`. Il [manuale italiano](docs/MANUAL.it.md) e l'[English manual](docs/MANUAL.en.md) spiegano la configurazione e tutte le sezioni dell'interfaccia.

## Motori torrent e l'Engine gx-core

Gextto conserva il piano di controllo (coda, punteggi, post-processing, archivio) e delega il trasferimento dati a un motore selezionabile in *Configurazione → Motore torrent*. Un solo motore gira alla volta: il passaggio da uno all'altro è una migrazione pulita e coordinata, senza conflitti di I/O o processi concorrenti sugli stessi file.

### gx-core & gx-torrent (Predefinito e consigliato)

**gx-torrent** è il motore predefinito ed è compilato in **Go puro** (`CGO_ENABLED=0`), senza alcuna dipendenza C/C++. Gira in un processo separato sorvegliato da Gextto, sopravvive ai riavvii e agli aggiornamenti dell'orchestratore (Gextto lo riaggancia se binario e opzioni coincidono), dispone di una propria interfaccia web ed è dove arrivano per prime le innovazioni di protocollo.

Il suo nucleo, **`gx-core`** (`internal/gxcore`), è un'implementazione BitTorrent sviluppata in questo repository (con porzioni derivate da progetti MIT/BSD; vedi `internal/gxcore/LICENSE`). Per chi conosce BitTorrent, ecco cosa c'è dentro.

#### Protocollo

- **BitTorrent v1, v2 e ibridi (BEP 52):** torrent v1, solo-v2 e ibridi sia da file `.torrent` sia da magnet (`urn:btmh:`), con layout dei pezzi per file (nessun blocco di padding tra file), verifica con albero Merkle SHA-256 e `piece layers` ricevuti dai peer (`hash request`, `hashes`, `hash reject`).
- **34 BEP implementati e testati — [`docs/bep.md`](docs/bep.md) è la fonte di verità:** wire di base (BEP 3), bit riservati (4), Fast Extension (6), `ut_metadata` da magnet (9), Extension Protocol (10), Peer Exchange `ut_pex` con IPv4 e IPv6 `added6`/`dropped6` (11), multitracker `announce-list` (12), Local Service Discovery IPv4/IPv6 (14), super-seeding (16), Web Seed con HTTP Range (19), convenzioni del peer ID (20), seed parziali `upload_only`/`event=paused` (21), liste peer compatte (23), IP esterno dal tracker (24), torrent privati (27), hint `similar`/`collections` (38), priorità canonica dei peer (40), file di padding (47) e `lt_donthave` (54); tracker: IPv6 `peers6` (7), tracker UDP (15), estensioni UDP `URLData` (41), retry `retry in`/`never` (31) e scrape (48); DHT: mainline (5), IPv6 (32), scrape DHT (33), node ID sicuri (42), nodi read-only (43) e store K/V con client `get`/`put` in uscita (44), più aggiornamenti magnet `urn:btpk:` (46); infine BitTorrent v2 (52) e la selezione `so=` dei magnet (53). BEP deprecati o di nicchia e WebTorrent/I2P sono fuori scope, con le motivazioni nello stesso documento.
- **Hint di dati locali (BEP 38) e swarm merging:** legge gli hint `similar`/`collections`, li conserva nell'export e indica gli altri torrent locali che condividono un hint (`GET /api/v1/torrents/{hash}/collections`); quando un altro torrent ha esattamente lo stesso contenuto, i suoi pezzi verificati vengono ri-hashati e riusati, così un re-pack o un torrent senza seed si completa senza riscaricare quei dati. I torrent privati non vengono mai mescolati (BEP 27).
- **Super-seeding (BEP 16) e choking deterministico:** un banco di simulazione dell'unchoker ottimizza throughput ed equità; il super-seeding diffonde un torrent nuovo un pezzo alla volta.

#### Rete e NAT

- **uTP (BEP 29) su una sola porta:** TCP e uTP in parallelo con fallback automatico; un solo socket UDP porta uTP e DHT, instradando gli handshake per info-hash, così ogni swarm annuncia la stessa porta.
- **Holepunching (BEP 55):** l'estensione `ut_holepunch` completa apre connessioni uTP dirette attraverso i NAT senza configurare il router, come client e come relay per gli altri peer.
- **Mappatura porte e diagnostica:** UPnP, NAT-PMP e **PCP** (RFC 6887), un test della porta che legge anche la porta esterna vista dal DHT (NAT cone o simmetrico), proxy SOCKS5 e HTTP CONNECT e un killswitch per interfaccia (`outgoing-interface`) che tiene tutto il traffico BitTorrent sulla VPN.
- **Sottosistema DHT (`internal/dht`):** mainline (BEP 5), IPv6 dual-stack (32), scrape (33), node ID sicuri con CRC32C (42), modalità read-only (43, spenta di default: un client completo deve servire il DHT che usa), store K/V firmato immutabile/mutabile con client `get`/`put` in uscita (44), aggiornamenti magnet `urn:btpk:` (46).

#### Disco, memoria e CPU

- **Nessuna cache gigante, per scelta:** i pezzi vengono scritti subito e la page cache del sistema (recuperabile) fa l'accorpamento; una cache di lettura con scadenza pigra e un limitatore di scrittura dimensionati su RAM e tipo di disco (HDD, SSD, NFS) tengono l'impronta nelle decine di MB.
- **Durabilità a lotti:** `fdatasync` prima di salvare il bitfield di resume, mai `O_SYNC` per scrittura; file sparsi e preallocazione `fallocate` (sicura su tmpfs); cartelle temporanee su RAM disk; storage mmap opzionale su Linux (`-mmap`).
- **Protezione del disco:** quando una cartella di download scende sotto la soglia di spazio libero (1 GiB di default, lo *spazio libero minimo* di Gextto se impostato) i download non finiti lì vanno in pausa da soli e ripartono quando lo spazio torna; il seed continua, nessun download finisce in errore di scrittura e con `-mmap` un disco pieno non può più far cadere il demone.
- **Percorsi critici senza allocazioni:** RC4 di MSE/PE cifra sul posto nel buffer di rete (nessuna allocazione per blocco), i bitfield si contano con `math/bits.OnesCount64` e la profondità della coda di richieste per peer si adatta alla sua velocità di consegna.
- **Blocklist IP in microsecondi:** formati CIDR, intervalli, P2P ed eMule uniti in intervalli ordinati e cercati per bisezione senza allocazioni; una lista da URL viene riscaricata a intervalli regolari.

#### Streaming, sicurezza e gestione

- **Streaming:** il selettore dei pezzi distingue la finestra del player (`SetStreamWindow`) dall'orizzonte di readahead (`SetReadaheadWindow`), e le priorità per file (0 = salta, 1–4 normale, 5–7 alta) si applicano a caldo; MKV/MP4 serviti al browser o a VLC con HTTP Range.
- **Protezione della LAN:** web seed e sorgenti magnet `xs=`/`as=` arrivano dai dati del torrent, quindi di default non possono raggiungere indirizzi privati (router, NAS, altri servizi della rete locale): il controllo è sull'IP risolto, immune al rebinding DNS, e si può allentare per un web seed in LAN.
- **Protezione degli swarm:** torrent privati BEP 27 rigorosi (niente DHT, PEX, LSD), quarantena di 30 minuti per i peer che inviano blocchi corrotti, tracker rimossi dopo `never` (BEP 31) o un lungo guasto, diagnostica per pezzo, dati di resume in BoltDB, Server-Sent Events in tempo reale (`/ui/events`).
- **Ovunque:** motore interno di Gextto o client autonomo con UI web e API compatibile qBittorrent, come servizio systemd su Linux e servizio Windows nativo su Windows (amd64/arm64).

### Confronto dei motori

La tabella riassume le alternative al predefinito; come includere libtorrent è
più sotto.

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
  con grafici e diagnostica in tempo reale, gestione categorie e tag e ricerca
  multi-indexer che legge prima l'**archivio** locale (alimentato dai feed RSS e
  dagli indexer per i titoli monitorati) e interroga gli indexer configurati
  (Jackett, Prowlarr, MIRCrew) in tempo reale solo su richiesta, ognuno con il
  link alla propria pagina web. Ogni indexer si configura con il semplice
  indirizzo base, esattamente come in Gextto: il percorso Torznab aggregato di
  Jackett, l'API di ricerca aggregata di Prowlarr e l'`/api` di un servizio
  Torznab diretto vengono ricavati da soli.
- **Una suite RSS e automazione all'altezza di qBittorrent e Deluge/YaRSS2:**
  - **Schede dedicate:** separazione pulita tra **Wanted** (`/ui/wanted`) per serie TV e film con tracciamento degli episodi mancanti (gap search) e ricerche manuali istantanee, e **RSS & Indexer** (`/ui/rss`) per feed, indexer e regole di automazione.
  - **Feed & Indexer:** i feed sono URL RSS o elenchi HTML (pagine uploader di ext.to e TorrentGalaxy), con ultimo controllo, elementi, contatori nuovi/aggiunti, riscontro istantaneo al click su **Controlla feed** (`✓ N feed controllati: X nuovi · Y aggiunti`) e ricarica per singola riga; preset indexer Torznab (Jackett, Prowlarr) con test in tempo reale, più un **URL FlareSolverr** opzionale per superare feed e indexer dietro una sfida Cloudflare.
  - **Regole di automazione:** regole ordinate PASS/FAIL (include/escludi, regex, dimensione, seed, età, filtro episodi, smart episode, *scarica una volta*, pausa tra corrispondenze) con percorso, categoria, tag e opzioni, modificabili sul posto e con **anteprima dal vivo** di ciò che catturerebbero.
  - **Lista Wanted & Gap search:** traccia serie TV e film cercati con menu a discesa per lingua e qualità (fino a 2160p 4K+), ricerca automatica degli episodi mancanti (gap search), pulsante di ricerca manuale istantanea globale o per singolo titolo e stato visuale immediato. I feed **ingrossano l'archivio** e gli indexer vengono interrogati **solo** per i titoli monitorati e per i buchi (e anch'essi finiscono in archivio), quindi le ricerche leggono l'**archivio** e rispondono subito; un feed senza regole e senza include/escludi non scarica nulla (c'è il flag *prendi tutto* per seguirlo per intero) e la query dal vivo è a una casella di distanza (*Cerca anche negli indexer*).
- **Archivio:** ogni elemento mai restituito dai feed resta in un database
  locale (deduplicato per info-hash) e si può cercare — parole, frasi tra
  virgolette, parole `-escluse`, per feed, solo scaricati, ordinati per data,
  seed, dimensione o titolo — con badge di risoluzione ed episodio, aggiungi,
  copia magnet ed elimina. Una ricerca su 100.000 release richiede ~7 ms. Una
  casella **cerca anche negli indexer** interroga dal vivo gli indexer
  Jackett/Prowlarr configurati, così una release presente nell'indexer ma mai
  restituita dai feed compare e si può aggiungere subito.
- **Controllo della banda:** limiti globali e per torrent, programmazione
  giornaliera della banda e un'**auto-velocità sulla latenza** che abbassa
  l'upload quando il ping sale, per tenere la linea reattiva (anti-bufferbloat).
- **Ottimizzazione con un clic:** *Ottimizza coda* (3 download, 3 seed, coda
  dinamica tra 1 e 10 slot), *Ottimizza cache* (dimensionata su RAM e disco) e
  *Ottimizza banda* (upload all'80% del picco misurato, auto-velocità sulla
  latenza attiva), come l'*Ottimizza* di Gextto.
- **IP filter sempre aggiornato:** un file locale o un URL (P2P, eMule, CIDR,
  semplice o gzip), caricato a richiesta con *Carica ora*, riscaricato ogni N ore
  e mostrato con numero di regole, ora di caricamento e prossimo aggiornamento.
  La tile della porta peer diventa verde quando la porta è raggiungibile.
- **Architettura Multipiattaforma:**
  - **Linux (x86_64, aarch64):** pacchetto standalone `tar.gz`, unit systemd inclusa
    (utente o sistema) e container Docker non privilegiato (`Dockerfile`).
  - **Windows nativo (amd64, arm64):** gira come **servizio Windows nativo**
    (dettagli in *Windows* più sotto).

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

**Primo avvio.** Apri `http://<host>:8890/`: all'avvio il demone stampa già gli
indirizzi da aprire (`web UI: http://<lan-ip>:8890/` e
`http://127.0.0.1:8890/`), quindi non serve cercare l'IP. Il wizard guida lingua, cartelle
di download e temporanea (con un **selettore di cartelle** che sfoglia il
server; entrambe sono precompilate con due sottocartelle della directory del
demone — `scarico`/il termine della lingua scelta e `download-temp` — pronte da
accettare o cambiare), porta peer con test di raggiungibilità, limiti di banda
globali, utente e password, accesso dalla LAN e indexer facoltativi. Scegliendo Prowlarr o
Jackett l'URL dell'indexer viene riempito con il suo solito indirizzo locale e
porta (comunque modificabile); ogni altro servizio Torznab si aggiunge come tipo
personalizzato. La pagina collega a **RSS e indexer**, dove si gestiscono senza
modificare il JSON a mano la lista *Cercati* (i titoli da cercare, ricercati
negli indexer e nei feed configurati, con gli episodi mancanti cercati di nuovo)
e i feed, le regole e gli indexer — all'**Archivio** di tutto ciò che i feed
hanno trovato, e a **Impostazioni**, il pannello del client: cartelle, rete e
porte, IP filter, protocolli (cifratura, DHT/uTP/LSD…), coda e cache,
ottimizzazione con un clic, banda, scheduler giornaliero e
auto-speed sulla latenza (anti-bufferbloat), lingua e notifiche. In
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
Consulta l'[analisi di accessibilità](docs/evoluzione.md#13-accessibility) per ambito e
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
| Ambito e test accessibilità | [Analisi accessibilità](docs/evoluzione.md#13-accessibility) |

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

Gextto è distribuito con [EUPL-1.2](LICENSE). Le note di copyright e i testi di
licenza dei componenti di terze parti (inclusi e opzionali) sono in
[THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES), mostrato anche nella pagina
*Licenza* dell'interfaccia web.
