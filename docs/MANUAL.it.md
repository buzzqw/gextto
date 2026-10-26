# Manuale utente di Gextto

Questa è la guida operativa per la UI web e la TUI terminale di Gextto. La UI è
disponibile in italiano, inglese, tedesco, francese, spagnolo e polacco; qui sono usate le etichette italiane.
Per le etichette inglesi consulta [MANUAL.en.md](MANUAL.en.md). La UI tedesca
francese, spagnola e polacca usano i rispettivi cataloghi incorporati e, per la
documentazione estesa, il manuale inglese come fallback. La voce **Manuale**
apre il manuale aggiornato online nella lingua dell'interfaccia; la voce
**Changelog** apre le novità. Manuale e changelog non sono incorporati nel
binario: si aggiornano senza ricompilare Gextto.

> [!IMPORTANT]
> Inizia in **dry-run**. Verifica percorsi, accesso alle sorgenti e un titolo di
> prova prima di abilitare i download. La UI è amministrativa e per
> impostazione predefinita è aperta: lasciala in una rete fidata oppure, per
> l'accesso da fuori, attiva il login (*Configurazione → Accesso e servizi*) e usa HTTPS.

## Come usare questa guida

Gextto è un demone unico: ricerca le sorgenti, valuta le release, gestisce
il motore torrent attivo, rinomina i file e li archivia. Non è necessario avviare componenti
separati per il funzionamento normale.

L'interfaccia web funziona anche da telefono: sotto i 900 px il layout diventa
compatto, con navigazione in alto scorrevole, metriche della dashboard su due
colonne, finestre a tutta larghezza e tabelle che scorrono orizzontalmente
dentro il loro pannello. Ogni azione resta raggiungibile col tocco. Le pagine
e i file dell'interfaccia viaggiano compressi e i fogli di stile e gli script
restano nella cache del browser, così la UI resta usabile anche su una rete
lenta.

La UI ufficiale si apre su `http://<host>:5000/`; il vecchio prefisso `/v2`
reindirizza alla radice e la vecchia route `/ui` non esiste più. La pagina **Scarico** aggiorna
automaticamente la sessione ogni 5 secondi; la voce **Aggiorna ogni 5 s** del menu
**Strumenti** disattiva o riattiva l'aggiornamento, e il pulsante **Aggiorna**
della barra in alto (accanto a **Aggiungi torrent**) ricarica subito i dati.
Anche le velocità e i carichi nella barra superiore vengono aggiornati ogni 5
secondi. La barra mostra **CPU e RAM
per processo**: **Gextto** (il demone), **gx-torrent** (il motore torrent, quando
il demone è locale) e **aMule** (quando **eD2k e Kad tramite aMule** è attivo e
`amuled` gira sulla stessa macchina). I badge dei processi remoti non compaiono,
perché quei dati non sono leggibili da lì.

Il percorso consigliato per una nuova installazione è:

1. configurare percorsi e sorgenti;
2. lasciare il demone in **dry-run**;
3. aggiungere un solo titolo di prova;
4. eseguire una ricerca o un ciclo manuale;
5. controllare Salute e Log;
6. abilitare la modalità attiva solo dopo aver verificato i risultati.

## Accessibilità

La UI web è stata migliorata per l'uso con tastiera e tecnologie assistive:

- i controlli di ricerca, configurazione e lingua hanno nomi accessibili;
- i dialoghi gestiscono ingresso del focus, `Tab`, `Esc` e ritorno al controllo
  che li ha aperti;
- le intestazioni ordinabili delle tabelle sono attivabili da tastiera e
  comunicano l'ordinamento corrente;
- aggiornamenti asincroni, errori, progressi e notifiche usano regioni live;
- tabelle, progress bar, tab e landmark espongono semantica aggiuntiva;
- il layout supporta viewport stretti, reflow e contrasto migliorato nel tema
  chiaro.

Le verifiche automatiche si eseguono dal checkout sorgente:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

La suite corrente esegue 14 controlli axe-core/Playwright sulle sezioni
principali, compresi dialoghi, ordinamento da tastiera, polling e reflow a 320
px. Il risultato non è una dichiarazione di conformità WCAG o normativa: per
un'attestazione servono anche audit manuale con screen reader, tastiera,
ingrandimento e tecnologie assistive, oltre alla valutazione dei requisiti
applicabili. Per il dettaglio vedere
[`ACCESSIBILITY.md`](ACCESSIBILITY.md).

Il manuale distingue sempre tra:

- **ricerca manuale**: serve a ispezionare risultati e accodare una scelta;
- **ciclo automatico**: cerca i titoli monitorati e decide cosa scaricare;
- **archivio**: i file già importati o archiviati nella libreria;
- **sessione torrent**: i download ancora gestiti dal motore torrent attivo.

### Checklist iniziale

Prima di abilitare i download reali verifica:

- `http://<host>:5000` è raggiungibile;
- *Salute* non segnala problemi di permessi o spazio;
- la cartella temporanea è scrivibile;
- la cartella libreria/NAS è montata e scrivibile dall'utente del servizio;
- almeno una sorgente risponde a *Verifica*;
- una ricerca manuale restituisce release coerenti;
- in dry-run Gextto non ha prodotto errori inattesi.

Se usi un NAS, prova prima a creare un file nella destinazione con lo stesso
utente che esegue `gextto.service`. Un percorso visibile dalla shell dell'utente
personale può non essere visibile al servizio systemd.

**Sicurezza.** La porta web è un'interfaccia amministrativa, aperta per
impostazione predefinita. Lasciala sul loopback o sulla rete di casa; per
l'accesso da fuori attiva il login facoltativo (vedi *Accesso*, la rete locale
resta libera) dietro un reverse proxy HTTPS o una VPN. Nell'installazione standard, `/opt/gextto/gexttod --version`
indica quale build è in esecuzione. Vedi
[`SECURITY.md`](SECURITY.md) per il modello di rete completo.

