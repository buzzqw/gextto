# Manuale utente di Gextto

Questa è la guida operativa per la UI web e la TUI terminale di Gextto. La UI è
disponibile in italiano, inglese, tedesco, francese, spagnolo e polacco; qui sono usate le etichette italiane.
Per le etichette inglesi consulta [MANUAL.en.md](MANUAL.en.md). La UI tedesca
francese, spagnola e polacca usano i rispettivi cataloghi incorporati e, per la
documentazione estesa, il manuale inglese come fallback. La pagina **Manuale**
dell'app segue la lingua selezionata nell'intestazione.

> [!IMPORTANT]
> Inizia in **dry-run**. Verifica percorsi, accesso alle sorgenti e un titolo di
> prova prima di abilitare i download. La UI è amministrativa e per
> impostazione predefinita è aperta: lasciala in una rete fidata oppure, per
> l'accesso da fuori, attiva il login (*Configurazione → Accesso e servizi*) e usa HTTPS.

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
  della libreria), *Fonti* (un indexer Prowlarr/Jackett e la chiave TMDB,
  consigliata, oppure in alternativa la chiave TVDB con l'eventuale PIN),
  *Primo titolo* (la ricerca su TMDB, o su TVDB se manca la chiave TMDB, con il
  pulsante per aggiungere) e *Attiva*
  (ciclo automatico e uscita dalla modalità prova). Si può saltare e riaprire
  in ogni momento da `/?view=setup`; un'installazione che ha già serie, film o
  archivio non la mostra.
- **Attivo vs dry-run**: in dry-run non partono download reali; abilita la
  *modalità attiva* (interruttore *Ricerca e download automatici* in
  *Configurazione → Generale*) solo quando sei pronto.
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

- `gexttod --version` — versione installata, build number, marker di release e
  libtorrent inclusa;
- `gexttod --help` — riepilogo d'uso;
- `gexttod --update` — scarica e installa l'ultimo payload del canale installato
  (continuous o stabile); tiene la versione precedente in `gexttod.prev` e la
  rimette da sola se la nuova non parte;
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
dati del servizio in `/var/lib/gextto` e il programma in `/opt/gextto`; un file
opzionale `/etc/gextto/gextto.env`, se presente, viene caricato come variabili
d'ambiente (il token API non è lì: si imposta in *Configurazione → Accesso e
servizi*). Installer e `gexttod --update` installano lo stesso payload. Per
impostazione predefinita il servizio gira con **l'utente che esegue
l'installer**; `--user NAME` (o `GEXTTO_USER`) sceglie un altro utente e
`--user gextto` l'account di sistema isolato e senza login.

Per un'installazione senza root da un checkout sorgente:

