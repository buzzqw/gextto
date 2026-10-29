# Docker: stack completo con Gextto, indexarr, Jackett, Prowlarr, FlareSolverr e aMule

Questa guida avvia **tutto** in Docker: Gextto con il suo `gx-torrent` gestito,
indexarr con le sue fonti native, Jackett e Prowlarr per gli indexer Torznab, FlareSolverr per le pagine protette
da Cloudflare e `amuled` per eD2k/Kad. È pensata per un host Linux con Docker
Engine e il plugin `docker compose`; non sostituisce l'installazione con
`install.sh`, che resta quella consigliata su una macchina dedicata.

Il file pronto è:

```text
docker-full/docker-compose.yml
```

- [Cosa avvii](#cosa-avvii)
- [Requisiti](#requisiti)
- [Avvio rapido](#avvio-rapido)
- [Cartelle e permessi](#cartelle-e-permessi)
- [Prima configurazione di Gextto](#prima-configurazione-di-gextto)
- [indexarr](#indexarr)
- [Jackett, Prowlarr e FlareSolverr](#jackett-prowlarr-e-flaresolverr)
- [aMule, eD2k e Kad](#amule-ed2k-e-kad)
- [gx-torrent e identità del client](#gx-torrent-e-identità-del-client)
- [Percorsi visti dai container](#percorsi-visti-dai-container)
- [Aggiornare](#aggiornare)
- [Backup](#backup)
- [Problemi comuni](#problemi-comuni)
- [Sicurezza](#sicurezza)
- [Limiti di questa modalità](#limiti-di-questa-modalità)

## Cosa avvii

| Servizio | Immagine | Porta | Ruolo |
|---|---|---|---|
| `gextto` | compilata da `docker-full/Dockerfile` | 5000 | UI/API, daemon, `gx-torrent` gestito |
| `indexarr` | compilata dal checkout di indexarr (il suo `Dockerfile`) | 8788 | indexer Torznab/Newznab con fonti native, eD2k compreso |
| `amuled` | `ngosang/amule` | 4712 EC, 4662 TCP, 4672 UDP | eD2k e Kad |
| `jackett` | `lscr.io/linuxserver/jackett` | 9117 | indexer Torznab |
| `prowlarr` | `lscr.io/linuxserver/prowlarr` | 9696 | indexer Torznab |
| `flaresolverr` | `ghcr.io/flaresolverr/flaresolverr` | 8191 | superamento Cloudflare |

Gextto, indexarr e `amuled` usano **`network_mode: host`**: aMule ha bisogno
della rete dell'host per l'UPnP e per le porte eD2k, Gextto trova indexarr,
Jackett, Prowlarr, FlareSolverr e aMule su `127.0.0.1`, e indexarr vi trova
FlareSolverr e la web API di aMule, come in un'installazione normale. Jackett,
Prowlarr e FlareSolverr restano su rete bridge e pubblicano le porte solo su
`127.0.0.1` (vedi [Sicurezza](#sicurezza)).

## Requisiti

- Linux con Docker Engine e Docker Compose v2 (`docker compose version`).
- Almeno 4 GB di RAM liberi e spazio per i download, la libreria e i database.
- Le porte **5000** (UI), **6881 TCP/UDP** (gx-torrent), **4662 TCP** e
  **4672 UDP** (aMule), **4712** (EC di aMule) libere sull'host.
- La porta **8788** (indexarr) libera, oppure un'altra scelta con
  `INDEXARR_PORT`.
- Le porte 9117, 9696 e 8191 libere se non usi altri container con gli stessi
  servizi.
- Il sorgente di **indexarr** accanto a questo repository (l'immagine si compila
  da lì): `git clone https://github.com/buzzqw/indexarr.git ../indexarr`. Una
  posizione diversa va in `INDEXARR_SRC` in `docker-full/.env`.
- Un utente con UID/GID che possiede le cartelle montate. Gli esempi usano
  `1000:1000`: cambia `PUID`/`PGID` in `docker-full/.env` se il tuo utente ha
  un altro ID (`id -u`, `id -g`).

> [!IMPORTANT]
> `network_mode: host` non è disponibile su Docker Desktop per macOS e Windows.
> Su quegli host usa l'installazione nativa di Gextto, oppure adatta il compose
> a una rete bridge (perdi UPnP e la comodità di `127.0.0.1`).

## Avvio rapido

Dalla radice del repository:

```bash
cp docker-full/env.example docker-full/.env
# Modifica .env: PUID, PGID, AMULE_EC_PASSWORD e, se vuoi, TZ.
```

Prepara le cartelle **prima** di avviare i container, così non vengono create
come root:

```bash
mkdir -p docker-full/data/{gextto,indexarr,amule,jackett,prowlarr,flaresolverr} \
         docker-full/media/amule/{incoming,temp} \
         docker-full/library
sudo chown -R "$(id -u):$(id -g)" docker-full/data docker-full/media docker-full/library
```

Compila le immagini con la versione dei checkout (per Gextto legge
`build_number`, `version_series`, `gx-torrent.build_number` e
`gx_torrent.version_series`; per indexarr il suo `git describe`):

```bash
./scripts/docker-full-build.sh
```

Avvia lo stack:

```bash
docker compose -f docker-full/docker-compose.yml up -d
```

Apri:

```text
http://IP-DELL-HOST:5000
```

Segui il wizard. I log sono in:

```bash
docker compose -f docker-full/docker-compose.yml logs -f gextto
```

e nei file `docker-full/data/gextto/gextto.log` e
`docker-full/data/gextto/gx-torrent/gx-torrent.log`.

Per fermare tutto senza cancellare i dati:

```bash
docker compose -f docker-full/docker-compose.yml down
```

## Cartelle e permessi

| Host | Dentro Gextto | Uso |
|---|---|---|
| `docker-full/data/gextto` | `/data` | database, configurazione, log, stato di gx-torrent |
| `docker-full/media` | `/media` | download e libreria multimediale |
| `docker-full/library` | `/library` | archivio finale (opzionale, puoi usare `/media`) |
| `docker-full/media/amule/incoming` | `/amule/incoming` | file completati da aMule, da importare |
| `docker-full/media/amule/temp` | non montato in Gextto | file temporanei di aMule |
| `docker-full/data/amule` | non montato in Gextto | configurazione di aMule |
| `docker-full/data/indexarr` | `/data` in indexarr | database, configurazione, log e chiave del vault di indexarr |

Il percorso di aMule **deve essere lo stesso** dentro `amuled` e dentro Gextto:
per questo `./media/amule/incoming` è montato come `/amule/incoming` in
entrambi. Se lo cambi, cambia entrambi i mount e la cartella di arrivo nelle
impostazioni di Gextto.

I file creati da Gextto appartengono all'UID/GID con cui gira il container
(`PUID`/`PGID`). Se vedi `permission denied`, correggi il proprietario delle
cartelle host e riavvia.

## Prima configurazione di Gextto

Nel wizard:

1. scegli la lingua e imposta la password/API key se vuoi proteggere l'API;
2. come **cartella download** usa `/media` (o una sottocartella, ad esempio
   `/media/downloads`);
3. come **archivio** usa `/library` (o `/media/library`);
4. lascia **gx-torrent** come motore: il binario è già nell'immagine e Gextto lo
   avvia e lo sorveglia da solo;
5. attiva i download quando sei pronto.

Non impostare `gxtorrent_listen` su un indirizzo pubblico senza token: il
default `127.0.0.1:8890` è quello giusto per questo stack. La pagina di
gx-torrent è raggiungibile dall'interno del container; per esporla usa
`gxtorrent_listen` e `gxtorrent_token` con cautela.

## indexarr

indexarr è un indexer Torznab/Newznab con fonti native proprie: sostituisce
Jackett e Prowlarr per i siti che copre, e Gextto lo legge come qualsiasi altro
indexer. Apri la sua UI su `http://IP-HOST:8788` (o sulla `INDEXARR_PORT` che
hai scelto).

1. **Impostazioni → Sicurezza**: imposta subito utente e password, perché la UI
   ascolta su tutte le interfacce dell'host (rete host). La stessa pagina mostra
   la **API key**.
2. **Impostazioni → Rete**: indirizzo di FlareSolverr `http://127.0.0.1:8191`,
   poi **Testa FlareSolverr**.
3. **Indexer → Aggiungi indexer**: aggiungi dal catalogo le fonti che ti
   servono.
4. In Gextto: **Configurazione → Sorgenti → Aggiungi indexer**, gestore
   **indexarr**, URL `http://127.0.0.1:8788`, la API key del punto 1. Salva e
   verifica.

Per le fonti eD2k indexarr può mandare i link allo stesso `amuled`: in **App →
Client di download** aggiungi aMule con l'indirizzo della web API
`http://127.0.0.1:4711` e la password `AMULE_WEBUI_PASSWORD`.

Le guide di indexarr su fonti, impostazioni e API sono solo in inglese:
[README](https://github.com/buzzqw/indexarr/blob/main/README.md) e
[guida d'uso](https://github.com/buzzqw/indexarr/blob/main/docs/USAGE.md).
La sua interfaccia invece è tradotta anche in italiano.

La chiave del vault che cifra password e cookie di indexarr viene creata in
`docker-full/data/indexarr/.config/indexarr/master.key`. Senza di essa i
segreti salvati non si possono leggere: tienine una copia separata dai backup.

## Jackett, Prowlarr e FlareSolverr

I tre servizi sono già in rete con Gextto. Apri le loro UI **dall'host** (o con
un tunnel SSH, vedi sotto):

- Jackett: `http://127.0.0.1:9117`
- Prowlarr: `http://127.0.0.1:9696`
- FlareSolverr: `http://127.0.0.1:8191`

### Jackett

1. Apri Jackett, aggiungi gli indexer che ti servono e copia la **API key**
   dalla pagina iniziale.
2. In Gextto: **Configurazione → Sorgenti → Aggiungi indexer**.
3. Nome: `jackett`; URL: `http://127.0.0.1:9117`; API key: quella di Jackett;
   gestore: **Jackett**. Salva e prova la connessione.

### Prowlarr

1. Apri Prowlarr, aggiungi gli indexer e copia la **API key** da
   *Settings → General*.
2. In Gextto aggiungi un indexer con URL `http://127.0.0.1:9696`, la API key di
   Prowlarr e gestore **Prowlarr**.
3. Non serve configurare Gextto come "app" dentro Prowlarr: Gextto legge
   direttamente l'endpoint Torznab di Prowlarr.

### FlareSolverr

In **Configurazione → Sorgenti → FlareSolverr** inserisci:

```text
http://127.0.0.1:8191
```

e premi **Test**. FlareSolverr viene usato solo per gli indexer esterni che
richiedono di superare Cloudflare; un indexer in rete locale non ci passa.

### Accedere alle UI da un altro computer

Le porte 9117, 9696 e 8191 sono pubblicate solo su `127.0.0.1` dell'host. Da un
altro computer puoi usare un tunnel SSH:

```bash
ssh -L 9117:127.0.0.1:9117 -L 9696:127.0.0.1:9696 -L 8191:127.0.0.1:8191 utente@host
```

e poi aprire `http://127.0.0.1:9117` e `http://127.0.0.1:9696` dal tuo browser.
Se preferisci esporle sulla LAN, sostituisci nel compose
`"127.0.0.1:9117:9117"` con `"9117:9117"` (e analogamente per le altre) e
proteggile con firewall e password.

## aMule, eD2k e Kad

`amuled` parte con la rete dell'host e genera la configurazione al primo avvio.
Il valore `AMULE_EC_PASSWORD` di `.env` è la password **in chiaro** delle
connessioni esterne (EC); l'immagine la trasforma nell'MD5 che aMule salva in
`amule.conf`.

In Gextto:

1. **Configurazione → eD2k e Kad**;
2. attiva **eD2k e Kad tramite aMule**;
3. indirizzo `127.0.0.1`, porta EC `4712`, password = `AMULE_EC_PASSWORD`;
4. salva, apri **Scoperta → eD2k** e premi **Connetti**.

La cartella di arrivo di aMule è `/amule/incoming` sia nel container sia in
Gextto. I file completati compaiono in **Scarico** e vengono importati come gli
altri download.

> [!NOTE]
> Se cambi `AMULE_EC_PASSWORD` dopo il primo avvio, aMule continua a usare
> l'hash vecchio: il file è `docker-full/data/amule/amule.conf`. Ferma
> `amuled`, aggiorna `ECPassword` (MD5 della nuova password:
> `printf '%s' 'nuova' | md5sum`) e riavvia, oppure elimina il file di
> configurazione e lascia che l'immagine lo rigeneri. Aggiorna anche la
> password in Gextto.

Le porte eD2k predefinite sono TCP 4662 e UDP 4672. Aprile sul router (o attiva
l'UPnP di aMule modificando `amule.conf`, che di default è generato con
`UPnPEnabled=0`). Senza porte aperte aMule funziona con ID basso e trova meno
fonti.

## gx-torrent e identità del client

Il motore predefinito è **gx-torrent**, già incluso nell'immagine e avviato da
Gextto. Annuncia il nome e la versione reali:

```text
peer id  -GX1.1.<build>-…
BEP 10   gx-core 1.1.<build>
tracker  gx-core/1.1.<build>
```

In **Configurazione → gx-torrent → identità del client annunciata** puoi
scegliere un preset di compatibilità:

- `gx-core` (default, versione reale);
- `qBittorrent` (5.2.3);
- `Transmission` (4.1.3);
- `Rain` (2.4.4);
- `anacrolix/torrent` (1.61.0).

Il campo **versione** permette di sovrascrivere la versione del preset. Cambia
solo le stringhe annunciate (peer ID, BEP 10, User-Agent dei tracker HTTP e
WebSocket, creatore del `.torrent`), non il comportamento del protocollo.
Alcuni tracker privati accettano solo client in whitelist: controlla le loro
regole e non usare un preset per aggirare un divieto.

### Come è stato verificato

L'identità è stata provata con client reali in Docker, con un tracker locale e
un torrent di test:

| Client osservatore | Identità di Gextto | Cosa vede il client |
|---|---|---|
| qBittorrent 5.3.0 | `gx-core` | `client: gx-core 1.1.252`, `peer_id_client: -GX1.1.2` |
| qBittorrent 5.3.0 | preset `qbittorrent` | `client: qBittorrent 5.2.3`, peer id `-qB5230-` |
| Transmission 4.1.3 | `gx-core` | `clientName: gx-core 1.1.252` |
| Transmission 4.1.3 | preset `qbittorrent` | `clientName: qBittorrent 5.2.3` |

Il test dimostra che il peer remoto riceve davvero l'identità scelta. Non
dimostra che un tracker privato l'accetti: quello dipende dalle sue regole.

## Percorsi visti dai container

Gextto vede i file come:

- download: `/media`
- archivio: `/library`
- arrivo aMule: `/amule/incoming`

Se Jellyfin o Plex girano in un altro container e vedono la stessa libreria con
un altro percorso, imposta la mappatura in **Configurazione → Jellyfin/Plex**
(ad esempio `percorso_gextto=percorso_jellyfin`). Lo stesso vale per
qBittorrent, se un giorno lo usi al posto di gx-torrent.

## Aggiornare

```bash
git pull
git -C ../indexarr pull
./scripts/docker-full-build.sh
docker compose -f docker-full/docker-compose.yml up -d
```

`docker compose up -d` ricrea solo i container con un'immagine nuova; i volumi
in `docker-full/data`, `docker-full/media` e `docker-full/library` restano.
Non usare l'aggiornamento interno di Gextto (`gexttod --update`) né
`scripts/upgrade.sh` di indexarr in questo stack: installer e servizi systemd
non sono attivi nei container.
Aggiorna Jackett, Prowlarr, FlareSolverr e aMule con:

```bash
docker compose -f docker-full/docker-compose.yml pull
docker compose -f docker-full/docker-compose.yml up -d
```

## Backup

Ferma Gextto prima di copiare i database:

```bash
docker compose -f docker-full/docker-compose.yml stop gextto
cp -a docker-full/data/gextto /percorso/di/backup/gextto-$(date +%F)
docker compose -f docker-full/docker-compose.yml start gextto
```

Lo stesso per indexarr:

```bash
docker compose -f docker-full/docker-compose.yml stop indexarr
cp -a docker-full/data/indexarr /percorso/di/backup/indexarr-$(date +%F)
docker compose -f docker-full/docker-compose.yml start indexarr
```

La copia contiene anche la chiave del vault: conservala in un posto sicuro. Per
un backup completo copia anche `docker-full/data/amule`,
`docker-full/data/jackett`, `docker-full/data/prowlarr` e, se vuoi,
`docker-full/library`. I download in `docker-full/media` sono spesso grandi:
escludili o copiali con uno strumento incrementale.

## Problemi comuni

| Sintomo | Cosa controllare |
|---|---|
| La porta 5000 è già in uso | Un'altra istanza di Gextto è attiva sull'host. Fermala (`systemctl --user stop gextto`) oppure cambia `GEXTTO_LISTEN` nel compose e la porta pubblicata. |
| La porta 8788 è già in uso | indexarr è già installato sull'host. Fermalo (`systemctl --user stop indexarr`) oppure imposta un'altra `INDEXARR_PORT` in `.env` e usa quella porta nell'URL di indexarr in Gextto. |
| `indexarr checkout not found` durante il build | Clona indexarr accanto a questo repository oppure imposta `INDEXARR_SRC` in `docker-full/.env` (relativo a `docker-full/` o assoluto). |
| Gextto non vede indexarr | L'URL è `http://127.0.0.1:8788` (rete host), non `http://indexarr:8788`; controlla `docker compose ... logs indexarr`. |
| `permission denied` su `/data` o `/media` | `PUID`/`PGID` in `.env` devono corrispondere al proprietario delle cartelle; esegui `chown -R` sulle cartelle host. |
| Gextto non vede Jackett/Prowlarr | Gextto usa la rete host: l'URL è `http://127.0.0.1:9117` / `http://127.0.0.1:9696`, non `http://jackett`. Verifica che le porte siano pubblicate. |
| FlareSolverr non risponde | Controlla `docker compose ... logs flaresolverr` e apri `http://127.0.0.1:8191` dall'host. |
| aMule non raggiungibile | La password EC in Gextto deve essere quella in chiaro di `AMULE_EC_PASSWORD`; la porta è 4712; `amuled` deve essere in esecuzione. |
| aMule ha ID basso | Apri TCP 4662 e UDP 4672 sul router, oppure abilita UPnP in `amule.conf`. |
| Nessun peer nei torrent | Apri 6881 TCP/UDP sul router/firewall; verifica che `gx-torrent` sia attivo. L'identità del client non c'entra con la raggiungibilità. |
| Il container non parte su macOS/Windows | `network_mode: host` non è supportato da Docker Desktop: usa Linux o l'installazione nativa. |
| Versione mostrata `1.1.0` | Hai compilato con i valori di default. Usa `scripts/docker-full-build.sh`, che legge i file di build del checkout. |

## Sicurezza

- Cambia `AMULE_EC_PASSWORD` e `AMULE_WEBUI_PASSWORD` prima del primo avvio.
- Gextto ascolta su `0.0.0.0:5000` (rete host): mettilo dietro un reverse proxy
  con HTTPS o limitane l'accesso con il firewall. Proteggi l'API con la sua
  API key.
- indexarr ascolta su tutte le interfacce sulla porta 8788 (rete host) e non
  chiede login finché non lo imposti in **Impostazioni → Sicurezza**: fallo al
  primo avvio, oppure limita la porta con il firewall.
- Jackett, Prowlarr e FlareSolverr sono pubblicati solo su `127.0.0.1` per
  default. FlareSolverr non ha autenticazione: non esporlo sulla LAN.
- Non pubblicare la porta EC 4712 di aMule su Internet.
- Il gruppo Docker equivale a root sull'host: limita l'accesso al socket Docker.

## Limiti di questa modalità

- Lo stack è per **Linux**: la rete host e l'UPnP di aMule non funzionano su
  Docker Desktop.
- Non ci sono systemd, `install.sh`, `gexttod --update` né il servizio
  `gextto-update.path`: aggiorna con `git pull` + compose.
- I container Jackett/Prowlarr/FlareSolverr/aMule hanno i loro aggiornamenti;
  `AUTO_UPDATE=true` per Jackett è comodo ma può cambiare versione senza un
  tuo intervento.
- L'immagine include `ffprobe`, `ffmpeg` e `mkvmerge`: servono alla pulizia
  opzionale delle tracce audio e non vanno installati a mano.
- Le immagini compilano dal sorgente: il primo build richiede qualche minuto e
  scarica `golang:1.27` e Debian slim per Gextto, `golang:1.26-alpine` e Alpine
  per indexarr.

Per l'integrazione aMule completa (protocollo EC, cartelle, diagnostica) vedi
anche [eD2k e Kad con aMule](amule.it.md). Per il motore e le identità client
vedi [gx-torrent](gx-torrent.md).
