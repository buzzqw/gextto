# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** è un demone self-hosted che
cerca, seleziona, scarica, verifica, rinomina e archivia serie TV, film e
fumetti. L'interfaccia web è il piano di controllo; il demone continua a
funzionare come servizio.

> **English:** [README.md](README.md) · **Guida completa:**
> [manuale italiano](docs/MANUAL.it.md)

## In breve

- Un solo servizio gestisce ciclo di ricerca, database SQLite, coda torrent,
  post-processing e archivio.
- Il motore predefinito è libtorrent integrato. qBittorrent-nox è un'alternativa
  opzionale, non un servizio aggiuntivo necessario.
- La scelta delle release considera qualità, sorgente, codec, audio, HDR, lingue
  e dimensione. `ffprobe` può aggiungere le caratteristiche del file archiviato.
- UI web responsive e TUI mostrano salute, log, backup, manutenzione e
  integrazioni con Simkl, Jellyfin e Plex.

### Motori torrent

Gextto supporta due motori di trasferimento: libtorrent integrato e l'adapter
Web API di qBittorrent-nox.

## Interfaccia web e menu

La UI web è un piano di controllo completo, non solo una pagina di stato. È
responsive su desktop e mobile e il selettore di lingua supporta attualmente
**Italiano, English, Deutsch, Français, Español e Polski**. Il manuale esteso è
integrato in italiano e inglese; per le altre lingue la documentazione lunga usa
il manuale inglese come fallback. Nella barra superiore sono disponibili anche
ricerca impostazioni, metriche CPU/RAM e trasferimenti, tema, dimensione testo,
aggiornamento e stato del servizio.

Il menu a discesa **Font** include preset generici sicuri e, nei browser che
supportano la Local Font Access API, può elencare le famiglie installate sul
dispositivo dell'utente. La scelta viene applicata subito all'interfaccia e ai
log e viene salvata localmente nel browser. I browser senza questa API mantengono
i preset e il campo per inserire manualmente il nome della famiglia; per leggere
i font installati può essere richiesta un'autorizzazione del browser.

L'interfaccia ufficiale è disponibile alla radice `http://<host>:5000/`; `/v2`
resta un alias tecnico. La vecchia route `/ui` è stata rimossa. Il menu **Scarico**
aggiorna automaticamente la sessione ogni 5 secondi, includendo progressione e
velocità dei download HTTP dei fumetti; le metriche live nella barra superiore
seguono lo stesso intervallo.

| Voce | Funzioni |
|---|---|
| **Dashboard** | Avvio del ciclo completo o limitato a un dominio, prossimo ciclo, titoli configurati, spazio libero, torrent attivi, ultimi download, prossime uscite, risultati dei feed, grafici dischi/risorse e collegamenti rapidi. |
| **Scarico** | Sessione torrent e HTTP completa: aggiunta magnet o `.torrent`, coda/progresso/ETA/peer/ratio, pausa/ripresa/recheck/rimozione, dettagli, tracker, priorità file, limiti, spostamento storage, seeding, download fumetti e storico. |
| **Serie TV** | Libreria monitorata con stagioni, episodi, qualità/lingue/sottotitoli, alias, percorso NAS con sfoglia, ricerca mancanti, upgrade, ricerca manuale, scansione archivio, cast collegato a TVDB/TMDB e rinomina/riparazione controllata. |
| **Film** | Libreria con identità titolo/anno, qualità, requisiti linguistici leggibili, esclusioni, ricerca immediata, migliori risultati, corrispondenze archivio, cast collegato a TMDB, decisioni upgrade, riscaricamento e modifica metadati. |
| **Mancanti** | Vista orientata ai gap per episodi e stagioni mancanti, con filtri, ricerche e azioni consapevoli di forza, ignora, riattiva e riscarica. |
| **Esplora** | Scoperta TMDB (tendenze, popolari, più votati, programmazione e prossime uscite), ricerca titoli, ricerca release e aggiunta alla libreria, con protezione dai duplicati già monitorati. |
| **Archivio** | Ricerca full-text delle release archiviate, paginazione, dettagli sorgente/qualità, accodamento multiplo, copia magnet, eliminazione e spiegazione **Perché non questa?**; include anche i titoli visti nei feed. |
| **Fumetti** | Fumetti monitorati tramite GetComics, scelta del post, gestione metadati/copertina/tag, estrazione link, weekly pack, download HTTP e storico con azioni. |
| **Configurazione** | Daemon, sorgenti RSS/indexer/motori web/FlareSolverr, libtorrent, motore torrent, scoring, template di rinomina, acquisizione, notifiche, percorsi/NAS, retention avanzate e traduzioni. |
| **Integrazioni** | Autenticazione/watchlist Simkl, aggiornamento libreria Jellyfin/Plex e hook per programmi esterni. |
| **Manutenzione** | Backup, azioni di ripristino, pulizia cestino, scansioni archivio, backfill MediaInfo, ricalcolo scoring, pulizia duplicati, rinomina manuale cartelle, manutenzione database e riavvio servizio. |
| **Salute** | Integrità database, percorsi e permessi, spazio libero, stato provider/backend, CPU/RAM e diagnostica del servizio. |
| **Log** | Coda live e storico del log daemon con filtro, numero righe e controlli segui/aggiorna, per analizzare cicli, provider e vita dei torrent. |
| **Blocklist** | Consultazione e gestione delle release bloccate per qualità, identità, provider o scelta utente, evitando che rientrino silenziosamente nei cicli. |
| **Manuale** | Guida operativa integrata nella UI; segue la lingua selezionata, con versioni complete italiana e inglese e fallback inglese per le altre lingue. |
| **Licenza** | Licenza EUPL-1.2 del progetto e note sulle dipendenze di terze parti incluse o opzionali. |