```bash
make build          # Go puro (predefinita); 'make build-libtorrent' per libtorrent
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

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
il pacchetto Linux autonomo descritto nel README (*Pacchetto Linux autonomo*).

## 2. Dashboard

- **Controlli dell'interfaccia** — la barra superiore offre tema, dimensione del
  testo e menu a discesa **Font**. Applica un preset generico, un font locale
  rilevato o una famiglia inserita manualmente all'interfaccia e ai log. La scelta
  resta locale al browser; il rilevamento dei font installati dipende dal supporto
  del browser alla Local Font Access API e può richiedere un'autorizzazione.
- **Ricerca manuale globale** — cerca in archivio + indexer + motori web.
- **Ricerca automatica** — avvia un ciclo completo o di un solo dominio (Serie,
  Film, Fumetti); mostra prossima esecuzione, intervallo ed esito dell'ultimo
  ciclo; backup immediato.
- **In sessione** — striscia compatta in cima: velocità, torrent in scarico,
  fermi e in seed, banda degli ultimi 30 giorni e accesso diretto a Scarico.
- **Da controllare** — sotto la striscia: i download **fermi** (con il motivo,
  il prossimo tentativo e quando verranno abbandonati), gli **spostamenti in
  libreria in sospeso** (destinazione e tentativi) e gli avvisi, gli errori e i
  recuperi di pack 🛟 delle **ultime 24 ore**, uno per download. *Storia* apre
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
- **Dettagli** (tab Generale, Tracker, Contenuto, Peers, Pezzi, Limiti, Storage):
  copia magnet, limiti per torrent/giorni di seed, reannounce, pin, riavvia,
  segna come fallito, sposta storage. In **Generale** trovi anche **Esporta
  .torrent**, **Super seeding** (con gx-torrent vale solo a torrent completato e
  riduce di proposito l'upload del seed) e l'aggiunta/rimozione di **web seed**; in
  **Tracker** puoi modificare l'intera lista (`tier|url` per riga); in
  **Contenuto** imposti la **priorità per file** (Salta/Normale/Alta/Massima).
  In **Pezzi** c'è la mappa dei pezzi (verde scaricato, giallo in corso, grigio
  mancante, spento saltato): compare con i motori che la espongono (gx-torrent).
  Con gx-torrent ogni file ha un pulsante **▶** che apre lo **streaming HTTP**
  del file (con Range): i pezzi della parte letta vengono scaricati per primi,
  così puoi iniziare a guardare prima che il download finisca.
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

Il Cestino deve essere configurato in *Configurazione → Archivio e spazio*. Se non c'è una
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
| Niente upgrade | una release migliore non sostituisce più i file già archiviati di questa serie |
| Anime (numerazione assoluta) | per le serie le cui release numerano gli episodi senza stagioni, vedi sotto |

**Anime.** Molte release anime numerano gli episodi dall'inizio, senza
stagioni: `[SubsPlease] One Piece - 1071 (1080p)`, `One Piece Ep 1071 SUB ITA`,
`One.Piece.1071.SUB.ITA`. Con **Anime (numerazione assoluta)** attivo Gextto
riconosce questi titoli per la serie, converte il numero in stagione ed episodio
secondo le stagioni di TMDB, o di TVDB senza chiave TMDB (episodio 30 con stagioni da 12, 12 e 24 = S03E06) e,
cercando un episodio mancante, cerca anche il numero assoluto. Anche
`Titolo S01E1071`, prodotto da alcuni indexer, viene letto come numero assoluto
quando la stagione 1 ha meno episodi. Senza chiave TMDB né TVDB il numero assoluto vale
come episodio della stagione 1. L'opzione riguarda solo le serie marcate:
per le altre i titoli con un numero non vengono mai interpretati così.

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
copre più episodi. A parità di punteggio resta preferito un REMUX e, a ulteriore
parità, vince la release con **seed noti** (uno swarm sano è meno a rischio di
stallo).

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
  aggiunta alla libreria. Le liste esistono solo su TMDB: con la sola chiave
  TVDB la ricerca dei titoli passa a TVDB. Le card sono mostrate in una griglia a cinque colonne;
  un titolo già in libreria porta il badge **Già in lista** e il pulsante di
  aggiunta è disattivato, così non crei duplicati per errore.
- **Archivio** — ricerca full-text delle release passate con paginazione, accoda
  in blocco, copia magnet, elimina; tab **Serie/Film dal feed**. La ricerca
  nell'archivio locale è **immediata**: parte da sola mentre digiti (dai tre
  caratteri in su) e usa l'indice FTS5, quindi non pesa sul database. Per i
  termini più corti (es. `IT`) premi **Invio** o **Cerca**: la soglia vale solo
  per la ricerca automatica, non per quella esplicita. Il filtro `-parola`
  esclude i titoli che la contengono. La casella **Cerca anche nel
  web** estende la ricerca a indexer e motori online: è molto più lenta e per
  questo parte solo quando premi **Cerca**, non a ogni tasto. La colonna
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
  Nei file **CBZ** scaricati direttamente (HTTP o Mega) Gextto aggiunge un
  `ComicInfo.xml` con serie, numero e anno ricavati dal titolo (es.
  «Poison Ivy #41 (2025)»): Komga, Kavita e i lettori su tablet lo usano per
  raggruppare e ordinare gli albi. Un file che ha già il suo `ComicInfo.xml` non viene
  toccato; i fumetti scaricati via torrent restano identici, perché sono in
  seed, e i CBR (RAR) non si possono modificare.

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
   anche i backoff delle sorgenti scaduti. Alla fine vengono compattati con
   `VACUUM` solo i database con almeno il 20% (o 64 MB) di spazio libero da
   recuperare. L'housekeeping gira una volta per intervallo anche se Gextto si
   riavvia: un riavvio non lo fa ripartire.
  La retention dell'Archivio è separata e si trova in **Configurazione →
  Manutenzione automatica → Pulizia archivio release**.
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

### Motore torrent (gx-torrent, qBittorrent-nox, libtorrent opzionale)

Gextto possiede sempre database, coda, punteggi, post-processing, rinomina e
archivio; è sostituibile solo il piano di trasferimento, scelto in
*Configurazione → Motore torrent*. Un torrent è di un solo motore alla volta,
quindi il passaggio è una migrazione controllata, non due client sugli stessi
dati.

- **gx-torrent** (predefinito) — demone BitTorrent in Go puro, avviato e
  sorvegliato da Gextto in un processo separato; non richiede
  `libtorrent-rasterbar`. Imposta **URL Web API**, **indirizzo di ascolto**
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
  IP** si aggiorna all'avvio e poi una volta a settimana. Se il demone non
  riesce a restare attivo (6 avvii anomali in 10 minuti) Gextto torna da solo a
  libtorrent, se è compilato. Supporta il **download sequenziale** e la **prima/ultima parte**
  dei file (si impostano all'aggiunta, o per tutti i nuovi torrent in
  *Configurazione → Code e prestazioni → Modalità di download*; valgono per i torrent
  nuovi). Non supporta torrent **solo-v2** né WebTorrent/WebRTC.
- **libtorrent (build opzionale)** — la sessione libtorrent nello stesso processo
  di Gextto, presente **solo se hai compilato Gextto con libtorrent**
  (`make build-libtorrent`); i pacchetti di release non la includono. Valgono
  tutte le voci *libtorrent*.
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
raggiungibilità/percorsi, così puoi validare un backend prima di passare. Il
pannello **Salute → Motore torrent** (icona 🩺 nella barra) riassume lo stato del
motore attivo (coda, velocità, porta/router, DHT/uTP/LSD, cifratura, proxy,
filtro IP, cache, connessioni in entrata, I/O disco con job in coda e totali di
letture/scritture, totali di sessione). Quale
che sia il motore attivo, la schermata **Scarico** e tutte le automazioni restano
identiche.

### Prestazioni: RAM e CPU

La logica del daemon è trascurabile; il costo del trasferimento è quasi tutto
del motore finché ha torrent in sessione. Con **libtorrent integrato** vive nello
stesso processo di Gextto; con un motore esterno (**gx-torrent** o
**qBittorrent-nox**) vive nel suo processo. Per ridurre RAM e CPU:

- **RAM** — con libtorrent i valori che contano sono la **cache disco**
  (`cache_size`, blocchi da 16 KiB) e `max_queued_disk_bytes`; il pulsante
  **Ottimizza** (o *Ottimizzazione continua*) li dimensiona in base alla RAM.
  Con **gx-torrent** la cache è **adattiva**: il demone la ricalcola da memoria
  disponibile, download/seed attivi e tipo di storage (più grande su HDD/NFS,
  piccola su SSD). Un valore di `libtorrent_cache_size` maggiore di zero è un
  override manuale. Il daemon libera
  la memoria al sistema (`malloc_trim`) dopo i completamenti, dopo ogni ciclo e
  ogni 15 minuti, così l'RSS non resta al picco del download.
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

Il ritardo si imposta in *Configurazione → Generale → Ritardo prima di
scaricare*. Lo stato dei backoff (livello, scadenza e ultimo errore) è in
*Salute → Stato provider*. Usa **Azzera** per singola sorgente dopo aver corretto la causa; non usarlo per
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

- **Aggiornamenti** — versione installata e canale, ultima versione pubblicata e
  l'elenco delle novità (i commit successivi a quello installato, con il link a
  GitHub). **Controlla ora** interroga subito GitHub; **Aggiorna ora** fa un
  backup dei database e chiede l'aggiornamento al servizio di sistema
  `gextto-update`, installato dall'installer: Gextto attende la fine delle copie
  verso la libreria, si riavvia con la nuova versione e il pannello lo segnala
  da solo. Se la nuova versione non parte torna la precedente; il log
  dell'ultimo aggiornamento è in fondo al pannello. Quando c'è una versione
  nuova compare anche il pulsante **Aggiornamento disponibile** sopra **Dona**.
  Su un'installazione da sorgenti il pannello indica di usare `git pull` e
  `make build`.

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
  viene tenuta la versione nella lingua preferita (*Configurazione → Libreria e rinomina →
  Lingua predefinita*) e il duplicato che dichiara esplicitamente un'altra lingua
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
- **Ricerche** — nel riquadro omonimo della pagina Salute c'è lo storico degli
  ultimi cicli: quando, durata, release esaminate, download avviati, errori e
  quante sorgenti non hanno risposto (il motivo compare passando il mouse sul
  numero). Serve a riconoscere una sorgente che fallisce ripetutamente o un ciclo
  diventato più lento. API: `GET /api/last_cycles`.
- **Log web** — stream SSE live con filtro testuale, numero righe e segui/pausa.
  Le righe sono in inglese e in formato esplicito: `data ora  LIVELLO messaggio
  · campo: valore`, con parole chiave evidenziate (NAS, download, sorgenti,
  filtri, errori). Ogni ciclo stampa un **SOURCE REPORT** con l'esito di ogni
  sorgente, le decisioni dei filtri e gli eventi di download (avvio, metadati,
  completamento, eventuale spostamento su NAS e importazione in archivio). I
  messaggi torrent includono sempre nome o titolo leggibile; gli hash torrent
  sono nascosti nel log utente.
- **ID acquisizione e storia dei download** — le righe che riguardano un
  download finiscono con `acq: 7f3a2c`: un ID corto che resta lo stesso
  dall'avvio del download all'arrivo in libreria. Scrivendolo nel filtro del log
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

Configura Telegram, e-mail (SMTP) o un webhook — anche verso Discord, Slack,
ntfy, Gotify o Pushover — e invia un test. Le notifiche di completamento
includono dimensione, tempo di download e velocità media.

### Notifiche di stato (Gextto e gx-torrent)

Oltre agli eventi di download, Gextto notifica i problemi del servizio:

- **riavvio di Gextto** — a ogni avvio dopo il primo, con il numero di avvio e
  da quanto era fermo; se l'arresto precedente non è stato registrato (crash,
  `kill -9`) lo segnala come arresto anomalo;
- **arresto per errore** — quando il servizio termina per un errore fatale;
- **gx-torrent** — uscita inattesa del demone gestito, riavvio dopo l'uscita e
  passaggio al motore libtorrent se non riesce a restare attivo;
- **problemi di salute** — quando il controllo di salute rileva un problema e
  quando torna operativo.

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

- **Una sorgente non risponde** — controlla *Integrazioni → Verifica sorgenti*
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
contenuto. Entrando in Configurazione la barra dei menu principale si nasconde,
così le sezioni non restano schiacciate tra il menu e le voci: il pulsante
**Menu** in alto a sinistra la fa ricomparire sopra il contenuto (Esc per
richiuderla).

Quando una sezione raccoglie più gruppi di impostazioni (per esempio **Ciclo di
ricerca** ed **Episodi mancanti** in *Manutenzione automatica*), il titolo del
gruppo resta **agganciato in alto** mentre scorri, con una barretta colorata
d'accento: l'elenco lungo delle opzioni non ti fa più perdere il filo.

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
  dell'ora e i giorni della programmazione sono sette caselle.
- Le opzioni che dipendono da un interruttore (per esempio i campi della
  programmazione velocità o del RAM disk) **compaiono solo quando l'interruttore
  è acceso**, anche prima di salvarlo.
- Un valore diverso dal predefinito mostra **modificato**, il valore
  *Predefinito* e il pulsante **↺ Predefinito**, che rimette il valore
  predefinito nel campo (va poi salvato).
- La chiave tecnica (es. `refresh_interval`) si vede con **Mostra chiavi
  tecniche**; la ricerca la trova comunque.
- Le impostazioni che il motore torrent attivo non usa sono raccolte, chiuse,
  in **Non usate dal motore attivo** (o nel pannello del motore non attivo):
  restano salvate e tornano attive cambiando motore. Le opzioni rare sono nel
  pannello chiuso **Per esperti**.

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
| Ricerca e download automatici | Attiva o disattiva il daemon Gextto: cicli automatici e download. |
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
| Cartelle osservate | Aggiunge automaticamente i .torrent/.magnet trovati nelle cartelle indicate (ricorsiva, elimina dopo). |

#### Qualità e upgrade

*Come vengono confrontate le release e quando una versione migliore sostituisce quella in libreria.*

I pesi sono raggruppati in Risoluzione, Sorgente, Codec, Audio e Bonus (Dolby
Vision, HDR, PROPER, REPACK, REAL). Per ognuno: **più alto = più preferito**.
Il pannello **Gruppi custom** aggiunge un bonus o una penalità a un release group
(il nome deve corrispondere al tag finale della release, es. TBK). Usa
*Manutenzione → Ricalcola punteggi* dopo aver cambiato i pesi.

**Upgrade e sostituzione**

| Impostazione | Cosa fa |
|---|---|
| Differenza minima score per upgrade | Differenza minima di punteggio per sostituire un file con un upgrade migliore. |
| Smetti di migliorare oltre questo punteggio | Tetto agli upgrade: quando il file in libreria ha almeno questo punteggio non viene più sostituito da release migliori; resta accettato solo un REPACK o PROPER, che corregge una release difettosa. Riferimenti con i punteggi predefiniti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. Si somma alla differenza minima: la prima dice *di quanto* deve migliorare, questa *fino a dove*. Per fermare del tutto gli upgrade di un solo titolo c'è «Niente upgrade» nella sua scheda. «Perché non questa?» indica quando una release è scartata per questa soglia. *(0 = mai)* |
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
| Motore torrent | Motore torrent attivo (gx-torrent, libtorrent integrato o qBittorrent-nox). |

**gx-torrent**

| Impostazione | Cosa fa |
|---|---|
| gx-torrent — URL Web API | URL dell'API di gx-torrent (es. http://127.0.0.1:8890). Gextto lo usa per pilotare il demone. |
| gx-torrent — indirizzo di ascolto (LAN) | Indirizzo di ascolto del demone gestito. Predefinito `127.0.0.1:8890`: pagina web e API restano solo su questo server. Per esporle in LAN usa `0.0.0.0:8890` e imposta `gxtorrent_token` (senza token Gextto avvia il demone in `-insecure`). La porta viene allineata a quella dell'URL. |
| gx-torrent — gestione automatica (cache e coda) | Attiva l'autogestione di gx-torrent: coda dinamica e cache adattiva (dimensionata su memoria disponibile, download/seed attivi e tipo di storage). Predefinito attivo. Disattivalo per fissare a mano cache e slot. |
| gx-torrent — proxy (socks5:// o http://) | Proxy per peer, tracker HTTP e web seed; con un proxy DHT e tracker UDP vengono spenti (non visualizzato). |
| gx-torrent — timeout richieste | Timeout in secondi delle richieste HTTP verso gx-torrent. *(Unità: secondi)* |
| gx-torrent — intervallo polling | Intervallo minimo in millisecondi tra due letture dello stato dei torrent (la coda la gestisce il demone). *(Unità: ms)* |

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
| qBittorrent-nox — mappatura percorsi | Mappatura dei percorsi tra Gextto e qBittorrent-nox, una per riga (locale=remoto). |

**libtorrent integrato**

| Impostazione | Cosa fa |
|---|---|
| libtorrent integrato — client abilitato | Attiva o disattiva del tutto il client libtorrent integrato. |

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
| Limite download globale | Limite globale di download in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* |
| Limite upload globale | Limite globale di upload in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* |

**Programmazione velocità**

| Impostazione | Cosa fa |
|---|---|
| Programmazione velocità attiva | Attiva la fascia oraria con limiti di velocità diversi. |
| Programmazione — ora inizio | Ora di inizio della programmazione (HH:MM). Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — ora fine | Ora di fine della programmazione (HH:MM). Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — giorni | Giorni attivi: 0=Lun … 6=Dom (es. 0,1,2,3,4). *(nessun giorno = mai attiva)* Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — download | Limite di download in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — upload | Limite di upload in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Compare solo con «Programmazione velocità attiva» attivo. |

**Porte e interfacce**

| Impostazione | Cosa fa |
|---|---|
| Porta minima | Porta minima della sessione libtorrent (richiede il riavvio del servizio). |
| Porta massima | Porta massima della sessione libtorrent (richiede il riavvio del servizio). |
| Interfacce listen | Indica dove libtorrent accetta connessioni: 0.0.0.0:6881-6891 per tutte le interfacce, 127.0.0.1:6881 solo in locale, oppure wg0:6881/tun0:6881 per una VPN. Il valore proposto va bene nella maggior parte dei casi. |
| Interfaccia uscente | Killswitch VPN: interfaccia usata per tutto il traffico BitTorrent in uscita. |
| Test porte | Verifica che la porta peer sia in ascolto e che il router la inoltri (il messaggio dice "aperta", "in ascolto ma non inoltrata" o "chiusa"). Disponibile con il motore **gx-torrent**. |

**Ricerca peer e tracker**

| Impostazione | Cosa fa |
|---|---|
| DHT | Abilita la rete DHT per trovare peer senza tracker. |
| PEX | Peer Exchange: scambio peer con altri client. |
| LSD | Local Service Discovery: trova peer nella rete locale. |
| UPnP | Apre le porte del router automaticamente con UPnP. |
| NAT-PMP | Apre le porte del router automaticamente con NAT-PMP. |
| uTP | Abilita il protocollo uTP (UDP) oltre a TCP. |
| Holepunching (BEP 55) | Apre una connessione diretta ai peer dietro NAT tramite un peer che fa da relè (BEP 55). Richiede uTP. |
| Nodi bootstrap DHT | Nodi DHT iniziali (host:porta separati da virgola). |
| Annuncia a tutti i tracker | Annuncia a tutti i tracker, non solo al primo di ogni tier. |
| Annuncia a tutti i tier | Annuncia a tutti i tier, non solo al primo. |
| Intervallo announce | Intervallo minimo (secondi) tra due announce allo stesso tracker. *(Unità: secondi)* |

**Cifratura e filtro IP**

| Impostazione | Cosa fa |
|---|---|
| Cifratura | Politica di cifratura: 0 disabilitata, 1 abilitata, 2 forzata. |
| Preferisci RC4 | Preferisce la cifratura RC4 sulle connessioni. |
| Applica IP filter | Applica il filtro IP anche ai tracker. |
| IP filter (file/URL) | File locale o URL della lista IP da bloccare. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Applica ora | Riapplica subito le impostazioni alla sessione attiva (altrimenti valgono dal ciclo successivo). |
| Filtro IP → Carica / aggiorna ora | Scarica la lista dall'URL configurato (o usa il file locale) e la applica subito al motore attivo, gx-torrent compreso. |

#### Code e prestazioni

*Quanti torrent restano attivi, connessioni, RAM disk e cache.*

**Coda e slot**

| Impostazione | Cosa fa |
|---|---|
| Ottimizzazione continua (periodica) | Applica periodicamente l'ottimizzazione di cache, buffer e coda in base alle risorse. |
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
| Limite connessioni totali | Numero massimo di connessioni peer simultanee a livello di sessione. |
| Slot upload | Numero di peer non bloccati in upload (-1 = automatico). *(-1 = auto)* |
| Half-open limit | Numero massimo di connessioni in fase di apertura (-1 = automatico). *(-1 = auto)* |
| Connessioni max per torrent | Limite di connessioni per singolo torrent (-1 = illimitato). *(-1 = illimitato)* |
| Upload max per torrent | Limite di upload per singolo torrent (-1 = illimitato). *(-1 = illimitato)* |
| Più connessioni per IP | Permette più connessioni dallo stesso indirizzo IP. |
| Connect boost | Numero di tentativi di connessione extra all'avvio del torrent. |

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

**Per esperti**

| Impostazione | Cosa fa |
|---|---|
| Thread AIO disco | Thread dedicati alle operazioni su disco (-1 = automatico). *(-1 = auto)* |
| Cache disco | Dimensione della cache disco in blocchi (-1 = automatico). *(Unità: blocchi; -1 = auto)* |
| Scadenza cache | Secondi di inattività dopo cui un blocco esce dalla cache. *(Unità: secondi)* |
| Coda alert | Dimensione della coda degli alert di libtorrent. |
| Impostazioni libtorrent avanzate | Impostazioni libtorrent avanzate, una per riga nel formato chiave=valore. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Ottimizza | Calcola cache e buffer in base alla RAM; con l'ottimizzazione continua i campi di coda e cache diventano *Auto*. |
| Applica ora | Riapplica subito le impostazioni alla sessione attiva. |

#### Seed e completamento

*Quanto restare in seed, cosa fare a download finito e come gestire i torrent bloccati.*

**Seed**

| Impostazione | Cosa fa |
|---|---|
| Seed ratio globale | Rapporto upload/download dopo cui fermare il seeding (0 = infinito). *(0 = infinito)* |
| Seed massimo | Limite principale di seeding in giorni; se maggiore di 0 prevale sul limite in minuti. *(Unità: giorni; 0 = usa il limite in minuti)* |
| Seed massimo (fallback) | Limite di seeding in minuti, usato solo se Seed massimo (giorni) è 0. *(Unità: minuti)* |

**A download completato**

| Impostazione | Cosa fa |
|---|---|
| Elimina i completati dopo il seed | Attivo: a fine seed il torrent completato viene tolto dalla sessione (come «Pulisci completati»). Spento: a fine seed il torrent resta nell'elenco come Completato e lo rimuovi tu con «Pulisci completati». Non influisce su dove vengono spostati i file. |
| Sposta gli episodi/pack in archivio (non copiare) | Attivo: al termine del seed la sorgente scaricata viene eliminata (il file resta in libreria). Spento: la sorgente scaricata viene copiata in libreria e mantenuta. |
| Hardlink invece della copia durante il seed | Attivo (predefinito): un file che resta in seed entra in libreria come hardlink, senza occupare spazio due volte; se download e libreria sono su filesystem diversi si copia. Vedi «Hardlink al posto della copia». |

**Torrent bloccati**

| Impostazione | Cosa fa |
|---|---|
| Considera bloccato dopo | Dopo questi minuti senza avanzamento il torrent viene considerato stalled. *(Unità: minuti)* |
| Riprova i torrent bloccati ogni | Intervallo tra i tentativi di reannounce dei torrent stalled. *(Unità: minuti)* |
| Rimuovi i torrent bloccati dopo | Dopo questo periodo senza progresso il torrent viene rimosso automaticamente. Imposta 0 per disattivare completamente la rimozione automatica per stallo. *(Unità: minuti; 0 = mai)* |
| Rimuovi i torrent senza seeder dopo | Stallo con zero seeder (dead swarm): rimosso dopo questo periodo, più breve, e la puntata viene ricercata di nuovo subito. Imposta 0 per usare la soglia generale. *(Unità: minuti; 0 = usa la soglia generale)* |

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
| Spazio libero minimo per scaricare | Spazio libero minimo (GB) sulla cartella download: sotto questa soglia il ciclo non avvia download. *(Unità: GB; 0 = nessun controllo)* |

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
| Pulizia automatica archivio | Abilita la pulizia automatica dell'archivio secondo età massima e numero minimo da conservare. |
| Archivio — età massima | Età massima delle release in archivio, in giorni (0 = nessun limite). *(Unità: giorni; 0 = nessun limite)* Compare solo con «Pulizia automatica archivio» attivo. |
| Archivio — mantieni almeno N voci | Numero minimo di release recenti da conservare sempre in archivio. Compare solo con «Pulizia automatica archivio» attivo. |

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
| Event hook | Esegue un programma su determinati eventi (nome, eventi, programma, argomenti, timeout). |

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

**Chiavi API**

| Impostazione | Cosa fa |
|---|---|
| Chiave API (script, TUI, calendario) | Per gli accessi senza browser: header `X-Api-Key: <chiave>` oppure `?apikey=<chiave>` nell'indirizzo. La TUI la legge dalla variabile `GEXTTO_API_KEY`. |
| gx-torrent — token di accesso (pagina e API in LAN) | Nella scheda **Accesso**: segreto condiviso richiesto dalla pagina e dall'API; obbligatorio se il demone ascolta in rete (non visualizzato). |

**Servizi metadati**

| Impostazione | Cosa fa |
|---|---|
| TMDB API key (consigliata) | Chiave API TMDB, gratuita: fonte principale di titoli, poster e metadati. Se c'è, ha la precedenza su TVDB. |
| TVDB API key (alternativa a TMDB) | Chiave API v4 di TheTVDB: senza la chiave TMDB fornisce tutti i metadati. |
| TVDB PIN abbonato | PIN dell'abbonamento TheTVDB, richiesto al login solo dalle chiavi «user-supported». |

#### Metadati: TMDB o TVDB

Gextto usa i metadati per sapere quanti episodi ha ogni stagione (episodi
mancanti, pack di stagione, completezza), lo stato della serie, la numerazione
degli anime, il calendario, i titoli degli episodi nei nomi dei file, le
locandine e le schede di serie e film. Basta una delle due chiavi:

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
| Controlla gli aggiornamenti | Ogni 6 ore scarica da GitHub le informazioni sull'ultima versione e, se ce n'è una nuova, mostra «Aggiornamento disponibile». Non invia dati e non aggiorna da solo: l'aggiornamento parte solo da Manutenzione. |

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
| Nome | Etichetta dell'indexer (es. `jackett` / `prowlarr`). |
| URL base | URL base del servizio; Gextto aggiunge il percorso Torznab. |
| API key | Chiave API dell'indexer. |
| Tipo | Rilevato automaticamente, Prowlarr o Jackett. |
| Attivo | Abilita o disabilita l'indexer. |
| Verifica | Testa l'indexer (per Jackett usa `t=caps`). |

Un indexer Torznab **diretto** (non Jackett/Prowlarr), per esempio il servizio
`mircrew-indexer` sulla stessa macchina o in LAN, si aggiunge qui con l'URL che
finisce in `/api`. Le ricerche automatiche aspettano ogni indexer fino a 45
secondi, così anche una risposta che richiede una ricerca sul forum arriva nello
stesso ciclo. Se un indexer su un indirizzo locale o di rete privata chiude la
connessione o non risponde, Gextto **non** ritenta via FlareSolverr (Cloudflare
non protegge un servizio in LAN): l'errore resta visibile così com'è. Per
`mircrew-indexer` *Salute → Stato provider* mostra se il servizio risponde e se
il suo login al forum MirCrew è riuscito; se non lo è, rifai il login dalla web
UI del servizio.

Gextto legge una volta le capacità di ogni indexer (le *caps* Torznab, per
Prowlarr l'elenco dei suoi indexer, aggiornate ogni 6 ore) e invia solo i
parametri che l'indexer dichiara. Per esempio l'aggregato «all» di Jackett non
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

### Log

| Voce | Cosa fa |
|---|---|
| Log gx-torrent | Ultime righe del log del motore torrent (`gx-torrent/gx-torrent.log`), con filtro testo, solo avvisi/errori, numero di righe e scelta tra file attivo e rotazioni. |

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
