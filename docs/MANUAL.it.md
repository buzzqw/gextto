# Manuale utente di Gextto

Questa è la guida operativa per la UI web e la TUI terminale di Gextto. La UI è
disponibile in italiano, inglese, tedesco, francese, spagnolo e polacco; qui sono usate le etichette italiane.
Per le etichette inglesi consulta [MANUAL.en.md](MANUAL.en.md). La UI tedesca
francese, spagnola e polacca usano i rispettivi cataloghi incorporati e, per la
documentazione estesa, il manuale inglese come fallback. La pagina **Manuale**
dell'app segue la lingua selezionata nell'intestazione.

> [!IMPORTANT]
> Inizia in **dry-run**. Verifica percorsi, accesso alle sorgenti e un titolo di
> prova prima di abilitare i download. La UI è amministrativa e senza
> autenticazione: lasciala in una rete fidata o proteggila con firewall e
> reverse proxy HTTPS autenticato.

## Come usare questa guida

Gextto è un demone unico: ricerca le sorgenti, valuta le release, gestisce
libtorrent, rinomina i file e li archivia. Non è necessario avviare componenti
separati per il funzionamento normale.

L'interfaccia web funziona anche da telefono: sotto i 900 px il layout diventa
compatto, con navigazione in alto scorrevole, metriche della dashboard su due
colonne, finestre a tutta larghezza e tabelle che scorrono orizzontalmente
dentro il loro pannello. Ogni azione resta raggiungibile col tocco.

La UI ufficiale si apre su `http://<host>:5000/`; il vecchio prefisso `/v2`
reindirizza alla radice e la vecchia route `/ui` non esiste più. La pagina **Scarico** aggiorna
automaticamente la sessione ogni 5 secondi; il pulsante **Auto: on/off** consente
di disattivare o riattivare il polling. Anche i tile CPU/RAM e le velocità nella
barra superiore vengono aggiornati ogni 5 secondi.

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

La suite corrente esegue 12 controlli axe-core/Playwright sulle sezioni
principali, compresi dialoghi, ordinamento da tastiera, polling e reflow a 320
px. Il risultato non è una dichiarazione di conformità WCAG o normativa: per
un'attestazione servono anche audit manuale con screen reader, tastiera,
ingrandimento e tecnologie assistive, oltre alla valutazione dei requisiti
applicabili. Per il dettaglio vedere
[`accessibility-analysis.md`](accessibility-analysis.md).

Il manuale distingue sempre tra:

- **ricerca manuale**: serve a ispezionare risultati e accodare una scelta;
- **ciclo automatico**: cerca i titoli monitorati e decide cosa scaricare;
- **archivio**: i file già importati o archiviati nella libreria;
- **sessione torrent**: i download ancora gestiti da libtorrent.

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

**Sicurezza.** La porta web è un'interfaccia amministrativa senza autenticazione.
Lasciala sul loopback oppure limita l'accesso con firewall/reverse proxy prima di
esporla in rete. Nell'installazione standard, `/opt/gextto/gexttod --version`
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

- Se non esiste ancora una data directory, completa la procedura di setup iniziale.
- **Attivo vs dry-run**: in dry-run non partono download reali; abilita la
  *modalità attiva* in *Configurazione → Daemon* solo quando sei pronto.
- Aggiungi serie/film da **Esplora** (TMDB) oppure da **Serie TV / Film → Aggiungi**.

### Procedura consigliata per il primo ciclo

1. In *Configurazione → Percorsi* controlla cartella download, temporanea,
   libreria e cestino.
2. In *Configurazione → Sorgenti* aggiungi una sola sorgente funzionante e premi
   **Verifica**. Aggiungi le altre solo dopo aver validato la prima.
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

- `gexttod --version` — versione installata, build number, marker di release e
  libtorrent inclusa;
- `gexttod --help` — riepilogo d'uso;
- `gexttod --update` — scarica e installa l'ultimo payload;
- `gexttod --config <file>` e `gexttod --dry-run` — usati dal servizio e per le
  prove locali.

#### TUI terminale

La TUI si avvia con `/opt/gextto/gexttod tui` dopo aver avviato il daemon
(sostituisci la directory se l'installazione è personalizzata). È un processo
separato che usa esclusivamente le API HTTP e lo stream SSE del daemon: non
legge direttamente i database e può quindi essere usata anche da una shell SSH.

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang it
GEXTTO_URL=http://host:5000 /opt/gextto/gexttod tui
```

Le opzioni sono `--url`/`-u` e `--lang`/`-l`; la variabile d'ambiente per l'URL è
`GEXTTO_URL`. Le sette schede sono Stato,
Torrent, Log, Salute, Archivio, Mancanti e Blocklist. La guida completa dei tasti,
dei prompt e delle azioni è in [`docs/tui.md`](tui.md).

**Installazione e aggiornamento.** A ogni push su `main` viene pubblicato un
payload Linux `continuous` testato. L'installer ufficiale lo scarica, verifica il
checksum SHA-256 e lo installa senza compilare Go o C++ sul server:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer deve essere eseguito come root e richiede systemd. Usa `GEXTTO_REPO`
e `GEXTTO_RELEASE` per scegliere un repository o una release diversi. Salva i
dati del servizio in `/var/lib/gextto`, il programma in `/opt/gextto` e il token
API generato in `/etc/gextto/gextto.env`. Installer e `gexttod --update`
installano lo stesso payload.

Per un'installazione senza root da un checkout sorgente:

```bash
make build
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

Il servizio per-utente ascolta per default sulla porta 5000 su tutte le
interfacce. Limita l'accesso con firewall o reverse proxy; usa
`loginctl enable-linger "$USER"` se deve rimanere attivo dopo il logout.
`--update` scarica `gextto-linux-<arch>.tar.gz`, verifica il `.sha256`
pubblicato quando presente, prepara i file e poi sostituisce l'eseguibile (con la
UI web inclusa), la `lib/` e `run.sh` con rename atomici. Dati e configurazione in
`GEXTTO_DATA_DIR` (default `/var/lib/gextto`) non vengono mai toccati: un
download, un checksum o un'estrazione falliti lasciano l'installazione in
esecuzione invariata, e uno swap fallito viene ripristinato. Il marker `VERSION`
accanto all'eseguibile viene aggiornato ed è mostrato da `--version`.

