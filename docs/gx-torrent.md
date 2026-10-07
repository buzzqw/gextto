# gx-torrent: il motore torrent alternativo in puro Go

`gx-torrent` è un piccolo demone BitTorrent scritto in Go puro sopra la libreria
[`cenkalti/rain`](https://github.com/cenkalti/rain) (base v2.4.2). È il motore predefinito di
Gextto (`torrent_backend = gx-torrent`): non richiede `libtorrent-rasterbar`,
gira in un processo separato e si controlla via REST su `127.0.0.1:8890`, dove
espone anche una pagina web operativa apribile dal browser (per default su tutta
la LAN). Il motore integrato libtorrent resta selezionabile e fa da fallback
automatico se gx-torrent non parte.

Usa una copia modificata di rain in `third_party/rain`. Le modifiche sono
descritte in `third_party/rain/GEXTTO.md`: porta unica, interfaccia uscente,
proxy, filtro IP, selezione dei file e alcune correzioni.

Codice:

| File | Ruolo |
|---|---|
| `cmd/gx-torrent/main.go` | flag, avvio, segnali, spegnimento pulito |
| `cmd/gx-torrent/daemon.go` | sessione rain, stato persistente, ciclo della coda, operazioni |
| `cmd/gx-torrent/queue.go` | pianificatore della coda (funzione pura) e coda dinamica |
| `cmd/gx-torrent/storage.go` | symlink per torrent, spostamento e cancellazione sicuri |
| `cmd/gx-torrent/api.go` | REST API con token |
| `cmd/gx-torrent/network.go` | porta, interfacce, proxy, cifratura, filtro IP |
| `cmd/gx-torrent/portmap.go` | apertura della porta sul router (UPnP, NAT-PMP) |
| `cmd/gx-torrent/selection.go` | selezione dei file |
| `gxtorrent_engine.go` | adapter `TorrentEngine` lato Gextto |
| `gxtorrent_runtime.go` | avvio e sorveglianza del demone da parte di Gextto |

## Attivazione

1. `make build` (o `scripts/build-daemon.sh`) compila `bin/gx-torrent` accanto
   a `bin/gexttod`; `make gx-torrent` compila solo il demone. Il pacchetto, `install.sh`
   e `gexttod --update` installano `gx-torrent` accanto a `gexttod`.
2. `gx-torrent` è il motore predefinito di una nuova installazione: non serve
   sceglierlo. Per usare invece libtorrent integrato imposta *Motore torrent* su
   `embedded` e riavvia. Una configurazione che ha già salvato un motore resta
   com'era: il nuovo default vale solo per le installazioni nuove.
3. Gextto **avvia sempre e sorveglia** il demone quando gx-torrent è il motore
   attivo:
   - dati in `DATA_DIR/gx-torrent`;
   - log in `DATA_DIR/gx-torrent/gx-torrent.log`;
   - cartella di scarico predefinita uguale a quella di libtorrent;
   - il demone viene riavviato se si chiude e fermato con SIGTERM all'uscita,
     con 45 secondi di grazia prima del `Kill()` (rain salva i resume data);
   - dopo 6 avvii/arresti anomali in 10 minuti Gextto smette di riprovare e
     torna a libtorrent. Uno spegnimento pulito (riavvio di Gextto o del
     servizio, ad esempio per un aggiornamento) non conta: il budget considera
     solo le uscite inattese e gli avvii falliti, e un demone che resta attivo
     abbastanza a lungo lo azzera.

   Se sull'URL risponde già un gx-torrent (ad esempio un servizio systemd tuo),
   Gextto usa quello.

Impostazioni (scheda *Motore torrent*, gruppo *gx-torrent*):

| Chiave | Default | Note |
|---|---|---|
| `gxtorrent_url` | `http://127.0.0.1:8890` | URL con cui Gextto raggiunge il demone |
| `gxtorrent_listen` | `0.0.0.0:8890` | indirizzo di ascolto del demone gestito: la pagina e l'API sono aperte a tutta la LAN. La porta viene allineata a quella di `gxtorrent_url` (Gextto deve poterlo raggiungere). Per tenerlo solo su questo server usa `127.0.0.1:8890`. Un ascolto non loopback richiede `gxtorrent_token` (senza token Gextto avvia il demone in `-insecure` e lo segnala nel log) |
| `gxtorrent_token` | vuoto | header `X-Gx-Token`; obbligatorio se il demone ascolta in rete |
| `gxtorrent_auto` | `true` | autogestione di gx-torrent: coda dinamica e cache adattiva. Disattivala per fissare a mano slot e cache |
| `gxtorrent_request_timeout_secs` | `15` | 1–300 |
| `gxtorrent_poll_interval_ms` | `1500` | intervallo minimo tra due letture dello stato (250–60000) |

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
qBittorrent. Si aggiorna da sola ogni 5 secondi senza ricaricare la pagina.

- **Riepilogo sessione**: stato torrent, velocità, totali, porta/router, DHT,
  uTP, cifratura, filtro IP, cache, spazio libero; barra di stato in basso.
- **Aggiunta** da magnet, da URL a un `.torrent` (incolli l'indirizzo) o da file
  locale caricato, con destinazione, pausa, "in cima alla coda", download
  sequenziale, prima/ultima parte e limiti seed.
- **Tabella** con ricerca/filtro per nome, **filtro per stato** nella barra
  laterale, colonne ordinabili, selezione multipla e azioni di gruppo
  (pausa/riprendi/verifica/ri-annuncio/cima/rimozione).
- **Dettaglio per torrent** a schede: *Generale* (dati, pezzi, spazio,
  date, copia magnet, esporta `.torrent`, pin, sposta, limiti seed), *File*
  (scarica/salta), *Peer* (flag di connessione, trasporto, cifratura),
  *Tracker* (stato, sciame, aggiunta).
- **Filtro IP** da URL o file.
- Scorciatoie: `/` per cercare, `Esc` per chiudere il dettaglio.

Il comando definitivo resta comunque Gextto; la pagina è una comodità per
l'operatore.

**La pagina (e l'API) sono aperte a tutta la LAN** con il default
`gxtorrent_listen = 0.0.0.0:8890`. Gextto continua a parlare col demone
sull'URL configurato; la porta di `gxtorrent_listen` viene **allineata** a
quella di `gxtorrent_url` per garantire che il demone resti raggiungibile. Per
tenere pagina e API solo su questo server imposta `gxtorrent_listen =
127.0.0.1:8890`.

Un ascolto non loopback richiede `gxtorrent_token`: senza token Gextto avvia il
demone con `-insecure` (coerente con la LAN fidata di default di Gextto) e lo
segnala con un avviso nel log. Se è impostato `gxtorrent_token`, la pagina lo
chiede al primo accesso (accetta anche `?token=…`) e lo ricorda in un cookie;
l'API resta protetta come prima.

## Uso standalone

```
gx-torrent [-listen 127.0.0.1:8890] [-data ~/.local/share/gx-torrent]
           [-download-dir DIR] [-token SEGRETO] [-allowed-roots /srv/media,/data]
           [-peer-ports 6881-6891] [-listen-interface IP|iface]
           [-outgoing-interface wg0] [-proxy socks5://host:porta]
           [-encryption 0|1|2] [-no-dht] [-no-pex] [-no-utp] [-no-lsd]
           [-no-upnp] [-no-natpmp] [-dht-bootstrap host:porta,...]
           [-ipfilter FILE] [-ipfilter-trackers=true] [-debug] [-version]
```

Ogni flag ha la sua variabile d'ambiente `GX_TORRENT_*`, ad esempio:

- `GX_TORRENT_LISTEN`, `GX_TORRENT_DATA`, `GX_TORRENT_DOWNLOAD_DIR`;
- `GX_TORRENT_TOKEN`, `GX_TORRENT_ALLOWED_ROOTS`;
- `GX_TORRENT_PEER_PORTS`, `GX_TORRENT_LISTEN_INTERFACE`,
  `GX_TORRENT_OUTGOING_INTERFACE`;
- `GX_TORRENT_PROXY`, `GX_TORRENT_ENCRYPTION`;
- `GX_TORRENT_NO_DHT`, `GX_TORRENT_NO_PEX`, `GX_TORRENT_NO_UPNP`,
  `GX_TORRENT_NO_NATPMP`, `GX_TORRENT_DHT_BOOTSTRAP`;
- `GX_TORRENT_IPFILTER`, `GX_TORRENT_IPFILTER_TRACKERS`;
- `GX_TORRENT_DEBUG`.

Gextto, in modalità gestita, passa da solo questi valori dalle impostazioni
*libtorrent* (porte, interfacce, cifratura, DHT, PEX, uTP, LSD, UPnP, NAT-PMP,
filtro IP, nodi bootstrap DHT) e dall'impostazione `gxtorrent_proxy`. Il limite
globale di connessioni (`libtorrent_connections_limit`) viene ripartito tra
dial uscenti e accept entranti di rain. Proxy e token viaggiano nell'ambiente,
non sulla riga di comando. Le modifiche valgono dal riavvio di gextto.

In modalità gestita il demone **non ha restrizioni di percorso**: Gextto decide
le destinazioni, le valida e le limita già con le proprie regole. Il flag
`-allowed-roots` esiste solo per l'**uso standalone** del demone (vedi sotto) e
Gextto non lo imposta mai.

Il demone rifiuta di ascoltare su un indirizzo non loopback senza token (salvo
`-insecure`). Il server RPC interno di rain è disattivato.

## Rete

- **Porta unica, come libtorrent.**
  - Il demone usa la prima porta libera dell'intervallo (`-peer-ports`,
    default 6881-6891): TCP per i peer, UDP per il DHT.
  - Tutti i torrent la condividono. L'handshake, anche cifrato, sceglie il
    torrent dall'info hash.
  - `-peer-ports 0` torna alla porta per torrent di rain originale.
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
  `AddOptions.FirstLast` di Gextto (impostazione *Download sequenziale*,
  `libtorrent_sequential`, e la casella prima/ultima parte).
- `first_last` scarica per primi i bordi di ogni file e poi prosegue
  **rarest-first**: è indipendente dall'ordine sequenziale e utile allo
  streaming senza rinunciare alla salute dello sciame.
- Il valore predefinito per i torrent aggiunti dopo si imposta con
  `POST /api/v1/config` (`{"sequential":true}`): Gextto lo fa quando cambia
  l'impostazione. `first_last` non ha un default di sessione.
- rain fissa l'ordine quando il torrent viene aggiunto: l'opzione vale per i
  torrent **nuovi**, non cambia quelli già in corso. Lo stato è persistito e
  riportato in `GET /api/v1/torrents` (`sequential`, `first_last`).

## Torrent BitTorrent v2

rain gestisce i torrent v1 e la parte v1 dei torrent ibridi (v1+v2), cioè la
grande maggioranza. I torrent **solo v2** (magnet con solo `btmh`, `.torrent`
senza `pieces`) vengono rifiutati con un errore chiaro. Gextto in quel caso:

1. mette la release in blocklist, così non la ritenta a ogni ciclo;
2. passa al candidato successivo.

Il supporto completo a v2 richiederebbe in rain:

- gli alberi di hash SHA-256;
- l'handshake con l'hash troncato;
- lo scambio dei piece layer.

È un lavoro grosso, non fatto.

## Statistiche

La pagina **Salute** di gextto ha un pannello "Motore torrent", aggiornato
ogni 15 secondi, per tutti i motori.

Con gx-torrent mostra:

- versione e torrent per stato;
- velocità e slot della coda;
- porta e apertura sul router (in rosso se non riuscita);
- DHT (nodi), uTP (connessioni uTP e TCP), LSD (peer trovati);
- cifratura, proxy, regole del filtro IP;
- disco e cache, totali della sessione.

Con libtorrent mostra:

- torrent per stato;
- nodi DHT, peer TCP e uTP;
- connessioni in entrata, job su disco, totali.

Gli stessi dati sono in `GET /api/v1/stats` del demone e in
`GET /api/libtorrent/session-stats` di gextto. Rispetto a libtorrent:

- l'**overhead di protocollo** è disponibile (`protocol_overhead_bytes`: byte
  scambiati sulle connessioni peer meno il payload, cifratura compresa);
- **mancano** i tempi dei job su disco e i pezzi falliti per peer: rain non li
  misura e non sono implementati.

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
    massimo una volta ogni 10 minuti, perché rain legge la cache solo alla
    creazione della sessione (un cambio = riapertura sessione).
  - rain non ha una cache write-back: il valore è un **tetto sui pezzi in volo**,
    e la cache di scrittura/coalescing vera la fa il kernel. La policy serve
    soprattutto a non sovra-dimensionare quando il carico è basso e a dare più
    read cache durante il seed.
- **`libtorrent_cache_size` > 0**: **override manuale**; la dimensione indicata
  (blocchi da 16 KiB) vale per lettura e scrittura e la policy adattiva tace.
- **`libtorrent_cache_expiry`**: dopo quanto scade un blocco in cache.
- **`libtorrent_preallocate`**: i file nuovi vengono riservati per intero con
  `fallocate`; i file esclusi dalla selezione restano sparsi.
- **"Ottimizza impostazioni"**: con gx-torrent riporta la cache in automatico e
  la applica subito.
- Un cambio di questi valori riapre la sessione del demone, come per i limiti
  di velocità.
- Il pannello Salute e `GET /api/v1/stats` mostrano la cache effettiva, il
  target scelto (`cache_read_mb`, `cache_write_mb`), il **motivo**
  (`cache_reason`), la classe storage (`cache_storage`) e la memoria
  disponibile (`mem_available_mb`).

## RAM disk

Il flusso RAM disk di gextto (scaricare su tmpfs e spostare quando non c'è
più spazio o a fine download) funziona: lo spostamento tra filesystem diversi
copia i file e riparte dal punto in cui era. È provato da un test che sposta
un download al 20% da `/dev/shm` al disco.

## Layout su disco e sicurezza dei dati

rain salva ogni torrent in `DataDir/<id>` e alla rimozione esegue sempre
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
2. il demone attende che rain chiuda i file;
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

Sopravvive ai riavvii insieme al database di rain (`DATA/session.db`).

## Coda autogestita

rain non ha una coda: la gestisce il demone stesso, ogni 3 secondi (e subito
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
| `error` | errore di rain o spostamento fallito; un `resume` o un `verify` lo azzerano |

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

## Limiti di banda

rain legge i limiti globali solo alla creazione della sessione. Quando Gextto
cambia i limiti (programmazione, limite temporaneo), il demone riapre la
sessione:

- in un momento senza spostamenti in corso;
- solo se i valori cambiano davvero (Gextto invia la configurazione solo quando
  cambia);
- i torrent riprendono esattamente come prima, grazie al flag "started" di rain
  e allo stato del demone.

## REST API (v1)

Tutte le richieste richiedono `X-Gx-Token` (o `Authorization: Bearer`) se il
token è impostato.

| Metodo e percorso | Descrizione |
|---|---|
| `GET /api/v1/health` | stato e versione |
| `GET /api/v1/stats` | contatori: in download, seed, in coda, stalled, lenti, velocità, peer, slot effettivi |
| `GET /api/v1/torrents` | lista completa (progresso %, dimensioni, velocità, peer, sciame, stato, percorso, limiti di seed, flag di coda) |
| `POST /api/v1/add` | campi form: `magnet`, `destination`, `paused`, `top`, `sequential`, `first_last`, `stop_at_metadata`, `seed_ratio`, `seed_days`. Risponde `{hash, existing}` |
| `POST /api/v1/add-file` | multipart `torrent` più gli stessi campi |
| `DELETE /api/v1/torrents/{hash}?delete_files=1` | rimozione |
| `POST /api/v1/torrents/{hash}/{azione}` | vedi elenco sotto |
| `GET /api/v1/torrents/{hash}/files\|peers\|trackers\|torrent-file` | ispezione (i file includono `priority`) |
| `POST /api/v1/pins/clear` | toglie tutti i pin |
| `POST /api/v1/ipfilter` | ricarica il filtro IP (`path` facoltativo) |
| `GET /api/v1/config` | configurazione e slot effettivi |
| `POST /api/v1/config` | JSON parziale: chiavi sconosciute rifiutate |

Azioni disponibili su `POST /api/v1/torrents/{hash}/{azione}`:

- `pause`, `resume`, `verify`, `reannounce`;
- `restart` (con `probe_secs`), `park`, `unpark`;
- `pin` (con `pinned=1/0`), `top`;
- `move` e `associate` (con `destination`);
- `seed-limits`, `trackers` (con `urls`, uno per riga);
- `file-priorities` (con `priorities`, separate da virgola).

Chiavi accettate da `POST /api/v1/config`:

- `active_downloads`, `active_seeds`, `active_limit`;
- `dont_count_slow`, `slow_rate`, `slow_after_secs`, `slow_rotate_secs`;
- `dynamic_queue`, `dynamic_min`, `dynamic_max`;
- `speed_limit_download`, `speed_limit_upload` (KiB/s);
- `max_peer_dial`, `max_peer_accept`;
- `sequential` (predefinito per i torrent aggiunti dopo).

## Limiti noti (rain)

Le operazioni che rain non supporta rispondono con un errore esplicito di
capacità (`ErrCapabilityUnavailable`), mai con un falso successo:

- livelli di priorità dei file oltre a incluso/escluso;
- web seed aggiunti a mano;
- limiti di velocità e connessioni per singolo torrent;
- super-seeding e upload mode;
- rimozione di tracker (l'aggiunta funziona);
- torrent solo v2 (vedi sopra);
- IPv6: il listener a porta unica, il DHT e uTP usano socket IPv4.

Il **download sequenziale** e la priorità **prima/ultima parte** sono supportati
dalla base v2.4.2 (vedi la sezione dedicata): rain scarica per primi i bordi di
ogni file (~1% della dimensione, fino a 8 MB), così i player trovano subito
l'indice. Il fork rende la prima/ultima parte **indipendente** dall'ordine
sequenziale.

Gli slot di upload e le connessioni per torrent restano quelli predefiniti di
rain.

## Test

- `go test ./cmd/gx-torrent/` copre:
  - il pianificatore (slot, lenti, rotazione con raffreddamento, tetto rigido,
    forzati, coda dinamica);
  - un ciclo di vita reale su una sessione rain senza rete (aggiunta, verifica,
    pausa utente rispettata dalla coda, spostamento, rimozione che preserva i
    dati, cancellazione mirata, parcheggio e probe, persistenza dopo il riavvio
    della sessione);
  - token e configurazione;
  - trasferimenti reali tra due demoni in locale:
    - attraverso la porta unica, in chiaro e con cifratura forzata;
    - con selezione dei file e riattivazione di un file escluso;
    - via proxy SOCKS5 e HTTP;
    - con spostamento da RAM disk a metà download;
    - solo in uTP, con il DHT sullo stesso socket UDP;
    - con il peer trovato via LSD (multicast reale);
  - caricamento del filtro IP.
- `go test ./internal/blocklist` in `third_party/rain` copre i formati del
  filtro IP.
- `go test -run GxEngine .` copre l'adapter:
  - stati ed eventi;
  - cache su interruzione;
  - park, probe e unpark;
  - invio della politica di coda;
  - operazioni e token;
  - `storage_moved`.
