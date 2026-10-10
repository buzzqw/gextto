# gx-torrent: il motore torrent alternativo in puro Go

`gx-torrent` è un piccolo demone BitTorrent scritto in Go puro sopra la libreria
[`cenkalti/rain`](https://github.com/cenkalti/rain) (base v2.4.2). È il motore predefinito di
Gextto (`torrent_backend = gx-torrent`): non richiede `libtorrent-rasterbar`,
gira in un processo separato e si controlla via REST su `127.0.0.1:8890`, dove
espone anche una pagina web operativa apribile dal browser (per default solo da
questo server: vedi *Interfaccia web* per aprirla alla LAN). Il motore integrato
libtorrent resta selezionabile — sulle build che lo includono — e fa da
fallback automatico se gx-torrent non parte.

Usa una copia del motore gx-core in `internal/gxcore`. Le modifiche sono
descritte in `internal/gxcore/GEXTTO.md`: porta unica, interfaccia uscente,
proxy, filtro IP, selezione dei file e alcune correzioni.

Codice:

| File | Ruolo |
|---|---|
| `cmd/gx-torrent/main.go` | flag, avvio, segnali, spegnimento pulito |
| `cmd/gx-torrent/daemon.go` | sessione gx-core, stato persistente, ciclo della coda, operazioni |
| `cmd/gx-torrent/queue.go` | pianificatore della coda (funzione pura) e coda dinamica |
| `cmd/gx-torrent/storage.go` | symlink per torrent, spostamento e cancellazione sicuri |
| `cmd/gx-torrent/api.go` | REST API con token |
| `cmd/gx-torrent/network.go` | porta, interfacce, proxy, cifratura, filtro IP |
| `cmd/gx-torrent/portmap.go` | apertura della porta sul router (UPnP, NAT-PMP) |
| `cmd/gx-torrent/selection.go` | selezione dei file |
| `cmd/gx-torrent/cache.go` | cache adattiva (RAM disponibile, carico, tipo di storage) |
| `cmd/gx-torrent/stream.go` | streaming HTTP (Range) con readahead dei pezzi |
| `cmd/gx-torrent/ui.go` | pagina web del demone (riepilogo, tabella, dettaglio, azioni) |
| `cmd/gx-torrent/i18n.go` | traduzioni della pagina web (it/en/de/fr/es/pl) |
| `cmd/gx-torrent/lsd.go` | scoperta in rete locale (BEP 14) |
| `cmd/gx-torrent/tracker_health.go` | rimozione dei tracker che non hanno mai risposto |
| `cmd/gx-torrent/lifecycle.go` | log ruotato del demone, watchdog degli orfani, coda del log di Gextto |
| `gxtorrent_engine.go` | adapter `TorrentEngine` lato Gextto |
| `gxtorrent_runtime.go` | avvio e sorveglianza del demone da parte di Gextto |

## Indice

- [Attivazione](#attivazione)
- [Interfaccia web](#interfaccia-web)
  - [Esposizione in rete](#esposizione-in-rete)
- [Uso standalone](#uso-standalone)
- [Rete](#rete)
- [uTP e LSD](#utp-e-lsd)
- [Holepunching (BEP 55)](#holepunching-bep-55)
  - [Verifica](#verifica)
- [Selezione dei file](#selezione-dei-file)
- [Download sequenziale (streaming)](#download-sequenziale-streaming)
- [Torrent BitTorrent v2](#torrent-bittorrent-v2)
- [Statistiche](#statistiche)
- [Diagnostica pezzi](#diagnostica-pezzi)
- [Streaming HTTP (HTTP Range)](#streaming-http-http-range)
- [Cache disco e preallocazione](#cache-disco-e-preallocazione)
- [RAM disk](#ram-disk)
- [Layout su disco e sicurezza dei dati](#layout-su-disco-e-sicurezza-dei-dati)
- [Coda autogestita](#coda-autogestita)
- [Stalled: divisione dei compiti](#stalled-divisione-dei-compiti)
- [Seed](#seed)
- [Super-seeding (BEP 16)](#super-seeding-bep-16)
- [Scritture sul disco di stato](#scritture-sul-disco-di-stato)
- [Limiti di banda](#limiti-di-banda)
- [REST API (v1)](#rest-api-v1)
- [Disponibilità dell'API (snapshot e lock)](#disponibilità-dellapi-snapshot-e-lock)
- [Limiti noti (motore)](#limiti-noti-motore)
- [Test](#test)

## Attivazione

1. `make build` (o `scripts/build-daemon.sh`) compila `bin/gx-torrent` accanto
   a `bin/gexttod`; `make gx-torrent` compila solo il demone. Il pacchetto, `install.sh`
   e `gexttod --update` installano `gx-torrent` accanto a `gexttod`. Il demone ha
   un **numero di build proprio** (`gx-torrent.build_number`), indipendente da
   quello di Gextto: cresce di uno a ogni sua ricompilazione e resta invariato
   quando la build non lo tocca. È la versione `1.1.<n>` riportata da
   `gx-torrent --version`, dalla sua pagina web e da `/api/v1/health`.
2. `gx-torrent` è il motore predefinito di una nuova installazione: non serve
   sceglierlo. Per usare invece libtorrent integrato imposta *Motore torrent* su
   `embedded` e riavvia. Una configurazione che ha già salvato un motore resta
   com'era: il nuovo default vale solo per le installazioni nuove.
3. Gextto **avvia sempre e sorveglia** il demone quando gx-torrent è il motore
   attivo:
   - dati in `DATA_DIR/gx-torrent`;
   - log in `DATA_DIR/gx-torrent/gx-torrent.log`, scritto dal demone stesso e
     ruotato a 5 MB tenendo 4 file; gli errori dei singoli peer (handshake
     scaduti, reset) e degli announce falliti sono a livello debug;
   - cartella di scarico predefinita uguale a quella di libtorrent;
   - il demone viene riavviato se si chiude;
   - **resta acceso quando Gextto si riavvia** (ad esempio per un
     aggiornamento): sotto systemd parte in uno scope proprio
     (`gextto-gx-torrent-*.scope`), quindi l'arresto del servizio non lo tocca.
     Al riavvio Gextto lo riaggancia se ha lo stesso binario e le stesse
     opzioni (`fingerprint` in `/api/v1/health`): peer, coda e trasferimenti
     proseguono senza interruzioni. Se il binario o un'opzione di avvio sono
     cambiati, Gextto lo ferma con SIGTERM (45 secondi di grazia, il motore salva i
     resume data) e ne avvia uno nuovo. `make build` sostituisce
     `bin/gx-torrent` solo se il suo codice è cambiato
     (`bin/gx-torrent.code-sha256`), così un aggiornamento che tocca solo
     gexttod non ferma i download;
   - se Gextto non torna, il demone si ferma da solo dopo 15 minuti senza
     richieste (`-orphan-timeout`); se il motore attivo non è più gx-torrent,
     Gextto all'avvio ferma il demone rimasto acceso. Gextto riaggancia o
     ferma solo un demone con la propria cartella dati;
   - dopo 6 avvii/arresti anomali in 10 minuti Gextto smette di riprovare.
     Su una build con libtorrent integrato torna al motore `embedded` e riavvia
     il servizio; su una build di **puro Go** (quella predefinita) non c'è un
     motore di ripiego, quindi continua con gx-torrent in **modalità sicura**
     (senza uTP né holepunch) e attese crescenti tra i tentativi (1 m, 5 m,
     15 m, 1 h). Uno spegnimento pulito (riavvio di Gextto o del servizio, ad
     esempio per un aggiornamento) non conta: il budget considera solo le uscite
     inattese e gli avvii falliti; un demone che resta attivo abbastanza a lungo
     lo azzera (10 minuti per il budget degli arresti, 30 minuti per azzerare le
     attese crescenti).

   Se sull'URL risponde già un gx-torrent (ad esempio un servizio systemd tuo),
   Gextto usa quello.

Impostazioni (scheda *Motore torrent*, gruppo *gx-torrent*):

| Chiave | Default | Note |
|---|---|---|
| `gxtorrent_url` | `http://127.0.0.1:8890` | URL con cui Gextto raggiunge il demone |
| `gxtorrent_listen` | `127.0.0.1:8890` | indirizzo di ascolto del demone gestito: per default pagina e API restano solo su questo server. Per aprirle alla LAN vedi *Interfaccia web → Esposizione in rete*. La porta viene allineata a quella di `gxtorrent_url` (Gextto deve poterlo raggiungere) |
| `gxtorrent_token` | vuoto | header `X-Gx-Token`; obbligatorio se il demone ascolta in rete |
| `gxtorrent_auto` | `true` | autogestione di gx-torrent: coda dinamica e cache adattiva. Disattivala per fissare a mano slot e cache |
| `gxtorrent_request_timeout_secs` | `15` | 1–300 |
| `gxtorrent_poll_interval_ms` | `1500` | intervallo minimo tra due letture dello stato (250–60000) |
| `gxtorrent_proxy` | vuoto | proxy per gx-torrent: `socks5://[utente:password@]host:porta` o `http://host:porta`. Peer, tracker HTTP e web seed passano dal proxy; DHT e tracker UDP vengono spenti. Viaggia nell'ambiente del demone, non sulla riga di comando |

Le impostazioni di coda e banda restano quelle della sezione *libtorrent*:

- download e seed attivi, limite totale;
- "non contare i torrent lenti";
- coda dinamica con minimo e massimo;
- limiti di velocità standard, programmati e temporanei.

Gextto le inoltra al demone.

## Interfaccia web

All'indirizzo indicato da `gxtorrent_url` (default `http://127.0.0.1:8890`) il
demone non espone solo l'API REST: `GET /` (o `/ui`) apre una **pagina web
operativa** pensata per chi apre quell'indirizzo dal browser, come la Web UI di
qBittorrent. Si aggiorna da sola ogni 2 secondi senza ricaricare la pagina.
La scheda del browser mostra l'icona del demone (una freccia di download blu),
inclusa nella pagina e servita anche da `GET /favicon.ico` senza token.
La pagina è **tradotta** (italiano, inglese, tedesco, francese, spagnolo,
polacco): all'avvio Gextto passa la lingua dell'interfaccia al demone con
`-lang`, e `?lang=xx` la sovrascrive per la singola richiesta.

- **Riepilogo sessione**: in cima i riquadri con i conteggi (torrent, downloading,
  seeding, stalled, paused, moving), porta peer, regole del filtro IP, cache
  read/write, operazioni di I/O e connessioni in entrata; i valori live
  (velocità, scaricato/caricato nella sessione, spazio libero, DHT, porta,
  cifratura) stanno nella barra di stato in basso, sempre visibile, con il
  pulsante *Test ports*.
- **Aggiunta** da un'unica form: magnet, URL a un `.torrent` o file locale
  caricato, con destinazione, pausa, "in cima alla coda", download
  sequenziale, prima/ultima parte e super-seeding; i limiti di seed e velocità
  si impostano per torrent nel dettaglio *Generale*.
- **Tabella** a due righe per torrent (nome e cartella; avanzamento, stato,
  dimensioni, velocità, peer, seed, ratio di condivisione, ETA), con ricerca/filtro per nome, **filtro per stato** nella barra
  laterale, colonne ordinabili, selezione multipla e azioni di gruppo
  (pausa/riprendi/verifica/ri-annuncio/cima/rimozione).
- **Dettaglio per torrent** a schede: *Generale* (dati, pezzi, spazio,
  date, copia magnet, esporta `.torrent`, pin, sposta, limiti di
  velocità/connessioni/upload e seed, **web seed**), *File* (scarica/salta e
  **streaming HTTP** con ▶), *Peers* (flag di connessione, trasporto, cifratura),
  *Trackers* (stato, sciame, aggiunta e **rimozione**), *Pezzi* (mappa colorata).
- **Filtro IP** da URL o file; il campo è precompilato con il filtro
  configurato in Gextto.
- **Test porte**: dalla barra di stato un pulsante verifica che la porta peer
  sia in ascolto e che il router la inoltri (la stessa verifica del pulsante
  *Test porte* di Gextto, sopra `GET /api/v1/portcheck`).
- **Scheda *Gextto log***: le ultime righe di `gextto.log` (200–2000), lette
  solo quando apri la scheda o premi *Ricarica*, con filtro testuale, DEBUG
  nascosti di default e avvisi/errori colorati. Mentre è aperta la tabella
  torrent non si aggiorna.
- **Liste grandi** (solo standalone): la tabella rende al più `?rows=` torrent
  (predefinito 300) e mostra *Showing X of Y · Show more* per allargare la
  finestra; in managed rende sempre tutti i torrent, come prima.
- Scorciatoie: `/` per cercare, `Esc` per chiudere il dettaglio.

Il comando definitivo resta comunque Gextto; la pagina è una comodità per
l'operatore.

### Esposizione in rete

**Per default pagina e API restano solo su questo server**
(`gxtorrent_listen = 127.0.0.1:8890`). Per aprirle a tutta la LAN imposta
`gxtorrent_listen = 0.0.0.0:8890` **e** un `gxtorrent_token`.

- La porta di `gxtorrent_listen` viene **allineata** a quella di
  `gxtorrent_url`: Gextto deve poter raggiungere il demone, quindi una porta
  diversa non ha effetto (viene segnalato nel log).
- Un ascolto non loopback richiede `gxtorrent_token`: senza token Gextto avvia
  il demone in `-insecure` (coerente con la LAN fidata di default di Gextto) e
  lo annota nel log a livello debug.
- Con `gxtorrent_token`, la pagina lo chiede al primo accesso (accetta anche
  `?token=…`) e lo ricorda in un cookie; l'API resta protetta come prima.

## Uso standalone

```
gx-torrent [-listen 127.0.0.1:8890] [-data ~/.local/share/gx-torrent]
           [-download-dir DIR] [-token SEGRETO] [-allowed-roots /srv/media,/data]
           [-peer-ports 6881-6891] [-listen-interface IP|iface]
           [-outgoing-interface wg0] [-proxy socks5://host:porta]
           [-encryption 0|1|2] [-no-dht] [-no-pex] [-no-utp] [-no-holepunch]
           [-no-lsd] [-no-upnp] [-no-natpmp] [-dht-bootstrap host:porta,...]
           [-ipfilter FILE] [-ipfilter-trackers=true] [-lang it]
           [-mode managed|standalone] [-log-file FILE] [-insecure]
           [-debug] [-version]
```

`-mode` sceglie come gira il demone. Senza il flag: **managed** se c'è
`-fingerprint` (lo passa Gextto), **standalone** altrimenti. In managed le flag
passate da Gextto hanno sempre la precedenza e il file `settings.json` nella
cartella dati **non** viene letto (così l'impronta resta stabile); in standalone
`settings.json` fornisce i valori che Gextto non passa — lingua, `listen`,
`download-dir`, `peer-ports`, `auth-user`/`auth-password`, `local-bypass`,
`indexers` — e una flag esplicita vince comunque.

In standalone le POST della pagina (`/ui/*`) sono accettate solo se
`Origin`/`Referer` coincide con l'host della richiesta (protezione CSRF) e l'host
della richiesta (`Host`) deve essere un IP o `localhost`, mai un nome che risolve
a questa macchina (protezione DNS rebinding). La stessa protezione CSRF vale per
le **POST di `/api/v2`** (che non usano un token): un client che non manda
`Origin`/`Referer` resta ammesso. In managed la pagina si comporta come prima (le
POST dell'API v1 restano protette dal token).

**Piattaforme.** Il demone è Go puro e gira anche su **Windows** (amd64 e
arm64): `GOOS=windows CGO_ENABLED=0 go build ./cmd/gx-torrent/`. Le letture di
sistema usano le API Win32, quindi la cache e la diagnostica restano piene:
memoria (`GlobalMemoryStatusEx`) per dimensionare la cache, spazio libero
(`GetDiskFreeSpaceEx`), filesystem di rete (`GetDriveType`), classe del disco
HDD/SSD (seek penalty via `IOCTL_STORAGE_QUERY_PROPERTY`) e gateway di default
per UPnP/NAT-PMP (`GetBestRoute`, cioè la rotta che Windows usa davvero anche
con VPN e LAN insieme, con fallback alla rotta `0.0.0.0` a metrica minore). La
cartella dati di default è
`%LOCALAPPDATA%\gx-torrent`. Il **link di libreria** di ogni torrent
(`DataDir/<id>`) è una **junction** su Windows, non un symlink: la junction non
richiede il privilegio `SeCreateSymbolicLinkPrivilege` (né la Modalità
sviluppatore). `os.Readlink` la legge come un symlink, ma da Go 1.23 `os.Lstat`
la riporta come `ModeIrregular`: il demone la riconosce con `isDirLink`
(`link_*.go`). Rimuovere la junction non tocca mai i dati a cui punta; per
ripuntarla dopo un move la nuova junction è costruita completa sotto un nome
temporaneo e poi scambiata, così una lettura concorrente non vede mai il link
mancante. Su Unix
resta il symlink atomico. La migrazione tra volumi riconosce anche l'errore Windows
`ERROR_NOT_SAME_DEVICE`, quindi il fallback copia funziona come su Unix; senza
il privilegio symlink, un symlink del payload che punta a un file viene copiato
come file invece di far fallire lo spostamento (su Unix resta un symlink).
Restano senza equivalente nativo solo due **ottimizzazioni** di I/O — il
readahead (`fadvise`) e la preallocazione con `fallocate` — che usano il
fallback (`Truncate`): funzionano, senza l'ottimizzazione.

**Installazione su Windows.** Il demone riconosce di essere avviato dal
Service Control Manager (`svc.IsWindowsService`, `service_windows.go`) e gira
come **servizio nativo**: un solo comando avvia il server HTTP e i torrent, e lo
stop del servizio li ferma in modo pulito. Il pacchetto
`scripts/package-gx-torrent-windows.sh` produce
`gx-torrent-windows-<arch>.zip` con `gx-torrent.exe`, `install-service.ps1`
(`New-Service`, avviato in PowerShell elevato), `uninstall-service.ps1` e
`README.txt` con l'alternativa `sc create`. Dati e log in
`%ProgramData%\gx-torrent`; il servizio gira in **sessione 0**, quindi non apre
il browser: l'installazione guidata resta il wizard web su `/ui/setup`.
Installer e unit systemd restano per Linux.

In breve, sul PC Windows:

```powershell
# PowerShell con privilegi di amministratore, nella cartella dello zip estratto
.\install-service.ps1     # registra e avvia il servizio (avvio automatico)
Stop-Service gx-torrent    # ferma
.\uninstall-service.ps1   # rimuove il servizio (i dati restano)
```

Lo zip è pubblicato in ogni release (`gx-torrent-windows-amd64.zip`,
`gx-torrent-windows-arm64.zip`, con checksum `.sha256`). La CI lo verifica su
`windows-latest` a due livelli: il job `windows-platform` esegue i test di
piattaforma (junction, memoria, gateway, setup, move tra volumi, ciclo di vita
del demone `TestDaemonStartServeStop`); il job `windows-service`, non
bloccante, installa davvero il servizio con lo stesso `binPath` del pacchetto,
lo avvia, attende `/api/v1/health` e ne verifica lo stop pulito
(`scripts/gx-torrent-service-selftest.ps1`, riusabile anche a mano).

In standalone, se `settings.json` contiene `auth-password` (hash bcrypt col
prefisso `bcrypt:`), la pagina chiede il login (`/ui/login`, sessione in un
cookie `gx_session`) tranne che dalle sorgenti LAN quando `local-bypass` è attivo
(predefinito). Senza password la pagina resta aperta, così la prima
configurazione è raggiungibile. Altre chiavi: `auth-user` (predefinito `admin`),
`local-bypass` (`true`/`false`).

Al primo avvio in standalone, finché il setup non è completato, la pagina
reindirizza al **wizard** (`/ui/setup`): lingua, cartella di download e cartella
temporanea, porta peer con **test di raggiungibilità** (`/api/v1/portcheck`:
esito, metodo UPnP/NAT-PMP, IP esterno), **limiti di banda** globali
(download/upload in KiB/s, 0 = illimitato), utente e password, "LAN senza
password" e indexer facoltativi. Il wizard scrive `settings.json` e marca il
setup completato; lingua e limiti di banda valgono subito (se i limiti non si
applicano, l'errore è mostrato nella pagina e il setup non viene marcato
completato), cartella e porta al prossimo avvio (la chiave `peer-ports` è letta
quando la flag non è passata).

Il demone apre da solo il browser sul wizard, **dopo** che il server è in
ascolto e solo se c'è una sessione grafica: `rundll32` su Windows (saltato in
sessione 0, cioè servizi e attività senza utente collegato), `xdg-open` su Unix
con fallback `gio`/`sensible-browser` (solo con `DISPLAY`, `WAYLAND_DISPLAY` o
`XDG_SESSION_TYPE`). Negli altri casi il log stampa comunque l'URL da aprire.

In standalone il demone espone anche un'**API compatibile qBittorrent**
(`/api/v2`) per Sonarr, Radarr e le app mobili: `auth/login` e `auth/logout`
(cookie `SID`, la stessa password del login), `app/version`,
`app/webapiVersion`, `app/preferences|setPreferences`, `transfer/info` e
`torrents/info|add|delete|pause|resume|recheck`, più categorie e tag
(`torrents/categories`, `createCategory`, `removeCategories`, `setCategory`,
`torrents/tags`, `createTags`, `deleteTags`, `addTags`, `removeTags`; categoria
e tag viaggiano anche su `torrents/add`), `torrents/properties|files|trackers`
e `torrents/reannounce|setLocation` (più gli alias 5.x `stop`/`start`),
`torrents/toggleSequentialDownload|setSuperSeeding|addTrackers|removeTrackers|editTracker|filePrio|export|setForceStart|setAutoManagement|setDownloadLimit|setUploadLimit|setShareLimits`,
`transfer/downloadLimit|uploadLimit|setDownloadLimit|setUploadLimit` e
`sync/maindata|torrentPeers`. Senza
password configurata l'API è aperta. In managed **non** è esposta: comanda solo
Gextto. La compatibilità è verificata dai test usando il client qBittorrent di
Gextto (`internal/qbittorrent`) contro il demone.

### Container

Il demone standalone si può eseguire in container: il `Dockerfile` nella radice
compila `gx-torrent` (Go puro) e lo impacchetta in `debian:12-slim` con i
certificati CA, in **modalità standalone**, dati in `/data`, UI su `0.0.0.0:8080`
e porta peer `6881` (TCP+UDP); il processo gira come utente non privilegiato.

```sh
docker build -t gx-torrent .
docker run -d --name gx-torrent \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v gx-torrent-data:/data gx-torrent
```

Alla prima apertura (`http://HOST:8080/`) la pagina reindirizza al wizard
(`/ui/setup`).

In standalone la pagina ha anche un filtro **Categorie** nella barra laterale e,
nel dettaglio *Generale*, i campi **Categoria** e **Tag** (scrivere una categoria
nuova la crea). In managed questi controlli non compaiono e l'endpoint
`/ui/category` rifiuta la richiesta.

La **ricerca su indexer** (standalone) usa la chiave `indexers` di
`settings.json`, un array JSON di endpoint Torznab
(`[{"name":"Jackett","url":"http://host:9117","apikey":"…"}]`, così Jackett e
Prowlarr): `GET /ui/search?q=…` interroga tutti, unisce i risultati e li ordina
per seed. Con almeno un indexer configurato compare nella pagina la **casella di
ricerca**, con i risultati e un pulsante *Add* per aggiungere il torrent. In
managed non è disponibile. **Jackett, Prowlarr e MIRCrew** parlano tutti Torznab
(il servizio `mircrew-indexer` espone un endpoint Torznab), quindi si configurano
allo stesso modo.

L'**RSS** (standalone) usa la chiave `feeds`: un array JSON di feed con le
regole, ad esempio
`[{"name":"Serie","url":"http://…rss","include":"1080p,ita","exclude":"cam","category":"tv","savepath":"/srv/tv"}]`.
Un worker (ogni `feed-interval-secs`, predefinito 15 minuti) scarica ogni feed,
applica `include`/`exclude` sul titolo e aggiunge magnet e `.torrent` non ancora
visti (l'elenco degli elementi già aggiunti è persistito, quindi un riavvio non
li ripropone). Nella pagina compaiono un riquadro *Feeds* con *Check feeds* e
`GET /ui/feeds`/`POST /ui/feeds/poll` per lo stato e un controllo immediato. In
managed non è disponibile.

Un feed può anche essere una **ricerca**: al posto di `url` si usa `search` (una
query) e il worker interroga gli **indexer** Torznab configurati, trattando i
risultati come articoli del feed (stesse regole, dedup e azioni). Esempio:
`[{"name":"cerca-serie","search":"Serie S01","include":"1080p"}]`.

Con la chiave `rules` (array JSON) si passa al **set di regole ordinate**, come
BiglyBT: la prima regola che corrisponde decide (`fail: true` scarta, altrimenti
aggiunge). Ogni regola ha `name`, `feeds` (per limitarla ad alcuni feed; vuoto =
tutti), `fail`, `match` e `action`:

- `match`: `include`/`exclude` (sottostringhe), `regex`/`not_regex`,
  `min_size`/`max_size` (byte), `min_seeders`/`max_seeders`, `min_peers`,
  `max_age_days`, `require_episode`, `smart_episode` (aggiunge un episodio di
  una serie una volta sola, tenendo traccia dell'ultimo: come lo smart episode
  filter di qBittorrent);
- `action`: `savepath`, `category`, `tags`, `paused`, `sequential`, `first_last`,
  `top`.

Esempio:
`[{"name":"no-cam","fail":true,"match":{"regex":"(?i)cam"}},{"name":"shows","match":{"require_episode":true,"smart_episode":true},"action":{"category":"tv","savepath":"/srv/tv"}}]`.

La pagina **`/ui/rss`** (standalone) gestisce tutto questo: textarea per feed,
regole e indexer (salvate con validazione), e per ogni feed un pulsante che
mostra gli articoli correnti con la regola che li prenderebbe e un *Add* manuale
(la vista "articoli corrispondenti" di qBittorrent).

Lo **scheduler di banda** (standalone) applica limiti globali alternativi in una
finestra oraria quotidiana, come qBittorrent: con `schedule-enabled` attivo e
`schedule-start`/`schedule-end` (`HH:MM`, anche a cavallo della mezzanotte) il
demone usa `schedule-download`/`schedule-upload` (KiB/s) dentro la finestra e i
limiti normali fuori. I limiti cambiano a caldo, senza riaprire la sessione.

La maggior parte dei flag ha la sua variabile d'ambiente `GX_TORRENT_*`, ad
esempio:

- `GX_TORRENT_LISTEN`, `GX_TORRENT_DATA`, `GX_TORRENT_DOWNLOAD_DIR`;
- `GX_TORRENT_TOKEN`, `GX_TORRENT_ALLOWED_ROOTS`;
- `GX_TORRENT_PEER_PORTS`, `GX_TORRENT_LISTEN_INTERFACE`,
  `GX_TORRENT_OUTGOING_INTERFACE`;
- `GX_TORRENT_PROXY`, `GX_TORRENT_ENCRYPTION`;
- `GX_TORRENT_NO_DHT`, `GX_TORRENT_NO_PEX`, `GX_TORRENT_NO_UTP`,
  `GX_TORRENT_NO_HOLEPUNCH`, `GX_TORRENT_NO_LSD`, `GX_TORRENT_NO_UPNP`,
  `GX_TORRENT_NO_NATPMP`, `GX_TORRENT_DHT_BOOTSTRAP`;
- `GX_TORRENT_IPFILTER`, `GX_TORRENT_IPFILTER_TRACKERS`;
- `GX_TORRENT_LANG`, `GX_TORRENT_MODE`, `GX_TORRENT_LOG_FILE`, `GX_TORRENT_DEBUG`,
  `GX_TORRENT_NOTIFY_URL` (webhook per le notifiche dei feed).

Alcuni flag sono interni (`-insecure`, `-version`, `-fingerprint`,
`-orphan-timeout`, `-ipfilter-source`, `-gextto-log`): li usa solo Gextto per
avviare, riagganciare e sorvegliare il demone.

Gextto, in modalità gestita, passa da solo questi valori dalle impostazioni
*libtorrent* (porte, interfacce, cifratura, DHT, PEX, uTP, LSD, UPnP, NAT-PMP,
filtro IP, nodi bootstrap DHT) e dall'impostazione `gxtorrent_proxy`. Il limite
globale di connessioni (`libtorrent_connections_limit`) viene ripartito tra
dial uscenti e accept entranti del motore. Proxy e token viaggiano nell'ambiente,
non sulla riga di comando. Le modifiche valgono dal riavvio di gextto.

In modalità gestita il demone **non ha restrizioni di percorso**: Gextto decide
le destinazioni, le valida e le limita già con le proprie regole. Il flag
`-allowed-roots` esiste solo per l'**uso standalone** del demone (vedi sotto) e
Gextto non lo imposta mai.

Il demone **in managed** rifiuta di ascoltare su un indirizzo non loopback senza
token (salvo `-insecure`); **in standalone** è permesso, perché l'accesso è
protetto dal login e dalle regole LAN (o aperto se non c'è password). Il server
RPC interno del motore è disattivato.

## Rete

- **Porta unica, come libtorrent.**
  - Il demone usa la prima porta libera dell'intervallo (`-peer-ports`,
    default 6881-6891): TCP per i peer, UDP per il DHT.
  - Tutti i torrent la condividono. L'handshake, anche cifrato, sceglie il
    torrent dall'info hash.
  - `-peer-ports 0` torna alla porta per torrent originale.
- **Apertura sul router.**
  - UPnP (IGD v1 e v2) e NAT-PMP, rinnovati ogni 20 minuti e rimossi allo
    spegnimento.
  - Esito e indirizzo esterno sono in `GET /api/v1/stats` (`port_mapping`).
  - Se nessuno dei due funziona, il log dice di aprire la porta a mano.
- **Interfaccia in ascolto**: `-listen-interface` accetta un IP o un nome di
  interfaccia. Da gextto arriva da `libtorrent_listen_interfaces`
  (es. `wg0:51413`), che ha la precedenza su `libtorrent_port_min/max`.
- **Interfaccia uscente (killswitch VPN).** Con `-outgoing-interface wg0`
  peer, tracker, web seed e DHT escono solo da quell'interfaccia. Se non ha un
  indirizzo, non esce nulla finché non torna.
- **Proxy.**
  - `socks5://[utente:password@]host:porta` oppure `http://host:porta`
    (tunnel CONNECT).
  - Peer, tracker HTTP e web seed passano dal proxy; i nomi dei tracker li
    risolve il proxy.
  - DHT e tracker UDP vengono spenti, perché non possono passare da un proxy
    TCP e altrimenti uscirebbero fuori.
  - Le connessioni in ingresso restano dirette sulla porta unica.
- **Cifratura** (`libtorrent_encryption`):
  - 0: le connessioni in uscita sono in chiaro;
  - 1: si prova la cifratura, con ripiego in chiaro;
  - 2: cifratura obbligatoria in entrata e in uscita.
- **Filtro IP.**
  - Formati accettati: CIDR, intervalli `a.b.c.d-e.f.g.h`, P2P
    (`nome:inizio-fine`) ed eMule `ipfilter.dat`. Le regole eMule con livello
    ≥ 128 consentono il traffico; le righe IPv6 vengono ignorate.
  - Il filtro si applica ai peer in entrata e in uscita, e ai tracker se
    `libtorrent_apply_ip_filter` è attivo.
  - Il pulsante "Aggiorna IP filter" di gextto funziona anche con gx-torrent:
    scarica la lista se è un URL e la ricarica nel demone.
  - Il file viene **aggiornato all'avvio del servizio** e poi **una volta a
    settimana** mentre il servizio resta attivo (oltre al pulsante manuale).

## uTP e LSD

- **uTP** (`libtorrent_utp`, attivo di default) usa la stessa porta UDP dei
  peer, condivisa con il DHT come in libtorrent.
  - In uscita si tentano uTP e TCP insieme e vince il primo che si connette.
  - In entrata l'handshake viene instradato come per TCP.
  - La lista dei peer indica quali sono connessi in uTP.
  - Con un proxy uTP è spento; con l'interfaccia uscente il socket UDP è
    legato all'IP della VPN.
- **LSD**, la scoperta in rete locale (BEP 14, `libtorrent_lsd`).
  - Annunci multicast su 239.192.152.143:6771 ogni 5 minuti per ogni torrent
    attivo non privato.
  - I peer della LAN che annunciano gli stessi torrent vengono aggiunti.
  - È spento con proxy o interfaccia uscente, per non annunciare i torrent
    fuori dal tunnel.

## Holepunching (BEP 55)

Un peer dietro NAT che non può ricevere connessioni in entrata può comunque
essere raggiunto passando da un peer che fa da **relè** (`ut_holepunch`). È lo
standard BEP 55, quindi interoperabile con µTorrent, BitComet, qBittorrent e
libtorrent; non è il relè via nodo DHT proprietario di libtorrent.

- Si attiva con l'impostazione **Holepunching (BEP 55)** in *Configurazione →
  Motore torrent → Ricerca peer e tracker* (attiva di default). Richiede uTP
  (`libtorrent_utp`): senza uTP (o con un proxy) è spento, perché il "buco" si
  apre sulle mappature UDP. Sul demone il flag è `-no-holepunch`
  (`GX_TORRENT_NO_HOLEPUNCH=1`).
- Quando un dial diretto fallisce, gx-torrent chiede a un peer connesso
  (al massimo 8) di fare da relè per quell'endpoint, una volta sola; il relè,
  se è connesso al bersaglio e questi supporta l'estensione, manda un
  `connect` a entrambi e ognuno avvia una connessione uTP verso l'altro.
- Il bersaglio viene ricercato tra i peer connessi per IP e porta; gli errori
  di protocollo (`NoSuchPeer`, `NotConnected`, `NoSupport`, `NoSelf`) vengono
  riportati come previsto dal BEP.
- Limite noto: come per `ut_metadata`/`ut_pex`, il messaggio in entrata viene
  riconosciuto tramite l'ID locale dell'estensione; un peer che assegna a
  `ut_holepunch` un ID diverso da 3 non viene capito (limite preesistente
  dell'architettura estensioni del motore).

### Verifica

Il codec, la decisione del relè, l'inoltro della configurazione e la mappatura
della sorgente sono coperti dai test (`make test-engine`,
`go test ./cmd/gx-torrent/`, `go test .`). Per una prova **reale** su NAT c'è un
harness root-only, `scripts/holepunch-netns-test.sh`:

```bash
make gx-torrent          # costruisce bin/gx-torrent
sudo scripts/holepunch-netns-test.sh
```

Crea due reti client **distinte** dietro **firewall stateful** (nessun NAT: i
client conservano IP e porta peer), un tracker HTTP minimo e quattro daemon con
uTP e holepunch attivi: un seed pubblico e un relay, su IP diversi in `gx-lab`,
più due client (A dietro gx-r1, B dietro gx-r2). Il firewall lascia uscire, DROPa
le nuove connessioni in entrata e accetta le risposte: è il comportamento che
conta per il BEP 55 (filtro "cone" port-preserving). B entra come leecher,
scarica dal seed e completa restando connesso al relay: serve un relay **leecher
interessato** ai suoi dati, perché un seed scarta i peer non interessati quando
completa. Solo allora entra A (leecher): A non raggiunge B direttamente, chiede
l'introduzione al relay; il relay manda `connect` a entrambi e i due dialano su
uTP. Chi diala per primo compare con origine **`holepunch`**, l'altro vede la
connessione come `incoming`: il test passa se compare su uno dei due. I router
rifiutano il TCP verso le porte peer, così il buco si apre su uTP. `--keep` lascia
la topologia attiva per il debug; lo script riporta cosa ha osservato (holepunch
/ diretta / niente) e, sui fallimenti, le righe `holepunch` dei log (rendezvous
inviato/ricevuto, `connect` ricevuto).

Perché firewall e non NAT: netfilter Linux **non emula un NAT port-translating
"cone"**. Un pacchetto diretto all'IP WAN del router (il dial diretto del
leecher, prima del rendezvous) crea una entry conntrack locale che occupa la
porta esterna del peer e impedisce al target di creare la propria mappatura
quando riceve il `connect`; con un solo IP esterno condiviso il buco non può
aprirsi. Il firewall stateful, senza traduzione di indirizzo, riproduce lo stesso
comportamento osservabile (inbound non richiesto bloccato, buco che si apre solo
dopo il dial incrociato) e rende il test deterministico. Il comportamento è
verificato dal criterio `source`/`incoming` su `/api/v1/torrents/<hash>/peers`
(la pagina web del demone mostra la stessa origine).

Nota: seed e relay devono stare su IP diversi perché il motore deduplica i peer per
IP (`connectedPeerIPs`); due daemon sullo stesso IP verrebbero visti come un solo
peer. Entrambi vanno avviati con `-outgoing-interface` uguale all'IP di ascolto,
altrimenti annunciano al tracker il proprio IP "di default" (sbagliato).

## Selezione dei file

Si possono scaricare solo alcuni file di un torrent: priorità 0 = escluso,
qualsiasi altro valore = incluso. Si imposta dalla scheda file di gextto o con
`POST /api/v1/torrents/{hash}/file-priorities` (`priorities=4,0,4`).

- I file esclusi non compaiono mai nella cartella di destinazione. I pezzi a
  cavallo con un file incluso vengono scritti in `DATA/parts/<id>`, come il
  partfile di libtorrent.
- Progresso, dimensione e completamento si riferiscono ai soli file scelti:
  gextto riceve "completato" quando sono pronti quelli.
- Cambiare la selezione ferma il torrent per un attimo, sposta i file
  interessati tra `parts` e destinazione e lo fa ripartire.

## Download sequenziale (streaming)

Con `sequential` il torrent scarica i pezzi in ordine di indice invece che
"rarest-first", e per prima cosa i bordi di ogni file (primo e ultimo ~1%,
fino a 8 MB): è la modalità pensata per lo streaming, così un player può
iniziare mentre il download prosegue. È più lenta nel complesso e peggiora la
salute dello sciame, quindi resta **opzionale e spenta di default**.

- Si imposta al momento dell'aggiunta: dalla pagina web (caselle
  *sequential* e *first/last*), con `sequential=1` / `first_last=1` su
  `POST /api/v1/add`, oppure dai campi `AddOptions.Sequential` /
  `AddOptions.FirstLast` di Gextto (impostazioni *Download sequenziale*,
  `libtorrent_sequential`, e *Prima/ultima parte dei file*,
  `libtorrent_first_last`, oltre alle caselle del form di aggiunta).
- `first_last` scarica per primi i bordi di ogni file e poi prosegue
  **rarest-first**: è indipendente dall'ordine sequenziale e utile allo
  streaming senza rinunciare alla salute dello sciame.
- Il valore predefinito per i torrent aggiunti dopo si imposta con
  `POST /api/v1/config` (`{"sequential":true}`): Gextto lo fa quando cambia
  l'impostazione. **Cambiare questo valore a caldo applica l'ordine anche ai
  torrent già in corso**, come libtorrent. `first_last` non ha un default di
  sessione nel demone: con *Prima/ultima parte dei file* attiva è Gextto ad
  aggiungere `first_last=1` a ogni nuovo torrent.
- il motore può cambiare l'ordine a caldo anche sul singolo torrent
  (`Torrent.SetSequential`/`SetFirstLast` internamente); lo stato è persistito e
  riportato in `GET /api/v1/torrents` (`sequential`, `first_last`).

## Torrent BitTorrent v2

Il motore scarica i torrent **v1**, la parte v1 degli **ibridi** (v1+v2) e i
torrent **solo v2** aggiunti da un file `.torrent` che include i **`piece
layers`** (BEP 52). I pezzi sono costruiti **file per file**: il pezzo di coda di
un file è più corto di `piece length` e **non** contiene padding; ogni pezzo è
verificato con un **nodo Merkle SHA-256** sui blocchi da 16 KiB (foglie mancanti
= hash zero). L'identità del torrent è lo SHA-256 troncato a 20 byte, quindi
handshake, DHT e tracker restano identici a v1.

Anche i torrent **solo v2** senza `piece layers` (i **magnet** v2, `urn:btmh:`)
sono supportati: l'info dict arriva via BEP 9 e i **`piece layers`** per-file
vengono richiesti ai peer con i messaggi `hash request`/`hashes`/`hash reject`
(BEP 52), verificati contro i `pieces root` e persistiti nel resume. Il bit
riservato **v2** (byte 7, `0x10`) è annunciato nell'handshake solo per un torrent
con identità v2, così i peer libtorrent accettano le `hash request`. Il seed
risponde alle richieste del **layer dei pezzi** (`base = piece layer`) dai suoi
`piece layers`, senza leggere i file. Risponde anche alle richieste del **layer
dei blocchi** (`base = 0`), che un leecher libtorrent usa per verificare i
blocchi mentre li scarica: l'albero dei blocchi è costruito dai dati dei pezzi
verificati, **in cache per-file** e solo per un file **interamente presente**
(altrimenti `hash reject`, perché un albero con foglie mancanti non si
ancorerebbe al `pieces root`). Progetto completo in `docs/gx-torrent-v2.md`.

## Statistiche

La pagina **Salute** di gextto ha un pannello "Motore torrent", aggiornato
ogni 15 secondi, per tutti i motori.

Con gx-torrent mostra:

- versione e torrent per stato;
- velocità e slot della coda;
- porta e apertura sul router (in rosso se non riuscita);
- DHT (nodi), uTP (connessioni uTP e TCP), LSD (peer trovati);
- connessioni in entrata e I/O disco (job in coda, letture/scritture totali);
- cifratura, proxy, regole del filtro IP;
- disco e cache, totali della sessione.

Con libtorrent mostra:

- torrent per stato;
- nodi DHT, peer TCP e uTP;
- connessioni in entrata, job su disco, totali.

Gli stessi dati sono in `GET /api/v1/stats` del demone e in
`GET /api/libtorrent/session-stats` di gextto. I contatori condivisi usano i
**nomi di libtorrent** anche per gx-torrent (`net.recv_payload_bytes`,
`peer.num_tcp_peers`, `disk.num_read_ops`, …), così l'endpoint è uniforme; i
contatori specifici del motore (cache, velocità disco, traffico DHT) restano
accanto. Rispetto a libtorrent:

- l'**overhead di protocollo** è disponibile (`protocol_overhead_bytes`: byte
  scambiati sulle connessioni peer meno il payload, cifratura compresa);
- **mancano** i tempi dei job su disco e i pezzi falliti per peer: il motore non li
  misura e non sono implementati.

Il pulsante *Test porte* (in Gextto e nella pagina del demone) non legge
`/api/v1/stats`: interroga `GET /api/v1/portcheck`, che risponde se la porta peer
è in ascolto e se il router la inoltra.

## Diagnostica pezzi

`GET /api/v1/torrents/{hash}/pieces` riporta lo stato di ogni pezzo come
intervalli compatti (`begin`/`end` inclusivi, `state`): `have` (scaricato),
`downloading` (in corso), `skipped` (file escluso) o `""` (mancante). La pagina
web del demone ha una scheda **Pieces** con la mappa colorata; Gextto mostra la
stessa mappa nella scheda **Pezzi** del dettaglio torrent. gx-torrent è il primo
motore di Gextto a esporre la diagnostica dei pezzi (libtorrent risponde "non
disponibile").

## Streaming HTTP (HTTP Range)

Oltre al download sequenziale, il demone sa servire un file a un player mentre
lo scarica: `GET /ui/stream?hash=<hash>&file=<indice>` restituisce il file e
supporta una singola intestazione `Range: bytes=…` (risposta `206` con
`Content-Range`, `Accept-Ranges: bytes`). I pezzi della finestra richiesta, più
circa 16 MB di readahead, vengono chiesti per primi; se un pezzo non è ancora
arrivato il demone aspetta (fino a 2 minuti) invece di servire zeri. Richieste
non soddisfacibili rispondono `416`.

L'autenticazione è quella della pagina web (cookie, header o `?token=`), quindi
anche un player esterno (VLC, mpv) può usare l'URL completo di token. Da Gextto
`GET /api/torrents/{hash}/stream?file=<indice>` reindirizza al demone aggiungendo
il token lato server; nella scheda **Contenuto** del dettaglio torrent ogni file
ha un pulsante **▶**, e la pagina del demone lo ha nella scheda **Files**.

## Cache disco e preallocazione

Le impostazioni libtorrent di gextto valgono anche per gx-torrent, ma la cache
di gx-torrent è **adattiva**: il demone la ricalcola dal carico reale invece di
usare valori fissi.

- **`libtorrent_cache_size` = -1** (predefinito): **cache adattiva**. Il demone
  la ricalcola ogni 3 minuti da:
  - **RAM disponibile** (`MemAvailable`, memoria reclamabile), non la RAM
    totale: su un server occupato la cache si sgonfia da sola;
  - **download attivi** (buffer di scrittura) e **seed attivi** (read cache);
  - **classe dello storage** della cartella di scarico: su HDD o NFS i valori
    raddoppiano (più coalescing, meno round-trip), su SSD/NVMe restano bassi.
  - Tetti: scrittura 96 MB–1,5 GB, lettura 32–512 MB, mai oltre 1/4 della RAM e
    1/8 della memoria disponibile.
  - **Isteresi**: si riapplica solo se il target cambia di oltre il 25%, al
    massimo una volta ogni 10 minuti. Il motore ridimensiona read cache e
    buffer di scrittura sulla sessione in corso: nessuna riapertura.
  - il motore non ha una cache write-back: il valore è un **tetto sui pezzi in volo**,
    e la cache di scrittura/coalescing vera la fa il kernel. La policy serve
    soprattutto a non sovra-dimensionare quando il carico è basso e a dare più
    read cache durante il seed.
- **`libtorrent_cache_size` > 0**: **override manuale**; la dimensione indicata
  (blocchi da 16 KiB) vale per lettura e scrittura e la policy adattiva tace.
- **`libtorrent_cache_expiry`**: dopo quanto scade un blocco in cache.
- **`libtorrent_preallocate`**: i file nuovi vengono riservati per intero con
  `fallocate`; i file esclusi dalla selezione restano sparsi, e su tmpfs (RAM
  disk) restano sparsi sempre: riservarli toglierebbe subito tutta la loro
  dimensione alla RAM.
- **"Ottimizza impostazioni"**: con gx-torrent riporta la cache in automatico e
  la applica subito.
- La dimensione della cache cambia a caldo; scadenza della cache e
  preallocazione invece riaprono la sessione del demone.
- Il pannello Salute e `GET /api/v1/stats` mostrano la cache effettiva, il
  target scelto (`cache_read_mb`, `cache_write_mb`), il **motivo**
  (`cache_reason`), la classe storage (`cache_storage`) e la memoria
  disponibile (`mem_available_mb`).

## RAM disk

Il flusso RAM disk di gextto (scaricare su tmpfs e spostare quando non c'è
più spazio o a fine download) funziona: lo spostamento tra filesystem diversi
copia i file e riparte dal punto in cui era. È provato da un test che sposta
un download al 20% da `/dev/shm` al disco.

- Le copie `.torrent` nella cartella scelta dall'operatore
  (`libtorrent_torrent_copy_dir`) prendono il nome del torrent, come con
  libtorrent; le vecchie copie `<hash>.torrent` vengono rinominate all'avvio.
- Un torrent in download che non ha ancora nessun pezzo verificato viene
  spostato senza copiare i suoi file vuoti: vengono ricreati nella
  destinazione, senza scrivere gigabyte di zeri sul NAS e senza il controllo
  completo che il motore farebbe trovando file già presenti.
- Un torrent con dimensione sconosciuta all'aggiunta (magnet) non parte sul RAM
  disk: resta su disco finché i metadati non rivelano la dimensione, poi viene
  spostato sul RAM disk solo se rientra nella soglia. Un file troppo grande non
  tocca mai il tmpfs. La regola vale anche per gli add manuali e per ogni motore
  con supporto `ramdisk`; qBittorrent (`ramdisk: none`) scarica sempre su disco.
- Lo spazio "riservato" sul RAM disk non conta i torrent che ne stanno già
  uscendo.

## Layout su disco e sicurezza dei dati

il motore salva ogni torrent in `DataDir/<id>` e alla rimozione esegue sempre
`os.RemoveAll(DataDir/<id>)`. Per questo gx-torrent:

- crea **sempre** `DATA/links/<id>` come symlink verso la cartella di
  destinazione reale. I file finiscono in `destinazione/<nome torrent>`, con lo
  stesso layout di libtorrent, quindi `CompletionPath` funziona invariato;
- rimuovere un torrent cancella solo il symlink. I dati si cancellano solo con
  `delete_files=1`, e solo il file o la cartella del torrent, mai la
  destinazione intera né altri file;
- una vecchia cartella reale al posto del symlink (layout precedente) viene
  spostata in `DATA/orphaned/<id>` prima della rimozione, mai cancellata;
- le destinazioni devono essere percorsi assoluti; il flag `-allowed-roots`
  (solo uso standalone, Gextto non lo imposta) può limitarle.

**Spostamento**:

1. il torrent viene fermato;
2. il demone attende che il motore chiuda i file;
3. il contenuto viene spostato, con `rename` oppure con copia e rimozione se
   cambia filesystem (tramite file `.gxpart`);
4. il symlink viene ripuntato in modo atomico;
5. il torrent riparte.

Gextto riceve `storage_moved` quando il nuovo percorso appare nella lista, e
`storage_move_failed` in caso di errore. `AssociateStorage` ripunta senza
spostare e riverifica.

Lo stato proprio del demone sta in `DATA/state.json`, scritto in modo atomico.
Per ogni torrent contiene:

- percorso, posizione in coda, pausa utente, parcheggio per stallo, pin;
- probe in corso, rotazione, limiti di seed;
- dimensione dello sciame dall'ultimo scrape;
- data di completamento ed errore.

Sopravvive ai riavvii insieme al database del motore (`DATA/session.db`).

## Coda autogestita

il motore non ha una coda: la gestisce il demone stesso, ogni 3 secondi (e subito
dopo ogni comando), con la stessa politica che Gextto applica a libtorrent.

- **Slot.** I download attivi (`active_downloads`) partono in ordine di
  posizione in coda. I seed (`active_seeds`) hanno slot propri.
- **Torrent lenti** (`dont_count_slow`, predefinito attivo). Un torrent che da
  `slow_after_secs` (120 s) trasferisce meno di `slow_rate` (2 KiB/s) continua
  a girare ma libera il suo slot, così parte il successivo. Uno sciame morto non
  blocca mai i download sani. Appena avviato, un torrent ha un periodo di grazia.
- **Limite totale** (`active_limit`). È un tetto rigido sui torrent in
  esecuzione, lenti compresi. Quando viene superato, il demone:
  1. non riavvia i torrent appena messi da parte;
  2. ferma i seed lenti, poi i download lenti, dal fondo della coda;
  3. solo dopo rinuncia ad avviare torrent sani.

  I torrent lenti fermati vengono **ruotati** in fondo alla coda e riprovano dopo
  `slow_rotate_secs` (30 min). Così ogni download bloccato viene ritentato
  periodicamente mentre quelli sani procedono, senza avvii e fermate continui
  tra un turno e l'altro.
- **Mai toccati dalla coda.** La coda non avvia mai:
  - i torrent messi in pausa dall'utente (o da Gextto a fine seed);
  - quelli parcheggiati come stalled da Gextto;
  - quelli in errore;
  - quelli in spostamento.
- **Pin e probe.** I torrent con pin e quelli in probe (vedi sotto) girano
  sempre e non occupano slot.
- **Coda dinamica** (`dynamic_queue`, con minimo e massimo). È la stessa logica
  del bridge libtorrent:
  - restringe gli slot se il limite globale di download resta saturo (≥ 90%)
    per dieci minuti;
  - li allarga rapidamente se la banda resta inutilizzata (≤ 70%) e ci sono
    torrent in attesa;
  - con download in coda da 5 minuti, i seed scendono a uno.
- **Mettere in testa.** `QueueTop` all'aggiunta, o l'azione `top`.

Stati riportati a Gextto:

| Stato | Significato |
|---|---|
| `downloading` | in esecuzione (anche allocazione) |
| `downloading_metadata` | magnet in attesa dei metadati |
| `checking_files` | verifica in corso |
| `seeding` | completo, in condivisione |
| `paused` | `auto_managed=true`: in coda, in attesa di uno slot. `auto_managed=false`: in pausa utente o fermo a fine seed |
| `stalled` | parcheggiato da Gextto, oppure messo da parte dalla coda perché non scaricava nulla. Resta `stalled` finché non riparte, così l'orologio degli stalli di Gextto non si azzera |
| `moving` | spostamento in corso |
| `error` | errore del motore o spostamento fallito; un `resume` o un `verify` lo azzerano |

## Stalled: divisione dei compiti

La gestione a lungo termine degli stalli resta in Gextto (`MonitorStalled`),
identica per tutti i motori:

1. dopo `libtorrent_stall_after_min` senza progressi Gextto chiama
   `MarkStalled`, cioè l'azione `park` (fermo, fuori coda, stato `stalled`);
2. i tentativi seguono il backoff 1 h → 3 h → 6 h → 12 h → 24 h e usano
   `Restart`, cioè l'azione `restart`: il torrent gira fuori coda per una
   finestra di probe di 15 minuti e viene riannunciato a tracker e DHT;
3. se arrivano byte, Gextto chiama `ClearStalled` (`unpark`); altrimenti
   lo riparcheggia;
4. scaduto il give-up, Gextto rimuove il torrent e cerca un'altra release.

Il demone aggiunge la gestione **a breve termine**: torrent lenti fuori dagli
slot e rotazione al limite totale. Il parcheggio sopravvive ai riavvii del
demone.

Per la diagnosi (`dead_swarm`, `no_connected_seed`, ...):

- seeder e leecher dello sciame vengono dall'ultimo scrape riuscito dei tracker
  e restano noti anche a torrent fermo;
- i "seed connessi" sono i peer da cui si sta effettivamente ricevendo.

## Seed

I limiti per torrent (`SetLimits` con ratio e giorni) vengono salvati dal
demone e riportati nella lista (`-1` = globale, `0` = infinito). Li applica
`EnforceSeedPolicy` di Gextto, come per gli altri motori: pausa a fine seed,
poi spostamento o rimozione.

## Super-seeding (BEP 16)

Su un torrent **completato** si può attivare il super-seeding: il motore non
annuncia l'intero bitfield, ma **un pezzo alla volta** per peer, e serve solo i
pezzi offerti a quel peer. Serve per il *seeding iniziale*: spinge lo sciame a
scambiarsi i dati invece di scaricarli tutti dal seed, al prezzo di un upload del
seed **volutamente più basso**.

- `POST /api/v1/torrents/{hash}/super-seeding` con `enabled=1/0`, o la casella
  nella pagina del demone; Gextto lo attiva dal tab **Generale** del dettaglio.
- Vale solo a torrent completato: attivato prima, entra in funzione al
  completamento; su un torrent in download o fermo è inerte.
- È una **strategia di seeding**: non tocca coda, slot, seed policy né limiti di
  banda. Quando la seed policy di Gextto (ratio/giorni) ferma il torrent, il
  super-seeding si ferma con lui.
- Il flag è persistito nel resume del demone, quindi sopravvive a un riavvio.
- Come libtorrent, `have` e `not-interested` fanno avanzare gli annunci; i pezzi
  offerti non si ripetono e, quando sono finiti, il peer viene liberato col
  bitfield pieno.

Gextto segnala la capacità `super_seeding` per gx-torrent (`full`).

## Scritture sul disco di stato

Lo stato del demone (`DATA_DIR/gx-torrent`: `session.db`, `state.json`, log)
resta sul disco locale: `session.db` è un database bbolt (mmap e lock), non
adatto a NFS. Le scritture sono contenute: il motore salva statistiche e bitfield
ogni 2 minuti (il demone alza a 2 minuti i 30 s predefiniti di
`ResumeWriteInterval`), circa 100 MB al giorno a riposo; il log ruota a
5 MB × 4.

## Limiti di banda

I limiti globali cambiano a caldo (programmazione, limite temporaneo): il fork
del motore usa un limitatore il cui ritmo si aggiorna mentre i peer lo usano,
quindi nessuna riapertura della sessione e nessun peer perso. Gextto invia la
configurazione solo quando cambia. Riaprono la sessione solo i limiti di peer
(`max_peer_dial`/`max_peer_accept`), la scadenza della cache e la
preallocazione.

Ogni torrent ha un **limite di velocità proprio** (download e upload, KiB/s):
`-1` eredita il globale, `0` è illimitato, un valore positivo è il limite. I
peer del torrent usano un limitatore dedicato che, quando il torrent eredita,
inoltra al limitatore di sessione: un cambio vale subito, senza riconnettere, e
il limite globale continua a valere per la somma dei torrent che lo ereditano.
Si imposta con `POST /api/v1/torrents/{hash}/seed-limits` (campi
`download_limit`, `upload_limit`) o dalla pagina del demone; Gextto lo fa dal
tab **Limiti** del dettaglio torrent. Gextto conserva i valori tra un riavvio e
l'altro e il demone li riapplica alla sessione ricaricata.

Ogni torrent ha anche un **limite di connessioni** e di **slot di upload**
propri (`POST /api/v1/torrents/{hash}/conn-limits`, campi `max_connections` e
`max_uploads`; `-1` globale, `0` illimitato). Il limite di connessioni vale
sulle connessioni instaurate; se lo si abbassa sotto il numero corrente, le
eccedenti vengono chiuse al tick successivo. Gli slot di upload cambiano il
numero di peer che l'unchoker tiene sbloccati.

## REST API (v1)

Tutte le richieste richiedono `X-Gx-Token` (o `Authorization: Bearer`) se il
token è impostato.

| Metodo e percorso | Descrizione |
|---|---|
| `GET /api/v1/health` | stato e versione |
| `GET /api/v1/stats` | contatori: in download, seed, in coda, stalled, lenti, velocità, peer, slot effettivi |
| `GET /api/v1/portcheck` | test porte: la porta peer è in ascolto e il router la inoltra |
| `GET /api/v1/torrents` | lista completa (progresso %, dimensioni, velocità, peer, sciame, stato, percorso, limiti di seed, flag di coda) |
| `POST /api/v1/add` | campi form: `magnet`, `destination`, `paused`, `top`, `sequential`, `first_last`, `super_seeding`, `stop_at_metadata`, `seed_ratio`, `seed_days`. Risponde `{hash, existing}` |
| `POST /api/v1/add-file` | multipart `torrent` più gli stessi campi |
| `DELETE /api/v1/torrents/{hash}?delete_files=1` | rimozione |
| `POST /api/v1/torrents/{hash}/{azione}` | vedi elenco sotto |
| `GET /api/v1/torrents/{hash}/files\|peers\|trackers\|pieces\|torrent-file` | ispezione (i file includono `priority`; `pieces` riporta gli intervalli di stato) |
| `POST /api/v1/pins/clear` | toglie tutti i pin |
| `POST /api/v1/ipfilter` | ricarica il filtro IP (`path` facoltativo) |
| `GET /api/v1/config` | configurazione e slot effettivi |
| `POST /api/v1/config` | JSON parziale: chiavi sconosciute rifiutate |

Azioni disponibili su `POST /api/v1/torrents/{hash}/{azione}`:

- `pause`, `resume`, `verify`, `reannounce`;
- `restart` (con `probe_secs`), `park`, `unpark`;
- `pin` (con `pinned=1/0`), `top`;
- `move` e `associate` (con `destination`);
- `seed-limits` (`seed_ratio`, `seed_days`, `download_limit`, `upload_limit` in KiB/s: -1 globale, 0 illimitato);
- `conn-limits` (`max_connections`, `max_uploads`: -1 globale, 0 illimitato);
- `trackers` (con `urls`, uno per riga);
- `set-trackers` (con `urls`, uno per riga: **sostituisce** la lista, elenco vuoto la azzera);
- `webseeds` (con `urls`, uno per riga, e `remove=1` per rimuoverli);
- `super-seeding` (con `enabled=1/0`: BEP 16 sul torrent completato);
- `file-priorities` (con `priorities`, separate da virgola).

Chiavi accettate da `POST /api/v1/config`:

- `active_downloads`, `active_seeds`, `active_limit`;
- `dont_count_slow`, `slow_rate`, `slow_after_secs`, `slow_rotate_secs`;
- `dynamic_queue`, `dynamic_min`, `dynamic_max`;
- `speed_limit_download`, `speed_limit_upload` (KiB/s);
- `max_peer_dial`, `max_peer_accept`;
- `cache_mb`, `cache_ttl_secs`, `preallocate` (cache e preallocazione: `cache_mb`
  ≤ 0 = adattiva);
- `auto` (autogestione: coda dinamica e cache adattiva);
- `sequential` (predefinito per i torrent aggiunti dopo).

## Disponibilità dell'API (snapshot e lock)

Gextto interroga `GET /api/v1/torrents` a ogni polling (1,5 s) con un timeout di
15 s: se la risposta non arriva, il demone viene considerato non raggiungibile.
La lista è quindi servita da uno **snapshot** pubblicato dal tick della coda e
dai comandi che modificano lo stato, **senza prendere `d.mu` e senza toccare il
run loop di alcun torrent**. Un torrent bloccato su I/O di storage (per esempio
una copia lunga verso un mount di rete) non può così far sembrare l'API morta.

Il tick lavora in tre fasi, per non tenere mai `d.mu` durante un'operazione che
può bloccarsi:

1. **Campionamento** (senza lock): per ogni torrent legge stats, peer e tracker
   dal suo run loop e classifica lo storage (lo `statfs` sul mount di rete può
   bloccarsi a lungo).
2. **Applicazione** (con `d.mu`): aggiorna meta e coda a partire dai campioni,
   calcola il piano e pubblica lo snapshot. Nessuna chiamata al run loop qui.
3. **Azioni** (senza lock): esegue `Start`/`Stop` decisi dal piano; se il run
   loop è occupato, l'azione viene semplicemente ritentata al tick successivo.

Lo snapshot può essere vecchio al massimo di un tick (3 s predefiniti); ogni
comando che cambia lo stato fa ripartire subito un tick, quindi la UI e Gextto
vedono la modifica quasi immediatamente.

## Limiti noti (motore)

Le operazioni che il motore non supporta rispondono con un errore esplicito di
capacità (`ErrCapabilityUnavailable`), mai con un falso successo:

- livelli di priorità dei file oltre a incluso/escluso;
- v2 come **seed**: le richieste `hash request` del layer dei blocchi
  (`base = 0`) sono servite solo per un file **interamente presente** e vengono
  costruite leggendo i dati (una volta per file, poi in cache); per un file
  incompleto rispondono `hash reject` (vedi *Torrent BitTorrent v2*);
- IPv6: **supporto parziale** (vedi *IPv6* qui sotto).

Il **download sequenziale** e la priorità **prima/ultima parte** sono supportati
dalla base v2.4.2 (vedi la sezione dedicata): il motore scarica per primi i bordi di
ogni file (~1% della dimensione, fino a 8 MB), così i player trovano subito
l'indice. Il fork rende la prima/ultima parte **indipendente** dall'ordine
sequenziale.

Gli slot di upload e le connessioni per torrent **non** sono un limite: si
impostano con `conn-limits` (e i limiti di banda con `seed-limits`), come visto
in *Limiti di banda*.

### IPv6

Il listener a porta unica (TCP) e la socket UDP condivisa da uTP e DHT sono
**dual-stack**: se il host di ascolto è non specificato (`0.0.0.0`, `::` o
vuoto) si apre un unico socket che accetta sia IPv4 sia IPv6 e, se la macchina
non ha IPv6, si ripiega su IPv4. Un indirizzo specifico resta mono-famiglia.

I peer IPv6 si scoprono dai tracker (campo `peers6`, BEP 7), dallo scambio PEX
(`added6`/`dropped6`, BEP 11) e dalla **scoperta DHT su IPv6** (BEP 32): il DHT
condivide la socket dual-stack, tiene i nodi v4 e v6 separati nei campi
`nodes`/`nodes6` e chiede entrambe le famiglie (`want`). Peer, tracker HTTP e
**UDP**, e web seed vengono risolti anche su IPv6 (con preferenza per IPv4
quando il nome ne ha entrambi). Il campo `yourip` dell'handshake esteso è
accettato sia in forma IPv4 sia IPv6, il **filtro IP** accetta regole CIDR IPv6
(le forme a intervallo/P2P/eMule restano IPv4) e il motore accetta i peer DHT
IPv6 (contatti da 18 byte).

Manca solo la scoperta locale (LSD) via multicast IPv6: non esiste un gruppo
multicast IPv6 standard (qBittorrent non la implementa), quindi non è prevista.

## Test

- `go test ./cmd/gx-torrent/` copre:
  - il pianificatore (slot, lenti, rotazione con raffreddamento, tetto rigido,
    forzati, coda dinamica);
  - un ciclo di vita reale su una sessione gx-core senza rete (aggiunta, verifica,
    pausa utente rispettata dalla coda, spostamento, rimozione che preserva i
    dati, cancellazione mirata, parcheggio e probe, persistenza dopo il riavvio
    della sessione);
  - token e configurazione;
  - la pagina web (pagina, frammenti live, azioni, token, favicon) e le
    traduzioni della sua lingua;
  - l'origine dei peer (incluso `holepunch`), l'holepunch legato a uTP e il
    test porte;
  - la scoperta LSD, la diagnostica dei pezzi e lo streaming con `Range`;
  - trasferimenti reali tra due demoni in locale:
    - attraverso la porta unica, in chiaro e con cifratura forzata;
    - con selezione dei file e riattivazione di un file escluso;
    - via proxy SOCKS5 e HTTP;
    - con spostamento da RAM disk a metà download;
    - solo in uTP, con il DHT sullo stesso socket UDP;
    - con il peer trovato via LSD (multicast reale);
  - caricamento del filtro IP.
  - (opt-in, `GX_MEASURE=1` / `make measure-seeding`) la misura seeding/choking
    su sciame locale, con byte e tempi per peer: metodo e metriche in
    `docs/evoluzione.md` (§6).
- `go test ./internal/blocklist` in `internal/gxcore` copre i formati del
  filtro IP.
- `make test-engine` copre anche l'harness deterministico del choking
  (`internal/unchoker/sim_test.go`) e il super-seeding
  (`torrent/torrent_superseed_test.go`).
- `go test -run GxEngine .` copre l'adapter:
  - stati ed eventi;
  - cache su interruzione;
  - park, probe e unpark;
  - invio della politica di coda;
  - operazioni e token;
  - `storage_moved`.