Il repository ufficiale pubblica attualmente asset Linux per `x86_64`. Un asset
`aarch64` funziona se fornito da un repository personalizzato tramite
`GEXTTO_REPO`.

- `--channel stable` / `--release <tag>` — scelgono la release da installare;
- `--install-dir <dir>` — installa altrove (default: la directory del binario);
- `--archive <file>` — installa da un archivio locale (offline);
- `--force` — reinstalla anche se la versione è invariata;
- `--no-restart` — non riavviare `gextto.service`.

Esegui `/opt/gextto/gexttod --update` come root per riavviare automaticamente
`gextto.service`; altrimenti stampa il
comando `systemctl` esatto. L'unità systemd non viene sovrascritta, così le
personalizzazioni locali (utente, porte, percorsi) restano. Lo stesso payload è
il pacchetto Linux autonomo descritto nel README (*Pacchetto Linux autonomo*).

## 2. Dashboard

- **Controlli dell'interfaccia** — la barra superiore offre tema, dimensione del
  testo e menu a discesa **Font**. Applica un preset generico, un font locale
  rilevato o una famiglia inserita manualmente all'interfaccia e ai log. La scelta
  resta locale al browser; il rilevamento dei font installati dipende dal supporto
  del browser alla Local Font Access API e può richiedere un'autorizzazione.
- **Ricerca manuale globale** — cerca in archivio + indexer + motori web.
- **Pulsanti ciclo** — avvia un ciclo completo o di un solo dominio (Serie, Film,
  Fumetti) o un backup immediato.
- **Card statistiche** — serie/film configurati, file scaricati, spazio libero,
  magnet in archivio, torrent in sessione, gruppi visti dai feed.
- **Rete e download attivi** — CPU/RAM e sparkline di rete in tempo reale.
- **Consumo e dischi**, **prossime uscite**, **ultimi download**,
  **attività recente** e **ultimi trovati nelle sorgenti**.

### Ricerca manuale dalla Dashboard

La ricerca manuale è utile per capire cosa vede Gextto prima di modificare una
configurazione o avviare un download:

1. inserisci un titolo o una query tecnica;
2. attendi che le sorgenti terminino o che il timeout segnali quelle lente;
3. confronta titolo, sorgente, qualità, dimensione e seed/peer;
4. usa **Filtra i risultati già caricati** per restringere localmente la lista;
5. apri **Perché non questa?** sui risultati interessanti;
6. usa **Accoda** solo dopo aver controllato il motivo e il confronto archivio.

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

## 3. Scarico

- **Aggiungi** un magnet/URL `.torrent` oppure carica un file `.torrent`;
  opzionalmente percorso di salvataggio, “Scarica subito” e “Non rinominare”.
- **Download in sessione**: velocità download/upload, numero torrent e peer;
  l'elenco si aggiorna automaticamente ogni 5 secondi.
- I download HTTP dei fumetti mostrano stato, progressione, byte scaricati e
  velocità nella stessa lista dei torrent.
- **Filtro per tag** e **limiti di velocità temporanei** (DL/UL per N minuti).
- **Colonne tabella**: Nome, Stato, Progresso, ↓, ↑, ETA, Peers, Ratio — clicca
  l'intestazione per ordinare. **Azioni bulk**: pausa, riprendi, recheck, rimuovi.
- **Azioni riga**: pausa/riprendi, recheck, dettagli, rimuovi.
- **Dettagli** (tab Generale, Tracker, Contenuto, Peers, Limiti, Storage):
  copia magnet, limiti per torrent/giorni di seed, reannounce, pin, riavvia,
  segna come fallito, sposta storage. In **Generale** trovi anche **Esporta
  .torrent**, **Super seeding** e l'aggiunta/rimozione di **web seed**; in
  **Tracker** puoi modificare l'intera lista (`tier|url` per riga); in
  **Contenuto** imposti la **priorità per file** (Salta/Normale/Alta/Massima).
- Quando usi **Sposta storage**, il log registra richiesta, destinazione e
  accettazione del comando; l'esito finale viene scritto quando libtorrent
  completa o rifiuta lo spostamento. Con **Check** il log distingue comando
  avviato e controllo terminato, includendo stato e byte verificati.
- **Storico download**: elenca i download conclusi (nativi e migrati). Colonne:
  nome (con badge **NAS** quando il file è archiviato), tipo/stagione/episodio,
  **tag NAS** (regola di cartella), score, stato (*Completato*), **percorso
  libreria/NAS** e data di conclusione.

### Torrent stalled

Quando un torrent non aumenta i byte completati per il tempo configurato, passa
allo stato **stalled**: Gextto lo mette in pausa anche in libtorrent, quindi non
occupa più gli slot attivi. Il torrent resta nella sessione; al tentativo
successivo viene ripreso e riannunciato. I valori sono in *Configurazione →
libtorrent*:

- **Considera stalled dopo** — default 60 minuti;
- **Retry stalled** — default 60 minuti;
- **Rimozione stalled** — default 20160 minuti (14 giorni), `0` = mai.

La presenza di peer senza aumento dei byte non resetta il timer. Nel log (in
inglese) cerca le righe `is stuck at`, che indicano anche il motivo, poi
`is still stuck` a ogni nuovo tentativo e `is downloading again` quando
riparte; solo dopo il limite finale compare `Gave up on`.

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
limiti di seed. Un torrent rimosso dalla sessione non equivale necessariamente a
un file rimosso dalla libreria.

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
- Un programma che modifica il video **sul posto** (riscrive i byte nello stesso
  file, come alcuni tagger o `mkvpropedit`) cambierebbe anche il file in seed.
  Gextto non lo fa; se usi strumenti del genere sulla libreria, disattiva
  l'opzione.

Si disattiva con *Configurazione → Seed e completamento → Hardlink invece della
copia durante il seed*.

Il Cestino deve essere configurato in *Configurazione → Percorsi*. Se non c'è una
destinazione NAS, Gextto non tratta la cartella Download come archivio e non la
sposta automaticamente nel Cestino. Lo spostamento automatico della cartella
sorgente nel Cestino avviene solo con NAS configurato, con una destinazione
**Cestino** valida e con la rimozione automatica dei completati attiva; se il
Cestino non è configurato i file sorgente non vengono mai cancellati
automaticamente (viene rimossa solo la voce dalla sessione torrent).

## 4. Serie TV