La shell mobile mantiene sempre raggiungibili le pagine operative principali:
Dashboard, Scarico, Serie TV, Film, Salute e Log; le altre pagine di scoperta e
sistema restano disponibili dalla navigazione.

## Efficienza delle risorse

Gextto è stato ottimizzato per mantenere un uso delle risorse prevedibile e
contenuto, soprattutto quando è inattivo o in attesa del ciclo programmato:

- il demone è un singolo servizio e usa libtorrent integrato, senza richiedere
  uno stack torrent separato;
- feed, indexer, ricerche web e attività in background usano concorrenza limitata
  invece di creare goroutine senza limite;
- backoff dei provider, finestre di retry e cooldown impediscono che gli errori
  ripetuti diventino raffiche di richieste, consumo CPU e traffico inutili;
- coda torrent, profili di velocità, riconciliazione del RAM disk e database
  vengono gestiti in modo incrementale, senza cicli di attesa attiva.

Il risultato è un basso uso della CPU al di fuori delle ricerche e dei download
attivi e un consumo di RAM contenuto per un servizio di automazione multimediale
self-hosted. Il consumo effettivo dipende dal numero di titoli monitorati, dalle
sorgenti configurate, dai torrent attivi, dalle scansioni dell'archivio e dalle
integrazioni opzionali.

### Misurazioni indicative

Sono osservazioni di riferimento su un'installazione Linux x86_64 con libtorrent
integrato, non benchmark indipendenti dall'hardware:

- dopo il riavvio, con cinque torrent ripristinati e nessun download attivo, il
  processo demone aveva un RSS di circa **75–85 MiB**. Il valore del cgroup
  systemd può essere molto più alto perché include anche la cache del filesystem:
  in un'osservazione era di circa **584 MiB**, di cui circa **525 MiB** di cache
  file e solo circa **51 MiB** di memoria anonima;
- in circa cinque minuti nello stesso stato prevalentemente inattivo, il tempo
  CPU accumulato è stato di circa **7 secondi** (circa **2% di un core in media**);
- durante un controllo successivo, con cinque torrent in sessione ma nessun
  download attivo, il processo era intorno a **168 MiB RSS** e **15 MiB di heap
  Go**; questo è un campione, non un valore di riposo garantito;
- i cicli di ricerca/download possono avere picchi temporanei: systemd ha
  registrato circa **2,1 GiB** come massimo di una precedente istanza del
  servizio. Il processo è stato poi riavviato e il suo RSS è tornato molto più
  basso. Il picco va quindi distinto dalla memoria residente stabile.

Usa questi valori come esempi di dimensionamento, non come garanzie. Per il
consumo residente effettivo del demone è più utile l'RSS del processo; il totale
cgroup include anche la cache del filesystem, in gran parte recuperabile. Le
variabili principali sono numero di torrent attivi, cache e connessioni
libtorrent, concorrenza dei provider, scansioni `ffprobe`/archivio e dimensione
del ciclo in corso.

## Installazione Linux

L'installer ufficiale è destinato a server Linux 64 bit con systemd. Eseguilo
come root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa il programma in `/opt/gextto`, conserva i dati del servizio in
`/var/lib/gextto` ed espone la UI sulla porta 5000.

> [!IMPORTANT]
> La UI web è un'interfaccia amministrativa senza autenticazione. Mantienila in
> una rete fidata oppure proteggila con firewall e reverse proxy HTTPS dotato di
> autenticazione. Leggi prima la [politica di sicurezza](docs/SECURITY.md).

### Installazione dal sorgente

Servono Go 1.26+, compilatore C++17 e header di sviluppo libtorrent-rasterbar.
La build normale include la UI web: non è richiesto un build frontend separato.
Consulta il [manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build
```

`make build` incrementa il numero della build locale e scrive il demone
versionato in `bin/gexttod`. Per ricompilare senza incrementare il numero usa
`make fast`. Verifica binario, versione del prodotto, numero di build e versione
di libtorrent collegata con:

```bash
./bin/gexttod --version
```

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

## Aggiornamento

Per l'installazione ufficiale ripeti l'installer. Se disponibile, verifica il
checksum della release e sostituisce il payload in modo atomico, senza toccare
dati e configurazione.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Da un checkout sorgente:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

Lo script di aggiornamento esegue la build versionata, riavvia il servizio
rilevato e lascia intatti dati e configurazione. Usa
`./scripts/update.sh --no-restart` se vuoi riavviare manualmente.

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

## Licenza

Gextto è distribuito con [EUPL-1.2](LICENSE). I componenti di terze parti sono
indicati in [NOTICE](NOTICE).
