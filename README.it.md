# Gextto — EXpert Torrent Transfer Orchestrator

**Di' a Gextto cosa vuoi vedere. Trova la release migliore, la scarica, la
controlla, le dà il nome giusto e la archivia nella tua libreria — poi continua
a cercarne una copia migliore.**

Gextto è un demone self-hosted di automazione media con un'interfaccia web
veloce e un motore BitTorrent proprio, scritto da zero in Go puro. Un binario
per componente, nessuna dipendenza C, nessuna build frontend Node.js/npm, sei
lingue d'interfaccia — e ogni ottimizzazione che contiene è stata misurata
prima di essere tenuta.

[![Licenza: EUPL-1.2](https://img.shields.io/badge/licenza-EUPL--1.2-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-puro%2C%20zero%20CGO-00ADD8?logo=go)
![Test](https://img.shields.io/badge/test-2.100%20Go%20%C2%B7%2057%20E2E%20%C2%B7%2023%20fuzz-success)
![Piattaforme](https://img.shields.io/badge/piattaforme-Linux%20%C2%B7%20Windows%20(gx--torrent)-informational)
![BEP](https://img.shields.io/badge/BEP%20BitTorrent-34%20implementati-success)
![Lingue](https://img.shields.io/badge/UI-IT%20%C2%B7%20EN%20%C2%B7%20DE%20%C2%B7%20FR%20%C2%B7%20ES%20%C2%B7%20PL-lightgrey)

> **English:** [README.md](README.md) · **Manuale:**
> [italiano](docs/MANUAL.it.md) · [English](docs/MANUAL.en.md) · **Segnalazioni:**
> [bug e richieste](https://github.com/buzzqw/gextto/issues) ·
> **Contribuire:** [guida per sviluppatori](docs/DEVELOPERS.md)

## Perché Gextto

- **Decide come un esperto, e spiega perché.** Ogni candidata riceve un
  punteggio su risoluzione, sorgente, codec, audio, HDR/Dolby Vision, lingua e
  dimensione; uno swarm morto non vince su un'alternativa valida, ogni scarto
  resta nel log con il suo motivo, e gli upgrade si fermano quando il file è
  abbastanza buono.
- **Protegge la tua libreria.** Hardlink durante il seeding, una guardia dei
  mount che distingue un NAS smontato da un file cancellato, un cestino per
  tutto ciò che sostituisce, spostamenti che riprendono dopo un riavvio.
- **Ha un motore suo.** `gx-core` è un'implementazione BitTorrent originale —
  v1, v2 e ibrido, TCP+uTP su una porta, holepunching NAT, DHT IPv6, streaming
  — che usa una frazione della memoria dei client tradizionali (~25 MB a
  riposo, ~100 MB a 100 MB/s; qBittorrent-nox fino a ~5 GB). Lo stesso motore è
  **[gx-torrent](https://github.com/buzzqw/gextto/releases)**, un client
  completo e autonomo per Linux e Windows.
- **Si regola da solo — e divide la linea.** Coda, cache disco e banda seguono
  l'hardware e la linea; con la velocità su **Auto** la linea è tutta di Gextto
  finché nessun altro ne ha bisogno, e la cede agli altri dispositivi di casa
  appena serve a loro.
- **È misurato e testato, non dichiarato.** 2.100 funzioni di test Go, 57 test
  end-to-end nel browser, 23 parser sotto fuzzing e 14 misure fatte su swarm
  reali, indexer reali e una linea reale. I numeri sono qui sotto.

### In breve

| | |
|---|---|
| **Memoria del motore** | ~25 MB a riposo, ~100 MB a 100 MB/s con 50 torrent (qBittorrent-nox: fino a ~5 GB) |
| **CPU del motore** | ~⅓ di un core Intel N97 a 9,4 MB/s con ~200 peer; 1–2 % senza nulla da trasferire |
| **Protocollo** | BitTorrent v1, v2 e ibrido (BEP 52) · 34 BEP implementati e testati ([elenco](docs/bep.md)) |
| **Rete** | TCP + uTP su una porta · UPnP, NAT-PMP, PCP · holepunching BEP 55 · proxy SOCKS5/HTTP · killswitch VPN |
| **Automazione** | Serie TV, film, fumetti · Torznab (Jackett, Prowlarr) · RSS · ricerca web · sostituzione dei download bloccati |
| **Integrazioni** | Jellyfin, Plex, Telegram, e-mail, Discord/Slack/ntfy/Gotify/Pushover, iCal |
| **Interfacce** | Web UI (SSR + HTMX, Go puro, PWA installabile) · TUI da terminale via SSH · API JSON |
| **Build** | Go puro (`CGO_ENABLED=0`) · Linux x86_64/aarch64 · gx-torrent anche su Windows |

## Misurato, non dichiarato

Gextto segue una regola: **una modifica su un percorso dati resta solo se una
misura la dimostra almeno equivalente a ciò che sostituisce**, e un'idea che la
misura non giustifica viene chiusa con i dati invece che implementata. Ogni
numero sta in [`docs/measurements.md`](docs/measurements.md), con macchina, data
e modo in cui è stato preso — un mini-PC Intel N97 con 16 GB di RAM, torrent
pubblici reali, gli indexer in uso e la vera linea di casa.

| Area | Risultato misurato |
|---|---|
| **Memoria del motore** | ~25 MB a riposo e ~100 MB a 100 MB/s con 50 torrent, contro fino a ~5 GB di qBittorrent-nox sullo stesso insieme. Download verso un NAS: ~0,5 GB fuori dalla cache di scrittura, che si dimensiona dalla RAM libera; ~60 MB subito dopo un riavvio |
| **CPU del motore** | Circa **⅓ di un core** a 9,4 MB/s con ~200 peer (metà su uTP, cifrati): **0,17 % per peer**, di cui il piece picker occupa il 9 % |
| **Ricerche sugli indexer** | Jackett e Prowlarr interrogati un indexer alla volta: un ciclo di 30 titoli in **110 s**, **205 richieste e nessuna fallita**, **5.935 release** |
| **Dividere la linea** | Un download a piena linea aggiunge ~40 ms di latenza a tutta la casa. Da solo sulla linea Gextto la usa tutta (**8,5 MB/s**); con un altro dispositivo che scarica, quel dispositivo ottiene **6,8–7,3 MB/s** dei 9 MB/s della linea |
| **Riuso dei dati locali** | Un torrent che condivide file con uno già su disco prende dalla rete solo il **25 %** del file (ISO Debian e Arch), anche dopo un riavvio e in qualunque ordine si aggiungano |
| **Scritture su disco** | Scritture da 16 KiB su NFS a **81 MiB/s** con durabilità a lotti; mmap opzionale ~7,0 GB/s su tmpfs |
| **Archivio dei feed** | **447.000 release in 320 MB**, una riga per torrent; 1.000 release archiviate in **41 ms**; l'archivio standalone cerca su 100.000 release in **7,4 ms** |
| **Demone a riposo** | **0,4 %** di CPU a riposo; **~5.000 chiamate ai metadati al giorno** per 55 serie; i controlli di salute non scrivono mai su un NAS, così i suoi dischi possono dormire |

Il metodo — come si ottiene ogni numero e come riprodurlo — è nello stesso
documento, insieme alle soluzioni che le misure hanno escluso (io_uring, I/O uTP
a lotti su questo carico, una cache write-back, …).

## Testato a fondo

| Livello | Cosa gira |
|---|---|
| **Unitari e integrazione** | 2.100 funzioni di test Go in 413 file: motore, DHT, uTP, storage, punteggi, post-elaborazione, API, rendering della UI. La TUI viene pilotata contro il router reale |
| **End-to-end** | 57 test Playwright pilotano il demone reale in un browser — pagine, mobile, sei lingue, sicurezza, impostazioni — compresi 14 test di accessibilità con audit WCAG axe-core |
| **Fuzzing** | 23 parser esposti alla rete (DHT, bencode, magnet, BEP 10/52, tracker, Torznab, RSS, MSE) sotto fuzzing in CI; ogni crash trovato diventa un seed committato |
| **Misure sul mondo reale** | 14 misure opzionali: swarm pubblici reali, la DHT pubblica, un peer libtorrent reale, riuso locale su ISO reali, choking e pipelining, i Jackett/Prowlarr in uso, la latenza della linea di casa |
| **Guardie statiche** | Ogni bottone della UI punta a una rotta registrata, ogni chiamata interna si risolve sul router reale, ogni impostazione è salvabile, tradotta in sei lingue e ha il suo tooltip, ogni rotta API è documentata |
| **Piattaforme e pacchetti** | La CI cross-compila motore e demone per Windows ed esegue i test di piattaforma Windows e un'installazione reale del servizio; gli installer hanno auto-test (ogni opzione, un dry-run che non cambia nulla) e l'immagine Docker standalone viene costruita e provata; CodeQL analizza ogni push |
| **Benchmark** | 42 benchmark Go sui percorsi caldi (picker, storage, uTP, archivio, riuso, logging), lanciati prima e dopo ogni modifica a un percorso dati |

## Funzionalità

### Trovare e scegliere le release

- **Punteggio attento alla qualità.** Punteggi deterministici su qualità,
  sorgente, codec, audio, HDR/Dolby Vision, lingua e dimensione; i file di
  libreria di qualità superiore sono protetti; un tetto opzionale ferma gli
  upgrade. La pagina impostazioni dà il punteggio a release di riferimento con i
  pesi correnti. `ffprobe` legge la qualità reale dei file archiviati.
- **Serie TV, film, fumetti.** `ComicInfo.xml` standard per gli archivi CBZ
  (Komga, Kavita, lettori).
- **Season pack solo quando servono.** Un pack si scarica solo per riempire un
  buco reale o migliorare un episodio; uno che non aggiungerebbe nulla viene
  rifiutato prima di scaricarlo, e un pack ancora in corso (*IN CORSO n/m*) si
  legge entro gli episodi che esistono.
- **Recupero dei download bloccati.** Un download che smette di avanzare, o un
  magnet fermo in attesa dei metadati, viene sostituito da un'alternativa valida
  dopo una finestra configurabile; la release abbandonata va in blocklist.
- **Sorgenti.** Feed RSS, elenchi HTML (ext.to, TorrentGalaxy, Corsaro),
  indexer Torznab e i gestori Jackett/Prowlarr, ricerca web, FlareSolverr
  opzionale. I gestori vengono interrogati **un indexer alla volta**, ognuno al
  ritmo che regge davvero, un tracker presente in entrambi una volta sola, e un
  tracker lento perde solo i propri risultati.

### Sicurezza della libreria

- **Hardlink durante il seeding**: i file completati compaiono subito in
  libreria, senza occupare spazio due volte.
- **Guardia NAS e mount**: una condivisione smontata non viene mai scambiata per
  file cancellati, un'ondata di file "mancanti" viene limitata, una condivisione
  bloccata non congela il motore; gli errori transitori si riprovano e gli
  spostamenti interrotti riprendono. L'avvio di un download resiste ai crash.
- **Cestino**: i file sostituiti da un upgrade e i dati rimossi "con i dati"
  vanno prima nel cestino, dopo che il torrent ha lasciato il motore.

### Banda e linea condivisa

- **Politiche di velocità**: limiti manuali, una programmazione per giorno della
  settimana (una scelta per giorno: **Non attiva / Limite / Auto**), un override
  temporaneo; *Auto* lascia la linea senza tetto, così il suo picco reale viene
  imparato e proposto come limite.
- **Auto rispetta la latenza** (attivo per default): quando la latenza verso
  Internet sale, Gextto si scansa per qualche secondo per capire di chi è la
  coda. Se è sua, si tiene la linea. Se è di qualcun altro, cede a passi e si
  riprende la linea quando gli altri hanno finito. I contatori UPnP del router,
  quando ci sono, dicono chi altro usa la linea; la pagina Download mostra il
  guardiano mentre agisce.

### Integrazioni, controllo e interfacce

- **Notifiche e media server**: Telegram, e-mail, webhook (Discord, Slack,
  ntfy, Gotify, Pushover), aggiornamento delle cartelle di Jellyfin e Plex, un
  feed iCal di arrivi e date di uscita.
- **Tracciabilità**: ogni download porta un **ID di acquisizione** attraverso i
  log di ricerca, scelta, trasferimento, post-elaborazione e archivio; il
  pannello **Da controllare** della dashboard tiene ogni avviso finché il suo
  download è attivo; un grafico live della banda a 30 minuti; avvisi quando una
  sorgente continua a fallire o rallenta.
- **Interfacce**: web UI responsive installabile come PWA, navigabile da
  tastiera e verificata con axe-core; vista a poster per serie e film; oltre 20
  temi verificati per il contrasto più quelli personalizzati; una TUI completa
  da terminale per SSH che si adatta allo schermo di un telefono. Sei lingue
  (IT, EN, DE, FR, ES, PL).

La web UI ascolta su `http://<host>:5000/`. La configurazione e ogni sezione sono
descritte nel [manuale italiano](docs/MANUAL.it.md) e nell'
[English manual](docs/MANUAL.en.md).

## Motore torrent

Gextto tiene il piano di controllo (coda, punteggi, post-elaborazione,
archiviazione) e delega i trasferimenti a un motore selezionabile
(*Configurazione → Motore torrent*); il cambio è una migrazione coordinata.

| Motore | Dove gira | Quando sceglierlo |
|---|---|---|
| **gx-torrent** (default) | Processo Go separato e supervisionato | Go puro, autonomo, v1/v2/ibrido, DHT e MSE/PE; anche standalone su Linux e Windows; niente WebTorrent/WebRTC |
| qBittorrent-nox | Demone esterno via Web API, o uno che Gextto scarica e gestisce in privato | Lo usi già o preferisci la sua UI ([guida](docs/qbittorrent-nox.md)); anche il primo passo del fallback automatico |

**Fallback automatico.** Se il motore attivo non resta in piedi, Gextto prima
riporta un `gx-torrent` aggiornato al binario precedente, poi passa a un
qBittorrent-nox gestito (scaricato al bisogno, verificato SHA-256) o a uno
esterno, e infine a uno stato di recupero con la UI attiva. Un periodo di attesa
evita il ping-pong; `GEXTTO_ENGINE=qbittorrent` (o `gx-torrent`) forza il
motore per un avvio.

### gx-core e gx-torrent

`gx-core` (`internal/gxcore`) e la sua DHT (`internal/dht`) sono codice
originale scritto in questo repository sotto EUPL-1.2, senza codice derivato da
terzi ([`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES) elenca solo le librerie
collegate). `gx-torrent` lo esegue come processo supervisionato che sopravvive
ai riavvii dell'orchestratore: Gextto si ricollega quando binario e opzioni
coincidono.

- **Protocollo.** BitTorrent v1, solo-v2 e ibrido (BEP 52) da `.torrent` e
  magnet (`urn:btmh:`), con verifica Merkle SHA-256. **34 BEP implementati e
  testati** — [`docs/bep.md`](docs/bep.md) è la fonte di verità: protocollo
  wire, Fast Extension, metadati, PEX con IPv6, multitracker, LSD,
  super-seeding, web seed, torrent privati, indicazioni sui dati locali,
  `lt_donthave`, tracker UDP con scrape, e la suite DHT (IPv6, scrape, ID di
  nodo sicuri, sola lettura, archivio K/V, `urn:btpk:`). Oltre i BEP: tracker
  `ws://`/`wss://` e **feed DHT** (`dht://<pubkey>`).
- **Riuso locale.** Pezzi e blocchi da 16 KiB già verificati da un altro
  torrent locale — stessa disposizione o lo stesso file v2 — vengono copiati e
  ricontrollati invece che scaricati; i torrent privati non si mescolano mai
  (BEP 27).
- **Rete.** TCP e uTP (BEP 29) in parallelo su una porta, un socket UDP per uTP
  e DHT; ritrasmissione uTP adattiva (soglia minima di latenza e backoff per
  connessione); `ut_holepunch` (BEP
  55) come client e relay; UPnP, NAT-PMP e **PCP**; un test della porta che
  distingue NAT cone da simmetrico; SOCKS5 e HTTP CONNECT; un killswitch
  `outgoing-interface` per le VPN.
- **Autogestione.** Il numero di download attivi segue il throughput; i
  torrent lenti e fermi si fanno da parte; la cache disco si dimensiona da RAM e
  classe di storage (NVMe, SSD, HDD, NFS); i download si mettono in pausa prima
  che una cartella si riempia mentre il seeding continua; le code di richiesta
  per peer si adattano e la connessione chiusa è quella più lenta e inattiva.
- **Impronta leggera per progetto.** I pezzi si scrivono direttamente e la page
  cache del sistema unisce l'I/O; `fdatasync` a lotti, mai `O_SYNC` per
  scrittura; file
  sparsi, `fallocate`, cartelle temporanee su RAM disk, mmap opzionale; percorsi
  caldi senza allocazioni (RC4 in place, bitfield a parole, uTP senza copie); la
  blocklist IP si cerca per bisezione senza allocare.
- **Streaming e sicurezza.** Finestra di riproduzione e readahead separate,
  priorità per file al volo, HTTP Range con il `Content-Type` giusto; un
  **media server DLNA/UPnP** opzionale per le TV, con la libreria del NAS e i
  suoi poster; web seed e `xs=`/`as=` protetti da SSRF; quarantena per i peer
  che mandano blocchi corrotti; diagnostica live per peer, tassi di I/O e
  Server-Sent Events.

Le statistiche del motore sono in **Salute → Motore torrent** (`/?view=health`),
in `GET /api/v1/stats` del demone e in `GET /api/torrents/session-stats`.

## Installazione su Linux

Per Linux a 64 bit con systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE
Leap 15.6+/Tumbleweed, Arch. Da root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa in `/opt/gextto`, tiene i dati in `/var/lib/gextto` e serve la UI
sulla porta 5000, con `gx-torrent` accanto a `gexttod` — Go puro, nient'altro da
installare. L'installer esegue prima il programma una volta, così una libreria
mancante o una glibc vecchia vengono segnalate subito. Per default il servizio
gira come **l'utente che lancia l'installer**; `--user gextto` usa un account di
sistema isolato e `--media-group <gruppo>` dà accesso alla libreria su un NAS
altrui.

> [!IMPORTANT]
> Gextto è pensato per una rete fidata: web UI e API sono aperte per default.
> Per l'accesso dall'esterno attiva il login opzionale (*Configurazione →
> Accesso e servizi*; la LAN resta libera) e usa comunque HTTPS tramite reverse
> proxy o VPN. Le scritture da una pagina di un'altra origine vengono rifiutate
> (protezione CSRF). Leggi la [politica di sicurezza](docs/SECURITY.md) prima di
> esporlo.

### Installazione dai sorgenti

Go 1.26.8+; la build è Go puro e la web UI è incorporata. Vedi il
[manuale per sviluppatori](docs/DEVELOPERS.md).

```bash
make build                 # gexttod e gx-torrent in bin/
```

`make build` fa avanzare il numero di build **committato** e condiviso con la
CI, così checkout e release mostrano lo stesso `1.1.<n>`; `gx-torrent` ha un
contatore suo, che avanza solo quando cambia il suo codice. `make test` esegue
la suite completa, `make test-ui` quella nel browser, `make fuzz-smoke` i
fuzzer.

### Primo avvio sicuro

1. Apri `http://<server>:5000`: la prima visita avvia la **procedura guidata**
   (password, cartelle con verifica dei permessi, indexer e TMDB, primo titolo).
   Si riapre da `/?view=setup`.
2. Tieni attivo il **dry-run**, aggiungi un titolo di prova e lancia una ricerca
   manuale.
3. Controlla **Salute** e **Log**, poi attiva la modalità attiva.

Configurazione del NAS e risoluzione dei problemi: il
[manuale utente](docs/MANUAL.it.md).

### Aggiornamento e disinstallazione

Quando esce una release compare il pulsante **Aggiornamento disponibile**;
*Manutenzione → Aggiornamenti* mostra le novità e **Aggiorna ora** fa il backup
dei database, installa e ripristina la versione precedente se la nuova non
parte. Rilanciare l'installer fa lo stesso da shell.

```bash
sudo bash install.sh --help       # tutte le opzioni
sudo bash install.sh --dry-run    # mostra cosa farebbe, senza cambiare nulla
sudo bash install.sh --uninstall  # ferma e rimuove il programma (tiene i dati)
sudo bash install.sh --uninstall --purge   # rimuove anche la cartella dei dati
```

`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, … restano supportati;
`GEXTTO_LOCAL_ARCHIVE=/percorso/gextto-linux-x86_64.tar.gz` installa offline. Da
un checkout: `git pull --ff-only && ./scripts/update.sh`.

## gx-torrent come client autonomo

gx-torrent è anche un client completo senza Gextto, come `qbittorrent-nox` o
`transmission-daemon` — **lo stesso binario** che Gextto pilota, un Gextto più
semplice con tutto ciò che serve a un client autonomo.

- **Pronto per gli `*arr`, e facile da integrare.** Web API v2 di qBittorrent
  (`/api/v2/...`, comprese le chiamate di qBittorrent 5) per Sonarr, Radarr,
  Lidarr, Readarr e le app mobili; la sua API JSON; **metriche Prometheus**
  (`/api/v1/metrics`) per Grafana o Home Assistant, e un **flusso di eventi**
  (`/api/v1/events`) per Node-RED, Home Assistant o script
  ([esempi](docs/API.md#integrating-with-other-systems)).
- **Una web UI familiare a chi usa qBittorrent.** Temi chiaro e scuro; filtri
  per stato, categoria, tag e tracker; scelta delle colonne (24) con colonne
  ridimensionabili salvate sul server; menu contestuale completo; pannello di
  dettaglio ridimensionabile (file raggruppati per cartella, rinomina, peer);
  anteprima del magnet mentre lo scrivi; annullamento dopo una rimozione;
  scorciatoie da tastiera, trascinamento, schede adatte al telefono e una
  pagina **Statistiche** (totali di sessione e di sempre, grafico 1 h/24 h/7 g).
  Ogni pagina verificata con axe-core.
- **RSS e automazione senza decisioni nascoste.** Una pagina **Cercati** che
  dice per ogni titolo cosa è scaricato, in download o visto ma scartato, e
  perché ogni release verrebbe presa o rifiutata; una pagina **RSS e indexer**
  con regole PASS/FAIL ordinate, anteprima live delle corrispondenze, modelli di
  regole, test dei feed, liste di feed incollate, e un **archivio** locale di
  tutto ciò che i feed hanno restituito.
- **Gestione.** Limiti, programmazione per giorno, auto-speed sulla latenza,
  ottimizzazione con un clic, filtro IP con aggiornamento programmato, categorie
  e tag, test della porta.
- **Notifiche** sugli stessi canali di Gextto, con un pulsante di prova.
- **Si aggiorna da solo** dalle build della CI quando cambia il suo codice,
  verificato SHA-256, con ripristino automatico.
- **Piattaforme.** Linux x86_64/aarch64 (unit systemd, `run.sh`, immagine Docker
  non privilegiata) e servizio Windows nativo su amd64/arm64.

Apri `http://<host>:8890/` per la sua procedura guidata. Riferimento completo:
il [documento tecnico di gx-torrent](docs/gx-torrent.md).

## Accessibilità

Navigazione da tastiera, nomi accessibili, gestione del focus nei dialoghi,
tabelle semantiche ordinabili, annunci live, reflow responsive e temi verificati
per il contrasto, coperti da axe-core e Playwright (`cd uiweb/end2end && npm ci
&& npm run test:a11y`). Non è una certificazione legale: servono ancora prove
manuali con screen reader e tecnologie assistive
([ambito e limiti](docs/measurements.md#6-accessibility-verification)).

## Documentazione

| Bisogno | Documento |
|---|---|
| Usare il servizio | [Manuale italiano](docs/MANUAL.it.md) · [English manual](docs/MANUAL.en.md) |
| Misure e metodo | [Misure](docs/measurements.md) |
| Stato, decisioni e prossimi passi | [Evoluzione](docs/evoluzione.md) |
| Architettura e progetto | [Architettura](docs/ARCHITECTURE.md) |
| Specifiche BitTorrent (BEP) | [Conformità BEP](docs/bep.md) |
| NAS, reverse proxy, recupero | [Guida avanzata](docs/ADVANCED.en.md) |
| Integrazioni HTTP | [Riferimento API](docs/API.md) |
| Spostare un'installazione | [Guida alla migrazione](docs/MIGRATION.md) |
| Client da terminale | [Riferimento TUI](docs/tui.md) |
| Motore gx-torrent e client autonomo | [Documento tecnico di gx-torrent](docs/gx-torrent.md) · [gx-core](docs/gx-core.md) |
| Backend qBittorrent-nox | [Guida](docs/qbittorrent-nox.md) |
| Compilare o contribuire | [Manuale per sviluppatori](docs/DEVELOPERS.md) |
| Sicurezza di rete e dei dati | [Politica di sicurezza](docs/SECURITY.md) |

Albero completo: l'[indice della documentazione](docs/README.md).

## Sostieni il progetto

Se Gextto ti è utile e vuoi sostenerne lo sviluppo, una donazione PayPal è
benvenuta — grazie. Un pulsante **Dona** è sempre visibile anche nella UI, in
basso a destra.

[![Dona con PayPal](https://img.shields.io/badge/Dona-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## Uso legale e corretto

Gextto è uno **strumento di automazione dei download**. Non ospita, indicizza né
distribuisce contenuti protetti da diritto d'autore.

- Si collega agli **indexer che configuri tu** (Jackett, Prowlarr, feed RSS
  pubblici). Non include alcun indice proprio.
- Ciò che scarichi è **una tua responsabilità**. Usa Gextto solo per contenuti a
  cui hai diritto di accedere — pubblico dominio, Creative Commons o media che
  possiedi.
- BitTorrent e i motori che Gextto pilota sono tecnologie neutre; questo
  progetto non incoraggia né facilita la pirateria.
- Rilasciato con licenza open source **EUPL 1.2**.

## Licenza

Gextto è distribuito con licenza [EUPL-1.2](LICENSE). Il nucleo del motore
(`gx-core`, `internal/gxcore`) e la DHT (`internal/dht`) sono codice originale
in questo repository con la stessa licenza. Vedi
[THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES) per le librerie di terzi collegate
(anche nella pagina *Licenza* della web UI).

## Ringraziamenti

Grazie a **[DeepSeek](https://www.deepseek.com/)** per il supporto e la pazienza
mentre questo progetto cresceva.