Aggiungi una serie via ricerca TMDB o manualmente (titolo, qualità, lingue,
stagioni, alias, esclusioni, percorso NAS, sottotitoli, timeframe).

- La lista mostra Nome, Stagioni, Qualità, Lingua, Episodi (scaricati/totali),
  Completezza, Ultimo download; filtrala con la casella di ricerca.
- Azioni bulk: imposta lingua, elimina selezionate.
- **Dettaglio serie**: locandina/trama/cast, badge (anno, rete, stato,
  completezza), link del cast alle schede TVDB/TMDB e, se configurate, un badge
  **“stagioni disattivate”**. Il percorso archivio è visibile nella testata e
  il form di modifica include **Sfoglia** per scegliere una cartella server.
- Azioni: cerca mancanti, scansiona archivio, aggiorna da TMDB, anteprima/esegui
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
3. scegli qualità, lingua, sottotitoli e stagioni da monitorare;
4. imposta il percorso archivio solo se non vuoi usare quello globale;
5. salva in dry-run e controlla la pagina di dettaglio.

I campi più importanti sono:

| Campo | Effetto |
|---|---|
| Qualità | restringe le release accettabili e partecipa allo score |
| Lingua | richiede la lingua configurata quando è riconoscibile nel titolo |
| Sottotitoli | gestisce la preferenza senza accettare sottotitoli hardcoded |
| Stagioni | decide quali stagioni sono monitorate; gli intervalli sono supportati |
| Alias | aiuta a collegare titoli diversi alla stessa serie |
| Esclusioni | parole nel titolo che devono bloccare la release |
| Consenti aggiornamenti | abilita o blocca la sostituzione di file già archiviati |

Lascia le stagioni non ancora disponibili abilitate se vuoi che il calendario e
la ricerca dei mancanti continuino a funzionare. Usa *Ignora* solo per episodi
che non vuoi più cercare: riattivandoli tornano candidati nei cicli successivi.

### Episodi mancanti e pack

Da **Cerca mancanti** o dalla scheda episodio:

1. controlla che la stagione sia monitorata;
2. avvia la ricerca del singolo episodio o del dominio serie;
3. confronta release singole e season pack;
4. se un episodio è già sul disco ma non nel database, usa la scansione archivio;
5. usa **Forza** solo per un’azione manuale consapevole.

Gextto evita normalmente di riscaricare episodi vecchi quando possiede episodi
successivi, a meno che si tratti di un buco riconosciuto o di un upgrade reale.
Un season pack può riempire più episodi, ma il confronto con l’archivio resta
episodio per episodio. Quando nello stesso ciclo ci sono un episodio singolo e
un pack che lo contiene con lo stesso punteggio, Gextto sceglie la release che
copre più episodi (a parità di punteggio un REMUX resta comunque preferito).

## 5. Film

- Tab **Monitorati / Scaricati**; colonne ordinabili (nome, anno, qualità, lingua).
- L'editor gestisce qualità, lingua base, sottotitoli, esclusioni e **Lingue
  richieste**. Nel form il valore è mostrato in formato leggibile, per esempio
  `ita,eng`, non come JSON interno.
- **Dettaglio film**: locandina, trama, cast, modifica, riscarica, **Cerca subito**,
  nomi del cast collegati alle schede TMDB, tabella dei “migliori trovati” dalle
  sorgenti e tabella **corrispondenze archivio** dove ogni riga offre **Perché
  non questa?**.

### Aggiungere e scegliere un film

1. cerca il film in **Esplora** e controlla anno e titolo originale;
2. aggiungilo alla libreria;
3. imposta qualità, lingua, sottotitoli ed eventuali esclusioni;
4. usa **Cerca subito** per vedere i risultati senza aspettare il ciclo;
5. apri **Perché non questa?** prima di accodare una release borderline.

Il film viene identificato usando titolo e anno quando disponibili. Evita di
creare duplicati con lo stesso film scritto in modi diversi: correggi i metadati
del film monitorato invece di aggiungerlo nuovamente.

Le lingue richieste sono inserite nel form come codici separati da virgola, per
esempio `ita,eng`. Il database/API può conservarle nella forma JSON canonica,
ma la UI converte il valore in una forma leggibile prima di mostrarlo. Le regole
interne distinguono requisiti obbligatori e preferenze opzionali; non modificare
manualmente il JSON se non stai usando direttamente l'API.

## 6. Esplora, Archivio, Fumetti

- **Esplora** — TMDB: tendenze oggi/settimana, popolari, più votati, in
  programmazione, prossime uscite; ricerca TMDB; ricerca release generica;
  aggiunta alla libreria. Le card sono mostrate in una griglia a cinque colonne;
  un titolo già in libreria porta il badge **Già in lista** e il pulsante di
  aggiunta è disattivato, così non crei duplicati per errore.
- **Archivio** — ricerca full-text delle release passate con paginazione, accoda
  in blocco, copia magnet, elimina; tab **Serie/Film dal feed**. La colonna
  **Sorgente** mostra solo il provider/dominio (l'URL completo è nel tooltip) e
  ogni riga offre **Perché non questa?** come nella tabella dei risultati.
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
  reinvia/elimina/forza. I download HTTP diretti compaiono in **Download in
  sessione** con stato, byte, progressione, velocità e azioni pausa/riprendi.

## 7. Configurazione

Tab: **Daemon, Sorgenti, libtorrent, Motore torrent, Punteggi, Rinomina, Avanzate,
Acquisizione, Notifiche, Percorsi, Traduzioni**. Le modifiche non salvate sono
evidenziate con la barra “Salva tutte”. L'elenco completo di ogni voce, tab per
tab, è nell'[Appendice A](#appendice-a-riferimento--configurazione).

- **Sorgenti** — lista feed RSS, indexer (Jackett/Prowlarr) con pulsante
  *Verifica*, URL FlareSolverr + test, motori web, filtri contenuto, blacklist.
  Per Jackett puoi indicare l'URL base, ad esempio `http://host:9117`, e la
  relativa API key: Gextto costruisce l'endpoint Torznab
  `/api/v2.0/indexers/all/results/torznab/api`. Il controllo di salute usa
  `t=caps` con la stessa API key, quindi verifica l'API e non soltanto la home di
  Jackett. Nei risultati la sorgente può apparire come `jackett:NomeTracker`.
- **libtorrent** — connessioni/prestazioni, protocolli/tracker, sicurezza,
  RAM disk e porte, limiti di velocità e scheduler; applica/ottimizza/verifica
  aggiornamenti. Se sposti lo storage di un torrent in una cartella che
  **contiene già i dati**, non viene rifiutato: Gextto associa il torrent ai file
  esistenti e li ricontrolla (seed) invece di riscaricarli. L'opzione
  **Prealloca lo spazio su disco** (attiva di default)
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
  o rimuovi il gruppo e assegna un bonus/penalità) e simulatore live. Il nome del
  gruppo deve coincidere con il tag release finale, per esempio `TBK`. La lingua
  audio è un requisito di ammissibilità per titolo, non un bonus fisso per
  l'italiano: si può quindi preferire l'inglese. I sottotitoli preferiti danno
  invece un piccolo bonus opzionale, senza rendere la release obbligatoria. Il
  punteggio effettivo è unico per acquisizione, ricerche, upgrade,
  post-processing, archivio e rescore; comprende anche il bonus dimensione. Usa
  **Manutenzione → Ricalcola scoring** dopo aver cambiato i pesi.
  - **Rinomina** — abilita rinomina, editor del template con token e anteprima,
  chiavi TMDB/TVDB, lingua e soglie di upgrade. La verifica recupera il
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
   anche i backoff delle sorgenti scaduti. Alla fine i database vengono
   compattati con `VACUUM`.
  La retention dell'Archivio è separata e si trova in **Configurazione →
  Avanzate**.
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

