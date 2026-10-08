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
- **Strumenti operativi inclusi:** controlli di salute, log, backup,
  manutenzione, notifiche, percorsi NAS, gestione seeding e blocklist.

La UI è disponibile su `http://<host>:5000/` e supporta italiano, inglese,
tedesco, francese, spagnolo e polacco. Il [manuale italiano](docs/MANUAL.it.md)
e l'[English manual](docs/MANUAL.en.md) spiegano la configurazione e tutte le
sezioni dell'interfaccia.

## Motori torrent

Gextto conserva il piano di controllo (coda, punteggi, post-processing, archivio)
e sostituisce solo il motore di trasferimento in *Configurazione → Motore
torrent*. Un motore gira alla volta, quindi il cambio è una migrazione
controllata, mai due client sugli stessi dati.

| Motore | Dove gira | Vantaggi | Svantaggi — quando sceglierlo |
|---|---|---|---|
| **gx-torrent** (predefinito) | Processo Go separato e sorvegliato, senza libtorrent | Go puro, nessun `libtorrent-rasterbar`; pagina web propria raggiungibile in LAN; **i trasferimenti proseguono mentre Gextto si riavvia o si aggiorna**; un crash resta nel suo processo; **download sequenziale e prima/ultima parte**; **impronta di memoria piccola e adattiva** (cache dimensionata su RAM disponibile, download/seed attivi e tipo di storage); se non riesce a restare attivo torna da solo a libtorrent | Solo torrent BitTorrent v1 e ibridi (niente solo-v2); limiti di velocità/connessioni per singolo torrent, web seed manuali o rimozione tracker; nessuna grande cache disco in-process | vuoi un motore autonomo con dipendenze C/C++ minime |
| **libtorrent** (integrato) | Stesso processo di Gextto (`libtorrent-rasterbar`) | Set completo: sequenziale, limiti per torrent, super-seeding, web seed, diagnostica pezzi; tutte le regolazioni avanzate | Gextto e il motore condividono un processo; richiede la libreria libtorrent | ti servono tutti i controlli avanzati o la massima compatibilità |
| **qBittorrent-nox** | Demone esterno, pilotato via Web API | Riusa un qBittorrent esistente e il suo ecosistema/Web UI; supporta il sequenziale e i suoi limiti | Servono le mappature percorsi se i due processi vedono path diversi; un processo e una dipendenza in più | hai già qBittorrent-nox o preferisci la sua UI |

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
| **gx-torrent** | ~25 MB | ~100 MB |
| **libtorrent integrato** (nel processo di Gextto) | ~0,5 GB | 3–5 GB |
| **qBittorrent-nox** | ~40 MB | ~5 GB |

libtorrent e qBittorrent tengono una grande cache disco in-process (GB di
memoria anonima). gx-torrent **non ha una grande cache write-back in-process, per
scelta**: scrive i pezzi in streaming e lascia alla page cache del kernel
(reclamabile) il compito di fondere le scritture, quindi la sua impronta resta
nelle decine/centinaia di MB anche sotto carico; non tiene dati in RAM solo per
occuparla. I valori variano con torrent, peer e storage; su HDD/NFS Gextto dà a
gx-torrent un buffer più grande, usato solo quando le scritture restano
indietro rispetto al download.

## Installazione Linux

L'installer ufficiale è destinato a server Linux 64 bit con systemd. Eseguilo
come root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa il programma in `/opt/gextto`, conserva i dati del servizio in
`/var/lib/gextto` ed espone la UI sulla porta 5000. Il motore torrent
predefinito, `gx-torrent` (Go puro, senza libtorrent), viene installato accanto
a `gexttod` e avviato in modalità gestita; libtorrent integrato resta come
fallback automatico e alternativa selezionabile.

> [!IMPORTANT]
> Gextto è pensato per una rete fidata: per impostazione predefinita la UI web e
> l'API sono aperte. Se lo raggiungi da fuori casa attiva il login facoltativo
> (*Configurazione → Accesso*, la rete locale resta libera) e usa comunque HTTPS
> tramite reverse proxy o VPN. Leggi prima la
> [politica di sicurezza](docs/SECURITY.md).

### Installazione dal sorgente

Servono Go 1.26+, compilatore C++17 e header di sviluppo libtorrent-rasterbar.
La build normale include la UI web: non è richiesto un build frontend separato.
Consulta il [manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build
```

`make build` incrementa il numero della build locale e scrive il demone
versionato in `bin/gexttod`, insieme al motore `gx-torrent` in puro Go. Per
ricompilare senza incrementare il numero usa `make fast`. Per compilare **solo**
il demone `gx-torrent` (non servono C++ né libtorrent) usa `make gx-torrent`.
Verifica binario, versione del prodotto, numero di build e versione di libtorrent
collegata con:

```bash
./bin/gexttod --version
```

## Primo avvio sicuro

1. Apri `http://<server>:5000` e completa il setup.
2. Configura percorsi, una sorgente e, se necessarie, credenziali TMDB/TVDB.
3. Mantieni il **dry-run**, aggiungi un titolo di prova ed esegui una ricerca o
   un ciclo.
4. Controlla **Salute** e **Log**, inclusi permessi filesystem e risultati della
   sorgente.
5. Abilita la modalità attiva solo quando l'esito è corretto.

Checklist, configurazione NAS e diagnostica sono nel
[manuale utente](docs/MANUAL.it.md).

## Aggiornamento e disinstallazione

Per l'installazione ufficiale ripeti l'installer. Se disponibile verifica il
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
tramite PayPal. Grazie!

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
