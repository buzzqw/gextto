# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** è un demone self-hosted che
cerca, seleziona, scarica, verifica, rinomina e archivia serie TV, film e
fumetti. L'interfaccia web è il piano di controllo; il demone continua a
funzionare come servizio.

> **English:** [README.md](README.md) · **Manuale:**
> [italiano](docs/MANUAL.it.md) · **Segnalazioni e proposte:**
> [GitHub Issues](https://github.com/buzzqw/gextto/issues) ·
> **Contribuire:** [guida sviluppatori](docs/DEVELOPERS.md)

## Cosa offre Gextto

- **Un servizio e un piano di controllo:** il demone gestisce ricerche
  programmate, trasferimenti torrent, post-processing e archivio. Il motore
  predefinito è gx-torrent (Go puro, processo sorvegliato); libtorrent integrato
  e qBittorrent-nox sono alternative opzionali.
- **Serie TV, film e fumetti:** monitora i titoli, cerca nelle sorgenti
  configurate e gestisce download e libreria da una UI web responsive, installabile
  sul telefono, o dalla TUI terminale. Le serie anime numerate per episodio
  assoluto («Titolo - 1071») vengono ricondotte a stagione ed episodio; nei
  fumetti CBZ viene scritto un `ComicInfo.xml` per Komga, Kavita e i lettori.
- **Automazione attenta alla qualità:** valuta release per qualità, sorgente,
  codec, audio, HDR, lingua e dimensione; protegge i file migliori già presenti e
  spiega perché un candidato è stato scartato. Una soglia facoltativa ferma gli
  upgrade quando il file è già abbastanza buono. `ffprobe` può arricchire le
  decisioni sui file archiviati.
- **Sorgenti e integrazioni flessibili:** RSS, indexer Torznab come Jackett e
  Prowlarr, motori web e FlareSolverr opzionale; integrazioni con Simkl, Jellyfin
  e Plex (che aggiornano solo la cartella cambiata) e un calendario iCal con gli
  arrivi in libreria, le messe in onda originali e le uscite italiane dei film.
- **Attenzione alla libreria:** un file esistente viene sostituito solo da un
  vero upgrade, e il file sostituito finisce nel Cestino (se configurato). Un
  NAS smontato viene riconosciuto e mai scambiato per file cancellati; gli
  errori temporanei (NAS irraggiungibile, disco momentaneamente pieno, timeout)
  vengono ritentati in automatico; uno spostamento verso l'archivio interrotto
  da un riavvio riprende da solo. Mentre un file è in seed entra in libreria come
  hardlink, senza occupare spazio due volte (se download e libreria stanno sullo
  stesso filesystem).
- **Recupero dai download bloccati:** un download che non avanza più — nessun
  byte per un po', o un magnet ancora in attesa dell'elenco file — può essere
  sostituito da un'alternativa adeguata dopo una finestra configurabile, mentre
  il torrent bloccato continua a tentare (vedi *Configurazione → Qualità e
  upgrade → Download bloccati*).
- **Strumenti operativi inclusi:** controlli di salute, log, backup,
  manutenzione, notifiche, percorsi NAS, gestione seeding e blocklist. La ricerca
  nell'archivio è immediata mentre digiti e il **log di gx-torrent** è leggibile
  da Manutenzione.

La UI è disponibile su `http://<host>:5000/` e supporta italiano, inglese,
tedesco, francese, spagnolo e polacco. Il [manuale italiano](docs/MANUAL.it.md)
e l'[English manual](docs/MANUAL.en.md) spiegano la configurazione e tutte le
sezioni dell'interfaccia.

## Motori torrent

Gextto conserva il piano di controllo (coda, punteggi, post-processing, archivio)
e sostituisce solo il motore di trasferimento in *Configurazione → Motore
torrent*. Un motore gira alla volta, quindi il cambio è una migrazione
controllata, mai due client sugli stessi dati.

gx-torrent è il motore che il progetto sviluppa e consiglia: è il predefinito,
è Go puro (niente C/C++), ha una pagina web propria, i trasferimenti proseguono
mentre Gextto si riavvia o si aggiorna e le novità di trasferimento arrivano
prima lì (streaming HTTP con Range, test porte integrato, gestione adattiva di
memoria/cache). Gli altri due restano quando ti serve un controllo che gx-torrent
non espone ancora, o la massima compatibilità; libtorrent è anche il fallback
automatico se gx-torrent non riesce a restare attivo.

| Motore | Dove gira | Quando sceglierlo |
|---|---|---|
| gx-torrent (predefinito) | Processo Go separato e sorvegliato, senza libtorrent | Il predefinito: Go puro, **nessuna dipendenza C/C++**, autonomo, sviluppo attivo. Solo torrent BitTorrent v1 e ibridi (niente solo-v2), niente WebTorrent/WebRTC né holepunching NAT |
| libtorrent (integrato) | Stesso processo di Gextto, libtorrent-rasterbar incluso nel pacchetto | Ti serve un controllo avanzato o la massima compatibilità; è anche il fallback automatico. Un crash coinvolge anche Gextto e gexttod non si compila senza libtorrent |
| qBittorrent-nox | Demone esterno, pilotato via Web API | Hai già qBittorrent-nox o preferisci la sua UI. Servono le mappature percorsi e un processo in più |

### Matrice delle capacità

Livelli: **sì** = supportato, **parziale** = supportato con limiti, **—** = non disponibile. La tabella è generata da `capabilityLevels` in [`torrent_engine.go`](torrent_engine.go), l'unica fonte di verità usata dalla UI; un test la tiene allineata, quindi la matrice si modifica lì, mai qui.

<!-- capability-matrix:start -->
| Capacità | gx-torrent | libtorrent integrato | qBittorrent-nox |
| --- | :--: | :--: | :--: |
| Aggiungi, Rimuovi, Pausa, Riprendi, Elenca, Ricontrolla, Sposta, Download sequenziale, Selezione file, Limiti per torrent, Peer, Tracker, Eventi, Statistiche, Categorie, Tag | sì | sì | sì |
| Prima/ultima parte | parziale | sì | sì |
| Policy di seed | sì | sì | parziale |
| Super-seeding (BEP 16) | sì | sì | parziale |
| Upload/share mode | — | sì | — |
| RAM disk | sì | sì | — |
| Fast resume | sì | sì | — |
| Diagnostica dei pezzi | sì | — | — |
| Preferenze del motore | parziale | sì | parziale |
| Statistiche di sessione | parziale | sì | parziale |
| Sincronizzazione della sessione | sì | — | sì |
| Filtro IP | sì | sì | parziale |
| Web seed | sì | sì | parziale |
<!-- capability-matrix:end -->

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
| libtorrent integrato (nel processo di Gextto) | ~0,5 GB | 3–5 GB |
| qBittorrent-nox | ~40 MB | ~5 GB |

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
predefinito, `gx-torrent` (Go puro, senza libtorrent), viene installato accanto
a `gexttod` e avviato in modalità gestita; libtorrent integrato resta come
fallback automatico e alternativa selezionabile. Non serve installare altro: il
pacchetto include sia il demone gx-torrent sia la libreria condivisa libtorrent.

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

Servono Go 1.26+, compilatore C++17 e header di sviluppo libtorrent-rasterbar.
libtorrent serve solo per compilare: il pacchetto di release la include già,
quindi un'installazione normale non richiede toolchain C/C++. La build normale
include la UI web: non è richiesto un build frontend separato. Consulta il
[manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build
```

`make build` incrementa il numero di build **committato** (`build_number`, la
stessa sorgente condivisa con la CI: checkout e versione installata mostrano lo
stesso `1.1.<n>`) e scrive il demone
versionato in `bin/gexttod`, insieme al motore `gx-torrent` in puro Go. Per
ricompilare senza incrementare il numero usa `make fast`. Per compilare **solo**
il demone `gx-torrent` (non servono C++ né libtorrent) usa `make gx-torrent`.
Verifica binario, versione del prodotto, numero di build e versione di libtorrent
collegata con:

```bash
./bin/gexttod --version
```

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

La suite automatica attuale copre le sezioni principali della UI con 12 test di
accessibilità. Questo non costituisce da solo una certificazione normativa:
servono ancora test manuali con screen reader, tastiera e tecnologie assistive.
Consulta l'[analisi di accessibilità](docs/accessibility-analysis.md) per ambito e
limitazioni note.

## Documentazione

| Esigenza | Documento |
|---|---|
| Usare il servizio | [Manuale italiano](docs/MANUAL.it.md) · [English manual](docs/MANUAL.en.md) |
| NAS, reverse proxy, ripristino | [Guida avanzata](docs/ADVANCED.it.md) |
| Integrazioni HTTP | [Riferimento API](docs/API.md) |
| Migrare un'installazione | [Guida migrazione](docs/MIGRATION.md) |
| Client da terminale | [Riferimento TUI](docs/tui.md) |
| Compilare o contribuire | [Manuale sviluppatori](docs/DEVELOPERS.md) |
| Rete e protezione dati | [Politica di sicurezza](docs/SECURITY.md) |
| Ambito e test accessibilità | [Analisi accessibilità](docs/accessibility-analysis.md) |

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
- L'integrazione torrent (libtorrent) è una tecnologia neutrale; il progetto non
  incoraggia né facilita la pirateria.
- Il progetto è distribuito con licenza open source **EUPL 1.2**.

## Licenza

Gextto è distribuito con [EUPL-1.2](LICENSE). I componenti di terze parti sono
indicati in [NOTICE](NOTICE).