### Motore torrent (integrato, qBittorrent-nox)

Gextto possiede sempre database, coda, punteggi, post-processing, rinomina e
archivio; è sostituibile solo il piano di trasferimento, scelto in
*Configurazione → Motore torrent*. Un torrent è di un solo motore alla volta,
quindi il passaggio è una migrazione controllata, non due client sugli stessi
dati.

- **libtorrent integrato** (default) — la sessione inclusa, nello stesso processo;
  valgono tutte le voci *libtorrent*.
- **qBittorrent-nox** — Gextto pilota un qBittorrent-nox esistente tramite la sua
  Web API. Imposta URL, utente/password, categoria, tag e intervallo di polling, e
  le **mappature percorsi** `locale=remoto` quando i due processi vedono percorsi
  diversi; senza mappature Gextto verifica che i percorsi necessari esistano in
  locale. Se un percorso non è traducibile, l'attivazione viene rifiutata, così
  uno spostamento non può finire nella cartella sbagliata. La **modalità gestita**
  scarica l'ultima release statica di qBittorrent-nox, la installa nella cartella
  dell'applicazione (accanto a `gexttod`), la avvia/ferma col servizio e la
  aggiorna con backup e rollback; un watchdog la riavvia dopo un'uscita inattesa
  e, dopo ripetuti crash, torna al motore integrato. Se qBittorrent è spento
  all'avvio, Gextto continua a riprovare invece di fallire.
Se la configurazione salvata seleziona un backend non più supportato, Gextto
torna al motore integrato e registra un avviso nel log.

Il riquadro di configurazione mostra il motore attivo, il suo stato e un test di
raggiungibilità/percorsi, così puoi validare un backend prima di passare. Quale
che sia il motore attivo, la schermata **Scarico** e tutte le automazioni restano
identiche.

### Prestazioni: RAM e CPU

La logica del daemon è trascurabile; con libtorrent attivo il costo è quasi tutto
del motore finché ha torrent in sessione. Se usi un motore esterno
(qBittorrent-nox), quel costo vive nel suo processo. Per ridurre RAM e CPU:

- **RAM** — i valori che contano sono la **cache disco** (`cache_size`, blocchi
  da 16 KiB) e `max_queued_disk_bytes`. Il pulsante **Ottimizza** (o
  *Ottimizzazione continua*) li dimensiona in base alla RAM; puoi anche usare i
  valori suggeriti da `/api/system/lt_mem_suggest`. Il daemon libera la memoria
  al sistema (`malloc_trim`) dopo i completamenti, dopo ogni ciclo e ogni 15
  minuti, così l'RSS non resta al picco del download.
- **CPU** — attiva la **coda dinamica** e *Non contare i torrent fermi negli
  slot attivi*: i torrent a 0 B/s non occupano slot e quelli **stalled** vengono
  messi in pausa e ritentati invece di girare a vuoto. Se la CPU è occupata,
  riduci `connections_limit` e `aio_threads`. Meno torrent attivi — e nessun
  torrent "morto" senza seeder — significano meno traffico DHT/tracker.
- **Diagnosi** — `GET /api/torrents/{hash}/why` spiega perché un torrent non
  scarica (`dead_swarm`, `no_peers`, `no_connected_seed`, `stalled`, …); *Salute*
  mostra RAM/CPU reali del processo e della sessione.

### Configurare un indexer Torznab

Per Jackett:

1. crea o verifica almeno un indexer dentro Jackett;
2. copia la API key dalla pagina Jackett;
3. in Gextto premi **+ Jackett**;
4. inserisci URL base, ad esempio `http://jackett:9117`, e API key;
5. salva e premi **Verifica**;
6. controlla *Salute → Stato API*.

Gextto usa automaticamente `/api/v2.0/indexers/all/results/torznab/api` per
Jackett. Non aggiungere quel percorso se stai usando l'URL base. Se usi un
percorso Torznab personalizzato già completo, lascialo configurato come endpoint
esplicito. Prowlarr usa invece il proprio endpoint di ricerca e normalmente la
porta `9696`.

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

In *Configurazione → Acquisizione* puoi vedere livello, scadenza e ultimo errore.
Usa il reset per singola sorgente dopo aver corretto la causa; non usarlo per
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
manuali restano sempre permessi). Per congelare un titolo usa **Consenti
aggiornamenti** nella scheda di serie/film.

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
  `force=false`).
- **Duplicati video** — *Anteprima duplicati* e *Pulisci duplicati* trovano i
  file video chiaramente inferiori (risoluzione strettamente più bassa) rimasti
  accanto alla versione migliore nella stessa cartella — es. il vecchio 480p
  accanto al nuovo 1080p — e li spostano nel cestino. Il controllo usa il nome
  del file e i dati tecnici dichiarati nel nome: non confronta il contenuto e
  non calcola hash. A **pari risoluzione**
  viene tenuta la versione nella lingua preferita (*Configurazione → Rinomina →
  lingua predefinita*) e il duplicato che dichiara esplicitamente un'altra lingua
  va nel cestino; i file senza tag lingua restano intatti. I file dei torrent
  ancora in sessione sono protetti e le sottocartelle svuotate vengono rimosse.
  La verifica di rinomina esegue la stessa pulizia automaticamente e la pulizia
  degli upgrade ora rispetta anche i motivi "forti" (risoluzione/sorgente/HDR/
  repack).