- [Accessibilità](#accessibilità)
- [1. Primo avvio](#1-primo-avvio)
- [2. Dashboard](#2-dashboard)
- [3. Scarico](#3-scarico)
- [4. Serie TV](#4-serie-tv)
- [5. Film](#5-film)
- [6. Esplora, Archivio, Fumetti](#6-esplora-archivio-fumetti)
- [7. Configurazione](#7-configurazione)
- [8. Integrazioni](#8-integrazioni)
- [9. Manutenzione](#9-manutenzione)
- [10. Salute, Log, Grafici](#10-salute-log-grafici)
- [11. Notifiche](#11-notifiche)
- [12. Riferimento rapido](#12-riferimento-rapido)
- [13. Risoluzione problemi](#13-risoluzione-problemi)

---

## 1. Primo avvio

Gextto gira come un unico servizio. Apri la UI all'indirizzo `http://<host>:5000`.

- Su un'installazione nuova la prima visita apre la **Configurazione iniziale**,
  in cinque passi: *Accesso* (password, con accesso libero dalla rete locale se
  vuoi), *Cartelle* (libreria, download, temporanei e cestino; **Sfoglia** apre
  il selettore delle cartelle del server e può crearne di nuove; **Verifica**
  controlla con l'utente del servizio se ogni cartella esiste ed è scrivibile,
  il tipo di disco, NAS compreso, lo spazio libero e se sta sullo stesso disco
  della libreria), *Fonti* (un indexer Prowlarr/Jackett/indexarr e la chiave TMDB,
  consigliata, oppure in alternativa la chiave TVDB con l'eventuale PIN),
  *Primo titolo* (la ricerca su TMDB, o su TVDB se manca la chiave TMDB, con il
  pulsante per aggiungere) e *Attiva*
  (ciclo automatico e uscita dalla modalità prova). Si può saltare e riaprire
  in ogni momento da `/?view=setup`; un'installazione che ha già serie, film o
  archivio non la mostra.
- **Attivo vs dry-run**: il ciclo automatico è **acceso di default**, quindi
  Gextto cerca da solo; con il **dry-run** attivo (la scelta *Scarica davvero*
  nel wizard) non parte nessun download reale. L'interruttore del ciclo è *Ciclo
  automatico (ricerca e download)* in *Configurazione → Generale*.
- Aggiungi serie/film da **Esplora** (TMDB) oppure da **Serie TV / Film → Aggiungi**.
- I metadati (stagioni, episodi, locandine, calendario) arrivano da TMDB o da
  TVDB: vedi *Configurazione → Metadati: TMDB o TVDB*.
- Il pulsante **Dona** (PayPal) è sempre visibile in basso a destra: se Gextto ti
  è utile puoi sostenere lo sviluppo; l'indirizzo è anche nel README.

### Procedura consigliata per il primo ciclo

1. In *Configurazione → Archivio e spazio* controlla cartella download, temporanea,
   libreria e cestino.
2. In *Configurazione → Sorgenti* aggiungi una sola sorgente funzionante e
   controllala in *Integrazioni → Verifica sorgenti*. Aggiungi le altre solo dopo aver validato la prima.
3. Lascia disattivati i download reali e aggiungi una serie con una sola stagione
   o un film di prova.
4. Dalla Dashboard avvia il ciclo del dominio interessato.
5. Apri *Salute* e *Log*: devi vedere il ciclo, le sorgenti interrogate e il
   motivo per cui una release è stata accettata o scartata.
6. Esegui una ricerca manuale e controlla un risultato con **Perché non questa?**.
7. Quando percorsi, filtri e risultati sono corretti, abilita *Modalità attiva*.

In caso di dubbi non modificare contemporaneamente qualità, percorsi e sorgenti:
una modifica alla volta rende il problema riproducibile.

### Percorsi e responsabilità

Gextto usa percorsi con ruoli diversi:

| Percorso | Uso | Può essere temporaneo? |
|---|---|---|
| Download | dati dei torrent in sessione | no, finché il torrent è attivo |
| Temporaneo/incomplete | metadati e dati incompleti | sì, ma deve essere scrivibile |
| Libreria/NAS | file finali archiviati | no |
| Cestino | file rimossi durante upgrade/pulizia | sì, secondo retention |
| Cartella osservata | `.torrent`/`.magnet` da importare | sì, ma non durante la copia |

Non usare la cartella temporanea come libreria finale e non cancellare a mano i
file di un torrent ancora attivo: usa le azioni della sessione torrent.

### Riga di comando e aggiornamenti

Il demone gira normalmente nel servizio systemd. Eseguito direttamente
comprende:

- `gexttod --version` — versione installata, build number e marker di release;
- `gexttod --help` — riepilogo d'uso;
- `gexttod --update` — scarica e installa l'ultimo payload del canale installato
  (continuous o stabile); tiene le versioni precedenti in `gexttod.prev` e
  `gx-torrent.prev` (lo fa anche rilanciare `install.sh`) e le rimette da sole
  se le nuove non partono;
- `gexttod --config <file>` e `gexttod --dry-run` — usati dal servizio e per le
  prove locali.

#### TUI terminale

La TUI si avvia con `/opt/gextto/gexttod tui` dopo aver avviato il daemon
(sostituisci la directory se l'installazione è personalizzata). È un processo
separato che usa esclusivamente le API HTTP e lo stream SSE del daemon: non
legge direttamente i database e può quindi essere usata anche da una shell SSH.
Su un terminale stretto — com'è tipicamente quello di un telefono collegato via
SSH — l'interfaccia si compatta da sola (sotto le 60 colonne: intestazione,
colonne e valori lunghi si adattano invece di essere tagliati).

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang it
GEXTTO_URL=http://host:5000 /opt/gextto/gexttod tui
```

Le opzioni sono `--url`/`-u` e `--lang`/`-l`; la variabile d'ambiente per l'URL è
`GEXTTO_URL`. Le nove schede sono Stato,
Torrent, Log, Salute, Archivio, Mancanti, Blocklist, Libreria e Manutenzione. La guida completa dei tasti,
dei prompt e delle azioni è in [`docs/tui.md`](tui.md).

**Installazione e aggiornamento.** A ogni push su `main` viene pubblicato un
payload Linux `continuous` testato. L'installer ufficiale lo scarica, verifica il
checksum SHA-256 e lo installa senza compilare nulla sul server:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer deve essere eseguito come root e richiede systemd. Usa `GEXTTO_REPO`
e `GEXTTO_RELEASE` per scegliere un repository o una release diversi. Salva i
dati del servizio in `/var/lib/gextto` e il programma in `/opt/gextto`; un file
opzionale `/etc/gextto/gextto.env`, se presente, viene caricato come variabili
d'ambiente (il token API non è lì: si imposta in *Configurazione → Accesso e
servizi*). Installer e `gexttod --update` installano lo stesso payload. Per
impostazione predefinita il servizio gira con **l'utente che esegue
l'installer**; `--user NAME` (o `GEXTTO_USER`) sceglie un altro utente e
`--user gextto` l'account di sistema isolato e senza login.

Per un'installazione senza root da un checkout sorgente:

```bash
make build          # Go puro, con gx-torrent
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

Per Docker (Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr e aMule insieme) vedi [Docker: stack completo](DOCKER.it.md).

Il servizio per-utente ascolta per default sulla porta 5000 su tutte le
interfacce. Limita l'accesso con firewall o reverse proxy; usa
`loginctl enable-linger "$USER"` se deve rimanere attivo dopo il logout.
`--update` scarica `gextto-linux-<arch>.tar.gz`, verifica il `.sha256`
pubblicato (obbligatorio: senza checksum l'aggiornamento viene rifiutato), prepara i file e poi sostituisce l'eseguibile (con la
UI web inclusa), la `lib/` e `run.sh` con rename atomici. Dati e configurazione in
`GEXTTO_DATA_DIR` (default `/var/lib/gextto`) non vengono mai toccati: un
download, un checksum o un'estrazione falliti lasciano l'installazione in
esecuzione invariata, e uno swap fallito viene ripristinato. Il marker `VERSION`
accanto all'eseguibile viene aggiornato ed è mostrato da `--version`.

Il repository ufficiale pubblica, per ogni release, gli asset Linux **x86_64 e
aarch64** (archivio, `.sha256` e manifest). L'installer e `gexttod --update`
scelgono l'asset in base all'architettura della macchina.

- `--channel stable` / `--release <tag>` — scelgono la release da installare;
- `--install-dir <dir>` — installa altrove (default: la directory del binario);
- `--archive <file>` — installa da un archivio locale (offline);
- `--force` — reinstalla anche se la versione è invariata;
- `--no-restart` — non riavviare `gextto.service`.

Esegui `/opt/gextto/gexttod --update` come root per riavviare automaticamente
`gextto.service`; altrimenti stampa il
comando `systemctl` esatto. L'unità systemd non viene sovrascritta, così le
personalizzazioni locali (utente, porte, percorsi) restano. Lo stesso payload è
il pacchetto di release usato dall'installer (README, *Installazione Linux*).

## 2. Dashboard

- **Controlli dell'interfaccia** — la barra superiore offre **Tema**, dimensione
  del testo e **Font**. **Tema** apre un unico dialogo: in alto la griglia dei
  temi con l'anteprima dei loro colori (*Scuro*, *Chiaro*, *Crema* — un chiaro
  caldo per letture lunghe — e i classici Nord, Dracula, Solarized, Gruvbox,
  Monokai, One Dark, Tokyo Night, Catppuccin, GitHub, Rosé Pine, Everforest,
  Nero OLED, Alto contrasto, nelle varianti scure e chiare); il tema si applica
  appena lo selezioni, anche con le frecce della tastiera. Sotto, *I tuoi temi*
  elenca i temi salvati e **Personalizza** parte dal tema scelto con quattro
  selettori (sfondo, pannelli, testo, accento): gli altri toni si calcolano da
  questi, il testo sui pulsanti d'accento diventa chiaro o scuro per restare
  leggibile e un avviso segnala un contrasto troppo basso. Dai un nome e premi
  *Salva tema*: i temi salvati stanno sul server e compaiono su ogni
  dispositivo (un nome già usato viene sovrascritto, ✕ elimina il tema). Il
  tema attivo, la dimensione del testo e il font restano invece una scelta del
  singolo browser; su telefono **Tema** è nel menu *Altro*. **Font** applica un
  preset generico, un font locale rilevato o una famiglia inserita manualmente
  all'interfaccia e ai log. Il rilevamento dei font installati dipende dal
  supporto del browser alla Local Font Access API e può richiedere
  un'autorizzazione.
- **Riepilogo in barra** — all'estrema destra della riga dei comandi della
  barra, sotto l'orologio, la Dashboard mostra due riquadri compatti: **Spazio
  libero** e **Magnet in archivio** (con quelli visti nei feed). Compaiono solo
  nella Dashboard; le altre pagine non li mostrano. Su schermo largo
  **Italiano** apre quella riga sotto il primo riquadro (CPU e RAM di Gextto).
- **Ricerca manuale globale** — cerca in archivio + indexer + motori web.
- **Ricerca automatica** — avvia un ciclo completo o di un solo dominio (Serie,
  Film, Fumetti); mostra prossima esecuzione, intervallo ed esito dell'ultimo
  ciclo; backup immediato.
- **In sessione** — un unico riquadro in cima: i torrent in scarico, fermi e in
  seed, la banda degli ultimi 30 giorni e l'accesso diretto a Scarico, con sotto
  il **grafico del traffico degli ultimi 30 minuti** (curve di **download** e
  **upload**, il picco, la velocità attuale e la linea del **limite di banda in
  vigore**: limiti fissi, programmazione o eccezione; non compare quando la
  velocità è *Auto*, cioè quando Gextto misura la linea e si adatta; in quel
  caso il riquadro mostra il badge **Auto**, e il ping misurato compare accanto
  alla velocità **↓ Download** nella barra in alto, non
  «Nessun limite»). Le velocità
  sotto 1 KB/s valgono 0: è solo rumore di protocollo, e il grafico resta
  piatto invece di tremare. Con eD2k attivo aMule ha **linee proprie
  tratteggiate** e una voce **eD2k ↓ ↑** accanto a quella dei **Torrent**; le
  velocità nella barra in alto sono invece la somma di torrent e aMule. Si
  aggiorna da solo.
- **Da controllare** — sotto la striscia: i download **fermi** (con il motivo,
  il prossimo tentativo e quando verranno abbandonati; il nome si ferma alla
  stagione/episodio e quello completo compare passando il mouse), gli
  **spostamenti in libreria in sospeso** (destinazione e tentativi) e gli
  avvisi, gli errori e i
  recuperi di pack 🛟, uno per download. Un avviso **resta finché il download è
  ancora in sessione** (o ha uno stallo o uno spostamento in sospeso): quando il
  download esce dalla sessione senza nulla in sospeso **sparisce da solo**, e un
  ciclo chiuso bene (file in libreria, copia sostituita) **non compare** mai —
  niente scadenza a 24 ore. **Pulisci** segna come letti quelli ancora mostrati
  (un nuovo avviso dello stesso download ricompare).
  *Storia* apre
  la storia completa di quel download. Si aggiorna da solo ogni minuto; se è
  tutto a posto dice «Niente da controllare».
- **Card statistiche** — libreria (serie/film), spazio libero, magnet in
  archivio e motore.
- **Rete e download attivi** — CPU/RAM e sparkline di rete in tempo reale.
- **Prossime uscite**, **ultimi download**, **attività recente** e **ultimi
  trovati nelle sorgenti**.

### Ricerca manuale dalla Dashboard

La ricerca manuale è utile per capire cosa vede Gextto prima di modificare una
configurazione o avviare un download:

1. inserisci un titolo o una query tecnica;
2. se l'integrazione con aMule è attiva (vedi [eD2k e Kad](#ed2k-e-kad-con-amule)),
   spunta **eD2k** accanto al pulsante di ricerca per cercare anche su eD2k/Kad,
   sulla rete scelta in Configurazione;
3. attendi che le sorgenti terminino o che il timeout segnali quelle lente;
4. confronta titolo, sorgente, qualità, dimensione e seed/peer;
5. usa **Filtra i risultati già caricati** per restringere localmente la lista;
6. apri **Perché non questa?** sui risultati interessanti;
7. usa **Accoda** solo dopo aver controllato il motivo e il confronto archivio.

Il filtro locale lavora sui risultati già ricevuti, inclusi titolo, sorgente e
campi tecnici della qualità. Scrivere nel filtro non interroga nuovamente gli
indexer e non modifica la query originale.

La ricerca manuale può mostrare release non idonee per permettere l’ispezione.
Il fatto che una release sia visibile non significa che il ciclo automatico la
scaricherebbe.

### Come leggere un ciclo

Un ciclo normale attraversa, in ordine, ricerca, filtri, confronto con archivio,
selezione e accodamento. Il numero di release trovate non equivale al numero di
download: una release può essere esclusa perché non monitorata, troppo vecchia,
bloccata, già presente o inferiore al file archiviato.

**Copie della stessa release.** Lo stesso torrent arriva spesso da più fonti: il
feed e la ricerca dello stesso indexer, Jackett e Prowlarr, indexarr. Gextto le
unisce in una sola release:
- quando hanno lo stesso infohash, anche se una fonte lo dichiara accanto a un
  link `.torrent` o lo scrive in base32;
- quando una fonte non dà l'infohash, ma nome e dimensione coincidono, a meno
  dell'arrotondamento con cui gli indexer scrivono le dimensioni.

Due torrent con infohash diversi restano sempre separati, anche se hanno lo
stesso nome. Restano separati anche i nomi che differiscono per gruppo,
risoluzione, lingua, PROPER o episodio: nel dubbio Gextto non unisce.

Della release unita usa la copia migliore: swarm vivo, più seeder, infohash
noto. Tiene le altre copie come riserva: se il link `.torrent` scelto non
funziona o è in quarantena, prova subito la stessa release da un'altra fonte,
invece di lasciare l'episodio al ciclo successivo.

## 3. Scarico

- **Cerca una release**: il pannello in cima a Scarico cerca su archivio,
  indexer, motori web e — se spunti **eD2k** e aMule è attivo — su eD2k/Kad,
  senza cambiare pagina. I risultati si aprono sotto il campo; dopo un'aggiunta
  resti in Scarico.
- Il modulo **Aggiungi torrent** si apre dal pulsante con lo stesso nome nella
  barra in alto, presente in **ogni pagina** (da un'altra pagina ti porta in
  Scarico e apre il modulo): da chiuso non occupa spazio. Accetta un magnet/URL
  `.torrent` oppure un file `.torrent`; opzionalmente percorso di salvataggio,
  “Scarica subito” e “Non rinominare”.
- **Download in sessione**: velocità download/upload, numero torrent e peer;
  l'elenco si aggiorna automaticamente ogni 5 secondi.
- I download HTTP dei fumetti mostrano stato, progressione, byte scaricati e
  velocità nella stessa lista dei torrent.
- I **download eD2k di aMule** compaiono nella stessa lista, con il badge
  **eD2k** colorato: stato, progressione, velocità, **byte caricati**, **tempo
  stimato** e **rapporto** caricato/scaricato, più le fonti (in trasferimento /
  totali) e **Pausa**, **Riprendi** e **🗑️**. Su un download in corso 🗑️ lo annulla e
  cancella la parte scaricata; su un file completato lo toglie solo dall'elenco
  di aMule (il file resta nella cartella di arrivo). **ⓘ** apre il dettaglio:
  stato, fonti, ultimo dato ricevuto, hash, link eD2k da copiare e la
  **priorità in aMule** (bassa, normale, alta, automatica). Il contatore in alto
  mostra anche **eD2k: N** quando l'integrazione è attiva; i download eD2k
  contano anche nella dashboard e nel numero accanto a **Scarico**.
- **Riga della vista**: casella per selezionare tutto, filtro per nome, filtro per
  tag, **Ordina per** con il verso (↑/↓), **⟳** per aggiornare subito e il menu
  **Strumenti**: **Pulisci completati**, **File orfani…**, **Aggiorna ogni 5 s**
  e il collegamento a **Rimozione a fine seed** (l'opzione «Elimina i completati
  dopo il seed» si imposta in Configurazione → Seeding; il segno ✓ dice se è attiva).
- **Barra di selezione**: compare solo quando spunti almeno un torrent e resta
  durante l'aggiornamento automatico. Mostra quanti ne hai scelti, **✕** per
  deselezionarli, **Pausa**, **Riprendi**, **Ricontrolla**, il menu **Tag** (un clic
  su un tag lo assegna; in fondo crei un tag nuovo o togli quello presente) e
  **Rimuovi dalla sessione**.
- **Banda** (in alto a destra del pannello). La velocità segue una **regola**
  scelta in Configurazione → Velocità e rete (Auto, limiti fissi o programmazione
  oraria); da qui fai un'**eccezione** per adesso, che vince sulla regola finché
  non scade o non la togli. Il pulsante mostra cosa vale: ⚡ un'eccezione (con i
  minuti che restano), ⏰ una fascia della programmazione, altrimenti la regola.
  Con eD2k attivo **aMule segue gli stessi limiti** (regola, eccezione,
  programmazione), riapplicati entro un minuto da ogni cambio. Il menu:
  - **Adesso**: cosa vale e da dove viene; con un'eccezione attiva dice quando
    finisce e quale regola torna dopo. **Togli l'eccezione** torna subito alla regola.
  - **Auto**: Gextto misura la linea e si adatta, senza tetto. È grigio con
    «È già la regola» quando la regola in quel momento è già Auto.
  - **Limita download** / **Limita upload**: scegli un valore (da 512 KB/s a 10 MB/s
    per il download, da 64 KB/s a 1 MB/s per l'upload, oppure **Senza limite**):
    l'eccezione parte subito e l'altro verso resta com'era.
  - **Durata** (con un'eccezione attiva, Auto compreso): 1 h, 3 h, 6 h, 12 h o
    **Finché non la tolgo**. Un'eccezione nuova dura finché non la togli.
  - **Valori personalizzati…**: DL e UL in KiB/s e durata in minuti (0 = finché
    non la togli), poi **Applica**.
  - **Cambia la regola** porta alla scelta in Configurazione.
- **Pin**: un torrent forzato fuori coda mostra il badge **📌 pin**; si sblocca
  dal pulsante **Sblocca pin** nella sua scheda di dettaglio (dove c'è anche **Pin**).
- **Colonne tabella**: Nome, Stato, Progresso, ↓, ↑, ETA, Peers, Ratio — si ordinano
  da **Ordina per**.
- **Azioni riga**: pausa/riprendi, recheck, rimuovi.
- **Dettagli**: si aprono cliccando il **nome del torrent** (o un punto qualsiasi
  della riga; da tastiera il nome è il pulsante che li apre) come riquadro
  **sopra lo Storico download**, non come finestra centrata: la sessione e lo
  storico restano visibili mentre imposti i valori.
- **Dettagli** (tab Generale, Tracker, Contenuto, Peers, Pezzi, Limiti, Storage):
  copia magnet, limiti per torrent/giorni di seed, reannounce, pin, riavvia,
  segna come fallito, sposta storage. Passando il mouse su un valore del tab
  **Generale**, sui pulsanti delle azioni o sulle intestazioni delle tabelle
  compare una spiegazione di cosa significa il dato o cosa fa il pulsante. In **Generale** trovi anche **Esporta
  .torrent**, **Super seeding** (con gx-torrent vale solo a torrent completato e
  riduce di proposito l'upload del seed) e l'aggiunta/rimozione di **web seed**;
  con gx-torrent la scheda **Generale** elenca anche gli **hint BEP 38**
  (`collections`/`similar`) con i torrent locali che li condividono, quando il
  torrent ne ha; in
  **Tracker** puoi modificare l'intera lista (`tier|url` per riga); in
  **Contenuto** imposti la **priorità per file** (Salta/Normale/Alta/Massima: i
  file ad alta priorità vengono scaricati per primi). Con gx-torrent cambiare
  **solo il livello** è immediato, senza fermare il torrent; escludere o
  reincludere un file lo riavvia per un attimo per spostarne i dati.
  In **Pezzi** c'è la mappa dei pezzi (verde scaricato, giallo in corso, grigio
  mancante, spento saltato): compare con i motori che la espongono (gx-torrent) e
  resta leggibile anche con il torrent in pausa o parcheggiato, perché usa i
  pezzi già verificati.
  Con gx-torrent ogni file ha un pulsante **▶** che apre lo **streaming HTTP**
  del file (con Range): i pezzi della parte letta vengono scaricati per primi,
  così puoi iniziare a guardare prima che il download finisca. Lo stesso motore
  di streaming serve il **media server DLNA/UPnP** (opzione *gx-torrent — server
  DLNA/UPnP*): TV e console sulla rete locale vedono i media **completati** senza
  configurare percorsi, e anche le **cartelle della libreria** (l'archivio sul
  NAS: serie e film, navigabili per cartelle; opzione *gx-torrent — DLNA:
  cartelle della libreria*, vuota = automatico). Video e cartelle mostrano le
  **locandine e miniature** già presenti accanto ai file, nel formato di
  Jellyfin, Kodi ed Emby (`episodio-thumb.jpg`, `film-poster.jpg`, `folder.jpg`,
  `poster.jpg`, le locandine di stagione `season01-poster.jpg`): vengono
  ridimensionate al volo, senza scrivere nulla nella libreria. I sottotitoli `.srt`/`.vtt` con lo stesso nome del video
  vengono agganciati al video. I lettori possono anche **cercare per titolo**
  (ContentDirectory Search) e **Xbox 360 e Windows Media Player** accettano il
  server grazie al servizio `X_MS_MediaReceiverRegistrar`. Nella pagina delle
  impostazioni di gx-torrent il pulsante **Cartella download** aggiunge alla
  lista la cartella di download già configurata. È disattivato di default e non ha autenticazione:
  per questo risponde **solo** ai dispositivi della rete locale (le richieste da
  indirizzi pubblici sono rifiutate) e serve solo i media elencati. Nella pagina
  delle impostazioni di gx-torrent compare l'indirizzo su cui è attivo.
- Quando usi **Sposta storage**, il log registra richiesta, destinazione e
  accettazione del comando; l'esito finale viene scritto quando il motore
  completa o rifiuta lo spostamento. Con **Check** il log distingue comando
  avviato e controllo terminato, includendo stato e byte verificati.
- **Storico download**: elenca i download conclusi (nativi e migrati). Colonne:
  nome (con badge **NAS** quando il file è archiviato), tipo/stagione/episodio,
  **tag NAS** (regola di cartella), score, stato (*Completato*), **percorso
  libreria/NAS** e data di conclusione.

### Torrent stalled

Quando un torrent non aumenta i byte completati per il tempo configurato, passa
allo stato **stalled**: Gextto lo mette in pausa anche nel motore, quindi non
occupa più gli slot attivi. Il torrent resta nella sessione; al tentativo
successivo viene ripreso e riannunciato. I valori sono in *Configurazione →
Seed e completamento → Torrent bloccati*:

- **Considera stalled dopo** — default 60 minuti;
- **Retry stalled** — default 60 minuti;
- **Rimozione stalled** — default 20160 minuti (14 giorni), `0` = mai.

La presenza di peer senza aumento dei byte non resetta il timer. Nel log (in
inglese) cerca le righe `is stuck at`, che indicano anche il motivo, poi
`is still stuck` a ogni nuovo tentativo e `is downloading again` quando
riparte; solo dopo il limite finale compare `Gave up on`.

Quando abbandona un **pack di stagione**, Gextto prima porta in libreria gli
episodi i cui file sono già completi (tutti i pezzi verificati), come farebbe con
un pack finito, e solo dopo rimuove il torrent e i dati parziali: nel log compare
`🛟 … already complete, added to the library before giving up` con l'elenco
degli episodi. Alla ricerca successiva vengono cercati solo quelli ancora
mancanti. Il recupero richiede una cartella della libreria (quella della serie o
quella generale); vale con ogni motore, anche per un torrent parcheggiato.

### Stati e azioni consigliate

| Stato | Significato | Azione consigliata |
|---|---|---|
| In coda | registrato ma non ancora avviato | attendere il ciclo/sessione |
| Download | trasferimento in corso | controllare velocità e peer |
| Stalled | nessun aumento reale dei byte | attendere il retry automatico |
| Seeding | download completato, seed ancora attivo | lasciare il torrent o rimuoverlo secondo policy |
| Errore | il torrent ha riportato un errore | leggere il motivo prima di rimuoverlo |
| Archiviato/NAS | il file finale è stato copiato nella libreria | verificare il percorso, non cancellare la sorgente mentre è in uso |

**Rimuovi** agisce sulla sessione torrent e può chiedere se eliminare i file.
**Pulisci completati** è più selettivo: rimuove i torrent che hanno raggiunto i
limiti di seed. Un torrent impostato a **seed infinito** (rapporto 0 oppure
giorni 0) non viene mai rimosso, nemmeno se è in pausa o già archiviato. Un
torrent rimosso dalla sessione non equivale necessariamente a un file rimosso
dalla libreria.

### Limiti di seed: regole per sorgente e seed inattivo

Quando un torrent finisce il seed lo decide Gextto, con lo stesso criterio per
gx-torrent e per qBittorrent. Per ogni limite (ratio, tempo, inattività) vale il
primo livello che lo imposta:

1. il limite del **torrent** (scheda di dettaglio: ratio e giorni di seed);
2. la prima **regola di seed** che corrisponde al torrent (*Configurazione → Seed
   e completamento → Regole di seed*);
3. il limite **globale** della stessa scheda.

**Seed infinito.** 0 vuol dire infinito, per il rapporto come per il tempo
(giorni e minuti a 0). **Basta che uno dei due sia infinito** perché il seed non
finisca mai: il torrent resta in seed, non viene fermato nemmeno per
inattività e, in modalità *Sposta*, non viene spostato in libreria (ne entra una
copia, o un hardlink). Un limite di tempo non ferma un torrent con rapporto
infinito, e viceversa. Quindi, per fermare il seed, rapporto **e** tempo devono
essere entrambi maggiori di 0. Lo stesso vale per i download eD2k: con seed
infinito il file viene copiato e aMule continua a condividerlo.

Una regola indica una **sorgente** (l'indexer da cui è arrivata la release, come
appare nell'elenco dei download: basta una parte del nome, senza distinguere
maiuscole e minuscole, quindi `mircrew` vale anche per `mircrew indexerr`) e/o
una **categoria** (Serie TV, Film, Fumetti, Altro). È l'equivalente dei limiti per
categoria di qBittorrent 5.2, con in più la sorgente: le regole di un tracker
privato seguono il tracker, non il tipo di contenuto. Nei campi della regola un
valore vuoto usa il limite globale, 0 vuol dire infinito (il torrent resta in
seed per sempre anche se il globale lo fermerebbe). Un torrent arrivato dalla ricerca aggregata
di Jackett (sorgente `jackett` senza nome dell'indexer) non porta il nome del
tracker: per lui valgono la categoria o il globale.

**Ferma il seed inattivo dopo** ferma un torrent in seed che non invia dati da
quei minuti, come il limite di inattività di qBittorrent. Conta solo il tempo in
seed attivo: un torrent in coda o in pausa non viene fermato perché fermo, e il
conteggio riparte a ogni riavvio di Gextto (un riavvio può solo allungare il seed,
mai accorciarlo). A fine seed segue quello che segue gli altri limiti:
archiviazione in libreria e, se attiva, rimozione. Un torrent impostato a seed
infinito non viene mai fermato per inattività.

### Completamento di file e cartelle

Il comportamento dipende dalla forma del torrent e dalla presenza di una
destinazione **Libreria/NAS** configurata:

| Contenuto completato | NAS configurato | NAS non configurato |
|---|---|---|
| Episodio singolo come file | il video viene archiviato e rinominato secondo le regole della serie | resta nel percorso di download secondo il comportamento del file singolo |
| Episodio singolo dentro una cartella | il video viene copiato nel NAS e rinominato; la cartella sorgente resta intatta durante il seeding e poi viene spostata nel **Cestino** | la cartella resta intatta nella cartella Download, anche dopo la fine del seeding |
| Season pack dentro una cartella | i video degli episodi vengono cercati ricorsivamente, copiati nel percorso NAS della serie e rinominati; il pack sorgente resta per il seeding e poi viene spostato nel **Cestino** | il pack resta intatto nella cartella Download, anche dopo la fine del seeding |

Per i season pack vengono importati nell'archivio i file video che riportano una
identità episodio riconoscibile, per esempio `S01E02` o `1x02`. Sottotitoli, NFO,
artwork e altri allegati del pack non vengono copiati nell'archivio: restano nella
cartella sorgente fino al suo spostamento nel Cestino. La cartella sorgente non
viene mai spostata o eliminata mentre il torrent sta ancora facendo seeding.

#### Hardlink al posto della copia

Quando un file deve restare in seed e intanto entrare in libreria (episodi in
cartella, season pack, episodi in modalità copia), Gextto prova prima a creare
un **hardlink**: un secondo nome, nella cartella della serie, per gli stessi dati
già scaricati. Il file compare in libreria subito e non occupa spazio una seconda
volta.

- I due nomi sono alla pari: cancellare per errore il file nella cartella di
  download **non** tocca quello in libreria (si interrompe solo il seed), e
  viceversa. I dati spariscono solo quando sono cancellati entrambi i nomi.
- Funziona solo se download e libreria stanno sullo **stesso filesystem**
  (stesso disco o stessa condivisione NAS). Se non è possibile (dischi diversi,
  RAM disk, condivisione senza hardlink) Gextto copia come prima e lo scrive una
  volta nel log con il motivo.
- Lo stesso NAS non basta se le due cartelle sono **mount separati** (per
  esempio `192.168.1.119:/volume1/Trasferimento` e
  `192.168.1.119:/volume1/SerieTVArchivio`): il kernel rifiuta l'hardlink tra
  mount point anche se i dati sono sullo stesso volume. Monta una radice comune
  (per esempio `192.168.1.119:/volume1`) o tieni la libreria dentro la stessa
  condivisione dei download; il log te lo dice esplicitamente.
- Un programma che modifica il video **sul posto** (riscrive i byte nello stesso
  file, come alcuni tagger o `mkvpropedit`) cambierebbe anche il file in seed.
  Gextto non lo fa; se usi strumenti del genere sulla libreria, disattiva
  l'opzione.

Si disattiva con *Configurazione → Seed e completamento → Hardlink invece della
copia durante il seed*.

Il Cestino deve essere configurato in *Configurazione → Archivio e spazio*. Se non c'è una
destinazione NAS, Gextto non tratta la cartella Download come archivio e non la
sposta automaticamente nel Cestino. Lo spostamento automatico della cartella
sorgente nel Cestino avviene solo con NAS configurato, con una destinazione
**Cestino** valida e con la rimozione automatica dei completati attiva; se il
Cestino non è configurato i file sorgente non vengono mai cancellati
automaticamente (viene rimossa solo la voce dalla sessione torrent).

## 4. Serie TV

Aggiungi una serie via ricerca TMDB o manualmente (titolo, qualità, lingue,
stagioni, alias, esclusioni, percorso NAS, sottotitoli, timeframe). Il percorso
NAS è precompilato con *cartella libreria + titolo*: lascia attivo «Crea
cartella» per crearlo all'aggiunta (togli la spunta per modificare il percorso a
mano). «Sottocartelle per stagione» archivia gli episodi in *Stagione 01*,
*Stagione 02*, … dentro la cartella della serie.

- La lista mostra Nome, Stagioni, Qualità, Lingua, Episodi (scaricati/totali),
  Completezza, Ultimo download; filtrala con la casella di ricerca.
- La lista si può leggere in due modi, col selettore **Elenco / Locandine** sopra
  la tabella: *Locandine* mostra una card per titolo con la copertina e i dati
  essenziali (qualità, lingua, episodi, stato), e si entra nel dettaglio dal
  titolo o dalla copertina. La scelta è ricordata e vale anche per la pagina
  Film, così l'elenco si apre sempre come preferisci.

- Azioni bulk: imposta lingua, elimina selezionate.
- **Dettaglio serie**: locandina/trama/cast, badge (anno, rete, stato,
  completezza), link del cast alle schede TVDB/TMDB e, se configurate, un badge
  **“stagioni disattivate”**. Il percorso archivio è visibile nella testata e
  il form di modifica include **Sfoglia** per scegliere una cartella server.
- Azioni: cerca mancanti, scansiona archivio, aggiorna da TMDB (o TVDB), anteprima/esegui
  rinomina, modifica.
  L'anteprima di rinomina elenca i nomi *Vecchio → Nuovo* e ha un pulsante
  **Forza rinomina**: rielabora anche i file che superano il controllo rapido,
  ricalcolando il nome esatto dal template e dai metadati TMDB/TVDB; è utile
  quando un file sembra corretto ma non rispetta il template configurato.
- **Episodi**: accordion per stagione; per ogni episodio puoi cercare, copiare il
  magnet, ignorare/riattivare, forzare, riscaricare o eliminare; la ricerca
  manuale segnala i risultati già presenti nel feed.

### Aggiungere e configurare una serie

Per una serie nuova il metodo più sicuro è la ricerca TMDB:

1. apri **Esplora**, cerca il titolo e seleziona il risultato corretto;
2. controlla titolo, anno, rete e poster;
3. scegli il **Profilo di qualità**: è lui a decidere qualità, audio minimo,
   lingua, sottotitoli, esclusioni e «Permetti upgrade» del nuovo titolo (il
   modulo non ha altri campi per questi valori, così non possono contraddirsi);
   aggiungi eventuali **Esclusioni in più** solo per questo titolo, che si
   sommano a quelle del profilo, e le stagioni da monitorare;
4. imposta il percorso archivio solo se non vuoi usare quello globale;
5. salva in dry-run e controlla la pagina di dettaglio.

I campi più importanti sono:

| Campo | Effetto |
|---|---|
| Profilo di qualità | all'aggiunta e nella scheda del titolo: dà al titolo qualità, audio minimo, lingua, sottotitoli e «Permetti upgrade» del profilo e somma le sue esclusioni a quelle del titolo (vedi *Profili di qualità*). Nella scheda è preselezionato il profilo che corrisponde ai valori del titolo, esclusioni comprese; se nessuno corrisponde compare **Valori attuali**, che salvando non cambia nulla. Il profilo è un modello: cambiarlo dopo non tocca i titoli già creati |
| Qualità | restringe le release accettabili e partecipa allo score |
| Audio minimo | arriva dal profilo di qualità all'aggiunta e si cambia con *Modifica selezionati* (non c'è nella scheda del titolo); preset che include già i canali: `Lossless 5.1+`, `Lossless 7.1`, `DTS / DD+ 5.1+`, `DTS / DD+ 7.1`, `AC3 / 5.1`, `AAC / Opus 5.1+`; **Qualsiasi** non impone nulla ed è il default, anche per le serie già caricate |
| Lingua | richiede la lingua configurata quando è riconoscibile nel titolo |
| Sottotitoli | gestisce la preferenza senza accettare sottotitoli hardcoded |
| Stagioni | decide quali stagioni sono monitorate; gli intervalli sono supportati |
| ID TMDB / ID TVDB | identificativi della serie nei due servizi. **TMDB è la fonte predefinita** dei metadati (titoli degli episodi, trame, locandine) quando la chiave TMDB è impostata; TVDB si usa quando è configurata solo quella chiave. Sono due campi indipendenti: salvarne uno non cancella l'altro, e un ID sbagliato si corregge qui |
| Alias | aiuta a collegare titoli diversi alla stessa serie |
| Esclusioni | parole nel titolo che devono bloccare la release |
| Permetti upgrade | **Sì** (predefinito): una release migliore sostituisce il file archiviato; **No**: i file già archiviati di questa serie restano. Arriva dal profilo di qualità all'aggiunta e si cambia con *Modifica selezionati*: non è nella scheda del titolo, così non c'è un secondo posto dove impostarlo |

Lascia le stagioni non ancora disponibili abilitate se vuoi che il calendario e
la ricerca dei mancanti continuino a funzionare. Usa *Ignora* solo per episodi
che non vuoi più cercare: riattivandoli tornano candidati nei cicli successivi.

### Episodi mancanti e pack

Tutte le puntate già uscite che non sono in libreria sono nella scheda
**Mancanti** di **Serie TV** (accanto a *Serie monitorate*; il numero dice
quante sono). Sono raggruppate per serie, ognuna richiudibile, con una riga per
stagione e le puntate come pulsanti (`S04E03`):

- **un clic su una puntata** la cerca e apre i risultati, prima dall'archivio
  locale e poi online, ognuno con **Scarica**;
- **Cerca mancanti** cerca tutte le puntate mancanti di quella serie e, finita
  la ricerca, scrive accanto quanti risultati ha trovato; poi un clic sulle
  puntate li mostra;
- **Filtra per serie** mostra solo le serie che contengono il testo scritto.

Il vecchio indirizzo della pagina *Mancanti* porta a questa scheda.

Da **Cerca mancanti** o dalla scheda episodio:

1. controlla che la stagione sia monitorata;
2. avvia la ricerca del singolo episodio o del dominio serie;
3. confronta release singole e season pack;
4. se un episodio è già sul disco ma non nel database, usa la scansione archivio;
5. usa **Forza** solo per un’azione manuale consapevole.
6. se torrent e archivio non trovano nulla e **Usa eD2k quando i torrent non
   trovano nulla** è attivo, la ricerca chiede l’episodio anche a eD2k/Kad: i
   risultati compaiono con la sorgente **eD2k/Kad** e il pulsante **Scarica** li
   passa ad aMule.

Con il fallback eD2k attivo la ricerca può richiedere più tempo: aMule raccoglie
i risultati per la durata configurata in **Durata della ricerca eD2k**, e la
ricerca di una serie fa una sola interrogazione per stagione.

Gextto evita normalmente di riscaricare episodi vecchi quando possiede episodi
successivi, a meno che si tratti di un buco riconosciuto o di un upgrade reale.
Un season pack può riempire più episodi, ma il confronto con l’archivio resta
episodio per episodio. Quando nello stesso ciclo ci sono un episodio singolo e
un pack che lo contiene con lo stesso punteggio, Gextto sceglie la release che
copre più episodi. A parità di punteggio resta preferito un REMUX e, a ulteriore
parità, vince la release con **seed noti** (uno swarm sano è meno a rischio di
stallo).

Mentre un pack scarica, Gextto legge l’elenco dei file appena arriva e lo
confronta con la libreria episodio per episodio. Se il pack non aggiunge nulla
lo scarta e, se l’approvazione aveva già sostituito un episodio, ripristina la
versione precedente; se solo alcuni file sono utili può scaricare soltanto
quelli. Il log dello scarto riporta il confronto (qualità e punteggio vecchi e
nuovi) e gli episodi tenuti o saltati. Le due opzioni sono in
*Configurazione → Seed e completamento → Controllo dei pack*; l’ispezione è
spenta per default.

Un episodio che ha già un torrent in download non è un mancante: non viene
cercato di nuovo e non compare tra i mancanti della serie, anche se il torrent è
stato aggiunto a mano, da una cartella osservata o dal telefono. Lo stesso vale
per tutta la stagione mentre se ne scarica il season pack. Se il download
finisce in errore l'episodio torna tra i mancanti.

Eccezione: se un download resta **fermo a 0 B/s o ancora in attesa dei
metadati** (l'elenco dei file non è mai arrivato) oltre il tempo configurato
(*Configurazione → Qualità e upgrade → Download bloccati*), la ricerca può
avviare un'alternativa della stessa puntata entro i punti tollerati, **senza
rimuovere** il torrent bloccato: se quello si riprende, la deduplica
dell'archivio terrà comunque la copia migliore. Vale anche per i season pack:
un pack bloccato rende idonee tutte le puntate che contiene, e l'alternativa può
essere una singola puntata o un altro pack.

## 5. Film

- Tab **Monitorati / Scaricati**; colonne ordinabili (nome, anno, qualità, lingua). Come per le serie, il selettore **Elenco / Locandine** legge la tabella come
  card con la copertina (la scelta è condivisa con la pagina Serie TV).
- L'editor gestisce qualità, **Lingua**, **Sottotitoli** con
  **Sottotitoli obbligatori** (Sì: una release senza quei sottotitoli è scartata;
  No: sono solo preferiti) ed esclusioni; **Permetti upgrade** e audio minimo si
  cambiano con *Modifica selezionati*. Ogni valore ha un
  solo campo: le vecchie «Lingue richieste» sono confluite in **Lingua** (che vale
  `chi`, `ita,eng`… anche oltre le voci dell'elenco) senza cambiare le decisioni.
- **Dettaglio film**: locandina, trama, cast, modifica, riscarica, **Cerca subito**,
  nomi del cast collegati alle schede TMDB, tabella dei “migliori trovati” dalle
  sorgenti e tabella **corrispondenze archivio** dove ogni riga offre **Perché
  non questa?**.

### Aggiungere e scegliere un film

1. cerca il film in **Esplora** e controlla anno e titolo originale;
2. aggiungilo alla libreria;
3. scegli il **Profilo di qualità** e aggiungi eventuali esclusioni del solo film;
4. usa **Cerca subito** per vedere i risultati senza aspettare il ciclo;
5. apri **Perché non questa?** prima di accodare una release borderline.

Il film viene identificato usando titolo e anno quando disponibili. Evita di
creare duplicati con lo stesso film scritto in modi diversi: correggi i metadati
del film monitorato invece di aggiungerlo nuovamente.

Più lingue si indicano come codici separati da virgola, per esempio `ita,eng`.

### Importare un file già presente

**Importa file** (scheda di **Manutenzione**) serve per un video che hai già sul disco e che
Gextto non ha scaricato: un episodio recuperato a mano, una vecchia stagione, un
film copiato da un altro disco. Scrivi il percorso, oppure premi **Sfoglia**:
si apre l'elenco delle cartelle del server (parte dalla cartella download) con
i video di ogni cartella; un clic su un video lo sceglie, **Seleziona** prende
la cartella intera. Poi premi
**Analizza**: Gextto legge i nomi, propone la serie o il film configurato, la
stagione e l'episodio e mostra la cartella di destinazione. Puoi correggere
serie, stagione, episodio (o film e anno) prima di confermare. **Importa ora**
rinomina il file secondo le regole, lo sposta nella libreria, registra
l'episodio (o il film) e aggiorna Jellyfin/Plex se sono configurati. Se nella
destinazione esiste già un file con lo stesso nome, l'import si ferma: metti la
spunta **Sostituisci il file esistente** per spostare la copia vecchia nel
cestino e mettere la nuova. I file multi-episodio vengono registrati per ogni
episodio che contengono.

### Modificare più titoli insieme

Nelle tabelle **Serie TV** e **Film** (vista *Elenco*) ogni riga ha una
casella: con **Modifica selezionati** cambi gli stessi valori su tutte le righe
spuntate in un colpo solo, ad esempio portare più film a `2160p` o mettere in
pausa più serie.

1. Spunta le righe nella tabella. La casella nell'intestazione spunta tutte le
   righe visibili: combinala con *Filtra l'elenco…* per prendere, ad esempio,
   tutti i titoli in `720p`.
2. Premi **Modifica selezionati**, accanto ad *Aggiorna elenco*: sotto si
   aprono i campi. Spunta solo quelli da
   cambiare (qualità, audio minimo, lingua, sottotitoli, esclusioni), scrivendo
   il nuovo valore. Un campo spuntato e lasciato vuoto viene svuotato; un campo
   non spuntato resta com'è in ogni titolo. *Permetti upgrade* e *Stato*
   cambiano solo se scegli un valore diverso da «Lascia com'è».
3. *Parti da un profilo* copia nei campi qualità, audio minimo, lingua,
   sottotitoli, esclusioni e upgrade di un profilo e li spunta, tranne le
   **esclusioni**: sono di ogni titolo (nella scheda del titolo quelle del
   profilo si sommano alle sue), quindi restano deselezionate e si spuntano solo
   se vuoi sostituirle con quelle del profilo.
4. Premi **Applica ai titoli selezionati**: la pagina si ricarica con i nuovi
   valori nelle colonne. Gli altri valori di ogni titolo (stagioni, cartella,
   alias…) non vengono toccati.

I requisiti obbligatori di un film (**Sottotitoli obbligatori**, e le
eventuali lingue richieste) seguono il valore che qualificano: cambiando lingua
o sottotitoli con **Modifica selezionati** viene aggiornato anche il requisito,
così il titolo non continua a chiedere quello vecchio.

Nella vista *Locandine* le caselle non ci sono: passa a *Elenco*. **Aggiorna
elenco** invece non modifica nulla: rilegge la tabella con il filtro scritto.

## 6. Esplora, Archivio, Fumetti

- **Esplora** — TMDB: tendenze oggi/settimana, popolari, più votati, in
  programmazione, prossime uscite; ricerca TMDB; ricerca release generica;
  aggiunta alla libreria. Le liste esistono solo su TMDB: con la sola chiave
  TVDB la ricerca dei titoli passa a TVDB. Le card sono mostrate in una griglia a cinque colonne;
  un titolo già in libreria porta il badge **Già in lista** e il pulsante di
  aggiunta è disattivato, così non crei duplicati per errore. Sotto la ricerca
  release c'è il campo per **incollare uno o più link `ed2k://`** (uno per riga
  o separati da spazi): Gextto li verifica, elimina i duplicati e li mostra come
  risultati con nome, dimensione e hash. Con **Aggiungi tutti** li passa in
  blocco ad aMule, oppure usi il pulsante **Aggiungi** della singola riga. Se
  spunti **eD2k**, i risultati eD2k compaiono nella stessa tabella con il badge
  **eD2k** e il numero di fonti, e si accodano con **Aggiungi** come i torrent:
  il link `ed2k://` passa ad aMule. Se l'integrazione è spenta o non risponde i
  risultati eD2k non compaiono.
- **Archivio** — ricerca full-text delle release passate con paginazione, accoda
  in blocco, copia magnet, elimina; tab **Serie/Film dal feed**. La ricerca
  nell'archivio locale è **immediata**: parte da sola mentre digiti (dai tre
  caratteri in su) e usa l'indice FTS5, quindi non pesa sul database. Per i
  termini più corti (es. `IT`) premi **Invio** o **Cerca**: la soglia vale solo
  per la ricerca automatica, non per quella esplicita. Il filtro `-parola`
  esclude i titoli che la contengono. La casella **Cerca anche nel
  web** estende la ricerca a indexer e motori online: è molto più lenta e per
  questo parte solo quando premi **Cerca**, non a ogni tasto. Con aMule attivo
  c'è anche **Cerca anche su eD2k**: anche questa parte solo da **Cerca**, e i
  risultati eD2k compaiono in un blocco sotto l'archivio, ognuno con
  **Scarica**. Il nome lungo resta su una riga accorciato con i puntini (il
  testo completo è nel tooltip) e la colonna **Azioni** con **Scarica** resta
  sempre visibile, anche sul telefono. La colonna
  **Sorgente** mostra solo il provider/dominio (l'URL completo è nel tooltip) e
  ogni riga offre **Perché non questa?** come nella tabella dei risultati. Il
  titolo va a capo su più righe e le azioni di riga sono icone compatte (⬇️
  accoda, 🔍 Perché non questa?, 🧲 copia magnet, 🗑️ elimina) per lasciare spazio
  al nome della release. Lo stesso torrent (stesso infohash) compare **una volta
  sola**, con la segnalazione più recente: le copie dello stesso contenuto con
  magnet o titolo diverso non riempiono più i risultati.
- **Visti dai feed** — ogni release vista nelle sorgenti, raggruppata per titolo
  (film/serie) con numero, risoluzione e score migliori; espandi un gruppo per
  accodare una singola release. Popolata ad ogni ciclo, anche per titoli non
  monitorati.
- **Perché non questa?** — nei risultati di ricerca puoi aprire una spiegazione
  read-only della release. Mostra esito, score e componenti dello score, regole
  superate o bloccanti, filtro sorgente, qualità/lingua/sottotitoli, blocklist,
  download attivi e confronto con il file già presente nell'archivio. Non accoda
  la release, non crea placeholder e non modifica il database. L'esito riguarda
  i controlli del candidato: la scelta finale del ciclo può dipendere anche da
  gap filling, smart episode, delay, spazio libero e confronto con altri
  candidati.
- **Fumetti** — in **Aggiungi fumetto** scrivi il titolo e premi **Trova**; scegli
  il risultato esatto di GetComics e poi conferma. Gextto salva il post scelto,
  il tag, la copertina e i metadati, e usa quel post per il download senza
  sostituirlo con un albo omonimo. Sono inoltre disponibili lista monitorati,
  estrazione link da un post, impostazioni weekly pack e storico con
  reinvia/elimina/forza. Un weekly pack viene accodato una volta sola: prima di
  scaricarlo Gextto guarda nel suo storico se lo ha già preso (e controlla se è
  ancora nella lista dei download), così un pack già scaricato non viene
  ripreso nemmeno se dopo averlo letto hai cancellato la cartella. La riga del
  log spiega anche il perché: nuovo pack della settimana, link di download
  arrivato dopo che il pack era stato trovato senza, oppure già scaricato in
  passato e quindi lasciato stare.
  I download HTTP diretti compaiono in **Download in sessione** con stato,
  byte, progressione, velocità e azioni pausa/riprendi.
  Un fumetto completato dal suo pipeline compare come **Completato · archiviato**
  al 100% anche se il torrent del pack è in pausa e ricontrollato a 0%.
  Nei file **CBZ** scaricati direttamente (HTTP o Mega) Gextto aggiunge un
  `ComicInfo.xml` con serie, numero e anno ricavati dal titolo (es.
  «Poison Ivy #41 (2025)»): Komga, Kavita e i lettori su tablet lo usano per
  raggruppare e ordinare gli albi. Un file che ha già il suo `ComicInfo.xml` non viene
  toccato; i fumetti scaricati via torrent restano identici, perché sono in
  seed, e i CBR (RAR) non si possono modificare.

- **eD2k** — la pagina di aMule: vedi la sezione seguente.

### eD2k e Kad con aMule

Gextto usa **aMule** per la rete eD2k e Kad: `amuled` fa il lavoro di rete
(server, Kad, fonti, code, crediti, trasferimenti) e Gextto lo comanda con il
protocollo delle **connessioni esterne (EC)**, lo stesso di `amulecmd` e
`amuleweb`. È tutto facoltativo e spento di default; con aMule spento i
torrent non cambiano. Se aggiorni da una versione che usava il nostro demone
`gx-ed2k`, l'aggiornamento rimuove il binario e le sue opzioni: per eD2k e Kad
resta aMule.

**Come si prepara.** Installa aMule (`amule-daemon` su Debian/Ubuntu) e
abilita le connessioni esterne in `amule.conf`:

```ini
[ExternalConnect]
AcceptExternalConnections=1
ECAddress=127.0.0.1
ECPort=4712
ECPassword=<md5 della password>
```

`ECPassword` è l'MD5 della password (`printf '%s' 'segreta' | md5sum`). Con
aMule in **Docker** (`ngosang/amule`) monta la cartella dei download nel
container **allo stesso percorso** dell'host e imposta lì `INCOMING_DIR` e
`TEMP_DIR`, così Gextto trova i file completati; usa una cartella di arrivo
dedicata, perché aMule condivide tutto quello che contiene. Per aprire le porte
sul router con l'**UPnP** il container deve usare la rete dell'host
(`network_mode: host`) con `UPnPEnabled=1` in `amule.conf`. Avvia
`amuled`, poi in **Configurazione → eD2k e Kad** accendi
**eD2k e Kad tramite aMule** e inserisci indirizzo, porta e password **in
chiaro**. Gextto deve vedere la cartella di arrivo di aMule allo stesso
percorso (succede quando girano sulla stessa macchina): è da lì che importa i
file completati. Apri sul router le porte TCP e UDP di aMule per avere un ID
alto e un Kad non dietro firewall, cioè più fonti.

**La pagina eD2k** (menu **Scoperta → eD2k**):

- **aMule**: versione, server eD2k a cui sei connesso con **ID alto/basso**,
  stato di Kad (connesso, dietro firewall, fermo), velocità e limiti, utenti e
  file della rete, coda di upload, byte scambiati in questa sessione e in
  totale. I pulsanti **Connetti**, **Disconnetti**,
  **Avvia/Ferma Kad** e **Lascia il server** comandano aMule; **Aggiorna**
  rilegge lo stato. Se aMule non risponde la pagina lo dice e spiega cosa
  controllare.
- **Server eD2k**: la lista di aMule con utenti, file e ping; **Connetti** su
  una riga collega aMule a quel server. Da qui scarichi anche un nuovo
  `server.met` o un `nodes.dat` per Kad da un indirizzo.
- **Cerca su eD2k e Kad**: scegli la rete (Kad, Kad + server eD2k, tutti i
  server eD2k, solo il server connesso) e cerca; aMule raccoglie i risultati per qualche secondo.
  Ogni risultato mostra nome, dimensione e fonti (totali / complete) e ha
  **Scarica**; **già noto** segnala un file che aMule ha già. aMule ha **una
  sola** lista di risultati, condivisa con la sua interfaccia e con ogni altro
  strumento collegato: se un'altra ricerca la sostituisce mentre Gextto cerca,
  Gextto rifà la ricerca e, se il disturbo continua, chiede di riprovare tra
  qualche secondo invece di mostrare file non attinenti. Sopra i risultati c'è
  il filtro (**Parole nel nome**, **Fonti minime**, **Dimensione minima**,
  **Nascondi già noti**, **Ordina per**): lavora sui risultati già ricevuti da
  aMule, quindi non fa ripartire la ricerca.
- **Incolla link eD2k**: uno o più link `ed2k://`, anche mescolati a testo;
  **Scarica con aMule** li aggiunge tutti.
- **Log di aMule**: **Carica il log** mostra le ultime righe del log di amuled.
- **Cartelle di aMule**: se la cartella di arrivo o quella temporanea non
  esistono (disco di rete smontato, cartella rimossa) la pagina lo segnala con
  **Crea le cartelle**: senza, aMule riempie il log di *Failed to get file
  system statistics* e non importa i file completati.
- **File condivisi**: quello che aMule condivide, con i byte caricati nella
  sessione e in totale, richieste e accettate (sessione e dall'inizio), client
  in coda, velocità di caricamento del file, ultimo caricamento, priorità e il
  link `ed2k://` da copiare; sotto, i client a cui stai caricando con velocità e
  posizione in coda. I contatori sono quelli di aMule: la colonna della sessione
  riparte da zero a ogni riavvio di `amuled`, quella totale resta. **Ricarica
  file condivisi** fa rileggere ad aMule le
  cartelle (utile se hai aggiunto o tolto file a mano); le cartelle condivise
  sono elencate in sola lettura.

I download aggiunti compaiono in **Scarico**. Un link a un file che non è un
video (pdf, programma, archivio) viene solo scaricato da aMule e resta nella
cartella di arrivo, come un torrent aggiunto a mano. Quando aMule completa un
video, Gextto aspetta che il file sia davvero nella cartella di arrivo, con la
dimensione attesa e stabile, poi lo tratta come un torrent completato: lo
rinomina, lo importa in libreria, lo controlla con MediaInfo e manda la
notifica. Lo stesso controllo di
qualità dei torrent gira prima dell'importazione: se in libreria c'è già un file
uguale o migliore il download viene scartato nel Cestino e non importato. Gli episodi e i film eD2k
seguono l'opzione **Sposta gli episodi/pack in archivio**: se è attiva vengono
spostati in libreria e rimossi dalla cartella di arrivo (aMule smette di
condividerli), se è spenta entrano con **hardlink o copia** e aMule continua a
condividerli (l'hardlink non occupa spazio in più se le due cartelle sono sullo
stesso disco). Per eD2k lo spostamento avviene al completamento, perché il
seeding è gestito da aMule e Gextto non ha un momento di fine seed da attendere.
Un download eD2k non ha limiti di seed propri: valgono le regole di seed e i
limiti globali. Con seed infinito (rapporto o tempo a 0) il file viene copiato e
resta condiviso anche con *Sposta* attiva, e la riga mostra il badge «seed ∞».

Per la guida completa — installazione di aMule, Docker, risoluzione dei
problemi e dettagli del protocollo — vedi [`docs/amule.it.md`](amule.it.md).

## 7. Configurazione

La pagina è divisa in quattro aree: **Cosa cercare** (Generale, Sorgenti,
eD2k e Kad, Qualità e upgrade), **Come scaricare** (Motore torrent, Velocità e rete, Code e
prestazioni, Seed e completamento), **Dove salvare** (Libreria e rinomina,
Archivio e spazio) e **Sistema** (Manutenzione automatica, Notifiche, Accesso e
servizi, Diagnostica e traduzioni). Le modifiche non salvate sono
evidenziate con la barra “Salva tutte”. L'elenco completo di ogni voce, tab per
tab, è nell'[Appendice A](#appendice-a-riferimento--configurazione).

- **Sorgenti** — lista feed RSS, indexer (Jackett, Prowlarr, indexarr o un
  Torznab diretto) con pulsante *Verifica*, URL FlareSolverr + test, motori web,
  filtri contenuto, blacklist. Per Jackett puoi indicare l'URL base, ad esempio
  `http://host:9117`, e la relativa API key: Gextto costruisce l'endpoint Torznab
  `/api/v2.0/indexers/all/results/torznab/api`. Il controllo di salute usa
  `t=caps` con la stessa API key, quindi verifica l'API e non soltanto la home di
  Jackett. Per indexarr l'URL base è di solito `http://host:8484`: la ricerca
  aggregata usa l'endpoint nativo `/api`, mentre l'elenco degli indexer e le
  singole lane usano i percorsi compatibili con Jackett. Nei risultati la
  sorgente può apparire come `jackett:NomeTracker` o `indexarr:NomeTracker`.
- **eD2k e Kad** — collegamento ad aMule (indirizzo, porta e password delle
  connessioni esterne), rete di ricerca (Kad, server o entrambi), ripiego
  automatico quando i torrent non trovano nulla e rinuncia ai download fermi.
  Il pannello **aMule** della scheda ha l'interruttore **Prealloca l'intero file
  in aMule** e i valori si
  applicano subito ad aMule, senza salvataggio separato. Lo stato e i comandi di
  aMule sono nella pagina eD2k.
- **Velocità, rete e prestazioni** — connessioni/prestazioni,
  protocolli/tracker, sicurezza, RAM disk e porte, limiti di velocità e
  scheduler; applica e ottimizza. Se sposti lo storage di un torrent in una cartella che
  **contiene già i dati**, non viene rifiutato: Gextto associa il torrent ai file
  esistenti e li ricontrolla (seed) invece di riscaricarli. L'opzione
  **Prealloca lo spazio su disco** (spenta di default)
  riserva subito l'intera dimensione di ogni nuovo torrent: evita la
  frammentazione su NAS/HDD e fa emergere subito la mancanza di spazio; puoi
  disattivarla per singolo torrent quando lo aggiungi a mano. **Copia i file
  .torrent in** indica una cartella in cui Gextto copia il `.torrent` di ogni
  torrent avviato (appena i metadati sono disponibili), utile per riusarlo con
  un altro client o come backup. In *Sicurezza,
  proxy e rete* l'**interfaccia VPN (killswitch)**
  vincola ascolto e traffico in uscita a una scheda scelta (es. `tun0`, `wg0`);
  l'elenco è letto dal server e la modifica si applica dopo il riavvio.
  Nella sezione RAM disk Gextto mostra i `tmpfs`/`ramfs` disponibili e consente
  di sceglierne uno. Se non c'è un percorso configurato, il pulsante dedicato
  crea `/dev/shm/gextto` e lo configura automaticamente. Il contenuto di
  `/dev/shm` è temporaneo e viene perso al riavvio della macchina. La scelta di
  un percorso calcola automaticamente dimensione massima per torrent, margine
  libero e spazio minimo; i valori restano modificabili. Il pulsante **Testa
  porte** verifica inoltre il bind locale delle porte indicate: non sostituisce
  il controllo del port-forwarding sul router/firewall.
- **Punteggi** — pesi per categoria, gestione dei gruppi custom (aggiungi, modifica
  o rimuovi il gruppo e assegna un bonus/penalità) e **Prova una release**. In
  testa ci sono le regole di upgrade, poi esempi, prova e profili di qualità; i
  pesi seguono in schede chiuse. I pesi
  audio sono una scheda per gruppo: Lossless (TrueHD, DTS-HD, FLAC, PCM),
  DTS/DD+ (con DTS-HD HRA), AC3/5.1, AAC/Opus e MP3; canali e Atmos/DTS:X stanno
  nella stessa scheda Audio. I pesi video sono in un'unica scheda **Video**
  con i sottogruppi 4K, Full HD, HD, SD, Codec video, Sorgente e HDR/DV;
  **Correzioni release** raccoglie PROPER, REPACK e REAL, mentre
  **Upgrade e sostituzione** (in testa alla pagina) contiene soglie e
  sostituzione. Il totale audio resta sotto la soglia di
  upgrade, quindi l'audio contribuisce al confronto ma non fa scaricare da solo
  un'altra release. Il nome del
  gruppo deve coincidere con il tag release finale, per esempio `TBK`. La lingua
  audio è un requisito di ammissibilità per titolo, non un bonus fisso per
  l'italiano: si può quindi preferire l'inglese. I sottotitoli preferiti danno
  invece un piccolo bonus opzionale, senza rendere la release obbligatoria. Il
  punteggio effettivo è unico per acquisizione, ricerche, upgrade,
  post-processing, archivio e rescore; comprende anche il bonus dimensione, che
  però non basta mai da solo per un upgrade. I punteggi della libreria si
  ricalcolano da soli dopo un cambio dei pesi o delle regole di calcolo.
  - **Rinomina** — abilita rinomina, editor del template con token e anteprima,
  chiavi TMDB/TVDB, lingua e soglie di upgrade. Un file che copre più episodi
  contigui viene nominato con l'intervallo (`S01E01-E02`), così i media server
  elencano entrambi gli episodi; un intervallo non contiguo torna al primo
  episodio. La verifica recupera il
  token sorgente dal titolo originale della release nel database ed elimina i
  blocchi placeholder vuoti (`[]`), così un `[WEB-DL]` perso viene ripristinato
  invece di restare `unknown`.
- **Avanzate** — spazio libero minimo, retention cestino, retention archivio,
  pagine feed, intervallo verifica rinomina, sposta episodi, flag di debug.
- **Acquisizione** — delay prima del download (serie/film, con bypass per
  punteggio alto), intervallo di housekeeping, **Cartelle osservate**: Gextto
  controlla le cartelle indicate e aggiunge i file `.torrent`/`.magnet` copiati,
  aspettando due rilevazioni stabili prima di leggerli e ritentando gli errori con
  backoff fino alla riuscita; li rimuove (o li rinomina `.imported`) dopo
  l'aggiunta; la scansione ricorsiva scende al massimo di 8 livelli; **Sorgenti in
  backoff**, con livello, scadenza, ultimo errore e reset per singola sorgente;
  e il **backfill MediaInfo** automatico (file per volta e intervallo
  configurabili), oltre al pulsante in Manutenzione per una scansione immediata.
  L'**housekeeping non è la pulizia dell'Archivio**: mantiene ordinato il
   database, ma non elimina i file della libreria, i download completati o le
   release elencate nell'Archivio. Il campo *statistiche cicli di ricerca
   conservate* riguarda solo i contatori di ogni ciclo — release analizzate,
   candidate, download avviati, gap riempiti ed errori: con valore `200`, il
   201° ciclo elimina i contatori del ciclo più vecchio, non la release
   scaricata.
  *Visti nel feed* elimina solo le righe storiche dei feed oltre il numero di
   giorni indicato (`0` = nessuna pulizia); lo *Storico download* della sezione
   **Scarico** elimina solo le righe dei torrent già rimossi (`0` = conserva),
   senza toccare l'Archivio. A ogni esecuzione vengono
   inoltre eliminati automaticamente torrent in errore, log dei gap e backup di
   upgrade più vecchi delle rispettive retention configurate (predefinite:
   7 giorni per gli errori e 30 per log gap e backup upgrade); vengono rimossi
   anche i backoff delle sorgenti scaduti. Alla fine vengono compattati con
   `VACUUM` solo i database con almeno il 20% (o 64 MB) di spazio libero da
   recuperare. L'housekeeping gira una volta per intervallo anche se Gextto si
   riavvia: un riavvio non lo fa ripartire.
  La retention dell'Archivio è separata e si trova in **Configurazione →
  Manutenzione automatica → Pulizia archivio release**: viene eseguita dallo
  stesso worker dell'housekeeping, con la stessa cadenza, ma solo se «Pulizia
  automatica archivio» è attiva. Rimuove esclusivamente le righe del catalogo
  release; file scaricati, libreria, storico download e torrent attivi restano
  intatti.
- **Percorsi** — root libreria, cestino, cartelle download/temp/RAM disk, regole
   per tag. Il percorso RAM disk selezionato resta configurato, ma la directory
   creata sotto `/dev/shm` va ricreata dopo un riavvio.
 - **Traduzioni** — pannello avanzato per esportare in YAML le traduzioni
   salvate per italiano o inglese, modificarle e reimportarle. Il formato è una
   mappa `chiave: valore`, ad esempio `"Testa porte": "Test ports"`.
   L'importazione aggiorna o aggiunge le chiavi presenti e non cancella quelle
  assenti dal file. Non cambia la lingua attiva: quella si sceglie dal
  selettore in alto. La UI traduce le stringhe a runtime e, se manca una
  traduzione, mostra la sorgente italiana.

### Motore torrent (gx-torrent, qBittorrent-nox)

Gextto possiede sempre database, coda, punteggi, post-processing, rinomina e
archivio; è sostituibile solo il piano di trasferimento, scelto in
*Configurazione → Motore torrent*. Un torrent è di un solo motore alla volta,
quindi il passaggio è una migrazione controllata, non due client sugli stessi
dati.

Se il motore attivo non riesce a restare in piedi Gextto passa da solo a un
altro motore: prima **qBittorrent-nox** (scaricato al momento e avviato con
profilo privato, oppure quello esterno già configurato), infine uno stato di
**recovery** con i trasferimenti fermi e la UI raggiungibile. Un demone
`gx-torrent` aggiornato che non parte viene prima ripristinato alla versione
precedente.

Il nucleo del motore predefinito, **`gx-core`** (`internal/gxcore`), e il DHT
(`internal/dht`) sono codice originale scritto in questo repository, quindi
coperti dalla stessa licenza EUPL-1.2 di Gextto (vedi la pagina *Licenza*
dell'interfaccia web).

Nell'intestazione dell'interfaccia, accanto alla versione di Gextto, compare il
**motore in uso con la sua versione**: `gx-core` seguito dal numero di versione
(per esempio `1.1.242`) con gx-torrent,
`qBittorrent-nox v…` con qBittorrent (il marchio e la versione del motore
attivo, così si vede a colpo d'occhio cosa sta trasferendo i file).

- **gx-torrent** (predefinito) — demone BitTorrent in Go puro, avviato e
  sorvegliato da Gextto in un processo separato; non ha dipendenze C/C++. Imposta **URL Web API**, **indirizzo di ascolto**
  (predefinito `127.0.0.1:8890`: pagina web e API solo su questo server; usa
  `0.0.0.0:8890` **e un token** per aprirle in LAN), **token** e **proxy**. All'indirizzo configurato apre anche una **pagina
  web operativa** (aggiungi magnet, pausa/riprendi, verifica, riannuncia, coda,
  rimozione, filtro IP, scheda con il log di Gextto). Il demone **resta acceso
  quando Gextto si riavvia** (ad esempio per un aggiornamento): al riavvio
  Gextto lo riaggancia e i trasferimenti non si interrompono; viene riavviato
  solo se il suo programma o le sue opzioni sono cambiati, e mai durante uno
  spostamento di file. Se Gextto resta spento, il demone si ferma da solo dopo
  15 minuti. La **cache disco è adattiva**: il demone la ricalcola
  ogni pochi minuti da memoria disponibile, download/seed attivi e tipo di
  storage (più grande su HDD/NFS, piccola su SSD) e la applica a caldo, come i
  limiti di velocità, senza perdere i peer; il **filtro
  IP** scaricato da un URL si aggiorna una volta al giorno; all'avvio si
  riscarica solo se la copia in cache ha più di 24 ore. Se il demone non
  riesce a restare attivo (6 avvii anomali in 10 minuti) Gextto torna da solo a
  qBittorrent-nox. Supporta il **download sequenziale** e la **prima/ultima parte**
  dei file (si impostano all'aggiunta, o per tutti i nuovi torrent in
  *Configurazione → Code e prestazioni → Modalità di download*; valgono per i torrent
  nuovi). Un magnet con il parametro `so=` (BEP 53) scarica da subito solo i file
  indicati; con `ws=` aggiunge web seed e con `dht=` un nodo DHT di bootstrap.
  Supporta i torrent **solo-v2** (BEP 52),
  sia da `.torrent` sia da
  magnet; non supporta WebTorrent/WebRTC.
- **qBittorrent-nox** — Gextto pilota un qBittorrent-nox esistente tramite la sua
  Web API. Imposta URL, utente/password, categoria, tag e intervallo di polling, e
  le **mappature percorsi** `locale=remoto` quando i due processi vedono percorsi
  diversi; senza mappature Gextto verifica che i percorsi necessari esistano in
  locale. Se un percorso non è traducibile, l'attivazione viene rifiutata, così
  uno spostamento non può finire nella cartella sbagliata. Funziona con
  **qBittorrent-nox 4.6 e successivi, compresa la 5.3**: parla sia le convenzioni
  della Web API 4.x sia quelle 5.x (login, pausa all'aggiunta, limiti di seed) e
  non richiede impostazioni diverse. La **modalità gestita**
  scarica l'ultima release statica di qBittorrent-nox, la installa nella cartella
  dell'applicazione (accanto a `gexttod`), la avvia/ferma col servizio e la
  aggiorna con backup e rollback; un watchdog la riavvia dopo un'uscita inattesa
  e, dopo ripetuti crash, esegue la catena di fallback tra i motori. Se qBittorrent è spento
  all'avvio, Gextto continua a riprovare invece di fallire. I passi completi
  (installazione, Web UI, mappature, modalità gestita, problemi comuni) sono in
  [`docs/qbittorrent-nox.md`](qbittorrent-nox.md).
Se la configurazione salvata seleziona un backend non più supportato, Gextto
torna a gx-torrent e registra un avviso nel log.

Il riquadro di configurazione mostra il motore attivo, il suo stato e un test di
raggiungibilità/percorsi, così puoi validare un backend prima di passare. Il
pannello **Salute → Motore torrent** (icona 🩺 nella barra) riassume lo stato del
motore attivo (coda, velocità, porta/router, DHT/uTP/LSD, cifratura, proxy,
filtro IP, cache, connessioni in entrata, I/O disco con job in coda e totali di
letture/scritture, totali di sessione), tutto nella lingua dell'interfaccia. Quale
che sia il motore attivo, la schermata **Scarico** e tutte le automazioni restano
identiche.

### Prestazioni: RAM e CPU

Gextto non richiede una macchina potente: gira su un mini PC o su un NAS e lascia
CPU e RAM al resto del sistema. A riposo non si nota; durante un trasferimento il
costo è del motore, che vive in un processo separato e restituisce la memoria
quando il download finisce. Non c'è niente da tarare: la cache si dimensiona da
sola in base alla memoria libera e al tipo di disco.

Se la macchina è piccola:

- attiva la **coda dinamica** e *Non contare i torrent fermi negli slot attivi*:
  i torrent a 0 B/s non occupano slot e quelli **stalled** vengono messi in pausa
  e ritentati, invece di continuare a girare a vuoto;
- non impostare un limite di cache: il valore automatico è quasi sempre quello
  giusto. Un valore manuale serve solo se hai una ragione precisa.

Se un torrent non scarica, **Salute** mostra lo stato del motore e la scheda del
torrent spiega il motivo (swarm morto, nessun peer, tracker irraggiungibile).

### Profili di qualità

In **Configurazione → Qualità e upgrade**, sotto *Profili di qualità*, dai un nome alle
combinazioni di qualità, audio minimo, lingua, sottotitoli, esclusioni e
upgrade («Permetti upgrade») che usi. La **Qualità** del profilo si sceglie da
un elenco con le risoluzioni che Gextto riconosce (360p … 2160p, il minimo con
«+», l'intervallo 720p-1080p, oppure Qualsiasi); un valore scritto a mano in
passato resta selezionato finché non lo cambi. **Lingua** e **Sottotitoli**
propongono le scelte comuni (ita, eng, ita,eng, multi, any) ma accettano
anche un valore scritto a mano, per esempio `fre` o `ita,eng,fre`; una lingua
lasciata vuota vale la lingua predefinita di Gextto (salvando viene scritta la
predefinita del momento). Gextto
genera automaticamente un profilo per ogni combinazione di qualità, audio
minimo, lingua, sottotitoli e «Permetti upgrade» già presente nella libreria (le
esclusioni di un singolo titolo e la qualità `any`, uguale a nessun limite, non
creano profili a parte) e li espone a Jellyseerr tramite
`/api/v3/qualityprofile`; il primo profilo è **Default**. La generazione
automatica avviene **finché non salvi la lista**: da quel momento i profili sono
quelli che hai salvato, e le combinazioni nuove non si aggiungono da sole.
Due profili non possono avere lo stesso nome: salvando un nome già usato la
modifica viene rifiutata con un messaggio, invece di far sparire una riga.

**I profili sono modelli, non regole.** Quando usi un profilo (scegliendolo nel
campo **Profilo di qualità** del modulo di aggiunta di una serie o di un film, da
Jellyseerr o da *Modifica selezionati* nelle tabelle Serie TV e Film), Gextto
**copia** i suoi valori nel titolo (le esclusioni scritte nel modulo di aggiunta
si sommano a quelle del profilo); da quel
momento a decidere sono i campi del titolo, come sempre. Se poi cambi il
profilo, i titoli già creati non cambiano: per aggiornarne molti insieme usa
*Modifica selezionati* nelle tabelle Serie TV e Film. A Gextto non serve una gestione "alla
Sonarr" in cui il profilo governa i titoli: il campo Qualità accetta già un
minimo (`1080p`), un massimo (`<1080p`) o un intervallo (`720p-1080p`), e
lingua, sottotitoli, esclusioni e upgrade si impostano per titolo. Ogni
profilo mantiene il suo numero anche se lo rinomini o ne togli e riordini altri: Jellyseerr
ricorda il numero, non il nome. Se togli tutti i profili, alla lettura
successiva Gextto li rigenera dalla libreria.

### Configurare un indexer Torznab

Per Jackett:

1. crea o verifica almeno un indexer dentro Jackett;
2. copia la API key dalla pagina Jackett;
3. in Gextto premi **+ Jackett** (o aggiungi una riga alla lista *Indexer
   Torznab* e scegli **Jackett** come tipo);
4. inserisci URL base, ad esempio `http://jackett:9117`, e API key;
5. salva e premi **Verifica**;
6. controlla *Salute → Stato API*.

Per indexarr:

1. copia la API key da indexarr;
2. in Gextto aggiungi una riga alla lista *Indexer Torznab* e scegli **indexarr**
   come tipo;
3. inserisci l'URL base, ad esempio `http://indexarr:8484`, e la API key;
4. salva e premi **Verifica**;
5. controlla *Salute → Stato API*.

Con Jackett, Prowlarr e indexarr Gextto legge l'elenco degli indexer dal manager
e ne interroga **uno alla volta** (l'endpoint Torznab del singolo indexer per
Jackett e indexarr, la ricerca per `indexerIds` per Prowlarr): ogni tracker ha
così il proprio timeout e la propria salute, senza che il più lento blocchi gli
altri. Se l'elenco non è leggibile torna alla ricerca aggregata: per Jackett è
`/api/v2.0/indexers/all/results/torznab/api`, per indexarr è il suo `/api`. Non
aggiungere il percorso Torznab all'URL base: basta l'indirizzo del manager
(Prowlarr normalmente sulla porta `9696`, indexarr sulla `8484`). Un percorso
Torznab personalizzato già completo va lasciato come endpoint esplicito.

Un indexer può essere raggiungibile ma non sano: per esempio Jackett può
rispondere HTTP 200 con un errore Torznab dovuto a API key errata o nessun
indexer abilitato. In questo caso *Salute* mostra **Errore API** e il dettaglio;
il log del ciclo mostrerà anche il backoff della sorgente quando applicabile.

### Punteggio, filtri e “Perché non questa?”

I controlli principali sono indipendenti:

1. titolo monitorato;
2. blocklist e duplicati;
3. filtri globali e filtro della sorgente;
4. sanità della release, inclusi sottotitoli hardcoded e dimensione minima;
5. qualità, lingua, sottotitoli ed esclusioni del titolo;
6. confronto con file già archiviati e download attivi.

Lo score aiuta a scegliere tra candidati ammessi; non rende valida una release
che fallisce un filtro bloccante. Nel pannello **Perché non questa?**:

- **Superata** significa che quel controllo è passato;
- **Bloccante** indica un motivo sufficiente a rifiutare il candidato;
- **Informativa** indica un controllo che dipende dal contesto dell'intero ciclo;
- il confronto archivio mostra se si tratta di primo download o di possibile
  upgrade.

La spiegazione è una diagnosi, non una prenotazione: aprirla non crea righe
torrent, non accoda magnet e non cambia la configurazione.

### Ritardi e backoff delle sorgenti

Il delay di acquisizione può trattenere una release prima dell'avvio per dare
tempo a un risultato migliore. Un punteggio alto può bypassare il delay secondo
la configurazione. Il backoff delle sorgenti è diverso: viene attivato da errori
ripetuti e impedisce temporaneamente nuove richieste alla sorgente problematica.

Il ritardo si imposta in *Configurazione → Generale → Ritardo prima di
scaricare*. Lo stato dei backoff (livello, scadenza e ultimo errore) è in
*Salute → Stato provider*, che elenca **sempre tutti gli indexer configurati e
attivi** (quelli senza problemi come «Nessun problema rilevato»), con la colonna
*Dettaglio tecnico* che riporta l'errore grezzo. Usa **Azzera** per singola sorgente dopo aver corretto la causa; non usarlo per
mascherare un'API key sbagliata, altrimenti il backoff ricomincerà.

### Configurare il NAS senza sorprese

Per una libreria su NAS:

1. monta il filesystem prima dell'avvio di Gextto;
2. assegna permessi di lettura/scrittura all'utente del servizio;
3. imposta spazio minimo prudente;
4. esegui una scansione archivio dopo aver copiato file esistenti;
5. controlla il percorso effettivo nella cronologia dopo il primo completamento.

Se il NAS non è montato, non sostituire temporaneamente il percorso con la root
locale senza aver capito l'effetto: potresti archiviare file nel posto sbagliato.
Meglio correggere il mount e lasciare il download in attesa.

Un NAS smontato non cancella lo storico: quando un file archiviato non si trova,
Gextto segna l'episodio come da recuperare solo se la cartella del file esiste
ancora o se il percorso montato non è vuoto. Un mount point vuoto viene trattato
come volume assente, e i dati dell'episodio restano intatti.

I controlli di sanità delle release sono **automatici** e non configurabili:
sottotitoli hardcoded (`HC`) e dimensioni assurde (una soglia per risoluzione
derivata da un archivio reale) vengono rifiutati. Gli scarti sono eventi
ordinari e vengono registrati a `DEBUG` con titolo, risoluzione, dimensione e
motivo, così il log `INFO` di produzione resta pulito; abilita il debug per
vederli. Anche la protezione dagli
episodi vecchi è fissa: Gextto non riscarica un episodio precedente fuori dai
buchi riconosciuti quando possiede già episodi successivi (gap-fill e azioni
manuali restano sempre permessi). Per congelare un titolo imposta **Permetti
upgrade** su No con *Modifica selezionati*.

I dati reali dei file (`ffprobe`: HDR, codec, audio, lingue) sono salvati per
episodio/film e **usati nei confronti di upgrade**: il file archiviato viene
letto com'è davvero, non solo dal nome. Sono additivi (non declassano mai un
file). I file nuovi vengono analizzati al completamento; gli altri vengono
coperti dal **backfill MediaInfo** incrementale schedulato (o dal pulsante in
Manutenzione per una scansione immediata). Se `ffprobe` non è installato il
backfill si mette in pausa da solo.

## 8. Integrazioni

L'elenco completo dei campi e delle azioni è nell'[Appendice B](#appendice-b-riferimento--integrazioni).

- **Simkl** — credenziali, flusso PIN, import watchlist, calendario e segna come
  visto.
- **Jellyfin / Plex** — URL + token del server e pulsante di aggiornamento
  libreria.
- **Metriche ed eventi** — gx-torrent espone `GET /api/v1/metrics` per
  Prometheus e `GET /api/v1/events` per Home Assistant o uno script. Con il
  token serve l'header `X-Gx-Token`. Esempi in `docs/API.md`.
- **Hook eventi** — esegue un programma esterno su eventi Gextto
  (`download_started`, `torrent_completed`, `season_pack_completed`,
  `torrent_error`, …). I campi accettano segnaposto come `{title}`, `{hash}`,
  `{path}`, `{series}`, `{episode}`; gli stessi valori sono esposti come variabili
  d'ambiente `GEXTTO_*`. I programmi sono eseguiti senza shell e con timeout di
  default pari a 60 secondi (massimo 24 ore; anche `0` usa il default). Ogni
  segnaposto diventa un solo argomento anche se il valore contiene spazi: un
  titolo come `The Office` arriva intero, non spezzato in due. Allo scadere del
  timeout viene terminato anche ogni processo figlio lanciato dal programma; al
  massimo 4 hook girano contemporaneamente, gli altri attendono il proprio turno.

### Jellyseerr / Sonarr / Radarr

Gextto espone un sottoinsieme dell'API v3 di Sonarr/Radarr per Jellyseerr:
`/api/v3/system/status`, `qualityprofile`, `languageprofile`, `rootfolder`,
`tag`, `series/lookup` con `POST series`, `movie/lookup` con `POST movie` e
`queue`. In Jellyseerr usa come URL il server Gextto e come chiave la **API key**
di Gextto (Configurazione → Accesso). Non è una compatibilità completa: le
richieste che Gextto non gestisce rispondono 404, e quality profile/custom
formats non riproducono la semantica degli *arr*.

Cosa succede a una richiesta: per una serie nuova Gextto segue solo le stagioni
richieste in Jellyseerr (nessuna stagione indicata = dalla 1 in poi); se la
serie è già in libreria, le stagioni richieste vengono aggiunte a quelle che
segue già, senza toglierne. Un film già in libreria non viene duplicato: se era
disattivato viene riattivato. La «ricerca immediata» di Jellyseerr
(`/api/v3/command`) non è gestita: il titolo aggiunto viene cercato dai normali
cicli di acquisizione di Gextto.

### Collegare un servizio esterno

Le integrazioni non sono necessarie per scaricare e archiviare i media. Attivale
una alla volta e usa sempre il pulsante di test quando disponibile:

- **Simkl**: completa il flusso PIN, verifica che l'account corretto sia
  visualizzato e solo dopo usa l'import watchlist;
- **Jellyfin/Plex**: inserisci URL raggiungibile dal demone e token con i permessi
  minimi necessari, poi prova l'aggiornamento libreria;
- **Hook**: configura prima un programma innocuo che scriva un log, verifica i
  placeholder e solo dopo collegalo a script di automazione o notifiche.

Gli hook vengono eseguiti senza shell: pipe, redirezioni e operatori come `&&`
non vengono interpretati. Se servono, inserisci un vero script eseguibile e
passagli i valori tramite i placeholder o le variabili `GEXTTO_*`. Non inserire
token o password negli argomenti se il comando finisce nei log del sistema.

## 9. Manutenzione

L'elenco completo delle azioni e dei parametri è nell'[Appendice C](#appendice-c-riferimento--manutenzione).

- **Aggiornamenti** — versione installata e canale, ultima versione pubblicata e
  l'elenco delle novità (i commit successivi a quello installato, con il link a
  GitHub). Il controllo avviene da solo ogni *Intervallo controllo
  aggiornamenti* ore (6 di default; *Impostazioni → Sistema → Aggiornamenti*).
  **Verifica aggiornamenti** interroga subito GitHub; quando c'è una build nuova
  il pulsante diventa **Aggiorna** (accanto resta **Ricontrolla**), che fa un
  backup dei database e chiede l'aggiornamento al servizio di sistema
  `gextto-update`, installato dall'installer: Gextto attende la fine delle copie
  verso la libreria, si riavvia con la nuova versione e il pannello lo segnala
  da solo. L'aggiornamento è riuscito solo quando l'interfaccia della nuova
  versione risponde: se il servizio cade, anche dopo qualche secondo, torna
  subito la versione precedente; se è vivo ma lento a rispondere (per esempio
  una migrazione lunga del database) viene solo segnalato. Il log dell'ultimo
  aggiornamento è in fondo al pannello. Al primo accesso dopo il
  riavvio un popup **Gextto aggiornato** mostra la versione di partenza, quella
  nuova e i commit che ha portato; si chiude con **OK** (o `Esc`) e non
  ricompare. Quando c'è una versione nuova compare anche il pulsante
  **Aggiornamento disponibile** sopra **Dona**.
  Su un'installazione da sorgenti il pannello indica di usare `git pull` e
  `make build`.
- **Versione esatta** — in fondo alla pagina una riga riporta la versione, la
  **build** completa e il **commit** da cui è compilata (il commit breve; il
  suggerimento del mouse mostra quello intero). Serve a riconoscere senza
  ambiguità la build installata: il terzo numero della versione è sempre la
  build (per esempio `1.1.1961`), quindi la build
  piena e il commit sono l'identità esatta del programma in esecuzione.

- Backup immediato, pulisci cestino, ricalcola scoring, scansiona archivi,
  **Aggiorna MediaInfo** (analizza con `ffprobe` i file archiviati senza dati e
  li salva) e riavvia il servizio.
- Le operazioni lunghe (**Scansiona archivi**, **Aggiorna MediaInfo**, **Rinomina
  tutto**) girano in background: la pagina non resta bloccata, puoi continuare a
  usare Gextto, annullarle dal messaggio e vederne l'esito quando finiscono.
- **Rinomina contenuto cartella** — inserisci una cartella e premi **Sfoglia** per
  sceglierla dal server. Gextto analizza ricorsivamente i file video, riconosce
  serie e film dal nome, confronta i titoli con TMDB/TVDB e mostra una proposta
  di rinomina con eventuali alternative.
- **Pulizia cestino** — sia **Pulisci trash** in *Manutenzione* sia **Svuota
  cestino** nel pannello **Trash** sono azioni manuali forzate: rimuovono subito
  il contenuto. `trash_retention_days` (0 = elimina tutto) si applica solo alle
  pulizie non forzate richieste via API (`/api/maintenance/clean-trash` con
  `force=false`). Dopo la pulizia un avviso dice quanti elementi sono stati
  rimossi, quanto spazio si è liberato e i nomi (fino a 8); se qualcosa non si è
  potuto rimuovere l'avviso lo dice invece di dichiarare un successo.
- **Duplicati video** — *Anteprima duplicati* e *Pulisci duplicati* trovano i
  file video chiaramente inferiori (risoluzione strettamente più bassa) rimasti
  accanto alla versione migliore nella stessa cartella — es. il vecchio 480p
  accanto al nuovo 1080p — e li spostano nel cestino. Il controllo usa il nome
  del file e i dati tecnici dichiarati nel nome: non confronta il contenuto e
  non calcola hash. A **pari risoluzione**
  viene tenuta la versione nella lingua preferita (*Configurazione → Libreria e rinomina →
  Lingua predefinita*) e il duplicato che dichiara esplicitamente un'altra lingua
  va nel cestino; i file senza tag lingua restano intatti. I file dei torrent
  ancora in sessione sono protetti e le sottocartelle svuotate vengono rimosse.
  La verifica di rinomina esegue la stessa pulizia automaticamente e la pulizia
  degli upgrade ora rispetta anche i motivi "forti" (risoluzione/sorgente/HDR/
  repack). La verifica mantiene la grafia dei tag già presenti nel nome (per
  esempio `EAC3` o `ddp`, `DV` o `DV HDR10`): non rinomina un file solo per
  cambiare la forma dello stesso codec, e un file già corretto non viene
  rinominato una seconda volta nello stesso controllo.
- **Importa file** — tab per importare un video già sul disco che Gextto non
  ha scaricato: vedi *Importare un file già presente*.
- **Manutenzione SQL dell'archivio** — tab avanzato e distruttivo per
  l'Archivio release. In cima un selettore sceglie la **lingua per le
  operazioni**; il tab elenca le operazioni incluse in `scripts/sql`: release
  più vecchie di 6 mesi o di 1 anno, release nella lingua scelta o senza quella
  lingua, titoli vuoti, duplicati per titolo e fonte, più alcune diagnostiche di
  sola lettura (release recenti, distribuzione per età, release per fonte, stato
  dell'archivio, titoli duplicati).
  L'archivio tiene una riga per ogni listing visto dagli scraper: per una
  release con magnet l'identità è l'infohash, per una con solo un link
  `.torrent` è l'identificatore stabile dell'indexer. Jackett rigenera il link
  di download a ogni richiesta; senza quell'identificatore la stessa release
  comparirebbe come nuova a ogni passata. **Duplicati per titolo e fonte**
  ripulisce le righe già accumulate, confrontando fonte e titolo e tenendo la
  più recente; non tocca i magnet.
  Un'operazione che elimina mostra prima l'anteprima con il numero di righe
  corrispondenti e le prime 200; solo dopo compare **Elimina definitivamente**,
  con conferma. Mentre la query è in corso compare un indicatore; al termine il
  pannello scorre al risultato. Non è una console SQL libera: esegue solo i file inclusi e non
  tocca libreria, torrent attivi o impostazioni. La lingua è dedotta dal titolo,
  quindi anche una release senza marcatore rientra nell'elenco “non nella
  lingua selezionata”. Fai un backup prima di una cancellazione grande; dopo,
  `VACUUM` compatta il file dell'archivio.
- **Ripristina sorgente** — *Ripristina sorgente nei nomi* rimette il token
  `[Source]` (WEB-DL, HDTV, BluRay…) nei nomi archiviati che l'hanno perso,
  recuperandolo dal titolo originale della release nel database. Non inventa la
  sorgente: se è sconosciuta il file resta invariato. Prima l'anteprima, poi
  l'esecuzione; nessun riscaricamento.
- **Log gx-torrent** — pannello con le ultime righe del log del motore torrent
  (`gx-torrent/gx-torrent.log`), separato dal log di Gextto e conservato tra i
  riavvii del servizio. Puoi filtrarne il testo, mostrare solo avvisi ed errori e
  scegliere tra il file attivo e le rotazioni (`.1`, `.2`, `.3`). Utile quando un
  download o un trasferimento non si comporta come previsto.
 - **Database**: prune per cicli/età errori, **retention dei "visti dai feed"**
   (giorni; 0 conserva tutto), prune per keyword con elenco delle righe
    corrispondenti e **VACUUM / ANALYZE** su tutti i database.
    La pulizia dei "visti" rimuove solo righe storiche dei feed, non file o
    download; applica anche la pulizia standard degli ultimi 50 cicli e degli
    errori torrent oltre la retention configurata (7 giorni per impostazione
    predefinita).
- **Backup**: retention, schedulazione (manuale, ogni N ore o a un orario fisso
   giornaliero HH:MM), FTP (host/utente/percorso) con **Test FTP** (verifica
   connessione, percorso e upload di prova), copia su cartella cloud/sync, invio
   Telegram, elenco backup. Lo snapshot contiene database e configurazione, non
  i media né lo stato della sessione torrent.

### Rinomina manuale di una cartella

La scansione è solo un'anteprima e non modifica i file. Per ogni riga puoi:

- scegliere un risultato TMDB/TVDB alternativo;
- accettare o rifiutare singolarmente la proposta;
- usare **Accetta tutte le proposte** e poi **Applica selezionate**.

Vengono rinominati solo i file selezionati, realmente presenti e con una
destinazione valida nella stessa sottocartella. I conflitti, i titoli non
riconosciuti e le destinazioni già esistenti restano invariati. I sidecar
associati (sottotitoli, immagini, NFO e simili) seguono il nuovo nome quando
possibile.

### Backup: cosa protegge e cosa no

Un backup di Gextto protegge database e configurazione. Non contiene i video,
gli archivi multimediali né lo stato completo della sessione torrent. Prima di
un ripristino:

1. ferma o metti in pausa i cicli automatici;
2. conserva una copia del database attuale;
3. verifica data e dimensione del backup;
4. ripristina solo su un'installazione compatibile;
5. controlla percorsi e permessi prima di riattivare i download.

Il ripristino di un database non sposta automaticamente i file multimediali. Se
la libreria è stata spostata, correggi i percorsi o esegui una scansione archivio
prima di avviare upgrade e ricerche mancanti.

**Cosa contiene un backup.** Uno ZIP chiamato `gextto-backup-<data>_<ora>.zip` in
`<dati>/backups` (e su FTP, se configurato) con:

- i quattro database come snapshot coerenti: `gextto_series.db` (serie,
  episodi, film, download, storico, salute delle sorgenti), `gextto_config.db`
  (tutte le impostazioni, gli indexer, i temi, le traduzioni),
  `gextto_archive.db` (l'archivio dei feed), `gextto_comics.db`;
- gli altri file di configurazione della cartella dati: il marker della
  procedura guidata, i domini Cloudflare, lo stato del motore gx-torrent
  (`gx-torrent/state.json`, `lifetime.json`).
- i file e le cartelle fuori dalla cartella dati elencati in *File e cartelle
  da includere* (uno per riga), sotto `extra/` con il loro percorso originale;
  i percorsi che non esistono più vengono saltati.

Lascia fuori ciò che si riscarica o si ricostruisce: la sessione torrent e i suoi
dati di ripresa (`gextto_torrents_state`, `gx-torrent/session.db`), il filtro IP,
le cache degli scraper, i fumetti, i log e il cestino predefinito
(`<dati>/trash`). Fuori dalla cartella dati non contiene
`/etc/gextto/gextto.env` (le tue variabili d'ambiente, se ne hai aggiunte):
conservane una copia tu.

**Ripristino sulla stessa macchina o su una nuova.**

1. Installa Gextto con le stesse opzioni di prima (`--user`, `--data-dir`,
   `--port`, …), oppure ferma il servizio esistente:
   `sudo systemctl stop gextto`.
2. Metti da parte la cartella dati attuale: `sudo mv /var/lib/gextto
   /var/lib/gextto.old` (usa la tua cartella dati se è altrove).
3. Estrai il backup in una cartella dati nuova e assegnala all'utente del
   servizio:
   ```bash
   sudo install -d /var/lib/gextto
   sudo unzip /percorso/gextto-backup-<data>.zip -d /var/lib/gextto
   sudo chown -R <utente del servizio>:<suo gruppo> /var/lib/gextto
   ```
4. Rimetti `/etc/gextto/gextto.env` se l'avevi, poi
   `sudo systemctl start gextto`.
5. Apri **Salute**: i database vengono controllati all'avvio. Con gx-torrent i
   download che erano in corso vengono riaggiunti dal loro magnet salvato nel
   database, e i dati già nelle cartelle di download si ricontrollano e si
   riusano.

### Pulizia e operazioni irreversibili

Usa prima le anteprime quando disponibili. In particolare:

- *Anteprima duplicati* mostra cosa finirebbe nel cestino;
- *Anteprima rinomina* mostra vecchio e nuovo nome;
- *Pulizia database* riguarda righe storiche, non la libreria;
- la pulizia del cestino può eliminare definitivamente i file;
- l'eliminazione di un torrent può chiedere di cancellare anche i dati.

Non confondere **cestino**, **Archivio**, **Storico download** e **sessione
torrent**: sono insiemi diversi e una pulizia in uno non implica automaticamente
la pulizia degli altri.

## 10. Salute, Log, Grafici

- **Salute** — stato processo/sistema, runtime (3 colonne), stato del servizio
  Gextto, **raggiungibilità degli indexer**, permessi percorsi, stato sorgenti,
  ultimi errori, dischi. Per Jackett il controllo usa l'endpoint Torznab `caps`;
  un problema di API key o di configurazione degli indexer viene quindi distinto
  dalla semplice raggiungibilità della macchina.
- **Ricerche** — nel riquadro omonimo della pagina Salute c'è lo storico degli
  ultimi cicli: quando, durata, release esaminate, download avviati, errori e
  quante sorgenti non hanno risposto (il motivo compare passando il mouse sul
  numero). Serve a riconoscere una sorgente che fallisce ripetutamente o un ciclo
  diventato più lento. API: `GET /api/last_cycles`. Su questi stessi dati Gextto
  avvisa da sé (riga WARN e notifica) se una sorgente ha fallito in tre delle
  ultime ricerche, se l'ultima ricerca ha impiegato più di tre ore o se non c'è
  una ricerca senza errori da tre intervalli (minimo due ore); avvisa anche
  quando tutto torna regolare. In più, se i link `.torrent` di una stessa
  sorgente falliscono su due release diverse nello stesso ciclo — di solito un
  login scaduto, come il 502 «login failed» di mircrew — Gextto scrive una riga
  WARN e manda una notifica (al massimo ogni 12 ore per sorgente) con il motivo
  restituito dall'indexer e cosa fare: rifare il login e, finché non funziona,
  disabilitare quella sorgente nelle ricerche. Un link che si risolve riarma
  l'avviso.
- **Coda titoli** — riquadro con i titoli trattenuti dal *delay profile* (serie e
  film) prima del download: titolo, da quanto sono in attesa (`first_seen_at`) e
  quanto manca al `due_at`. Serve a vedere quando la coda cresce o un titolo
  resta fermo.
- **Import falliti** — riquadro con il numero di import falliti per giorno (ultimi
  7), ricavato dagli eventi di acquisizione; utile a riconoscere un disco o un NAS
  che rifiuta gli spostamenti.
- **Log web** — stream SSE live con filtro testuale, numero righe e segui/pausa.
  Le righe sono in inglese e in formato esplicito: `data ora  LIVELLO messaggio
  · campo: valore`, con parole chiave evidenziate (NAS, download, sorgenti,
  filtri, errori). Ogni ciclo stampa un **SOURCE REPORT** con l'esito di ogni
  sorgente, le decisioni dei filtri e gli eventi di download (avvio, metadati,
  completamento, eventuale spostamento su NAS e importazione in archivio). I
  messaggi torrent includono sempre nome o titolo leggibile; gli hash torrent
  sono nascosti nel log utente. Le righe periodiche non si ripetono a ogni
  controllo: la liberazione della memoria di gx-torrent compare quando il riposo
  inizia e poi ogni sei ore, e un avviso su una sorgente guasta torna al massimo
  ogni sei ore.
- **ID acquisizione e storia dei download** — le righe che riguardano un
  download finiscono con `acq: 7f3a2c`: un ID corto che resta lo stesso
  dall'avvio del download all'arrivo in libreria, per i torrent come per i
  download eD2k (anche quelli avviati a mano da «Scarica» o da un link
  incollato). Scrivendolo nel filtro del log
  si vede solo quel download. Le stesse righe (INFO, WARN ed ERROR, anche con il
  log impostato su `warn`) vengono salvate come **storia**: scheda *Storia* nel
  dettaglio del torrent, pulsante 📜 su ogni puntata e pannello *Storia dei
  download* nella pagina della serie. Le righe ripetute uguali diventano una sola
  con il contatore (×N); la storia si conserva 180 giorni e resta leggibile anche
  dopo la rotazione del log e la rimozione del torrent. API: `GET
  /api/acquisitions?hash=…`, `?acq=…` oppure `?series=…&season=…&episode=…`.
- **Grafici** — sparkline CPU/RAM/download/upload/disco/RAM disk e consumo
  giornaliero.
- **Attività** — eventi torrent recenti e download.

### TUI: uso rapido

Nella TUI `1`-`9` o `Tab` cambiano scheda, `r` aggiorna e `?` apre l'aiuto.
Con eD2k attivo, nella scheda **Stato** compare la riga **aMule** (versione,
server e ID alto/basso, Kad, velocità di upload e coda) e nella scheda
**Torrent** i download di aMule sono elencati con gli altri: `Invio` apre il
dettaglio (stato, fonti, priorità, ultimo dato, link `ed2k://`), `y` copia il
link, `p` mette in pausa o riprende, `d` lo toglie da aMule.
Nella scheda **Log**, `/` filtra le righe e `f` attiva o ferma il follow; quando
il follow è fermo la posizione resta congelata anche se arrivano nuovi log.
Archivio, Mancanti e Blocklist dispongono di selezione e scrolling con le frecce
e i tasti pagina. Per l'elenco completo dei comandi vedi [`docs/tui.md`](tui.md).

### Salute: interpretare lo stato delle sorgenti

Per ogni indexer la tabella distingue:

- **OK**: risposta HTTP valida e nessun errore Torznab applicativo;
- **Errore API**: il servizio risponde, ma API key o configurazione indexer non
  sono accettate;
- **Non raggiungibile**: non è arrivata una risposta entro il timeout.

Questa distinzione è importante: riavviare Gextto non risolve un errore API key,
mentre un problema di rete può richiedere di controllare DNS, container, porta o
firewall.

Con eD2k attivo la pagina **Salute** ha anche una scheda **eD2k e Kad (aMule)**
con lo stato della connessione, le velocità, i byte scambiati nella sessione, le
cartelle di aMule mancanti e le ultime righe di problema dal log di aMule
(disconnessioni, server non validi, errori), così non devi aprire il log per
accorgerti che qualcosa non va.

## 11. Notifiche

Configura Telegram, e-mail (SMTP) o un webhook — anche verso Discord, Slack,
ntfy, Gotify o Pushover — e invia un test. Le notifiche di completamento
includono dimensione, tempo di download e velocità media. Per un season pack
l'avviso distingue gli episodi **nuovi** da quelli che hanno **sostituito una
versione inferiore** già in libreria: questi ultimi non vengono contati fra i
nuovi, così scaricare un episodio singolo e poi il suo season pack non sembra
un doppione.

### Notifiche di stato (Gextto e gx-torrent)

Oltre agli eventi di download, Gextto notifica i problemi del servizio:

- **riavvio di Gextto** — a ogni avvio dopo il primo, con il numero di avvio e
  da quanto era fermo; se l'arresto precedente non è stato registrato (crash,
  `kill -9`) lo segnala come arresto anomalo;
- **arresto per errore** — quando il servizio termina per un errore fatale;
- **gx-torrent** — uscita inattesa del demone gestito, riavvio dopo l'uscita,
  ripristino del binario precedente dopo un aggiornamento che non parte,
  demone attivo ma che non risponde più, e passaggio automatico a un altro
  motore (prima qBittorrent-nox, scaricato al momento, infine recovery con i
  trasferimenti fermi);
- **problemi di salute** — quando il controllo di salute rileva un problema e
  quando torna operativo.
- **login a MirCrew** — quando il login al forum del servizio
  `mircrew-indexer` resta rotto per più di 15 minuti, e di nuovo quando torna a
  funzionare. Il servizio continua a rispondere alle ricerche dal suo indice
  locale, quindi senza questo controllo un login rotto non produrrebbe altri
  avvisi.

Un processo morto non può notificare la propria caduta: il crash di Gextto si
riceve quindi al riavvio successivo. Se Gextto non riparte (per esempio il
servizio è disattivato) nessuna notifica parte: per quel caso usa un watchdog di
systemd (`OnFailure=`).

### Webhook: quale URL e quale formato

Il canale **Webhook** manda gli eventi a un servizio esterno senza bisogno di
un'app dedicata: scegli il **Formato** del servizio, incolla l'**URL** e, se
serve, metti la credenziale nel campo **Webhook token** (e in **Webhook user**
per Pushover). L'URL resta pulito, senza token dentro.

| Servizio | Webhook URL | Formato | Credenziale |
|---|---|---|---|
| Endpoint tuo | il tuo URL | **Gextto (JSON firmato)** | «Webhook secret» firma il corpo (HMAC SHA-256, header `x-gextto-signature`) |
| Discord | URL del webhook del canale | **Discord** | nessuna |
| Slack | URL del webhook in ingresso | **Slack** | nessuna |
| ntfy | `https://ntfy.sh/<argomento>` (o il tuo server) | **ntfy** | «Webhook token» = token Bearer, se l'argomento è protetto |
| Gotify | `https://gotify.esempio/message` | **Gotify** | «Webhook token» = application token |
| Pushover | `https://api.pushover.net/1/messages.json` | **Pushover** | «Webhook token» = application token, «Webhook user» = user key |

Note:

- con Formato **Gextto** il messaggio è un JSON `{"event":…,"data":…}` e, se
  imposti il segreto, viene firmato; con gli altri formati Gextto invia il
  **testo leggibile** già composto e la firma HMAC non viene applicata (è
  specifica del formato Gextto);
- la credenziale va nel campo **Webhook token**, non nell'URL; solo Discord e
  Slack non ne hanno bisogno;
- usa il pulsante di **test** nella scheda Notifiche: se l'URL o il token sono
  sbagliati lo dice subito, invece di fallire in silenzio alla prima consegna.

### Procedura di configurazione

1. salva le credenziali nella scheda **Notifiche**;
2. abilita solo gli eventi che vuoi ricevere;
3. invia il test dalla UI;
4. controlla sia la risposta del provider sia il log Gextto;
5. esegui un test di completamento solo con un download non importante.

Per un webhook verifica il segreto HMAC sul ricevente e non confondere il test
HTTP con la consegna dell'evento reale. Per SMTP controlla host, porta, TLS,
utente e mittente: un server raggiungibile può comunque rifiutare il mittente o
richiedere autenticazione diversa.

Le notifiche degli eventi partono in background: un provider lento o
irraggiungibile non rallenta download, seeding e archiviazione, e un eventuale
errore di consegna compare nel log. Il test dalla UI invece attende la risposta
e mostra subito l'errore. Una consegna SMTP che non risponde viene interrotta
dopo 30 secondi.

## 12. Riferimento rapido

### Quando usare quale azione

| Obiettivo | Azione |
|---|---|
| Capire cosa esiste online | Ricerca manuale |
| Riempire episodi mancanti | Cerca mancanti / ciclo Serie |
| Scegliere una release specifica | **Perché non questa?** → Accoda |
| Far riconoscere file già presenti | Scansiona archivio |
| Riscaricare una puntata già archiviata | ↻ Riscarica (sposta il file nel cestino e la rimette in coda) |
| Cambiare il nome senza riscaricare | Anteprima rinomina |
| Correggere un servizio esterno | Salute → sorgente → Verifica |
| Eliminare file inferiori | Anteprima duplicati → Pulizia |
| Salvare configurazione e DB | Backup |

### Glossario

- **Release**: risultato trovato da feed, indexer o motore web.
- **Ciclo**: una passata automatica di ricerca e selezione.
- **Gap**: episodio mancante riconosciuto nell'archivio.
- **Gap forzato**: richiesta esplicita di riscaricare una puntata (azione ↻
  Riscarica). Scavalca la copertura di un pack di stagione e la regola
  «episodio successivo già archiviato», così la puntata può essere ripresa
  anche quando normalmente non sarebbe considerata mancante.
- **Placeholder**: riga temporanea che rappresenta un download non ancora
  completato.
- **Upgrade**: sostituzione di un file archiviato con una release migliore.
- **Backoff**: pausa progressiva delle richieste a una sorgente che fallisce.
- **Seed**: condivisione del torrent dopo il completamento.
- **NAS**: destinazione di rete usata per la libreria o per i percorsi configurati.

## 13. Risoluzione problemi

- **Una sorgente non risponde** — controlla *Integrazioni → Verifica sorgenti*
  e il pannello stato sorgenti; per Jackett verifica URL base, API key e che
  almeno un indexer sia abilitato in Jackett; per indexarr controlla la API key
  e che l'URL base sia quello di indexarr (di solito porta `8484`). Gli errori
  Torznab vengono mostrati anche quando il manager risponde HTTP 200. I siti
  protetti da Cloudflare richiedono un FlareSolverr funzionante.
- **Jackett è raggiungibile ma mostra errore API** — copia nuovamente la API key,
  controlla che almeno un indexer sia abilitato in Jackett e verifica che l'URL
  inserito in Gextto sia quello base corretto. Non aggiungere due volte il
  percorso `/api/v2.0/indexers/all/results/torznab/api`. Lo stesso vale per
  indexarr: usa l'URL base (`http://host:8484`), non un percorso già completo,
  e scegli **indexarr** come tipo.
- **Non scarica nulla** — verifica la *modalità attiva*, che serie/film siano
  abilitati, e controlla filtri qualità/lingua e il limite di spazio libero.
- **Una puntata archiviata non si riscarica** — usa **↻ Riscarica** nel
  dettaglio serie: sposta il file nel cestino, cancella la riga dell'episodio e
  registra un *gap forzato*. È necessario quando la stagione è coperta da un
  pack o quando un episodio successivo è già archiviato; senza il gap forzato
  la puntata non risulta mancante. Uno speciale `SxxE00`, invece, è un singolo
  file e **non** copre la stagione.
- **Un torrent è stalled** — controlla i valori in *Configurazione →
  Seed e completamento → Torrent bloccati*. Il torrent è intenzionalmente pausato e fuori dalla coda attiva;
  attendi il retry oppure usa **Riprendi/Riavvia** manualmente.
- **Una cartella osservata non importa il file** — lascia il file con estensione
  `.torrent` o `.magnet`; Gextto aspetta che dimensione e timestamp restino
  stabili, poi ritenta automaticamente gli errori. Controlla il log del watcher.
- **Un file non viene rinominato** — conviene avere `mediainfo` installato (tag
  tecnici); controlla le impostazioni *Rinomina* e la chiave TMDB (o TVDB e il
  suo PIN). Senza metadati il titolo dell'episodio diventa «Episodio N».
- **La release è visibile ma non viene scelta** — apri **Perché non questa?**:
  controlla prima titolo monitorato, filtri globali, sanità, qualità/lingua e
  confronto archivio. Se tutti questi passano, guarda il controllo informativo
  sulla selezione del ciclo: altri candidati, delay, spazio libero e gap filling
  possono ancora cambiare il risultato.
- **Un file esiste sul NAS ma Gextto lo considera mancante** — controlla nome
  episodio, percorso associato alla serie e permessi; poi usa *Scansiona archivio*.
  La scansione riconosce i nomi video con stagione/episodio, non file arbitrari
  che non permettono di identificare il contenuto.
- **Il download è completo ma non compare nella libreria** — guarda il log per
  spostamento, permessi e spazio; non cancellare la sorgente finché il percorso
  archiviato non è visibile nella cronologia. Gli errori temporanei (NAS non
  raggiungibile, disco momentaneamente pieno, timeout, database occupato) vengono
  ritentati da soli dopo 1, 2, 4, 8, 16 e 32 minuti: solo dopo l'ultimo
  tentativo il torrent passa in errore. Uno spostamento verso l'archivio
  interrotto da un riavvio di Gextto riprende da solo circa un minuto dopo
  l'avvio. Lo stato dei download viene salvato ogni 2 minuti: dopo uno stop
  brusco (crash, mancanza di corrente) i download ripartono da dove erano, e
  quelli che il motore avesse comunque perso vengono riaggiunti all'avvio
  riusando i dati già scaricati.
- **Un episodio non è arrivato: dove guardare** — parti dalla serie e premi 📜
  sulla puntata.
  - *Nessun evento*: Gextto non l'ha mai scaricato. Usa 🔍 (ricerca manuale) e
    **Perché non questa?** per capire se la release non c'è nelle sorgenti o se
    è stata scartata da filtri, qualità, lingua o ritardi; controlla anche lo
    stato delle sorgenti in *Salute*.
  - *📥 Downloading e poi niente*: il download è in corso o fermo. Apri il
    torrent da *Scarico*: «⏸️ is stuck» indica pochi utenti che condividono,
    «❌ Gave up» che è stato abbandonato e alla prossima ricerca ne cercherà
    un'altra versione.
  - *🎉 downloaded ma nessun 📁*: il file è scaricato ma non è arrivato in
    libreria. Cerca «⚠️ still cannot be moved» o «Could not move»: di solito
    NAS non raggiungibile, permessi o spazio; i tentativi riprendono da soli.
  - *«discarded as inferior»*: in libreria c'era già una versione almeno
    altrettanto buona, quindi quella nuova è stata scartata. È il comportamento
    previsto.
  - *📁 added / ♻️ updated*: è in libreria; se il nome non è quello atteso vedi
    il punto sulla rinomina qui sopra.
  Per il dettaglio completo apri *Mostra nel log* dal torrent: filtra il log
  sulle righe di quel solo download.
- **Il backup FTP fallisce** — usa *Test FTP*: indica il passo che fallisce
  (connessione, login, percorso remoto, upload, rimozione) e lo registra nel log.
- **Log** — vedi `data/gextto.log` (rotazione a 5 MB) o il viewer nella UI.

## Appendice A. Riferimento — Configurazione

La pagina è divisa in quattro aree, ognuna con le sue sezioni: **Cosa cercare**
(Generale, Sorgenti, Qualità e upgrade), **Come scaricare** (Motore torrent,
Velocità e rete, Code e prestazioni, Seed e completamento), **Dove salvare**
(Libreria e rinomina, Archivio e spazio) e **Sistema** (Manutenzione automatica,
Notifiche, Accesso e servizi, Diagnostica e traduzioni). Su computer le sezioni
sono nella colonna a sinistra, con il numero di impostazioni; su telefono si
scelgono dalla tendina *Sezione*. L'indirizzo della pagina segue la sezione
aperta, quindi si può salvare nei preferiti. I link alle vecchie schede
(*Acquisizione*, *Avanzate*, *Traduzioni*) aprono la sezione che ne ha preso il
contenuto. Entrando in Configurazione la barra dei menu principale si nasconde; il
pulsante **Menu** in alto a sinistra la fa ricomparire sopra il contenuto (Esc
per richiuderla).

Quando una sezione raccoglie più gruppi di impostazioni (per esempio **Ciclo di
ricerca** ed **Episodi mancanti** in *Manutenzione automatica*), il titolo del
gruppo resta **agganciato in alto** mentre scorri, con una barretta colorata
d'accento.

Sotto il titolo della sezione c'è l'**indice dei gruppi** (chip cliccabili) per
saltare direttamente a un gruppo. La casella **Solo modificate**, accanto a
*Mostra chiavi tecniche*, nasconde tutte le impostazioni rimaste al valore
predefinito: comoda per rivedere solo ciò che hai cambiato. Le righe si
evidenziano al passaggio del mouse.

Come si legge una riga:

- **Nome e descrizione** a sinistra; il controllo al centro con l'**unità**
  (secondi, minuti, GB, KiB/s…) e, sotto, il significato dei **valori
  speciali** (per esempio «0 = mai»).
- Le opzioni sì/no sono **interruttori**; gli orari usano il selettore
  dell'ora. In **Velocità e rete**, **Come si decide la velocità** sceglie la
  regola: *Auto* (Gextto misura la linea e si adatta), *Limiti fissi* (i due
  limiti globali) o *Programmazione oraria*. Con la programmazione compaiono la
  tabella con una riga per giorno, dove per ogni giorno c'è **una scelta** tra
  *Non attiva*, *Limite* (i tuoi valori, unici a usare i campi di velocità) e
  *Auto*, più inizio e fine, e **Fuori dalle fasce orarie: Auto**, che decide se
  fuori dalle fasce vale Auto o i limiti fissi. Le
  vecchie impostazioni a fascia unica non compaiono più nella pagina: finché la
  tabella non viene salvata ne mostra i valori e restano in uso.
- Le opzioni che dipendono da un interruttore o da una scelta (per esempio i
  campi della programmazione velocità o del RAM disk) **compaiono solo quando
  servono**, anche prima di salvare.
- Un valore diverso dal predefinito mostra **modificato**, il valore
  *Predefinito* e il pulsante **↺ Predefinito**, che rimette il valore
  predefinito nel campo (va poi salvato).
- La chiave tecnica (es. `refresh_interval`) si vede con **Mostra chiavi
  tecniche**; la ricerca la trova comunque.
- Le impostazioni che il motore torrent attivo non usa sono raccolte, chiuse,
  in **Non usate dal motore attivo** (o nel pannello del motore non attivo):
  restano salvate e tornano attive cambiando motore. Con qBittorrent-nox ci
  finiscono anche le opzioni di rete, protocollo, coda, cache e RAM disk che
  legge solo gx-torrent: qBittorrent usa la propria configurazione. Le opzioni
  rare sono nel pannello chiuso **Per esperti**.

Ogni riga si salva con il suo **Salva**. Un campo cambiato e non ancora salvato
è segnato **non salvata** e compare la barra in basso *Modifiche non salvate*,
con **Salva tutto** e **Annulla modifiche**. Cambiare sezione o lasciare la
pagina con modifiche in sospeso chiede conferma. Gli editor (feed, filtri,
regole, hook) hanno il loro pulsante *Salva* e mostrano anch'essi **non
salvata** finché non li salvi.

### Cosa cercare

#### Generale

*Quando e quanto spesso Gextto cerca nuove release e gli episodi mancanti.*

**Ciclo di ricerca**

| Impostazione | Cosa fa |
|---|---|
| Ciclo automatico (ricerca e download) | Interruttore generale del ciclo: quando è spento Gextto non cerca e non scarica da solo. Acceso di default; con la prova (dry-run) attiva cerca senza scaricare. |
| Intervallo della ricerca automatica | Intervallo tra le ricerche automatiche di serie e film, in secondi (21600 = 6 ore). *(Unità: secondi)* |
| Ricerca titoli online nel ciclo | Cerca i titoli su indexer durante il ciclo: auto (solo se non ci sono feed configurati), yes (sempre), no (mai, usa solo feed e archivio locale). |
| Età massima delle release | Ignora le release più vecchie di N giorni (0 = nessun limite). Le release senza data sono considerate pubblicate oggi. *(Unità: giorni; 0 = nessun limite)* |

**Episodi mancanti**

| Impostazione | Cosa fa |
|---|---|
| Cerca gli episodi mancanti | Attiva il riempimento dei buchi (episodi mancanti) dalle release disponibili. |
| Episodi mancanti per serie a ogni ciclo | Numero massimo di gap da cercare per serie in un ciclo (0 = illimitato). *(0 = illimitato)* Compare solo con «Cerca gli episodi mancanti» attivo. |
| Intervallo della ricerca approfondita | Ogni quante ore fare una ricerca live mirata sugli indexer per i gap. *(Unità: ore)* Compare solo con «Cerca gli episodi mancanti» attivo. |
| Ricerche approfondite per ciclo | Numero massimo di ricerche live per ciclo. Compare solo con «Cerca gli episodi mancanti» attivo. |

**Ritardo prima di scaricare**

| Impostazione | Cosa fa |
|---|---|
| Ritardo per le serie | Ritarda l'avvio dei download delle serie di questo numero di minuti. 0 avvia subito. *(Unità: minuti; 0 = nessuno)* |
| Ritardo per i film | Ritarda l'avvio dei download dei film di questo numero di minuti. 0 avvia subito. *(Unità: minuti; 0 = nessuno)* |
| Ignora il ritardo da questo punteggio | Se una release raggiunge almeno questo punteggio, ignora il delay configurato. *(0 = mai)* |

#### Sorgenti

*Da dove arrivano le release: feed RSS, motori web, filtri e cartelle osservate.*

**Feed e blacklist**

| Impostazione | Cosa fa |
|---|---|
| Blacklist (una parola per riga) | Parole vietate, una per riga: le release che le contengono vengono scartate. |
| Pagine feed da leggere | Quante pagine di elenco leggere per ogni feed (3 è un buon compromesso). |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Feed RSS | Lista dei feed RSS da leggere a ogni ciclo (una riga per URL). |
| Motori web | Motori di ricerca web usati dal gap-filling quando feed e indexer non trovano nulla. |
| Filtri contenuto esclusi | Le release che contengono queste parole o script (es. [porno]) vengono escluse. |
| Filtri per sorgente | Parole chiave da accettare o scartare per una singola sorgente, con attivazione per riga. |
| Cartelle osservate | I .torrent e i .magnet trovati nelle cartelle indicate vengono **scaricati** e aggiunti automaticamente alla sessione (ricorsiva, elimina dopo). |

#### eD2k e Kad

*Collegamento ad aMule, ricerca su Kad e sui server eD2k, ripiego automatico e download fermi.*

eD2k e Kad passano da **aMule** (vedi [eD2k e Kad con aMule](#ed2k-e-kad-con-amule)):
Gextto comanda un `amuled` già avviato tramite le connessioni esterne (EC). È
spento per default: senza il tuo consenso Gextto non contatta la rete.

| Impostazione | Cosa fa |
|---|---|
| eD2k e Kad tramite aMule | Collega Gextto ad aMule: ricerca, link `ed2k://`, download in Scarico e pagina eD2k. Spento di default. |
| aMule — indirizzo | Macchina su cui gira `amuled`, di solito `127.0.0.1`. Gextto deve vedere la cartella di arrivo di aMule allo stesso percorso. |
| aMule — porta connessioni esterne (EC) | `ECPort` di `amule.conf`, 4712 di default. |
| aMule — password connessioni esterne (EC) | La password in chiaro; aMule conserva il suo MD5 in `ECPassword`. Non visualizzata. |
| Cartelle di aMule | La cartella di download (incoming) e quella temporanea sono mostrate in sola lettura: appartengono ad aMule e si cambiano da aMule/amuled. Dopo la modifica riavvia Gextto (oppure riapri questa pagina) perché usi il nuovo percorso. |
| Rete delle ricerche eD2k | **Kad** (default, non serve un server), **Kad + server eD2k** (entrambe le ricerche, risultati uniti), **Server eD2k (globale)** (tutti i server) o **Server eD2k (connesso)**. Vale per la ricerca manuale; il fallback automatico cerca sempre su Kad e sui server. La stessa scelta è nella pagina eD2k. |
| Usa eD2k quando i torrent non trovano nulla | Secondo interruttore, separato: quando la ricerca torrent di episodio e stagione non ha prodotto nulla, Gextto cerca lo stesso episodio su eD2k/Kad (con il nome della serie e tutti gli alias). Vale sia per il passaggio approfondito del gap filling sia per **Cerca mancanti** e per la ricerca del singolo episodio. Spento di default. |
| Fonti minime per il fallback | Soglia di fonti eD2k per accettare automaticamente un risultato. La ricerca manuale mostra tutto. *(Unità: fonti; 0 = nessun minimo)* |
| Durata della ricerca eD2k | Per quanti secondi aMule raccoglie i risultati prima che Gextto li mostri. *(Unità: secondi)* |
| Rinuncia a un download eD2k fermo dopo | Un download eD2k scelto da Gextto che non riceve dati per questi giorni viene annullato in aMule, la release va in blocklist e l'episodio torna da cercare. I download aggiunti a mano e quelli in pausa restano. *(Unità: giorni; 0 = mai)* |

Il fallback automatico accetta solo un risultato che corrisponde esattamente
all'episodio, ha una qualità riconosciuta, una dimensione plausibile e almeno le
fonti minime. In tutti gli altri casi il risultato resta visibile nella ricerca
manuale e il buco non si chiude: meglio un episodio mancante che un file
sbagliato in libreria. Se aMule non è raggiungibile, i torrent continuano senza
alcun effetto.

Nella pagina **Sorgenti** la voce **eD2k e Kad** mostra la spia della
connessione: **aperta** (connesso a un server con ID alto), **limitata**
(firewall/NAT: ID basso o solo Kad), **non connessa**, **aMule non
raggiungibile** oppure **sconosciuta**. Se è limitata, apri sul router le porte
TCP e UDP di aMule.

#### Qualità e upgrade

*Come vengono confrontate le release e quando una versione migliore sostituisce quella in libreria.*

La pagina parte da ciò che serve più spesso e lascia i pesi per ultimi:

1. **Upgrade e sostituzione** — le regole che decidono quando una release
   migliore sostituisce quella in libreria (tabella qui sotto).
2. **Esempi con i pesi attuali** — punteggi di riferimento (1080p WEB-DL,
   1080p H.265 DD+, 2160p Dolby Vision) e soglia di stop.
   **Impatto sulla libreria** — **Verifica ora** ripassa le release già viste
   nei feed con le regole e i pesi attuali e dice quanti episodi archiviati
   verrebbero sostituiti, con alcuni esempi. Usala dopo aver cambiato pesi o
   soglie: un numero alto vuol dire che i prossimi cicli riscaricherebbero molto.
   Non scarica nulla; le serie con «Permetti upgrade» su No non contano.
3. **Prova una release** — incolla il nome di una release: Gextto mostra il
   punteggio voce per voce, la copia già in libreria con il suo punteggio e se
   la sostituirebbe, con il motivo. Non scarica nulla.
4. **Profili di qualità** — i modelli di qualità, lingua, sottotitoli ed
   esclusioni (vedi *Profili di qualità*); la lista è quella esposta a
   Jellyseerr.
5. **Video**, **Audio**, **Correzioni release** — i pesi, in schede chiuse:
   aprile solo se vuoi cambiare le preferenze. Per ognuno: **più alto = più
   preferito**.
6. **Gruppi custom**.

Il pannello **Gruppi custom** aggiunge un bonus o una penalità a un release group
(il nome deve corrispondere al tag finale della release, es. TBK; maiuscole e
minuscole non contano). I punteggi della libreria vengono ricalcolati da soli
quando cambi i pesi o quando un aggiornamento di Gextto cambia le regole di
calcolo, così una modifica non viene scambiata per un upgrade;
*Manutenzione → Ricalcola punteggi* resta disponibile per forzarlo.

Un upgrade **per punteggio** richiede un miglioramento reale della qualità:
risoluzione, sorgente, codec, audio, HDR/DV, correzioni e gruppo devono valere
almeno la differenza minima in più. Dimensione del file, sottotitoli e salute
dello swarm aiutano a scegliere tra release equivalenti, ma da soli non fanno
mai riscaricare un episodio o un film della stessa qualità. La stessa regola
vale per i season pack e dopo il download: un file della stessa qualità non
sostituisce la copia archiviata solo perché ha un punteggio più alto. Se la
copia archiviata non dichiara la sorgente o il gruppo (per esempio un file
importato o rinominato con il solo titolo), quei campi non vengono accreditati
alla release trovata: senza la prova che la copia sia peggiore non viene
sostituita. `ffprobe` completa risoluzione, codec, audio e canali quando il nome
li ha persi.

Anche la regola che preferisce una sorgente WEB-DL a un WEBRip o un HDTV segue i
pesi: vale solo finché il peso **Sorgente** del WEB-DL è più alto. Se dai a
WEB-DL e WEBRip lo stesso peso, per Gextto sono la stessa qualità e un WEB-DL non
sostituisce più un WEBRip (restano validi HDR, risoluzione, REPACK/PROPER e il
punteggio).

**Verifica MediaInfo prima di entrare in libreria.** Quando un download completo
deve sostituire un episodio o un film già archiviato, Gextto legge con `ffprobe`
il file nuovo, ancora nella cartella di download, e la copia in libreria, prima
di spostare, collegare o copiare qualsiasi cosa. Risoluzione, codec video, audio,
canali e HDR10/HLG vengono presi dai flussi reali, non dal nome. Se il nome
prometteva più di quanto c'è (un «2160p» che è un 1080p ingrandito, un «HDR» senza
HDR, un «DDP 5.1» che è AAC stereo) e la qualità reale non è un upgrade, il
download viene scartato e messo in blocklist e la copia in libreria resta com'è;
il log lo spiega con «Upgrade not confirmed by MediaInfo». Sorgente, Dolby Vision
e REPACK/PROPER restano quelli del nome, perché `ffprobe` non li distingue in modo
affidabile. Senza `ffprobe`, o con la copia archiviata irraggiungibile, vale la
decisione presa prima del download. I pack di stagione non passano da questa
verifica.

**Upgrade e sostituzione**

| Impostazione | Cosa fa |
|---|---|
| Differenza minima score per upgrade | Differenza minima di punteggio per sostituire un file con un upgrade migliore. Conta solo la qualità: dimensione, sottotitoli e salute dello swarm non bastano a superarla. |
| Smetti di migliorare oltre questo punteggio | Tetto agli upgrade: quando il file in libreria ha almeno questo punteggio non viene più sostituito da release migliori; resta accettato solo un REPACK o PROPER, che corregge una release difettosa. Riferimenti con i punteggi predefiniti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. Si somma alla differenza minima: la prima dice *di quanto* deve migliorare, questa *fino a dove*. Per fermare gli upgrade di un solo titolo imposta «Permetti upgrade» su No con *Modifica selezionati* (o nel profilo con cui lo aggiungi). «Perché non questa?» indica quando una release è scartata per questa soglia. *(0 = migliora sempre: nessun tetto)* |
| Sostituisci le versioni già archiviate | Sostituisce versioni inferiori già archiviate con upgrade migliori. |
| Differenza minima score per cleanup | Differenza minima di punteggio per sostituire un file esistente con uno migliore (cleanup). |

**Download bloccati**

| Impostazione | Cosa fa |
|---|---|
| Prova un'alternativa dopo (stallo) | Se un download è fermo da almeno questi minuti — a 0 B/s oppure ancora in attesa del suo elenco di file (metadati, tipico dei magnet morti) — la ricerca può avviare una versione alternativa della stessa puntata o film. Il download fermo resta in sessione e può riprendersi; la deduplica dell'archivio terrà poi la copia migliore. *(0 = mai)* |
| Punteggio in meno per l'alternativa | Quanti punti in meno sono accettati per l'alternativa rispetto alla release bloccata: parte solo un'alternativa adeguata, mai una versione scadente. Il download bloccato non viene rimosso. *(0 = disattivato)* |

### Come scaricare

#### Motore torrent

*Quale motore scarica i torrent e come Gextto lo raggiunge.*

**Motore torrent**

| Impostazione | Cosa fa |
|---|---|
| Motore torrent | Motore torrent attivo (gx-torrent o qBittorrent-nox). |

**gx-torrent**

| Impostazione | Cosa fa |
|---|---|
| gx-torrent — URL Web API | URL dell'API di gx-torrent (es. http://127.0.0.1:8890). Gextto lo usa per pilotare il demone. |
| gx-torrent — indirizzo di ascolto (LAN) | Indirizzo di ascolto del demone gestito. Predefinito `127.0.0.1:8890`: pagina web e API restano solo su questo server. Per esporle in LAN usa `0.0.0.0:8890` e imposta `gxtorrent_token` (senza token Gextto avvia il demone in `-insecure`). La porta viene allineata a quella dell'URL. |
| gx-torrent — gestione automatica (cache e coda) | Attiva l'autogestione di gx-torrent: coda dinamica e cache adattiva (dimensionata su memoria disponibile, download/seed attivi e tipo di storage). Predefinito attivo. Disattivalo per fissare a mano cache e slot. |
| gx-torrent — proxy (socks5:// o http://) | Proxy per peer, tracker HTTP e web seed; con un proxy DHT e tracker UDP vengono spenti (non visualizzato). |
| gx-torrent — identità del client annunciata | Cosa vedono peer e tracker: `gx-core` con la versione reale (default) oppure un preset qBittorrent, Transmission, Rain o anacrolix/torrent. Cambia solo l'identità annunciata, non il protocollo: alcuni tracker privati accettano solo client in whitelist, controlla le loro regole. |
| gx-torrent — versione del client annunciata | Versione usata con il preset; vuoto usa quella predefinita del preset. |
| gx-torrent — timeout richieste | Timeout in secondi delle richieste HTTP verso gx-torrent. *(Unità: secondi)* |
| gx-torrent — intervallo polling | Intervallo minimo in millisecondi tra due letture dello stato dei torrent (la coda la gestisce il demone). *(Unità: ms)* |
| gx-torrent — server DLNA/UPnP | Abilita il media server DLNA/UPnP del demone: i media **completati** dei torrent sono visibili a TV e console sulla rete locale. Solo rete locale, senza autenticazione; disattivato di default. Funziona con il motore gx-torrent. |
| gx-torrent — DLNA: indirizzo HTTP | Indirizzo e porta di ascolto del media server (es. `:8200` per tutte le interfacce, `192.168.1.10:8200` per una sola). Vuoto = `:8200`. La discovery usa UDP 1900 sulla stessa interfaccia. |
| gx-torrent — DLNA: nome del server | Nome mostrato ai lettori DLNA sulla rete. Vuoto = `gx-torrent`. |
| gx-torrent — DLNA: cartelle della libreria | Cartelle (es. l'archivio sul NAS) mostrate dal media server accanto ai torrent, navigabili per cartelle; separale con `;`. Vuoto = automatico: la cartella archivio e le cartelle che contengono gli archivi delle serie. Sola lettura; il cestino, le cartelle nascoste e quelle di sistema del NAS (`@eaDir`, `#recycle`) sono escluse. |

**qBittorrent-nox**

| Impostazione | Cosa fa |
|---|---|
| qBittorrent-nox — scaricato e aggiornato da Gextto | Gextto scarica da sé l'ultima release di qBittorrent-nox, la installa nella cartella dell'applicazione (accanto a gexttod), la avvia e la ferma con il servizio e la aggiorna (con backup e rollback). Il motore in uso però si sceglie dalla voce «Motore torrent»: questa opzione non lo cambia. |
| qBittorrent-nox — URL Web API | URL dell'interfaccia Web di qBittorrent-nox (es. http://127.0.0.1:8080). |
| qBittorrent-nox — utente | Utente dell'interfaccia Web di qBittorrent-nox. |
| qBittorrent-nox — password | Password dell'interfaccia Web di qBittorrent-nox (non visualizzata). |
| qBittorrent-nox — categoria | Categoria applicata ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applicato ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — timeout richieste | Timeout in secondi delle richieste HTTP verso qBittorrent-nox. *(Unità: secondi)* |
| qBittorrent-nox — intervallo polling | Intervallo in millisecondi tra due letture dello stato dei torrent. *(Unità: ms)* |
| qBittorrent-nox — mappatura percorsi | Serve solo se qBittorrent vede le cartelle con percorsi diversi (Docker, NAS, altro host): una riga per percorso, forma locale=remoto. **Proponi** legge da qBittorrent le sue cartelle di download e temporanea e compila le righe; controllale e premi **Salva**. Stessa macchina e stesse cartelle — o modalità gestita —: lascia pure vuoto. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Applica motore | Verifica il motore configurato e indica se serve un riavvio (pannello *Motore torrent*). |
| Installa / Ottimizza qBittorrent-nox, Stato qBittorrent-nox, Test connessione | Nel pannello *qBittorrent-nox*: installa o aggiorna il binario gestito, mostra dove è installato e se c'è un aggiornamento, verifica la connessione. |

#### Velocità e rete

*Limiti di banda, fasce orarie, porte e protocolli della sessione torrent.*

**Limiti di velocità**

| Impostazione | Cosa fa |
|---|---|
| Come si decide la velocità | La regola della velocità: **Auto** (Gextto misura la linea e si adatta, senza tetto), **Limiti fissi** (i due limiti qui sotto) o **Programmazione oraria** (fasce per giorno, ognuna con limiti o Auto). In Scarico, dal menu **Banda**, puoi sempre fare un'eccezione temporanea, che vince sulla regola. Non è un'impostazione a sé: accende o spegne «Programmazione velocità attiva» e «Fuori dalle fasce orarie: Auto». |
| Limite download globale | Limite globale di download in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* Compare con «Limiti fissi» e «Programmazione oraria». |
| Limite upload globale | Limite globale di upload in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* Compare con «Limiti fissi» e «Programmazione oraria». |
| Programmazione velocità attiva | Si imposta da «Come si decide la velocità» e non compare nella pagina; resta disponibile dalle API. |
| Fuori dalle fasce orarie: Auto | Con la programmazione oraria: fuori dalle fasce la velocità è Auto invece dei limiti fissi. Compare solo con «Programmazione oraria». |
| Programmazione — per giorno | Una scelta per giorno: **Non attiva** (nessuna fascia), **Limite** (fascia con i tuoi valori di download e upload) o **Auto** (fascia in cui Gextto misura la linea e si adatta). Solo «Limite» abilita i due campi di velocità; gli orari restano sempre attivi. Una fascia oltre la mezzanotte appartiene al giorno in cui inizia. *(un giorno «Limite» ha i suoi valori (0 = illimitato); «Auto» lascia che Gextto misuri la linea e si adatti; una fascia oltre la mezzanotte appartiene al giorno in cui inizia)* Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — ora inizio | Ora di inizio della programmazione (HH:MM). Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — ora fine | Ora di fine della programmazione (HH:MM). Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — giorni | Giorni attivi: 0=Lun … 6=Dom (es. 0,1,2,3,4). *(nessun giorno = mai attiva)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — download | Limite di download in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — upload | Limite di upload in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Auto rispetta la latenza | Con la velocità su Auto, Gextto misura il ping verso Google e Cloudflare ogni 2 secondi (8.8.8.8, 8.8.4.4, 1.1.1.1, 1.0.0.1; si usa il tempo più basso). Finché il ping è buono alza download e upload; quando sale taglia il 15% e scende fino a 128/64 KiB/s. Ricorda la velocità alla quale il ping è peggiorato e si ferma appena sotto, riprovando a superarla una volta al minuto. Se il ping resta buono anche a piena velocità, il limite viene tolto; quando resta solo il traffico di fondo (DHT), il limite cade. La prima volta che applica un limite lo scrive subito nel log; poi scrive al massimo una riga al minuto, solo se la velocità cambia almeno del 10% e l'andamento lo conferma. Con una linea asimmetrica l'upload conta come carico già a un decimo della capacità (tra 32 e 256 KiB/s) e viene limitato prima che saturi; se comincia a caricare durante il controllo, il limite parte subito. Il ping misurato compare accanto alla velocità **↓ Download** nella barra in alto. Attiva per default. |
| Auto — cosa misura la latenza | `both` (default): il ping decide, i contatori del router sono solo diagnostica. `ping`: solo ping, il router non viene letto. `router`: vecchio comportamento, si cede solo agli altri dispositivi visti dal router. |
| Auto — host per misurare la latenza | Host (o host:porta) a cui misurare la latenza con una connessione TCP ogni 2 secondi, solo mentre Auto è attivo. Vuoto = Google (8.8.8.8, 8.8.4.4) e Cloudflare (1.1.1.1, 1.0.0.1), porta 443; si usa il tempo più basso, così un server lontano o lento non falsa la misura. |
| Auto — velocità massima linea, download | Massima velocità reale della linea in download, in KiB/s (es. 9000 per 9 MB/s). 0 = non nota: la scopre Auto. Se la indichi, Auto non sale oltre e trova il punto giusto più in fretta. |
| Auto — velocità massima linea, upload | Massima velocità reale della linea in upload, in KiB/s (es. 800). 0 = non nota: la scopre Auto. Se la indichi, Auto non sale oltre e trova il punto giusto più in fretta. |

**Porte e interfacce**

| Impostazione | Cosa fa |
|---|---|
| Porta minima | Porta minima della sessione torrent (richiede il riavvio del servizio). |
| Porta massima | Porta massima della sessione torrent (richiede il riavvio del servizio). |
| Interfacce listen | Indica dove il motore accetta connessioni: 0.0.0.0:6881-6891 per tutte le interfacce, 127.0.0.1:6881 solo in locale, oppure wg0:6881/tun0:6881 per una VPN. Il valore proposto va bene nella maggior parte dei casi. |
| Interfaccia uscente | Killswitch VPN: interfaccia usata per tutto il traffico BitTorrent in uscita. |
| Test porte | Verifica che la porta peer sia in ascolto e che il router la inoltri (il messaggio dice "aperta", "in ascolto ma non inoltrata" o "chiusa"). Disponibile con il motore **gx-torrent**. |

**Ricerca peer e tracker**

| Impostazione | Cosa fa |
|---|---|
| DHT | Abilita la rete DHT per trovare peer senza tracker. |
| PEX | Peer Exchange: scambio peer con altri client. |
| LSD | Local Service Discovery: trova peer nella rete locale. |
| UPnP | Apre le porte del router automaticamente con UPnP. |
| NAT-PMP | Apre le porte del router automaticamente con NAT-PMP (prova anche PCP, RFC 6887). |
| uTP | Abilita il protocollo uTP (UDP) oltre a TCP. |
| Holepunching (BEP 55) | Apre una connessione diretta ai peer dietro NAT tramite un peer che fa da relè (BEP 55). Richiede uTP. |
| Nodi bootstrap DHT | Nodi DHT iniziali (host:porta separati da virgola). |

**Cifratura e filtro IP**

| Impostazione | Cosa fa |
|---|---|
| Cifratura | Politica di cifratura: 0 disabilitata, 1 abilitata, 2 forzata. |
| Applica IP filter | Applica il filtro IP anche ai tracker. |
| IP filter (file/URL) | File locale o URL della lista IP da bloccare. Il valore predefinito è la lista eMule Security (`https://upd.emule-security.org/ipfilter.gz`), in formato P2P e aggiornata ogni giorno. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Filtro IP → Carica / aggiorna ora | Scarica la lista dall'URL configurato (o usa il file locale) e la applica subito al motore attivo, gx-torrent compreso. |

#### Code e prestazioni

*Quanti torrent restano attivi, connessioni, RAM disk e cache.*

**Gestione automatica**

| Impostazione | Cosa fa |
|---|---|
| Adatta i valori nel tempo | Ritocca nel tempo i valori scelti dal sistema (per esempio lo spazio libero minimo) quando cambiano le condizioni. Non tocca mai i valori impostati a mano; spegnilo per avere valori fissi. |

**Coda e slot**

| Impostazione | Cosa fa |
|---|---|
| Auto-gestione dinamica coda e risorse | Regola automaticamente quanti torrent sono attivi in base al carico. |
| Slot download dinamici minimi | Numero minimo di download dinamici. La coda cambia al massimo di uno per volta. Compare solo con «Auto-gestione dinamica coda e risorse» attivo. |
| Slot download dinamici massimi | Numero massimo di download dinamici. Servono campioni consecutivi coerenti prima di aumentare la coda. Compare solo con «Auto-gestione dinamica coda e risorse» attivo. |
| Non contare i torrent fermi negli slot attivi | I torrent che non trasferiscono dati non consumano uno slot attivo. |
| Download attivi | Valore base dei download attivi; con la coda dinamica viene adattato a runtime. |
| Seed attivi | Valore base dei seed attivi; con la coda dinamica scende a 1 quando ci sono download in coda. |
| Limite torrent attivi | Valore base del limite di torrent attivi; con la coda dinamica diventa max(base, download + seed + 2). |

**Modalità di download**

| Impostazione | Cosa fa |
|---|---|
| Download sequenziale | Scarica i file in ordine sequenziale invece che a pezzi sparsi. |
| Prima/ultima parte dei file | Scarica per primi l'inizio e la fine di ogni file in tutti i nuovi torrent, poi prosegue normalmente: utile per guardare un video mentre scarica. Vale con ogni motore; i torrent già in corso non cambiano. |
| Prealloca lo spazio su disco | Riserva subito tutto lo spazio su disco prima di iniziare il download. |

**Connessioni**

| Impostazione | Cosa fa |
|---|---|
| Connessioni automatiche (in base alla macchina) | Attivo (predefinito): Gextto lascia decidere al motore quante connessioni tenere per ogni torrent in base a core e RAM della macchina, e riduce quelle dei torrent che non progrediscono da un paio di minuti pur avendo molti peer. Con molti torrent attivi insieme il motore ha anche un totale di connessioni per tutta la macchina (75 per core), diviso tra i torrent con la precedenza a quelli che stanno scaricando; un torrent da solo ha sempre tutte le sue connessioni. Il valore qui sotto resta il tetto massimo. Spegnilo per usare un numero fisso. Solo gx-torrent. |
| Connessioni max per torrent | Tetto alle connessioni per singolo torrent (-1 = illimitato, predefinito). Con le connessioni automatiche attive è il massimo che il motore può usare; con l'automatico spento è il limite fisso. Vale per ogni torrent che non ha un limite proprio (scheda *Limiti* del dettaglio) e si applica subito, anche ai torrent in corso; solo gx-torrent. |

**RAM disk**

| Impostazione | Cosa fa |
|---|---|
| Usa il RAM disk | Scarica in RAM i torrent che rientrano nella soglia; i più grandi vanno su disco. |
| Cartella RAM disk | RAM disk da usare per i download in corso, se disponibile. Compare solo con «Usa il RAM disk» attivo. |
| Dimensione massima per torrent | Dimensione massima di un singolo torrent ammesso sul RAM disk (GB). *(Unità: GB)* Compare solo con «Usa il RAM disk» attivo. |
| Margine libero da mantenere | Spazio libero da lasciare sul RAM disk una volta completato il download (GB). *(Unità: GB)* Compare solo con «Usa il RAM disk» attivo. |
| Spazio minimo libero | Spazio minimo libero in byte richiesto per usare il RAM disk. 0 = usa il margine configurato. *(Unità: byte; 0 = usa il margine)* Compare solo con «Usa il RAM disk» attivo. |

La scelta avviene il più presto possibile: un torrent la cui dimensione è già
nota e supera la soglia va subito su disco; uno con dimensione **ancora
sconosciuta** (tipico dei magnet) resta su disco finché non arrivano i metadati,
poi viene spostato sul RAM disk **solo se** la dimensione reale rientra nella
soglia. Un file troppo grande non tocca mai il RAM disk. La regola vale per tutti
i download — automatici e aggiunti a mano — e per ogni motore che usa il RAM
disk; qBittorrent, che non lo supporta, scarica sempre su disco.

Gextto verifica anche la memoria disponibile della macchina: un `tmpfs` sta in
RAM, quindi un disco da 6 GB può risultare quasi vuoto mentre il sistema ha poca
memoria libera. Prima di iniziare, lo spazio usabile del RAM disk è limitato
dalla memoria disponibile (`MemAvailable`): il download va sul RAM disk solo se
ci stanno sia lo spazio del `tmpfs` sia la memoria del sistema, altrimenti resta
su disco come i file troppo grandi. Se la memoria non è leggibile vale il solo
spazio del `tmpfs`, come prima.

**Per esperti**

| Impostazione | Cosa fa |
|---|---|
| Cache disco | Dimensione della cache disco in blocchi (-1 = automatico). *(Unità: blocchi; -1 = auto)* |
| Scadenza cache | Secondi di inattività dopo cui un blocco esce dalla cache. *(Unità: secondi)* |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Ottimizza | Calcola cache e buffer in base alla RAM. |
| Ottimizza per il sistema | Propone i valori adatti a questo hardware (memoria, dischi, condivisione di rete, RAM disk), una riga per modifica con il motivo; nulla viene scritto finché non applichi le righe selezionate. Un valore salvato a mano non viene mai riproposto. |

#### Seed e completamento

*Quanto restare in seed, cosa fare a download finito, come controllare i pack in download e come gestire i torrent bloccati.*

**Seed**

| Impostazione | Cosa fa |
|---|---|
| Seed ratio globale | Rapporto upload/download dopo cui fermare il seeding. 0 = infinito: basta questo perché il seed non finisca mai e il file non venga spostato, anche con un limite di tempo. *(0 = infinito)* |
| Seed massimo | Limite principale di seeding in giorni; se maggiore di 0 prevale sul limite in minuti. Con giorni e minuti a 0 il tempo è infinito. *(Unità: giorni; 0 = usa il limite in minuti)* |
| Seed massimo (fallback) | Limite di seeding in minuti, usato solo se Seed massimo (giorni) è 0. Con giorni e minuti a 0 il tempo è infinito: il seed non finisce mai e il file non viene spostato. *(Unità: minuti)* |
| Ferma il seed inattivo dopo | Ferma un torrent in seed che non invia dati da questo numero di minuti (conta solo il tempo in seed attivo). Come gli altri limiti: a fine seed segue l'archiviazione o la rimozione. Con qBittorrent il valore viene inviato anche come `inactiveSeedingTimeLimit`. *(Unità: minuti; 0 = disattivato)* |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Regole di seed | Limiti di seed (ratio, giorni, inattività) per sorgente/indexer e categoria; vale la prima regola che corrisponde, un campo vuoto usa il globale, 0 toglie il limite. |

**A download completato**

| Impostazione | Cosa fa |
|---|---|
| Elimina i completati dopo il seed | Attivo: a fine seed il torrent completato viene tolto dalla sessione (come «Pulisci completati»). Spento: a fine seed il torrent resta nell'elenco come Completato e lo rimuovi tu con «Pulisci completati». Non influisce su dove vengono spostati i file. |
| Sposta gli episodi/pack in archivio (non copiare) | Attivo: la sorgente scaricata viene spostata in libreria e rimossa dalla cartella download (per i torrent al termine del seed; per eD2k al completamento, perché il seeding è gestito da aMule). Spento: la sorgente viene copiata o collegata e mantenuta per la condivisione. |
| Hardlink invece della copia durante il seed | Attivo (predefinito): un file che resta in seed entra in libreria come hardlink, senza occupare spazio due volte; se download e libreria sono su filesystem diversi si copia. Vedi «Hardlink al posto della copia». |

**Controllo dei pack**

| Impostazione | Cosa fa |
|---|---|
| Ispeziona i pack in download e scarta quelli inutili | Attivo: appena il motore conosce la lista dei file, Gextto controlla se almeno un episodio serve davvero (manca dalla libreria oppure è un vero upgrade). Se nessuno serve, il torrent viene rimosso con i dati parziali e messo in blocklist, così non viene più scaricato. Spento (predefinito): il pack viene scaricato per intero e deciso solo a download finito. |
| Nei pack utili scarica solo i file che servono | Attivo (con l'ispezione): quando il pack contiene solo alcuni episodi utili, il motore scarica soltanto quei file e salta gli altri (priorità 0); sottotitoli e file senza numero di episodio restano attivi. Spento: il pack utile viene scaricato per intero. |

**Torrent bloccati**

| Impostazione | Cosa fa |
|---|---|
| Considera bloccato dopo | Dopo questi minuti senza avanzamento il torrent viene considerato stalled. *(Unità: minuti)* |
| Riprova i torrent bloccati ogni | Intervallo tra i tentativi di reannounce dei torrent stalled. *(Unità: minuti)* |
| Rimuovi i torrent bloccati dopo | Dopo questo periodo senza progresso il torrent viene rimosso automaticamente. Imposta 0 per disattivare completamente la rimozione automatica per stallo. *(Unità: minuti; 0 = mai)* |
| Rimuovi i torrent senza seeder dopo | Stallo con zero seeder (dead swarm): rimosso dopo questo periodo, più breve, e la puntata viene ricercata di nuovo subito. Imposta 0 per usare la soglia generale. *(Unità: minuti; 0 = usa la soglia generale)* |
| Riammetti una release senza seeder dopo | Una release abbandonata perché lo swarm non aveva seeder resta in blocklist per questo periodo, poi torna fra i candidati: così un 4K che nel frattempo ha trovato seeder può essere scaricato come upgrade. La copia già in libreria resta nel frattempo. Imposta 0 per tenerla bloccata per sempre. *(Unità: giorni; 0 = per sempre)* |

### Dove salvare

#### Libreria e rinomina

*Come vengono nominati e organizzati i file in libreria.*

**Rinomina**

| Impostazione | Cosa fa |
|---|---|
| Rinomina episodi | Rinomina i file scaricati usando i metadati TMDB (o TVDB senza chiave TMDB). |
| Film come file singoli (spiana le cartelle) | Se Sì, un film arrivato dentro una cartella torrent viene spostato nella cartella film come file singolo, portando con sé sottotitoli e artwork. Se No, resta nella sua cartella. |
| Verifica dei file rinominati ogni | Ogni quante ore verificare che i file archiviati/rinominati siano ancora presenti. *(Unità: ore)* |

**Lingue dei metadati**

| Impostazione | Cosa fa |
|---|---|
| Lingua predefinita | Lingua preferita di default per serie e film (es. ita, eng). |
| Lingua TVDB | Lingua preferita per i metadati TVDB (es. ita, eng). |
| Lingua TMDB | Lingua usata per i metadati TMDB (es. it-IT, en-US). |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Composizione del nome | Editor del template con token e anteprima per comporre il nome dei file. |
| Regole tag → cartella | Associa un tag del torrent a una cartella temporanea e a una cartella finale. |

#### Archivio e spazio

*Cartelle di lavoro, cestino e spazio minimo su disco.*

**Cartelle**

| Impostazione | Cosa fa |
|---|---|
| Cartella archivio | Cartella di archivio predefinita per i contenuti senza percorso dedicato. |
| Cartella download | Cartella di download predefinita per tutti i motori. |
| Cartella temporanea | Cartella temporanea per i download in corso. |
| Copia i file .torrent in | Copia qui i file .torrent dei download (vuoto = nessuna copia). |

**Cestino**

| Impostazione | Cosa fa |
|---|---|
| Cartella cestino | Cartella dove vengono spostati i file sostituiti/duplicati (se lasciata vuota usa la sottocartella trash nella cartella dati). |
| Cosa fare con i file sostituiti | Cosa fare con i file sostituiti: sposta nel trash o elimina. |
| Conservazione nel cestino | Giorni di conservazione per le pulizie non forzate; 0 elimina tutto il contenuto del cestino. Le azioni manuali della UI svuotano sempre subito il cestino. *(Unità: giorni; 0 = svuota tutto)* |

**Spazio su disco**

| Impostazione | Cosa fa |
|---|---|
| Spazio libero minimo per scaricare | Spazio libero minimo (GB) sulla cartella download: sotto questa soglia il ciclo non avvia download e gx-torrent mette in pausa quelli in corso, che ripartono da soli quando lo spazio torna. *(Unità: GB; 0 = nessun controllo del ciclo, gx-torrent mantiene il suo default di 1 GiB)* |

### Sistema

#### Manutenzione automatica

*Pulizie periodiche dei dati tecnici, analisi MediaInfo e file orfani.*

**Housekeeping**

| Impostazione | Cosa fa |
|---|---|
| Housekeeping periodico attivo | Attiva la pulizia periodica dei dati tecnici e dello storico. |
| Housekeeping — intervallo | Intervallo tra due housekeeping automatici, in ore. *(Unità: ore)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — statistiche cicli di ricerca conservate | Numero di statistiche dei cicli di ricerca da conservare. Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — visti nel feed | Elimina le righe storiche delle release viste nei feed più vecchie di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — storico download | Elimina dallo Storico download le righe dei torrent rimossi più vecchie di N giorni. *(Unità: giorni; 0 = conserva sempre)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — schede errore | Elimina le schede dei torrent in errore più vecchie di N giorni (minimo 1). *(Unità: giorni)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — log ricerche gap | Elimina il log delle ricerche degli episodi mancanti più vecchio di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — backup upgrade | Elimina i backup dei file sostituiti dagli upgrade più vecchi di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |

**Pulizia archivio release**

| Impostazione | Cosa fa |
|---|---|
| Pulizia automatica archivio | Abilita la pulizia automatica dell'archivio a ogni housekeeping: elimina solo le righe del catalogo release più vecchie dell'età massima. File scaricati, libreria, storico download e torrent attivi non vengono toccati. |
| Archivio — età massima | Età massima delle release in archivio, in giorni (0 = nessun limite). *(Unità: giorni; 0 = nessun limite)* Compare solo con «Pulizia automatica archivio» attivo. |
| Archivio — mantieni almeno N voci | Numero minimo di release recenti da conservare sempre, anche se più vecchie dell'età massima. Compare solo con «Pulizia automatica archivio» attivo. |

**Analisi MediaInfo**

| Impostazione | Cosa fa |
|---|---|
| Backfill MediaInfo automatico | Analizza periodicamente con ffprobe i file già presenti che non hanno ancora MediaInfo. |
| Backfill MediaInfo — intervallo | Minuti tra due passaggi del backfill MediaInfo. *(Unità: minuti)* Compare solo con «Backfill MediaInfo automatico» attivo. |
| Backfill MediaInfo — file per volta | Numero massimo di file analizzati in ogni passaggio MediaInfo. Compare solo con «Backfill MediaInfo automatico» attivo. |

**File orfani**

| Impostazione | Cosa fa |
|---|---|
| Sposta nel cestino i dati orfani della cartella temporanea | Ogni 30 minuti sposta nel cestino file e cartelle della cartella temporanea dei download che non appartengono a nessun torrent in lista e non sono cambiati da almeno il numero di giorni indicato. Non cancella nulla: restano nel cestino finché non lo svuoti. |
| Dati orfani — fermi da almeno | Da quanti giorni un elemento della cartella temporanea deve essere fermo, senza torrent in lista, per essere spostato nel cestino. *(Unità: giorni)* Compare solo con «Sposta nel cestino i dati orfani della cartella temporanea» attivo. |

#### Notifiche

*Telegram, email, webhook e programmi da eseguire sugli eventi.*

**Telegram**

| Impostazione | Cosa fa |
|---|---|
| Telegram attivo | Invia le notifiche su Telegram. |
| Telegram bot token | Token del bot Telegram (da @BotFather). Compare solo con «Telegram attivo» attivo. |
| Telegram chat ID | ID della chat/canale dove inviare le notifiche. Compare solo con «Telegram attivo» attivo. |

**Email**

| Impostazione | Cosa fa |
|---|---|
| Email attiva | Invia le notifiche via email. |
| SMTP | Server SMTP nel formato host:porta (es. smtp.gmail.com:587). Compare solo con «Email attiva» attivo. |
| Email mittente | Indirizzo mittente delle email di notifica. Compare solo con «Email attiva» attivo. |
| Email destinatario | Destinatari delle email (separati da virgola). Compare solo con «Email attiva» attivo. |
| Password email | Password/app-password SMTP (non visualizzata). Compare solo con «Email attiva» attivo. |

**Webhook**

| Impostazione | Cosa fa |
|---|---|
| Webhook URL | URL del webhook a cui inviare gli eventi. |
| Webhook secret | Segreto HMAC per firmare le richieste al webhook. |
| Formato webhook | Formato del payload inviato all'URL del webhook (Gextto, Discord, Slack, ntfy, Gotify, Pushover). |
| Webhook token | Credenziale del provider (ntfy, Gotify, Pushover); non visualizzata. Non serve per Gextto, Discord e Slack. |
| Webhook user (Pushover) | Solo Pushover: la user key. Non serve per gli altri formati. |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Hook eventi | Esegue un programma su determinati eventi (nome, eventi, programma, argomenti, timeout). |

#### Accesso e servizi

*Login, chiavi API e credenziali dei servizi esterni.*

Gextto è pensato per una LAN fidata: l'accesso è **libero per impostazione
predefinita**. Se lo raggiungi da fuori casa (reverse proxy, port forwarding,
VPN) puoi chiedere il login a chi non è in rete locale.

**Login**

| Impostazione | Cosa fa |
|---|---|
| Richiedi l'accesso (login) | Spento (predefinito): nessun controllo. Acceso: chi non è in rete locale deve fare il login o usare la chiave API. Finché non imposti una password o una chiave resta tutto aperto (e il log lo segnala). |
| Nessun login dalla rete locale | Acceso (predefinito): da 127.0.0.1, 192.168.x.x, 10.x.x.x, 172.16–31.x.x e dagli indirizzi IPv6 locali non serve il login. Dietro un reverse proxy conta l'indirizzo reale del client inoltrato dal proxy (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`), quindi chi arriva da Internet attraverso il proxy deve comunque autenticarsi. Compare solo con «Richiedi l'accesso (login)» attivo. |
| Utente | Nome utente per il login (predefinito `admin`). Compare solo con «Richiedi l'accesso (login)» attivo. |
| Password | Salvata solo come hash bcrypt; cambiarla chiude tutte le sessioni aperte. Lascia il campo vuoto per non modificarla. Compare solo con «Richiedi l'accesso (login)» attivo. |

**Scritture da un'altra origine.** Indipendentemente dal login, Gextto rifiuta
le richieste di modifica che il browser dichiara provenire da un altro sito
(`Origin`/`Referer`, `Sec-Fetch-Site: cross-site`): una pagina malevola non può
riconfigurare Gextto (impostazioni, hook, azioni sui torrent) usando il browser
di chi ha l'interfaccia aperta. Le letture (GET) e i client senza browser
(curl, la TUI) non sono toccati.

**Chiavi API**

| Impostazione | Cosa fa |
|---|---|
| Chiave API (script, TUI, calendario) | Per gli accessi senza browser: header `X-Api-Key: <chiave>` oppure `?apikey=<chiave>` nell'indirizzo. La TUI la legge dalla variabile `GEXTTO_API_KEY`. |
| gx-torrent — token di accesso (pagina e API in LAN) | Nella scheda **Accesso**: segreto condiviso richiesto dalla pagina e dall'API; obbligatorio se il demone ascolta in rete (non visualizzato). |

I due segreti si generano dalla riga stessa: **Genera** crea un valore casuale
lungo e lo salva, **Copia** lo mette negli appunti. Per il token di gx-torrent
serve poi riavviare il demone perché venga applicato. Cosa sono e come si usano
è spiegato in [Chiavi di accesso](api-keys.it.md).

**Servizi metadati**

| Impostazione | Cosa fa |
|---|---|
| TMDB API key (consigliata) | Chiave API TMDB, gratuita: fonte principale di titoli, poster e metadati. Se c'è, ha la precedenza su TVDB. |
| TVDB API key (alternativa a TMDB) | Chiave API v4 di TheTVDB: senza la chiave TMDB fornisce tutti i metadati. |
| TVDB PIN abbonato | PIN dell'abbonamento TheTVDB, richiesto al login solo dalle chiavi «user-supported». |

#### Metadati: TMDB o TVDB

Gextto usa i metadati per sapere quanti episodi ha ogni stagione (episodi
mancanti, pack di stagione, completezza), lo stato della serie, il calendario,
i titoli degli episodi nei nomi dei file, le locandine e le schede di serie e
film. Basta una delle due chiavi:

- **TMDB (consigliato)** — chiave gratuita su themoviedb.org → Impostazioni →
  API. Se è impostata, Gextto usa sempre TMDB, anche quando c'è la chiave TVDB.
- **TVDB (alternativa)** — chiave v4 da thetvdb.com → Dashboard → API keys. Senza
  la chiave TMDB fornisce tutte le funzioni elencate sopra, tranne le tendenze e
  i popolari di **Esplora**, che esistono solo su TMDB. Le chiavi di tipo
  «user-supported» richiedono un abbonamento TheTVDB a pagamento: inserisci il
  suo PIN in *TVDB PIN abbonato*. I titoli degli episodi seguono la *Lingua
  TVDB* (es. `ita`); se un episodio non è tradotto resta il titolo originale.

Senza nessuna delle due chiavi Gextto scarica comunque, ma senza i dati sopra:
gli episodi mancanti si fermano all'ultimo già scaricato e i file prendono
titoli generici («Episodio N»). TMDB e TVDB usano ID diversi: una serie aggiunta
con TVDB conserva l'ID TVDB, e se in seguito imposti la chiave TMDB Gextto
ritrova la serie su TMDB dal nome.

La sessione del browser dura 30 giorni; `/logout` la chiude. Dopo cinque
password sbagliate dallo stesso indirizzo i tentativi vengono bloccati per un
minuto e ogni errore è annotato nel log. Se resti chiuso fuori, avvia Gextto con
la variabile d'ambiente `GEXTTO_AUTH_DISABLE=1`: il controllo è spento finché la
variabile è presente e puoi correggere le impostazioni. Per l'accesso da fuori
casa usa comunque HTTPS (reverse proxy con certificato o VPN), altrimenti la
password viaggia in chiaro.


#### Diagnostica e traduzioni

*Log dettagliati e testi dell'interfaccia.*

**Diagnostica**

| Impostazione | Cosa fa |
|---|---|
| Debug (log dettagliati) | Attiva log dettagliati e diagnostiche periodiche per il debug. |

**Aggiornamenti**

| Impostazione | Cosa fa |
|---|---|
| Controlla gli aggiornamenti | Scarica periodicamente da GitHub le informazioni sull'ultima build (ogni quante ore lo decide «Intervallo controllo aggiornamenti») e, se ce n'è una nuova, mostra «Aggiornamento disponibile». Non invia dati e non aggiorna da solo: l'aggiornamento parte solo da Manutenzione. |
| Intervallo controllo aggiornamenti | Ogni quante ore Gextto controlla se è uscita una nuova build (da 1 a 720, default 6). Il controllo scarica solo un piccolo file da GitHub; «Verifica aggiornamenti» in Manutenzione controlla subito. |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Traduzioni | Esporta/importa in YAML le traduzioni delle stringhe per italiano o inglese; l'import aggiorna o aggiunge chiavi senza cancellare le altre. |

## Appendice B. Riferimento — Integrazioni

La pagina è divisa in gruppi — **Servizi** (Simkl), **Media server** (Jellyfin,
Plex), **Sorgenti** (indexer, FlareSolverr, verifica sorgenti) e
**Collegamenti** (calendario iCal, handler del browser) — con il titolo del
gruppo agganciato in alto mentre scorri.

### Simkl

| Campo / azione | Cosa fa |
|---|---|
| ID client | ID client dell'app Simkl. |
| Giorni calendario | Quanti giorni avanti mostrare nel calendario Simkl. |
| Stato watchlist | Stato assegnato alle serie importate: Da guardare, In visione o Completato. |
| Segna come visto | Segna come visti su Simkl gli episodi scaricati. |
| Avvia accesso / Conferma | Avvia il flusso PIN e conferma il codice mostrato da Simkl. |
| Revoca | Revoca l'accesso e rimuove il token salvato. |
| Importa watchlist | Importa le serie della watchlist Simkl nella libreria. |
| Watchlist / Calendario | Tabelle di sola lettura con la watchlist e le prossime uscite. |

### Jellyfin

| Campo / azione | Cosa fa |
|---|---|
| Jellyfin URL | URL del server Jellyfin (es. `http://127.0.0.1:8096`). |
| Jellyfin API key | API key generata in Jellyfin → Dashboard → API Keys. |
| Jellyfin — mappatura percorsi | Solo se Jellyfin vede la libreria con percorsi diversi (es. Docker): una riga per cartella, `percorso_gextto=percorso_jellyfin`. |
| Test connessione | Verifica che Jellyfin risponda. |
| Aggiorna libreria | Chiede a Jellyfin di aggiornare tutta la libreria. |

### Plex

| Campo / azione | Cosa fa |
|---|---|
| Plex URL | URL del server Plex (es. `http://127.0.0.1:32400`). |
| Plex token | Token `X-Plex-Token` per accedere alla libreria. |
| Plex — mappatura percorsi | Solo se Plex vede la libreria con percorsi diversi (es. Docker): una riga per cartella, `percorso_gextto=percorso_plex`. |
| Test connessione | Verifica che Plex risponda. |
| Aggiorna libreria | Chiede a Plex di aggiornare tutta la libreria. |

Dopo ogni importazione Gextto chiede a Jellyfin e Plex di rileggere **solo la
cartella cambiata** (la stagione o il film appena arrivati), non tutta la
libreria: sul NAS si evita una scansione completa per ogni episodio e il file
compare in pochi secondi. Per Plex la cartella deve stare dentro una delle
cartelle di una sua libreria. Se la richiesta mirata non è possibile (percorso
non riconosciuto, server che la rifiuta) Gextto chiede l'aggiornamento completo
come prima. Se il server gira in Docker e vede i file sotto un altro percorso,
compila la mappatura percorsi.

### Calendario iCal

`/feed/calendar.ics` è un calendario a cui iscriversi da Thunderbird, Google
Calendar, Apple Calendar o dal telefono (link in *Integrazioni → Calendario
iCal*). Le serie arrivano in italiano spesso mesi o anni dopo la prima messa in
onda, quindi il calendario distingue tre cose:

| Voce | Cosa indica |
|---|---|
| 📥 Serie S01E04 | episodio o film **arrivato in libreria**, nel giorno in cui Gextto l'ha scaricato (ultimi 30 giorni). È l'unica data che dice quando è davvero disponibile. |
| 📺 Serie S02E03 · Titolo | **prima messa in onda originale** (TMDB, o TVDB) della stagione in corso, dall'ultima settimana ai prossimi due mesi. Non è la data della versione italiana; ✓ se l'episodio è già in libreria. |
| 🎬 Film | uscita del film monitorato **in Italia** (TMDB: digitale, poi home video, poi cinema; il paese segue la lingua TMDB, es. `it-IT`). Se per l'Italia non c'è ancora una data compare quella originale, indicata come «(uscita originale)». Con la sola chiave TVDB compare la prima uscita del film, sempre come «(uscita originale)». |

Serve la chiave TMDB oppure quella TVDB; il calendario si aggiorna al massimo ogni 30 minuti. Con
l'accesso protetto (vedi *Accesso*) aggiungi `?apikey=<chiave>` all'indirizzo.

### Indexer Torznab

| Campo / azione | Cosa fa |
|---|---|
| Nome | Etichetta dell'indexer (es. `jackett` / `prowlarr` / `indexarr`). |
| URL base | URL base del servizio; Gextto aggiunge il percorso Torznab. |
| API key | Chiave API dell'indexer. |
| Tipo | Rilevato automaticamente, Prowlarr, Jackett o indexarr. |
| Attivo | Abilita o disabilita l'indexer. |
| Verifica | Testa l'indexer (per Jackett e indexarr usa `t=caps`). |

Un indexer Torznab **diretto** (non gestito da Jackett, Prowlarr o indexarr), per esempio il servizio
`mircrew-indexer` sulla stessa macchina o in LAN, si aggiunge qui con l'indirizzo
base (es. `http://192.168.1.10:9118`) o con quello che finisce in `/api`: Gextto
usa `<base>/api`, la stessa regola di gx-torrent, quindi l'indirizzo da scrivere
è identico nei due. Le ricerche automatiche hanno un budget di **100 secondi per titolo**: un
indexer dietro Prowlarr può attendere fino a **90 secondi** (Prowlarr serve una
ricerca alla volta per indexer), un indexer diretto, Jackett o indexarr fino a **45**. Una
lane la cui coda supererebbe il budget viene saltata senza contare come errore,
così un tracker lento non ferma il ciclo. Se un indexer su un indirizzo locale o di rete privata chiude la
connessione o non risponde, Gextto **non** ritenta via FlareSolverr (Cloudflare
non protegge un servizio in LAN): l'errore resta visibile così com'è. Per
`mircrew-indexer` *Salute → Stato provider* mostra se il servizio risponde e se
il suo login al forum MirCrew è riuscito; se non lo è, rifai il login dalla web
UI del servizio.

Gextto legge dal manager l'elenco degli indexer e le loro capacità (le *caps*
Torznab; per Prowlarr l'elenco dei suoi indexer, riletto ogni 10 minuti) e invia
solo i parametri che il singolo indexer dichiara. Per esempio l'aggregato «all» di Jackett non
accetta l'ID TMDB nelle ricerche di serie e film, e gli indexer pubblici di
Prowlarr non cercano per ID: in questi casi la ricerca usa titolo, stagione ed
episodio invece di fallire o tornare vuota. Se un indexer rifiuta comunque un
ID, la ricerca viene ripetuta subito senza; l'errore mostrato in *Salute* riporta
la spiegazione dell'indexer (per esempio «HTTP 400: Torznab 100: Invalid API
Key») invece del solo codice HTTP.

### FlareSolverr

| Campo / azione | Cosa fa |
|---|---|
| URL | URL del servizio FlareSolverr usato per superare Cloudflare. |
| Test FlareSolverr | Verifica che FlareSolverr risponda. |

### Hook eventi (Configurazione → Notifiche)

| Campo / azione | Cosa fa |
|---|---|
| Nome | Etichetta dell'hook. |
| Attivo | Abilita o disabilita l'hook. |
| Eventi | Eventi che attivano l'hook (vuoto = tutti). |
| Programma | Eseguibile da lanciare (senza shell). |
| Argomenti | Argomenti con segnaposto `{title}`, `{hash}`, `{path}`, `{series}`, `{episode}`; ogni segnaposto resta un solo argomento anche con spazi. Gli stessi valori sono variabili `GEXTTO_*`. |
| Timeout (s) | Timeout in secondi (default 60, massimo 24 ore; `0` = default). |

### Handler del browser e verifica sorgenti

| Voce | Cosa fa |
|---|---|
| Gestore magnet / torrent | Scarica gli script per aprire magnet e file `.torrent` direttamente in Gextto. |
| Magnet / Torrent `.desktop` | Versioni `.desktop` per l'integrazione con il desktop Linux. |
| `install.sh` | Scarica lo script di installazione. |
| Verifica sorgenti / Aggiorna | Controlla feed, indexer e motori web; con una ricerca misura anche i risultati, senza cambiare le impostazioni. |

## Appendice C. Riferimento — Manutenzione

### Azioni rapide

| Azione | Cosa fa |
|---|---|
| Backup ora | Crea subito uno snapshot di backup dei database. |
| Pulisci trash | Svuota completamente il cestino (azione forzata: `trash_retention_days` non si applica). |
| Ricalcola punteggi | Ricalcola lo score delle release archiviate con i pesi attuali. |
| Scansiona archivi | Rilegge le cartelle archivio e aggiorna la libreria. |
| Aggiorna MediaInfo | Analizza con `ffprobe` i file archiviati senza MediaInfo. |
| Rinomina tutto | Rinomina tutti i file archiviati secondo il formato configurato. |
| Housekeeping | Pulizia dei dati tecnici e dello storico, senza toccare la libreria. |
| Riavvia servizio | Riavvia il daemon Gextto. |

### Rinomina contenuto cartella

| Voce | Cosa fa |
|---|---|
| Cartella / Sfoglia | Inserisci o scegli dal server la cartella da analizzare. |
| Scansiona e proponi | Analizza ricorsivamente i video, riconosce serie/film con TMDB/TVDB e propone i nuovi nomi (solo anteprima). |
| Scelta risultato per riga | Seleziona un risultato TMDB/TVDB alternativo. |
| Accetta / rifiuta | Approva o scarta la singola proposta. |
| Accetta tutte le proposte | Approva in blocco le proposte. |
| Applica selezionate | Rinomina i file selezionati realmente presenti e con destinazione valida. |
| Progresso rinomina | Avanzamento dell'operazione in background. |

### Libreria

| Voce | Cosa fa |
|---|---|
| Duplicati video — Anteprima duplicati | Elenca i duplicati inferiori (risoluzione più bassa) senza eliminare nulla. |
| Duplicati video — Pulisci duplicati | Sposta nel cestino le copie inferiori trovate. |
| Ottimizzazione database — VACUUM / ANALYZE | Compatta i database e aggiorna le statistiche del query planner. |
| Ottimizzazione database — Aggiorna dimensioni | Rilegge dimensioni e stato dei file database. |
| Ottimizzazione database — Verifica integrità e indici | Controlla integrità, chiavi esterne e indici (compreso FTS dell'archivio). |
| RAM disk | Mostra i `tmpfs`/`ramfs` scrivibili, permette di sceglierne uno, crearne uno dedicato e aggiornare l'elenco. |

### Pulizie

| Voce | Cosa fa |
|---|---|
| Cestino — Apri cestino | Mostra gli elementi nel cestino per eliminarli singolarmente. |
| Cestino — Svuota cestino | Elimina tutti gli elementi del cestino e mostra un avviso con i file rimossi e lo spazio liberato. |
| Pulizia database — Cicli da conservare | Numero di statistiche dei cicli da mantenere. |
| Pulizia database — Giorni errori | Conserva le schede dei torrent in errore per almeno N giorni prima di rimuoverle. |

### Log

| Voce | Cosa fa |
|---|---|
| Log gx-torrent | Ultime righe del log del motore torrent (`gx-torrent/gx-torrent.log`), con filtro testo, solo avvisi/errori, numero di righe e scelta tra file attivo e rotazioni. |

### Backup

| Voce | Cosa fa |
|---|---|
| Backup ora | Crea subito un archivio di backup, senza aspettare l'intervallo o l'orario programmato. Lo stesso comando è tra le Azioni rapide. Mentre lavora mostra un avviso e resta disattivato. |
| Salva impostazioni | Salva le impostazioni di backup della sezione (conservazione, programmazione, FTP, cartella cloud, percorsi extra, Telegram): non crea nessun archivio. |
| Backup da conservare | Numero di file ZIP locali da conservare nella cartella `backups`. |
| Intervallo (ore) | Intervallo del backup automatico in ore (0 disattiva l'intervallo). |
| Orario (HH:MM) | Orario locale giornaliero del backup automatico; se impostato ha la precedenza sull'intervallo. |
| FTP host | Host/indirizzo del server FTP (vuoto = nessun invio FTP). |
| FTP utente | Utente del server FTP. |
| FTP password | Password FTP (non visualizzata; vuoto = mantiene quella salvata). |
| FTP percorso | Cartella remota di destinazione dello ZIP. |
| Cartella cloud | Percorso locale di una cartella già sincronizzata da un servizio cloud. |
| File e cartelle da includere | Percorsi assoluti di file o cartelle da aggiungere allo ZIP (uno per riga). Finiscono nella cartella `extra/` dell'archivio, con il loro percorso originale; i percorsi che non esistono più vengono saltati. |
| Invia su Telegram | Invia una copia su Telegram usando il bot configurato. |
| Test FTP | Verifica connessione, login, percorso remoto, upload di prova e rimozione. |
| Backup disponibili / Verifica | Tabella degli snapshot con etichetta, nome, dimensione e data; il pulsante Verifica controlla l'archivio, avvisa mentre lavora e conferma quando lo ZIP è integro. |
