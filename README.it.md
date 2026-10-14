# Gextto — EXpert Torrent Transfer Orchestrator

**Di' a Gextto cosa vuoi vedere. Trova la release migliore, la scarica, la
controlla, le dà il nome giusto e la archivia nella tua libreria — poi continua
a cercarne una copia migliore.**

Gextto è un demone self-hosted di automazione multimediale con un'interfaccia
web veloce e un motore BitTorrent proprio, scritto da zero in Go puro. Un
binario per componente, nessuna dipendenza C, nessun runtime o build frontend
Node.js/npm, sei lingue nell'interfaccia.

[![Licenza: EUPL-1.2](https://img.shields.io/badge/licenza-EUPL--1.2-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-puro%2C%20zero%20CGO-00ADD8?logo=go)
![Piattaforme](https://img.shields.io/badge/piattaforme-Linux%20%C2%B7%20Windows%20(gx--torrent)-informational)
![BEP](https://img.shields.io/badge/BEP%20BitTorrent-34%20implementati-success)
![Lingue](https://img.shields.io/badge/UI-IT%20%C2%B7%20EN%20%C2%B7%20DE%20%C2%B7%20FR%20%C2%B7%20ES%20%C2%B7%20PL-lightgrey)

> **English:** [README.md](README.md) · **Manuale:**
> [italiano](docs/MANUAL.it.md) · [English](docs/MANUAL.en.md) · **Segnalazioni e proposte:**
> [GitHub Issues](https://github.com/buzzqw/gextto/issues) ·
> **Contribuire:** [guida sviluppatori](docs/DEVELOPERS.md)

## Perché Gextto

- **Decide come un esperto, e spiega le sue scelte.** Ogni release candidata
  riceve un punteggio su risoluzione, sorgente, codec, audio, HDR/Dolby
  Vision, lingua e dimensione. Vince la migliore; ogni scartata conserva nel
  log il motivo esatto. Gli upgrade si fermano quando il file è abbastanza
  buono.
- **Protegge la tua libreria.** I file completati entrano in libreria come
  hardlink durante il seed, una guardia distingue un NAS smontato da un file
  cancellato, i file sostituiti vanno in un cestino e gli spostamenti
  interrotti riprendono dopo un riavvio.
- **Ha un motore proprietario estremamente efficiente e perfezionato.** `gx-core`
  è un'implementazione BitTorrent originale scritta da zero per questo
  progetto — BitTorrent v2, TCP e uTP in parallelo su una sola porta,
  holepunching NAT, DHT IPv6 e streaming. Gira con una frazione della memoria dei
  client tradizionali (~25 MB a riposo, ~100 MB a 100 MB/s, fino a 50 volte più
  leggero di qBittorrent) grazie a percorsi critici a zero allocazioni e
  scrittura streaming diretta (vedi *Uso delle risorse*).
- **Funziona anche da solo.** Lo stesso motore esiste come
  **[gx-torrent](https://github.com/buzzqw/gextto/releases)**, un client
  completo per Linux e Windows con UI web propria, API compatibile con
  qBittorrent per Sonarr/Radarr e una suite RSS all'altezza dei migliori.
- **Si adatta, si calibra e si controlla da solo.** Coda dinamica, cache disco e
  upload si adattano continuamente all'hardware (RAM, NVMe/SSD/HDD/NFS) e alla
  latenza della linea (anti-bufferbloat); i download vanno in pausa prima che il
  disco si riempia; il motore premia i peer veloci e scarta quelli lenti o
  inattivi, riusando i dati già su disco tra swarm diversi. Diagnostica
  dettagliata in tempo reale, telemetria delle connessioni e tassi di I/O sono
  visibili a ogni livello.
- **Si controlla e si distribuisce con facilità.** Go puro senza CGO, nessuna
  dipendenza o build Node.js/npm, un processo sorvegliato per componente, oltre
  1.780 funzioni di test e controlli di accessibilità con axe-core.

### In breve

| | |
|---|---|
| **Memoria del motore** | ~25 MB a riposo, ~100 MB a 100 MB/s con 50 torrent (qBittorrent-nox: fino a ~5 GB) |
| **Protocollo** | BitTorrent v1, v2 e ibridi (BEP 52) · 34 BEP implementati e testati ([elenco](docs/bep.md)) |
| **Rete** | TCP + uTP su una porta · UPnP, NAT-PMP, PCP · holepunching BEP 55 · proxy SOCKS5/HTTP · killswitch VPN |
| **Automazione** | Serie TV, film, fumetti · Torznab (Jackett, Prowlarr) · RSS · ricerca web · recupero dei download bloccati |
| **Integrazioni** | Jellyfin, Plex, Telegram, e-mail, Discord/Slack/ntfy/Gotify/Pushover, iCal |
| **Interfacce** | UI web (SSR + HTMX, zero-Node/Go puro, PWA installabile) · TUI da terminale via SSH · API JSON |
| **Build** | Go puro (`CGO_ENABLED=0`) · motore libtorrent opzionale · Linux x86_64/aarch64 · gx-torrent anche su Windows |

## Funzionalità

### Punteggi e automazione

- **Valutazione di qualità.** Ogni candidato riceve un punteggio
  deterministico su qualità, sorgente, codec, audio, HDR/Dolby Vision, lingua
  e dimensione. I file migliori già in libreria sono protetti, gli scarti sono
  registrati con la motivazione e una soglia ferma gli upgrade quando il file
  raggiunge lo standard voluto. `ffprobe` può analizzare i media archiviati
  per arricchire le decisioni.
- **Serie, film, fumetti.** Per gli archivi CBZ vengono scritti metadati
  standard `ComicInfo.xml` (Komga, Kavita, reader).
- **Recupero dai blocchi.** I download fermi e i magnet in attesa prolungata
  dei metadati vengono affiancati da un'alternativa valida dopo una finestra
  configurabile (*Configurazione → Qualità e upgrade → Download bloccati*).
  L'originale continua a tentare in background; la release abbandonata va in
  blocklist, così la ricerca successiva ne sceglie un'altra.

### Sicurezza della libreria

- **Hardlink durante il seed.** Quando download e libreria condividono il
  filesystem, i file completati compaiono subito in libreria come hardlink —
  senza duplicare lo spazio occupato.
- **Guardia NAS e mount point.** Una condivisione smontata non viene mai
  scambiata per file cancellati. I guasti transienti (disco pieno, timeout di
  rete) ritentano da soli e gli spostamenti interrotti riprendono al riavvio.
- **Cestino.** I file sostituiti da un vero upgrade e i dati di un torrent
  rimosso "con i dati" passano prima dal cestino configurato. Il torrent esce
  dal motore prima che si tocchi alcun file (nulla resta aperto su NFS) e le
  copie tra filesystem girano in background, così la pagina risponde subito.

### Sorgenti, integrazioni e controllo

- **Sorgenti.** Feed RSS, elenchi HTML (ext.to, TorrentGalaxy, Corsaro),
  indexer Torznab (Jackett, Prowlarr), ricerca web dedicata, FlareSolverr
  opzionale.
- **Notifiche e media server.** Telegram, e-mail, webhook (Discord, Slack,
  ntfy, Gotify, Pushover) inclusi riavvii e anomalie del motore; refresh mirati
  per Jellyfin e Plex; feed iCal di arrivi e uscite.
- **Tracciabilità.** Ogni download ha un **ID di acquisizione** univoco
  seguito in tutti i log (ricerca, selezione, trasferimento, post-processing,
  archivio). Il pannello **Da controllare** raccoglie i casi anomali; gli
  avvisi a soglia segnalano le sorgenti che falliscono o rallentano.
- **Interfacce.** UI web responsive installabile come PWA, interamente da
  tastiera e verificata con axe-core; renderizzata interamente server-side
  (SSR + HTMX) con tutti gli asset incorporati — zero dipendenze o build
  Node.js/npm; un selettore **Tema** unico con oltre 20 temi verificati per il
  contrasto più temi personali salvati sul server, che ti seguono su ogni
  dispositivo; TUI completa via SSH. Sei lingue (IT, EN, DE, FR, ES, PL).

La UI è su `http://<host>:5000/`. Configurazione e sezioni sono nel
[manuale italiano](docs/MANUAL.it.md) e nell'[English
manual](docs/MANUAL.en.md).

## Motore torrent

Gextto conserva il piano di controllo (coda, punteggi, post-processing,
archivio) e delega i trasferimenti a un motore selezionabile in
*Configurazione → Motore torrent*. Gira un solo motore alla volta; il passaggio
è una migrazione pulita e coordinata, senza conflitti di I/O.

**Fallback automatico.** Se il motore attivo non riesce a restare in piedi — un
ciclo di crash, o un'attivazione rifiutata all'avvio — Gextto non si limita a
fermarsi. Percorre una catena e persiste la scelta: prima il
**qBittorrent-nox gestito**, scaricato al momento dalle release di
`userdocs/qbittorrent-nox-static` (l'ultima build libtorrent v2, verificata con
SHA-256) e avviato con un profilo privato — oppure un qBittorrent esterno già
tuo; poi il libtorrent integrato opzionale, se la build lo include; infine uno
stato di recovery in cui i trasferimenti restano fermi e la UI resta
raggiungibile per intervenire. Prima di cambiare motore tenta la soluzione più
economica: un `gx-torrent` aggiornato che non parte viene ripristinato alla
versione precedente. Un cooldown evita il ping-pong tra due motori che crashano
entrambi, e `GEXTTO_ENGINE=qbittorrent` (o `gx-torrent`) forza il motore per un
solo avvio, utile anche come recupero da riga di comando.

### gx-core e gx-torrent (predefinito, consigliato)

**gx-torrent** è il motore predefinito: Go puro (`CGO_ENABLED=0`), nessuna
dipendenza C/C++, in un processo separato sorvegliato che sopravvive a riavvii
e aggiornamenti dell'orchestratore (Gextto lo riaggancia se binario e opzioni
coincidono). Ha una propria interfaccia web ed è dove arriva per primo il
lavoro di protocollo.

Il suo nucleo, **`gx-core`** (`internal/gxcore`), è il motore BitTorrent nativo
di Gextto, sviluppato in questo repository. `gx-core` e il DHT
(`internal/dht`) sono codice originale scritto da zero in questo albero —
nessun codice derivato da terzi, nessun rate limiter o helper UUID di terze
parti (provenienza in [`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES)) — con la
stessa licenza EUPL-1.2 del resto di Gextto.

**Protocollo.** BitTorrent v1, solo-v2 e ibridi (BEP 52) da file `.torrent` e
magnet (`urn:btmh:`), con layout dei pezzi per file, verifica Merkle SHA-256 e
`piece layers` ricevuti dai peer. **34 BEP implementati e testati —
[`docs/bep.md`](docs/bep.md) è la fonte di verità** — dal wire protocol alle
estensioni Fast, `ut_metadata`, Extension Protocol, PEX con IPv6,
multitracker, LSD, super-seeding, Web Seed, seed parziali, peer compatti,
torrent privati, hint locali (BEP 38), priorità dei peer, padding,
`lt_donthave`, tracker IPv6/UDP con estensioni, retry e scrape, fino alla suite
DHT (mainline, IPv6, scrape, node ID sicuri, nodi read-only, store K/V,
aggiornamenti `urn:btpk:`), più v2 e selezione `so=` dei magnet. BEP
deprecati o di nicchia e WebTorrent/I2P sono fuori scope, con le motivazioni
nello stesso documento. Oltre i BEP: **tracker `ws://`/`wss://`** (protocollo
WebTorrent, con l'avvertenza che il tracker di riferimento restituisce solo peer
WebRTC) e **feed DHT** (URL di feed `dht://<pubkey>` risolti da un item
mutabile BEP 46) — vedi [`docs/beyond-bep.md`](docs/beyond-bep.md).

**Riuso locale.** Gli hint `similar`/`collections` (BEP 38) sono conservati
nell'export e confrontati con gli altri torrent locali
(`GET /api/v1/torrents/{hash}/collections`). I pezzi già verificati da un
altro torrent — stessa struttura, o lo stesso file BitTorrent v2 con pezzi di
dimensione diversa — vengono copiati e ricontrollati in background invece di
essere scaricati; le sorgenti parziali contribuiscono con i loro blocchi da 16
KiB verificati e dalla rete arriva solo il resto (sulle ISO Debian e Arch con
una sorgente completa al 75%, dalla rete è arrivato il 25% invece del 100%).
Il riuso sopravvive al riavvio: un torrent ripreso dallo stato salvato
ricostruisce i blocchi locali invece di riscaricarli; funziona anche
nell'ordine inverso di aggiunta: un torrent già in download recupera i dati che
un sibling pubblica dopo.
I torrent privati non vengono mai mescolati (BEP 27).

**Rete.** TCP e uTP (BEP 29) in parallelo con fallback automatico su una sola
porta — un unico socket UDP porta uTP e DHT, con handshake instradati per
info-hash. La ritrasmissione uTP si adatta al collegamento: un pavimento di
latenza di 5 ms evita le tempeste di RTO sub-millisecondo sui percorsi veloci,
e un backoff per connessione riprova un peer silenzioso o congestionato con
pazienza crescente (misurato: metà delle ritrasmissioni). `ut_holepunch`
completo (BEP 55) attraverso i NAT, come client e come relay. UPnP, NAT-PMP e **PCP** (RFC 6887); test della porta che legge
anche la porta esterna vista dal DHT (NAT cone o simmetrico); proxy SOCKS5 e
HTTP CONNECT; killswitch legato all'interfaccia (`outgoing-interface`) che
tiene tutto il traffico BitTorrent sulla VPN. Il DHT (`internal/dht`) copre
mainline IPv6, scrape, node ID sicuri CRC32C, modalità read-only (spenta di
default) e store K/V firmato con client `get`/`put` in uscita.

**Autogestione e taratura continua.** Il motore calibra costantemente i
propri parametri senza richiedere interventi manuali: i download attivi seguono
il throughput misurato (coda dinamica); i torrent lenti lasciano spazio a quelli
veloci e quelli fermi si fanno da parte. La cache disco si dimensiona in modo
dinamico su RAM libera, torrent attivi e classe di storage (NVMe, SSD, HDD,
NFS); l'auto-speed sulla latenza adatta l'upload prima che la linea si saturi
(anti-bufferbloat). Quando una cartella scende sotto la soglia di spazio libero
(1 GiB di default), i download non completati vanno in pausa da soli e riprendono
all'occorrenza — il seeding prosegue, nessun errore di scrittura I/O. La coda di
richieste di ogni peer si adatta alla sua velocità reale, i peer veloci lavorano
su due pezzi in parallelo e al limite massimo di connessioni cade la connessione
più lenta e improduttiva — mai una a caso. I pezzi dei file deselezionati non
rendono mai un peer "interessante".

**Impronta leggera, per scelta.** I pezzi vengono scritti direttamente in
streaming; la page cache del sistema operativo fa l'accorpamento, eliminando la
necessità di una cache in-process da vari gigabyte. Cache di lettura a scadenza
pigra e limitatore di scrittura adattivo tengono la memoria nelle decine di MB
anche sotto trasferimenti continui a 100 MB/s. `fdatasync` è eseguito a lotti
prima di salvare i bitfield di resume (mai `O_SYNC` per singola scrittura);
file sparsi, preallocazione `fallocate`, cartelle temporanee su RAM disk e
modalità mmap opzionale su Linux (`-mmap`). Percorsi critici a zero allocazioni
(RC4 di MSE/PE sul posto, bitfield vettoriali a parole di memoria, data-path uTP
senza copie, buffer adattivi per peer). La blocklist IP (CIDR, intervalli, P2P,
eMule) è unificata in intervalli ordinati e interrogata per bisezione senza
allocare memoria; gli elenchi da URL si aggiornano a intervalli programmati.

**Streaming, sicurezza e telemetria.** Il selettore distingue nettamente la
finestra di riproduzione del player (`SetStreamWindow`) dall'orizzonte di
readahead (`SetReadaheadWindow`); le priorità per file si applicano a caldo e
i file MKV/MP4 sono riprodotti fluidamente in streaming via HTTP Range, con il
`Content-Type` corretto così il browser li riproduce inline. Un **media server
DLNA/UPnP opzionale** espone i media completati a TV e console sulla LAN,
insieme alla **libreria sul NAS** navigabile per cartelle, con le **locandine e miniature** già salvate da Jellyfin/Kodi (navigazione a pagine, seek via Range, sottotitoli agganciati al video, solo LAN
per costruzione). Web seed
e sorgenti magnet `xs=`/`as=` implementano una rigida protezione SSRF (controllo
sull'IP risolto, immune al DNS rebinding). Torrent privati rigorosi (BEP 27:
niente DHT, PEX o LSD), quarantena di 30 minuti per i peer che inviano blocchi
corrotti, tracker rimossi su BEP 31 `never` o fallimento prolungato. Controllo e
telemetria completi: diagnostica per peer, tassi di lettura/scrittura e
profondità di coda del disco, monitoraggio delle connessioni (TCP vs uTP),
resume transazionale su BoltDB e flusso di Server-Sent Events in tempo reale
(`/ui/events`).

### Confronto dei motori

| Motore | Dove gira | Quando sceglierlo |
|---|---|---|
| gx-torrent (predefinito) | Processo Go separato e sorvegliato | Go puro, autonomo, sviluppo attivo. v1, ibridi e solo-v2, DHT e cifratura MSE/PE; anche autonomo su Linux e Windows; niente WebTorrent/WebRTC |
| qBittorrent-nox | Demone esterno via Web API, oppure uno che Gextto scarica e avvia in privato | Hai già qBittorrent-nox o preferisci la sua UI. Servono mappature percorsi e un processo in più ([guida alla configurazione](docs/qbittorrent-nox.md)); è anche il primo passo del fallback automatico, scaricato al momento |
| libtorrent (build opzionale) | Nel processo di Gextto, **solo se compilato** (`make build-libtorrent`) | Controlli avanzati o compatibilità di libtorrent; un passo del fallback automatico. Un crash coinvolge anche Gextto |

Le statistiche del motore attivo — torrent per stato, velocità e slot,
porta/router, DHT/uTP/LSD, cifratura, proxy, filtro IP, cache, **connessioni
in entrata**, **I/O disco** e i **totali di sempre** scaricati/inviati
(mantenuti tra i riavvii) — sono in **Salute → Motore torrent**
(`/?view=health`), in `GET /api/v1/stats` del demone e in
`GET /api/libtorrent/session-stats`.

## Uso delle risorse

Gextto resta leggero a riposo: un solo demone (più il piccolo processo
`gx-torrent` sorvegliato quando quel motore è attivo), concorrenza limitata
per sorgenti e attività in background, backoff invece di retry continui verso
i provider in errore. CPU e RAM dipendono da titoli, sorgenti, torrent attivi
e scansioni; i numeri su una singola macchina sono indicativi.

**Memoria del motore, misurata** — 50 torrent reali, ~100 MB/s su 16 GB:

| Motore | A riposo (torrent caricati) | Picco durante il trasferimento |
|---|---:|---:|
| gx-torrent | ~25 MB | ~100 MB |
| qBittorrent-nox | ~40 MB | ~5 GB |
| libtorrent (in-process, build opzionale) | ~0,5 GB | 3–5 GB |

libtorrent e qBittorrent tengono una grande cache disco in-process (GB di
memoria anonima). gx-torrent scrive i pezzi in streaming e lascia
l'accorpamento alla page cache reclamabile del kernel, così l'impronta resta
nelle decine/centinaia di MB sotto carico. I valori variano con torrent, peer
e storage; su HDD/NFS Gextto concede a gx-torrent un buffer più grande, usato
solo quando le scritture restano indietro.

## Installazione Linux

Per Linux 64 bit con systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE Leap
15.6+/Tumbleweed, Arch. Come root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa in `/opt/gextto`, tiene i dati in `/var/lib/gextto` ed espone la UI
sulla porta 5000. Il motore predefinito `gx-torrent` (Go puro) è installato
accanto a `gexttod` e avviato in modalità gestita. Il pacchetto è Go puro —
null'altro da installare. In alternativa, un qBittorrent-nox esterno
([guida alla configurazione](docs/qbittorrent-nox.md)); il
motore libtorrent richiede una build propria (vedi sotto).

Prima di toccare il sistema l'installer avvia una volta il programma
scaricato: librerie mancanti o glibc troppo vecchia vengono segnalate subito,
senza lasciare un servizio che non parte. Di default il servizio gira con
**l'utente locale che esegue l'installer**, così scrive nella tua libreria
senza permessi extra; `--user gextto` usa invece l'account di sistema isolato
e senza login. Per una libreria su un NAS altrui, `--media-group <gruppo>`
aggiunge l'utente del servizio a quel gruppo.

> [!IMPORTANT]
> Gextto è pensato per una rete fidata: UI web e API sono aperte di default.
> Per l'accesso da fuori, attiva il login facoltativo (*Configurazione →
> Accesso e servizi*, la rete locale resta libera) e usa comunque HTTPS via
> reverse proxy o VPN. Leggi la [politica di
> sicurezza](docs/SECURITY.md) prima di esporlo in rete.

### Installazione dal sorgente

Serve Go 1.26.8+; la build predefinita è in **Go puro**. Solo il motore
opzionale **libtorrent** richiede anche un compilatore C++17 e gli header
libtorrent-rasterbar. La UI web è incorporata — nessun build frontend.
Vedi il [manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build                 # Go puro (predefinita), con gx-torrent
make build-libtorrent      # include anche il motore opzionale libtorrent
```

`make build` incrementa il numero di build **committato** (`build_number`,
condiviso con la CI così checkout e release mostrano lo stesso `1.1.<n>`) e
scrive `gexttod` più `gx-torrent` in puro Go in `bin/`. `make fast`
ricompila senza incrementare; `make gx-torrent` compila solo il demone.
Verifica con `./bin/gexttod --version`. Il demone `gx-torrent` ha un
**proprio** contatore, avanzato solo quando il suo codice cambia;
`./bin/gx-torrent --version` e `/api/v1/health` lo riportano come `1.1.<n>`.

## gx-torrent come client autonomo

gx-torrent è anche un client completo usabile senza Gextto, come
`qbittorrent-nox` o `transmission-daemon`. È **lo stesso binario** che Gextto
pilota come motore. In sintesi, è una **versione più semplice e ridotta di
Gextto**: meno funzionalità, ma tutte quelle che servono a un client autonomo.

- **Pronto per gli `*arr`.** API Web v2 di qBittorrent (`/api/v2/...`) per
  Sonarr, Radarr, Lidarr, Readarr e app mobili, più API JSON propria
  (`/api/v1/...`) e pagina di accesso.
- **UI web moderna.** Temi chiaro e scuro nello spirito di VueTorrent/Flood:
  barra laterale con filtri per stato/categoria/tag e grafico del traffico,
  elenco denso ordinabile con selezione multipla, menu contestuale e
  scorciatoie, dettaglio in un pannello laterale, trascinamento dei `.torrent`
  e magnet incollati, schede su telefono, e una pagina **Statistiche**
  (velocità dal vivo, i totali di questa sessione e di tutte le sessioni —
  mantenuti tra i riavvii —, disco e cache, rete, coda e un grafico delle
  velocità su 1 h/24 h/7 d). Ogni pagina verificata con axe-core.
- **RSS e automazione.** Pagina **Cercati** (schede per titolo con episodi
  Have/Missing, gestore puntate per segnare cosa hai o manca e forzare una nuova
  ricerca, finestra guidata di aggiunta) e pagina **RSS & Indexer** (feed,
  regole, indexer, opzioni a schede; editor diviso in *quali elementi* / *cosa
  fare* con anteprima dal vivo). Regole PASS/FAIL ordinate, ricarica per riga,
  test indexer in tempo reale, FlareSolverr opzionale e un **archivio** locale
  di tutto ciò che i feed hanno restituito (100.000 release cercabili in ~7
  ms, con opzione di interrogare dal vivo Jackett/Prowlarr).
- **Gestione.** Limiti globali e per torrent, scheduler di banda con fascia e
  limiti propri per ogni giorno della settimana, auto-speed sulla latenza
  (anti-bufferbloat), ottimizzazione di coda/cache/banda con un clic, filtro IP
  da URL o file (di default la blocklist di Gextto) con refresh programmato,
  categorie e tag, diagnostica in tempo reale, test porte dalle impostazioni.
- **Notifiche.** Gli stessi canali di Gextto: Telegram, e-mail e webhook (JSON
  generico, Discord, Slack, Gotify, ntfy, Pushover) per download completati,
  match dei feed ed errori dei feed, con pulsante di prova.
- **Si aggiorna da solo.** In *Impostazioni → Manutenzione* controlla a
  intervalli (o quando premi **Verifica aggiornamenti**) la build pubblicata
  dalla CI; il pulsante diventa **Aggiorna** solo se è cambiato il codice di
  gx-torrent: scarica l'archivio, verifica lo SHA-256, prova il nuovo binario,
  lo sostituisce e si riavvia nello stesso processo, poi mostra in un popup i
  commit nuovi. Una spia *Aggiornamento disponibile* si accende nella barra di
  stato della pagina Torrents. Se la nuova versione non parte, torna da sola la precedente.
- **Piattaforme.** Linux x86_64/aarch64 (`gx-torrent-linux-<arch>.tar.gz`,
  unit systemd o `run.sh`, immagine Docker non privilegiata) e servizio
  Windows nativo amd64/arm64 (`gx-torrent-windows-<arch>.zip`,
  `install-service.ps1`, collegamenti via junction — nessun privilegio
  symlink). Gextto resta solo Linux.

Apri `http://<host>:8890/` per il wizard (lingua, cartelle con selettore sul
server, test porta, limiti, login, indexer); l'indirizzo da aprire è stampato
all'avvio. Riferimento completo: il [documento tecnico di
gx-torrent](docs/gx-torrent.md).

## Primo avvio sicuro

1. Apri `http://<server>:5000`: alla prima visita parte la **configurazione
   guidata** (password, cartelle con verifica permessi, indexer e TMDB, primo
   titolo, attivazione). Saltabile, riapribile da `/?view=setup`.
2. Imposta percorsi, una sorgente ed eventuali credenziali TMDB/TVDB.
3. Tieni attivo il **dry-run**, aggiungi un titolo di prova ed esegui una
   ricerca o un ciclo.
4. Controlla **Salute** e **Log**, inclusi permessi e risultati delle sorgenti.
5. Attiva la modalità operativa solo a esito corretto.

NAS e diagnostica: il [manuale utente](docs/MANUAL.it.md).

## Aggiornamento e disinstallazione

Con l'installazione ufficiale, all'uscita di una release compare il pulsante
**Aggiornamento disponibile** sopra **Dona**: in *Manutenzione →
Aggiornamenti* vedi versione installata, novità e pulsante **Aggiorna ora**.
Prima viene fatto un backup dei database; se la nuova versione non parte,
torna da sola la precedente. Il controllo (una piccola richiesta a GitHub ogni
6 ore) si disattiva in *Configurazione → Sistema → Aggiornamenti*.

Oppure ripeti l'installer: verifica il checksum, sostituisce il binario in
modo atomico, riavvia il servizio e in caso di fallimento torna indietro.
Dati e configurazione restano intatti.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

```bash
sudo bash install.sh --help       # tutte le opzioni
sudo bash install.sh --dry-run    # mostra cosa farebbe, senza modificare nulla
sudo bash install.sh --uninstall  # ferma e rimuove il programma (i dati restano)
sudo bash install.sh --uninstall --purge   # rimuove anche la directory dati
```

Le variabili d'ambiente (`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, …)
restano supportate. `GEXTTO_LOCAL_ARCHIVE=/percorso/gextto-linux-x86_64.tar.gz`
installa da un pacchetto locale (offline o CI). Da un checkout sorgente:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

(`--no-restart` lascia a te il riavvio.)

## Accessibilità

La UI offre navigazione da tastiera, nomi accessibili per i controlli, gestione
del focus nei dialoghi, tabelle ordinabili semantiche, annunci live, reflow
responsive e contrasto migliorato nel tema chiaro. Le regressioni sono coperte
da axe-core e Playwright:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

La suite automatizzata Playwright include 63 test end-to-end complessivi tra
flussi, dialoghi e navigazione, di cui 13 dedicati specificamente alle verifiche
di accessibilità WCAG con axe-core sulle sezioni principali. Non è una
certificazione legale: restano necessari test manuali con screen reader,
tastiera e tecnologie assistive. Ambito e limiti:
[verifica di accessibilità](docs/measurements.md#6-accessibility-verification).

## Documentazione

| Esigenza | Documento |
|---|---|
| Usare il servizio | [Manuale italiano](docs/MANUAL.it.md) · [English manual](docs/MANUAL.en.md) |
| Architettura e design | [Architettura](docs/ARCHITECTURE.md) |
| Specifiche BitTorrent (BEP) | [Conformità BEP](docs/bep.md) |
| Stato tecnico, misure e backlog | [Stato, misure e backlog](docs/evoluzione.md) |
| NAS, reverse proxy, ripristino | [Guida avanzata](docs/ADVANCED.it.md) |
| Integrazioni HTTP | [Riferimento API](docs/API.md) |
| Migrare un'installazione | [Guida migrazione](docs/MIGRATION.md) |
| Client da terminale | [Riferimento TUI](docs/tui.md) |
| Motore gx-torrent e client autonomo | [Documento tecnico di gx-torrent](docs/gx-torrent.md) |
| Backend qBittorrent-nox | [Guida alla configurazione](docs/qbittorrent-nox.md) |
| Compilare o contribuire | [Manuale sviluppatori](docs/DEVELOPERS.md) |
| Rete e protezione dati | [Politica di sicurezza](docs/SECURITY.md) |
| Ambito e test accessibilità | [Verifica di accessibilità](docs/measurements.md#6-accessibility-verification) |

Struttura completa: l'[indice della documentazione](docs/README.md).

## Supporta il progetto

Se Gextto ti è utile e vuoi sostenerne lo sviluppo, una donazione PayPal è
benvenuta — grazie. Un pulsante **Dona** è sempre visibile anche nella UI, in
basso a destra.

[![Dona con PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## Uso corretto e fair use

Gextto è uno strumento di automazione dei download: non ospita, indicizza né
distribuisce contenuti protetti da copyright.

- Si collega alle sorgenti configurate dall'utente (per esempio Jackett,
  Prowlarr o feed RSS pubblici); non include un indice proprio.
- L'utente è responsabile dei contenuti scaricati. Usa Gextto solo per
  contenuti a cui hai diritto di accedere — pubblico dominio, licenze Creative
  Commons o media di tua proprietà.
- BitTorrent e i motori pilotati sono tecnologia neutrale; il progetto non
  incoraggia né facilita la pirateria.
- Distribuito con licenza open source **EUPL 1.2**.

## Licenza

Gextto è distribuito con [EUPL-1.2](LICENSE). Il nucleo del motore (`gx-core`,
`internal/gxcore`) e il DHT (`internal/dht`) sono codice originale in questo
repository con la stessa licenza. Note di copyright e testi di licenza delle
librerie di terze parti collegate sono in
[THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES), mostrato anche nella pagina
*Licenza* dell'interfaccia web.

## Ringraziamenti

Grazie a **[DeepSeek](https://www.deepseek.com/)** per il supporto e la pazienza
mentre questo progetto cresceva.