- **Ripristina sorgente** — *Ripristina sorgente nei nomi* rimette il token
  `[Source]` (WEB-DL, HDTV, BluRay…) nei nomi archiviati che l'hanno perso,
  recuperandolo dal titolo originale della release nel database. Non inventa la
  sorgente: se è sconosciuta il file resta invariato. Prima l'anteprima, poi
  l'esecuzione; nessun riscaricamento.
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
gli archivi multimediali né lo stato completo della sessione libtorrent. Prima di
un ripristino:

1. ferma o metti in pausa i cicli automatici;
2. conserva una copia del database attuale;
3. verifica data e dimensione del backup;
4. ripristina solo su un'installazione compatibile;
5. controlla percorsi e permessi prima di riattivare i download.

Il ripristino di un database non sposta automaticamente i file multimediali. Se
la libreria è stata spostata, correggi i percorsi o esegui una scansione archivio
prima di avviare upgrade e ricerche mancanti.

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
- **Log web** — stream SSE live con filtro testuale, numero righe e segui/pausa.
  Le righe sono in inglese e in formato esplicito: `data ora  LIVELLO messaggio
  · campo: valore`, con parole chiave evidenziate (NAS, download, sorgenti,
  filtri, errori). Ogni ciclo stampa un **SOURCE REPORT** con l'esito di ogni
  sorgente, le decisioni dei filtri e gli eventi di download (avvio, metadati,
  completamento, eventuale spostamento su NAS e importazione in archivio). I
  messaggi torrent includono sempre nome o titolo leggibile; gli hash torrent
  sono nascosti nel log utente.
- **Grafici** — sparkline CPU/RAM/download/upload/disco/RAM disk e consumo
  giornaliero.
- **Attività** — eventi torrent recenti e download.

### TUI: uso rapido

Nella TUI `1`-`7` o `Tab` cambiano scheda, `r` aggiorna e `?` apre l'aiuto.
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

## 11. Notifiche

Configura Telegram, e-mail (SMTP) o un webhook (con segreto HMAC) e invia un
test. Le notifiche di completamento includono dimensione, tempo di download e
velocità media.

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
| Cambiare il nome senza riscaricare | Anteprima rinomina |
| Correggere un servizio esterno | Salute → sorgente → Verifica |
| Eliminare file inferiori | Anteprima duplicati → Pulizia |
| Salvare configurazione e DB | Backup |

### Glossario

- **Release**: risultato trovato da feed, indexer o motore web.
- **Ciclo**: una passata automatica di ricerca e selezione.
- **Gap**: episodio mancante riconosciuto nell'archivio.
- **Placeholder**: riga temporanea che rappresenta un download non ancora
  completato.
- **Upgrade**: sostituzione di un file archiviato con una release migliore.
- **Backoff**: pausa progressiva delle richieste a una sorgente che fallisce.
- **Seed**: condivisione del torrent dopo il completamento.
- **NAS**: destinazione di rete usata per la libreria o per i percorsi configurati.

## 13. Risoluzione problemi

- **Una sorgente non risponde** — controlla *Configurazione → Sorgenti → Verifica*
  e il pannello stato sorgenti; per Jackett verifica URL base, API key e che
  almeno un indexer sia abilitato in Jackett. Gli errori Torznab vengono mostrati
  anche quando Jackett risponde HTTP 200. I siti protetti da Cloudflare
  richiedono un FlareSolverr funzionante.
- **Jackett è raggiungibile ma mostra errore API** — copia nuovamente la API key,
  controlla che almeno un indexer sia abilitato in Jackett e verifica che l'URL
  inserito in Gextto sia quello base corretto. Non aggiungere due volte il
  percorso `/api/v2.0/indexers/all/results/torznab/api`.
- **Non scarica nulla** — verifica la *modalità attiva*, che serie/film siano
  abilitati, e controlla filtri qualità/lingua e il limite di spazio libero.
- **Un torrent è stalled** — controlla i tre valori in *Configurazione →
  libtorrent*. Il torrent è intenzionalmente pausato e fuori dalla coda attiva;
  attendi il retry oppure usa **Riprendi/Riavvia** manualmente.
- **Una cartella osservata non importa il file** — lascia il file con estensione
  `.torrent` o `.magnet`; Gextto aspetta che dimensione e timestamp restino
  stabili, poi ritenta automaticamente gli errori. Controlla il log del watcher.
- **Un file non viene rinominato** — conviene avere `mediainfo` installato (tag
  tecnici); controlla le impostazioni *Rinomina* e la chiave TMDB.
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
- **Il backup FTP fallisce** — usa *Test FTP*: indica il passo che fallisce
  (connessione, login, percorso remoto, upload, rimozione) e lo registra nel log.
- **Log** — vedi `data/gextto.log` (rotazione a 5 MB) o il viewer nella UI.

## Appendice A. Riferimento — Configurazione

Ogni tab raccoglie le impostazioni modificabili. La colonna *Cosa fa* riprende la descrizione mostrata nella UI.

### Daemon

| Impostazione | Cosa fa |
|---|---|
| Ricerca automatica serie/film (secondi) | Intervallo tra le ricerche automatiche di serie e film, in secondi (21600 = 6 ore). |
| Età massima release (giorni) | Ignora le release più vecchie di N giorni (0 = nessun limite). Le release senza data sono considerate pubblicate oggi. |
| Gap massimi per serie/ciclo | Numero massimo di gap da cercare per serie in un ciclo (0 = illimitato). |
| Gap filling attivo | Attiva il riempimento dei buchi (episodi mancanti) dalle release disponibili. |
| Intervallo deep search (ore) | Ogni quante ore fare una ricerca live mirata sugli indexer per i gap. |
| Deep search massime per ciclo | Numero massimo di ricerche live per ciclo. |
| Ricerca titoli online nel ciclo | Cerca i titoli su indexer durante il ciclo: auto (solo se non ci sono feed configurati), yes (sempre), no (mai, usa solo feed e archivio locale). |
| Attivo | Attiva o disattiva il daemon Gextto: cicli automatici e download. |

### Sorgenti

| Impostazione | Cosa fa |
|---|---|
| Blacklist (una parola per riga) | Parole vietate, una per riga: le release che le contengono vengono scartate. |
| Feed RSS | Lista dei feed RSS da leggere a ogni ciclo (una riga per URL). |
| Motori web | Motori di ricerca web usati dal gap-filling quando feed e indexer non trovano nulla. |
| Filtri contenuto | Le release che contengono queste parole o script (es. [porno]) vengono escluse. |

### Libtorrent

| Impostazione | Cosa fa |
|---|---|
| Client abilitato | Attiva o disattiva del tutto il client libtorrent integrato. |
| Auto-gestione dinamica coda e risorse | Regola automaticamente quanti torrent sono attivi in base al carico. |
| Ottimizzazione continua (periodica) | Applica periodicamente l'ottimizzazione di cache, buffer e coda in base alle risorse. |
| Prealloca lo spazio su disco | Riserva subito tutto lo spazio su disco prima di iniziare il download. |
| Slot download dinamici minimi | Numero minimo di download dinamici. La coda cambia al massimo di uno per volta. |
| Slot download dinamici massimi | Numero massimo di download dinamici. Servono campioni consecutivi coerenti prima di aumentare la coda. |
| Non contare i torrent fermi negli slot attivi | I torrent che non trasferiscono dati non consumano uno slot attivo. |
| Download sequenziale | Scarica i file in ordine sequenziale invece che a pezzi sparsi. |
| Download attivi | Valore base dei download attivi; con la coda dinamica viene adattato a runtime. |
| Seed attivi | Valore base dei seed attivi; con la coda dinamica scende a 1 quando ci sono download in coda. |
| Limite torrent attivi | Valore base del limite di torrent attivi; con la coda dinamica diventa max(base, download + seed + 2). |
| Limite connessioni totali | Numero massimo di connessioni peer simultanee a livello di sessione. |
| Slot upload | Numero di peer non bloccati in upload (-1 = automatico). |
| Half-open limit | Numero massimo di connessioni in fase di apertura (-1 = automatico). |
| Connessioni max per torrent | Limite di connessioni per singolo torrent (-1 = illimitato). |
| Upload max per torrent | Limite di upload per singolo torrent (-1 = illimitato). |
| Thread AIO disco | Thread dedicati alle operazioni su disco (-1 = automatico). |
| Cache disco (blocchi, -1 auto) | Dimensione della cache disco in blocchi (-1 = automatico). |
| Scadenza cache (s) | Secondi di inattività dopo cui un blocco esce dalla cache. |
| Coda alert | Dimensione della coda degli alert di libtorrent. |
| DHT | Abilita la rete DHT per trovare peer senza tracker. |
| PEX | Peer Exchange: scambio peer con altri client. |
| LSD | Local Service Discovery: trova peer nella rete locale. |
| UPnP | Apre le porte del router automaticamente con UPnP. |
| NAT-PMP | Apre le porte del router automaticamente con NAT-PMP. |
| uTP | Abilita il protocollo uTP (UDP) oltre a TCP. |
| Preferisci RC4 | Preferisce la cifratura RC4 sulle connessioni. |
| Annuncia a tutti i tracker | Annuncia a tutti i tracker, non solo al primo di ogni tier. |
| Annuncia a tutti i tier | Annuncia a tutti i tier, non solo al primo. |
| Più connessioni per IP | Permette più connessioni dallo stesso indirizzo IP. |
| Intervallo announce (s) | Intervallo minimo (secondi) tra due announce allo stesso tracker. |
| Connect boost | Numero di tentativi di connessione extra all'avvio del torrent. |
| Nodi bootstrap DHT | Nodi DHT iniziali (host:porta separati da virgola). |
| Cifratura | Politica di cifratura: 0 disabilitata, 1 abilitata, 2 forzata. |
| Applica IP filter | Applica il filtro IP anche ai tracker. |
| IP filter (file/URL) | File locale o URL della lista IP da bloccare. |
| Interfacce listen | Indica dove libtorrent accetta connessioni: 0.0.0.0:6881-6891 per tutte le interfacce, 127.0.0.1:6881 solo in locale, oppure wg0:6881/tun0:6881 per una VPN. Il valore proposto va bene nella maggior parte dei casi. |
| Interfaccia uscente | Killswitch VPN: interfaccia usata per tutto il traffico BitTorrent in uscita. |
| Cartella RAM disk | RAM disk da usare per i download in corso, se disponibile. |
| Usa il RAM disk | Scarica in RAM i torrent che rientrano nella soglia; i più grandi vanno su disco. |
| Dimensione massima per torrent (GB) | Dimensione massima di un singolo torrent ammesso sul RAM disk (GB). |
| Margine libero da mantenere (GB) | Spazio libero da lasciare sul RAM disk una volta completato il download (GB). |
| Spazio minimo libero (byte, 0 = dal margine) | Spazio minimo libero in byte richiesto per usare il RAM disk. 0 = usa il margine configurato. |
| Porta minima | Porta minima della sessione libtorrent (richiede il riavvio del servizio). |
| Porta massima | Porta massima della sessione libtorrent (richiede il riavvio del servizio). |
| Download globale (KiB/s, 0 = illimitato) | Limite globale di download in KiB/s (0 = illimitato). |
| Upload globale (KiB/s, 0 = illimitato) | Limite globale di upload in KiB/s (0 = illimitato). |
| Programmazione velocità attiva | Attiva la fascia oraria con limiti di velocità diversi. |
| Programmazione — ora inizio (HH:MM) | Ora di inizio della programmazione (HH:MM). |
| Programmazione — ora fine (HH:MM) | Ora di fine della programmazione (HH:MM). |
| Programmazione — giorni (0=Lun … 6=Dom, es. 0,1,2,3,4) | Giorni attivi: 0=Lun … 6=Dom (es. 0,1,2,3,4). |
| Programmazione — download (KiB/s) | Limite di download in KiB/s durante la programmazione. |
| Programmazione — upload (KiB/s) | Limite di upload in KiB/s durante la programmazione. |
| Impostazioni libtorrent avanzate | Impostazioni libtorrent avanzate, una per riga nel formato chiave=valore. |
| Azioni del tab | **Ottimizza** calcola cache e buffer in base alla RAM; **Applica ora** riapplica subito le impostazioni alla sessione attiva. |

### Motore torrent

| Impostazione | Cosa fa |
|---|---|
| Motore torrent | Motore torrent attivo (libtorrent integrato o qBittorrent-nox). |
| qBittorrent-nox — URL Web API | URL dell'interfaccia Web di qBittorrent-nox (es. http://127.0.0.1:8080). |
| qBittorrent-nox — utente | Utente dell'interfaccia Web di qBittorrent-nox. |
| qBittorrent-nox — password | Password dell'interfaccia Web di qBittorrent-nox (non visualizzata). |
| qBittorrent-nox — categoria | Categoria applicata ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applicato ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — timeout richieste (secondi) | Timeout in secondi delle richieste HTTP verso qBittorrent-nox. |
| qBittorrent-nox — intervallo polling (ms) | Intervallo in millisecondi tra due letture dello stato dei torrent. |
| qBittorrent-nox — mappatura percorsi | Mappatura dei percorsi tra Gextto e qBittorrent-nox, una per riga (locale=remoto). |
| qBittorrent-nox — scaricato e aggiornato da Gextto | Gextto scarica da sé l'ultima release di qBittorrent-nox, la installa nella cartella dell'applicazione (accanto a gexttod), la avvia e la ferma con il servizio e la aggiorna (con backup e rollback). Il motore in uso però si sceglie dalla voce «Motore torrent»: questa opzione non lo cambia. |
| Azioni del tab | Installa/Ottimizza qBittorrent-nox, Stato qBittorrent-nox, Applica motore e Test connessione. |

### Punteggi

I pesi sono raggruppati in: risoluzione (2160p/1080p/720p/576p), sorgente (BluRay, Remux, WEB-DL, WEBRip, HDTV, DVDRip), codec (H.265, H.264), audio (TrueHD, DTS-HD, DTS, DDP, AC3, 5.1, AAC, MP3), bonus (Dolby Vision, HDR, PROPER, REPACK, REAL). Per ognuno: **più alto = più preferito**. I gruppi custom si aggiungono qui. Usa *Manutenzione → Ricalcola punteggi* dopo aver cambiato i pesi.

### Rinomina

| Impostazione | Cosa fa |
|---|---|
| Rinomina episodi | Rinomina i file scaricati usando i metadati TMDB. |
| Lingua TVDB (es. ita, eng) | Lingua preferita per i metadati TVDB (es. ita, eng). |
| Lingua TMDB (es. it-IT) | Lingua usata per i metadati TMDB (es. it-IT, en-US). |
| Lingua predefinita (es. ita) | Lingua preferita di default per serie e film (es. ita, eng). |
| Cleanup upgrade | Sostituisce versioni inferiori già archiviate con upgrade migliori. |
| Differenza minima score per cleanup | Differenza minima di punteggio per sostituire un file esistente con uno migliore (cleanup). |
| Differenza minima score per upgrade | Differenza minima di punteggio per sostituire un file con un upgrade migliore. |
| TMDB API key | Chiave API TMDB per titoli, poster e metadati. |
| TVDB API key | Chiave API v4 di TheTVDB per ricerca serie e metadati. |
| Formato rinomina | Editor del template con token e anteprima per comporre il nome dei file. |

### Avanzate

| Impostazione | Cosa fa |
|---|---|
| Spazio libero minimo per scaricare (GB) | Spazio libero minimo (GB) sulla cartella download: sotto questa soglia il ciclo non avvia download. |
| Trash — giorni di conservazione (0 = elimina tutto) | Giorni di conservazione per le pulizie non forzate; 0 elimina tutto il contenuto del cestino. Le azioni manuali della UI svuotano sempre subito il cestino. |
| Pulizia automatica archivio | Abilita la pulizia automatica dell'archivio secondo età massima e numero minimo da conservare. |
| Archivio — età massima (giorni) | Età massima delle release in archivio, in giorni (0 = nessun limite). |
| Archivio — mantieni almeno N voci | Numero minimo di release recenti da conservare sempre in archivio. |
| Pagine feed da leggere | Quante pagine di elenco leggere per ogni feed (3 è un buon compromesso). |
| Verifica rinomina (ore) | Ogni quante ore verificare che i file archiviati/rinominati siano ancora presenti. |
| Debug (log dettagliati) | Attiva log dettagliati e diagnostiche periodiche per il debug. |
| Filtri per sorgente | Parole chiave da accettare o scartare per una singola sorgente, con attivazione per riga. |
| Regole tag → cartella | Associa un tag del torrent a una cartella temporanea e a una cartella finale. |
| Event hook | Esegue un programma su determinati eventi (nome, eventi, programma, argomenti, timeout). |
| Cartelle osservate | Aggiunge automaticamente i .torrent/.magnet trovati nelle cartelle indicate (ricorsiva, elimina dopo). |

### Acquisizione

| Impostazione | Cosa fa |
|---|---|
| Delay serie (minuti, 0 = nessuno) | Ritarda l'avvio dei download delle serie di questo numero di minuti. 0 avvia subito. |
| Delay film (minuti, 0 = nessuno) | Ritarda l'avvio dei download dei film di questo numero di minuti. 0 avvia subito. |
| Bypassa il delay sopra questo punteggio (0 = mai) | Se una release raggiunge almeno questo punteggio, ignora il delay configurato. |
| Housekeeping periodico attivo | Attiva la pulizia periodica dei dati tecnici e dello storico. |
| Housekeeping — intervallo (ore) | Intervallo tra due housekeeping automatici, in ore. |
| Housekeeping — statistiche cicli di ricerca conservate | Numero di statistiche dei cicli di ricerca da conservare. |
| Housekeeping — visti nel feed (giorni, 0 = mai) | Elimina le righe storiche delle release viste nei feed più vecchie di N giorni. |
| Housekeeping — storico download (giorni, 0 = conserva) | Elimina dallo Storico download le righe dei torrent rimossi più vecchie di N giorni. |
| Housekeeping — schede errore (giorni) | Elimina le schede dei torrent in errore più vecchie di N giorni (minimo 1). |
| Housekeeping — log ricerche gap (giorni, 0 = mai) | Elimina il log delle ricerche degli episodi mancanti più vecchio di N giorni. |
| Housekeeping — backup upgrade (giorni, 0 = mai) | Elimina i backup dei file sostituiti dagli upgrade più vecchi di N giorni. |
| Backfill MediaInfo automatico | Analizza periodicamente con ffprobe i file già presenti che non hanno ancora MediaInfo. |
| Backfill MediaInfo — intervallo (minuti) | Minuti tra due passaggi del backfill MediaInfo. |
| Backfill MediaInfo — file per volta | Numero massimo di file analizzati in ogni passaggio MediaInfo. |

### Seed e completamento

| Impostazione | Cosa fa |
|---|---|
| Considera stalled dopo (minuti) | Dopo questi minuti senza avanzamento il torrent viene considerato stalled. |
| Retry stalled (minuti) | Intervallo tra i tentativi di reannounce dei torrent stalled. |
| Rimozione stalled (minuti, 0 = disattivata) | Dopo questo periodo senza progresso il torrent viene rimosso automaticamente. Imposta 0 per disattivare completamente la rimozione automatica per stallo. |
| Seed ratio globale (0 = infinito) | Rapporto upload/download dopo cui fermare il seeding (0 = infinito). |
| Seed massimo (minuti, fallback) | Limite di seeding in minuti, usato solo se Seed massimo (giorni) è 0. |
| Seed massimo (giorni) | Limite principale di seeding in giorni; se maggiore di 0 prevale sul limite in minuti. |
| Elimina i completati dopo il seed | Attivo: a fine seed il torrent completato viene tolto dalla sessione (come «Pulisci completati»). Spento: a fine seed il torrent resta nell'elenco come Completato e lo rimuovi tu con «Pulisci completati». Non influisce su dove vengono spostati i file. |
| Sposta gli episodi/pack in archivio (non copiare) | Attivo: al termine del seed la sorgente scaricata viene eliminata (il file resta in libreria). Spento: la sorgente scaricata viene copiata in libreria e mantenuta. |
| Hardlink invece della copia durante il seed | Attivo (predefinito): un file che resta in seed entra in libreria come hardlink, senza occupare spazio due volte; se download e libreria sono su filesystem diversi si copia. Vedi «Hardlink al posto della copia». |

### Notifiche

| Impostazione | Cosa fa |
|---|---|
| Telegram attivo | Invia le notifiche su Telegram. |
| Telegram bot token | Token del bot Telegram (da @BotFather). |
| Telegram chat ID | ID della chat/canale dove inviare le notifiche. |
| Webhook URL | URL del webhook a cui inviare gli eventi. |
| Webhook secret | Segreto HMAC per firmare le richieste al webhook. |
| Email attiva | Invia le notifiche via email. |
| SMTP | Server SMTP nel formato host:porta (es. smtp.gmail.com:587). |
| Email mittente | Indirizzo mittente delle email di notifica. |
| Email destinatario | Destinatari delle email (separati da virgola). |
| Password email | Password/app-password SMTP (non visualizzata). |

### Percorsi

| Impostazione | Cosa fa |
|---|---|
| Cartella archivio | Cartella di archivio predefinita per i contenuti senza percorso dedicato. |
| Cartella trash | Cartella dove vengono spostati i file sostituiti/duplicati (se lasciata vuota usa la sottocartella trash nella cartella dati). |
| Azione cleanup | Cosa fare con i file sostituiti: sposta nel trash o elimina. |
| Cartella download | Cartella di download predefinita per tutti i motori. |
| Cartella temporanea | Cartella temporanea per i download in corso. |
| Copia i file .torrent in | Copia qui i file .torrent dei download (vuoto = nessuna copia). |

### Traduzioni

| Impostazione | Cosa fa |
|---|---|
| Traduzioni | Esporta/importa in YAML le traduzioni delle stringhe per italiano o inglese; l'import aggiorna o aggiunge chiavi senza cancellare le altre. |


## Appendice B. Riferimento — Integrazioni

### Simkl

| Campo / azione | Cosa fa |
|---|---|
| Client ID | Client ID dell'app Simkl. |
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

### Indexer Torznab

| Campo / azione | Cosa fa |
|---|---|
| Nome | Etichetta dell'indexer (es. `jackett` / `prowlarr`). |
| URL base | URL base del servizio; Gextto aggiunge il percorso Torznab. |
| API key | Chiave API dell'indexer. |
| Tipo | Rilevato automaticamente, Prowlarr o Jackett. |
| Attivo | Abilita o disabilita l'indexer. |
| Verifica | Testa l'indexer (per Jackett usa `t=caps`). |

### FlareSolverr

| Campo / azione | Cosa fa |
|---|---|
| URL | URL del servizio FlareSolverr usato per superare Cloudflare. |
| Test FlareSolverr | Verifica che FlareSolverr risponda. |

### Hook eventi (Configurazione → Avanzate)

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
| Magnet / Torrent handler | Scarica gli script per aprire magnet e file `.torrent` direttamente in Gextto. |
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
| Cestino — Svuota cestino | Elimina tutti gli elementi del cestino. |
| Pulizia database — Cicli da conservare | Numero di statistiche dei cicli da mantenere. |
| Pulizia database — Giorni errori | Conserva le schede dei torrent in errore per almeno N giorni prima di rimuoverle. |

### Backup

| Voce | Cosa fa |
|---|---|
| Backup da conservare | Numero di file ZIP locali da conservare nella cartella `backups`. |
| Intervallo (ore) | Intervallo del backup automatico in ore (0 disattiva l'intervallo). |
| Orario (HH:MM) | Orario locale giornaliero del backup automatico; se impostato ha la precedenza sull'intervallo. |
| FTP host | Host/indirizzo del server FTP (vuoto = nessun invio FTP). |
| FTP utente | Utente del server FTP. |
| FTP password | Password FTP (non visualizzata; vuoto = mantiene quella salvata). |
| FTP percorso | Cartella remota di destinazione dello ZIP. |
| Cartella cloud | Percorso locale di una cartella già sincronizzata da un servizio cloud. |
| Invia su Telegram | Invia una copia su Telegram usando il bot configurato. |
| Test FTP | Verifica connessione, login, percorso remoto, upload di prova e rimozione. |
| Backup disponibili / Verifica | Tabella degli snapshot con etichetta, nome, dimensione e data; il pulsante Verifica controlla l'archivio. |
