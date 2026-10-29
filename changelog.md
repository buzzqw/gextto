# Changelog di Gextto

Qui raccontiamo cosa cambia in Gextto dal punto di vista di chi lo usa, in ordine
di data. Le voci sono raggruppate per giorno e per effetto visibile: i commit di
sola build, test, documentazione o manutenzione interna non hanno una voce
separata, ma sono rappresentati nella voce della modifica a cui appartengono.
Per il dettaglio tecnico restano i commit e i documenti in `docs/`.

La versione installata è quella mostrata da Gextto (`<serie>.<build>`, per
esempio `1.2.2066`): il terzo numero è sempre la build, il secondo si alza a
mano quando si apre una nuova serie. Il manuale
aggiornato è online: [italiano](docs/MANUAL.it.md) ·
[English](docs/MANUAL.en.md). La versione inglese di questo changelog è
[changelog.en.md](changelog.en.md). Per segnalazioni:
[issue su GitHub](https://github.com/buzzqw/gextto/issues).

## 29 ottobre 2026
- **Un'importazione da eD2k nel log è una riga sola.** Quando un file preso da eD2k entrava in libreria il log scriveva due righe quasi uguali — quella dell'importazione con il nome dell'episodio e quella dello spostamento con il nome grezzo del download — e sembravano due eventi. Ora è una riga sola: dice cosa è entrato in libreria e dove, se ha sostituito una versione precedente, e che il file ha lasciato la cartella incoming di aMule (oppure che ci resta, in modalità copia, o che l'importazione era stata interrotta e ripresa). Niente da configurare.

- **Il controllo della qualità reale dice anche se ha trovato qualcosa.** La riga che ogni tanto riassume il backfill MediaInfo della libreria («Read the real quality of …») diceva solo quanti file aveva letto. Ora dice anche per quanti la lettura ha aggiunto qualcosa che il nome della release non diceva — e quindi per quanti il prossimo upgrade sarà deciso sul file invece che sul nome — oppure che i nomi dicevano già tutto e nessuna decisione cambia. I file che non è stato possibile leggere o che non sono più su disco restano in coda alla riga.

- **Lo stack Docker completo include indexarr.** `docker-full` avvia ora anche indexarr, l'indexer Torznab/Newznab con fonti native, accanto a Jackett, Prowlarr, FlareSolverr e aMule. L'immagine si compila dal sorgente di indexarr messo accanto a quello di Gextto (o dove indica `INDEXARR_SRC` in `docker-full/.env`) e `scripts/docker-full-build.sh` le dà la sua versione. indexarr usa la rete dell'host sulla porta 8788 (`INDEXARR_PORT` per cambiarla): in Gextto si aggiunge come indexer **indexarr** con URL `http://127.0.0.1:8788`. Prima di avviare crea anche `docker-full/data/indexarr` e imposta subito utente e password nella sua UI. La guida Docker spiega configurazione, aggiornamento e backup, compresa la chiave del vault da conservare a parte.

- **I backup non salvano più i download in corso.** Uno ZIP di Gextto non contiene più `gx-ed2k/incoming` (i file eD2k ancora in arrivo) né `gx-torrent/parts` (i pezzi temporanei del motore torrent): sono dati parziali che si riscaricano, e su un download grande potevano pesare quanto il file intero. La configurazione e le cartelle vicine restano nel backup. Vale per i backup automatici, manuali e da riga di comando; i backup già creati non vengono toccati.

- **Un torrent in blocklist non riempie più il log.** Quando Gextto scarta una release perché è in blocklist, la riga «Skipped … it is on the blocklist» passa da INFO a DEBUG: resta visibile alzando la verbosità, ma non compare più a ogni ciclo. Gli altri motivi di scarto restano a INFO.


- **Un upgrade si decide guardando il file che hai, non il nome della release.** Prima di scaricare una versione «migliore» Gextto confrontava il nome della release con il nome del file in libreria: se quel file era stato rinominato senza i suoi attributi — il nome standard tiene solo risoluzione e codec — una release qualsiasi con «2160p DV» nel nome sembrava migliore. Il 29 ottobre è successo a tre file in un giorno: scaricati, copiati in libreria e poi scartati perché il file che c'era già era uguale o migliore. Ora, quando la qualità reale della copia in libreria non è ancora stata letta, Gextto la legge dal file prima di decidere: se non è un vero miglioramento il download non parte, e la lettura resta registrata per le ricerche successive. Se il file non è leggibile o non c'è, la decisione resta quella di prima.

- **Un torrent arrivato al limite di seed non riparte più da solo.** Con gx-torrent capitava che un torrent che aveva raggiunto il limite — ratio o tempo — venisse messo in pausa e ripreso ogni pochi secondi, per minuti: due parti applicavano il limite con regole diverse e si annullavano a vicenda (il 29 ottobre, 24 pause e riprese in cinque minuti). In modalità gestita ora il limite lo applica solo Gextto, e la ripresa di un seed «senza limiti» guarda gli stessi limiti della pausa: una pausa resta una pausa, e il log la racconta con una riga sola.

- **Il log di gx-torrent si legge come quello di Gextto.** Ogni riga del motore torrent ha ora data, livello (INFO, WARN, ERROR, DEBUG) e una frase: prima erano righe senza livello, con la data in un altro formato e valori come `handshake=240 io_timeout=14`. All'avvio una sola riga dice versione, cartelle e indirizzo dell'interfaccia, e dice anche se i trasferimenti sono rimasti attivi; i tracker che smettono di rispondere si contano in una riga invece di una per tracker; i dettagli tecnici — tetto dei peer, cache, rumore degli sciami — si vedono solo avviando il demone con `-debug`. Anche lo spegnimento di Gextto dice quanto aspetta una copia in corso e, se resta altro lavoro, quale.

- **Un'importazione interrotta non butta più via il download.** Se Gextto si fermava — riavvio, arresto, aggiornamento — mentre copiava in libreria un file grande preso da eD2k, al tentativo successivo confrontava il download con la copia che aveva già messo in libreria, la giudicava «non migliore» e spostava nel Cestino il file di aMule, lasciando in libreria un file con il nome grezzo della release e la puntata ancora segnata come non archiviata. Ora quella copia viene riconosciuta come propria (stesso nome, stessa dimensione): l'importazione riprende da lì, il file viene rinominato e registrato e il download resta in aMule a condividere. Vale per gli episodi e per i film.

- **I tracker dietro Cloudflare tornano a funzionare.** Con il filtro IP applicato anche ai tracker (com'era impostato di default) e la lista predefinita, che contiene gli intervalli di Cloudflare, i tracker ospitati lì — molti privati — venivano bloccati prima ancora di essere contattati: nella scheda *Tracker* comparivano come non funzionanti con «unknown error in announce» e dopo un'ora il tracker veniva tolto dalla sessione per sei ore. Ora il filtro IP blocca i peer e, solo se lo riattivi, i tracker: l'opzione *Applica IP filter* in *Configurazione → Rete* è spenta di default. Quando un tracker non risponde la scheda dice il motivo vero — «tracker IP is blocked», certificato TLS rifiutato, proxy che rifiuta la connessione — invece di «unknown error in announce».

- **L'etichetta «seed ∞» compare appena imposti il seed infinito.** Prima si vedeva solo mentre il torrent era in seed: un torrent ancora in scarico impostato con ratio 0 e giorni 0 non la mostrava. Ora segue l'impostazione (ratio o tempo a 0 = seed senza limiti) e si vede anche durante il download.

- **I tracker si aggiungono e si tolgono uno alla volta.** Nella scheda *Tracker* del dettaglio la casella non contiene più l'elenco da riscrivere: si scrivono soltanto i tracker nuovi, che si aggiungono a quelli già presenti, e ogni riga ha il suo pulsante **Rimuovi** che toglie solo quel tracker. Prima bastava salvare la casella con una riga sola per cancellare tutti gli altri tracker del torrent. Vale sia con gx-torrent sia con qBittorrent, e anche l'ultimo tracker di un torrent qBittorrent ora si può togliere.

- **Limiti di un torrent: ogni campo si cambia da solo.** Nella scheda *Limiti* del dettaglio — e anche dalla TUI o dall'API — cambiare una cosa sola non azzera più le altre: mettere i **giorni di seed a 0** (seed infinito) non sposta più il **ratio** al valore globale, e cambiare il ratio non riporta più i giorni a -1. Lo stesso vale per i limiti di velocità: un campo che non viene inviato resta com'è invece di diventare «illimitato». Dopo il salvataggio la scheda mostra subito i valori nuovi. Chi ha un torrent con ratio o giorni sballati li rimette una volta dalla scheda *Limiti*.

- **Un download riscaricato non viene più preso per già finito.** Quando Gextto riscaricava una release che aveva già scaricato in passato — perché il file non è più in libreria, o perché il primo tentativo era fallito — il nuovo download veniva dichiarato finito appena il motore spostava i file, cioè subito dopo l'arrivo dei metadati e prima che fosse scaricato un solo byte. Il controllo di integrità trovava allora il file ancora pieno di zeri, lo metteva nel Cestino e segnava il download come errore: il 29 ottobre è successo a un film, che viene ripreso al prossimo giro. Ora il nuovo tentativo riparte pulito (stato, data di completamento e percorso archiviato del tentativo precedente vengono azzerati) e il file si scarica e si archivia normalmente. Il file vuoto che questo errore ha lasciato nel Cestino si può cancellare: non contiene nulla. Quando la copia in libreria è ancora al suo posto non cambia niente: riaggiungere lo stesso torrent resta un doppione già archiviato e non tocca la libreria.

- **I torrent si mandano dal browser anche con l'estensione di Firefox.** Gextto risponde a un sottoinsieme dell'API di qBittorrent (`/api/v2`) sul proprio indirizzo, quindi un'estensione scritta per qBittorrent — per esempio *Torrent Control* — può mandare magnet e file `.torrent` direttamente a Gextto: il torrent entra nella coda, lo scarica gx-core (o il motore attivo) ed è rinominato e archiviato come gli altri. Nell'estensione si sceglie il client *qBittorrent* e si mette come indirizzo del server quello di Gextto con la porta dell'interfaccia (`http://192.168.1.161:5000/`); utente e password sono quelli dell'accesso protetto, se è attivo. Le etichette/categorie vanno lasciate vuote: Gextto risponde con un errore esplicito invece di ignorarle, perché cambierebbero i tag della coda e le cartelle su disco. Non serve più il `.desktop` con gli handler per questo caso, anche se resta disponibile.

- **La ricerca di una puntata si apre dentro la pagina e dice cosa sta facendo.** Il 🔍 di una puntata non apre più una finestra che copre la serie: i risultati compaiono in un riquadro sotto la puntata stessa (nella scheda *Mancanti*, sotto la riga della stagione), prima le release già in archivio e poi quelle online. Mentre la ricerca online è in corso il riquadro dice a che punto è — quante sorgenti sta interrogando, quante hanno risposto (per esempio 12/106) e quando passa a eD2k/Kad — e a ricerca finita resta lì con il numero di sorgenti interrogate. **Chiudi** richiude il riquadro e riporta il cursore sul pulsante, e la puntata e le righe attorno restano visibili: non si perde il segno.

- **Le puntate già in libreria non spariscono più quando un download viene scartato.** Se un file arrivato da eD2k veniva rifiutato — per esempio perché la copia in libreria era migliore — Gextto annullava anche le righe di *tutte* le puntate arrivate da eD2k: nella scheda *Mancanti* ricomparivano episodi che erano sul NAS, e solo *Scansiona archivio* li riportava a posto. Il 29 ottobre è successo a 1.096 puntate. Ora l'annullamento tocca soltanto la riga del download rifiutato, mai le copie già archiviate. Chi ha già perso le righe le recupera con **Manutenzione → Scansiona archivi** (o con *Scansiona archivio* sulla singola serie). In più, la pulizia dei torrent completati non fallisce più sulla tabella dei film: il codice del torrent viene tolto davvero dalle righe archiviate, che quindi non possono più essere confuse con un download da annullare.

- **Anche le righe della scansione dell'archivio hanno l'icona.** «Archive scan completed», «Archive scan finished with errors» e i file scartati durante la scansione ora iniziano con un'icona come le altre righe del log, quindi non sembrano più attaccate al livello INFO.

- **Un feed si mette in pausa con un `#`.** In *Configurazione → Sorgenti → Feed RSS* una riga che inizia con `#` — con o senza spazio (`# https://…` o `#https://…`) — non viene più letta a ogni ciclo, ma resta nell'elenco: togliendo il cancelletto il feed riparte senza doverlo riscrivere.

## 28 ottobre 2026

- **L'interfaccia non aspetta più la lettura dei torrent.** Le pagine, il grafico del traffico e il controllo della latenza chiedevano l'elenco dei torrent al motore, che prima di rispondere poteva avviare un controllo verso gx-torrent (fino a 15 secondi con il daemon lento). Ora leggono l'ultimo elenco disponibile, che si aggiorna da sé; l'elenco è anche ordinato una volta sola invece di essere riordinato a ogni lettura: su 2.000 torrent una lettura passa da 1,42 ms a 0,6 ms.

- **Il motore spreca meno memoria per ogni pacchetto inviato.** uTP creava un timer, una struttura e una copia dei dati per ogni pacchetto: a 100 MB/s sono decine di migliaia di oggetti al secondo. Ora ogni connessione ha un solo timer che serve tutti i pacchetti in attesa, con la stessa ritrasmissione di prima: nel test di trasferimento in locale le allocazioni per blocco da 16 KiB scendono da 99 a 75 e la velocità resta la stessa (151-159 MB/s).

- **La ricerca dei film già in archivio legge la cartella una volta sola.** Per ogni film candidato Gextto rileggeva tutto l'archivio (con 200 film e 800 file: 17 secondi e 434 MB di memoria per ciclo). Ora legge l'archivio una volta per ciclo e confronta i candidati con quell'elenco: 200 film costano 140 ms.

- **La scansione della libreria non fa più una scrittura per file.** Quando Gextto ricontrolla la cartella di una serie, ogni episodio trovato costava una scrittura su disco separata: su 2.000 file erano circa 3,3 secondi spesi solo a salvare, e su un disco di rete molto di più. Ora i file di una serie si salvano in un'unica scrittura: la stessa scansione scende a circa 60 ms di salvataggio su questo disco, e resta istantanea su un disco in RAM.

- **La ricerca delle puntate mancanti costa meno.** Gextto confronta ogni release con tutte le serie configurate, e prima rielaborava ogni nome e alias della libreria per ogni release: con 500 serie erano millisecondi per release. Ora quell'elenco si prepara una volta sola e si aggiorna quando cambi le serie, quindi il confronto scende a un decimo di millisecondo. Anche il calcolo delle puntate mancanti legge la tabella degli episodi una volta invece di due.

- **L'archivio non blocca più il resto dell'interfaccia.** Mentre Gextto leggeva o contava nell'archivio — una ricerca, la pagina dei filtri contenuti, fino a qualche secondo su un archivio grande — tutte le altre richieste restavano in coda: la pagina si apriva solo quando quella lettura finiva. Ora le letture dell'archivio usano una connessione separata: ho misurato una lettura breve che passa da 116-122 ms a 0,26-0,35 ms proprio mentre una scansione completa è in corso. Nello stesso giro il *Ricalcola punteggi* della manutenzione passa da circa 215 ms a circa 55 ms per 2.000 torrent, e il motore non riordina più la tabella dei pezzi a ogni richiesta dei peer. Nessuna impostazione da cambiare.

- **L'interfaccia pesa meno e risponde prima.** La pagina dei filtri contenuti dell'archivio copiava i testi dei magnet di tutte le 364.348 release a ogni apertura — 131 MB di dati letti per mostrare al massimo 500 righe: ora legge solo i titoli e carica la pagina visibile in una query a parte. La tabella Download non elenca più i torrent due volte a ogni aggiornamento (uno ogni 5 secondi con la pagina aperta), il pannello *Coda titoli* della pagina Salute non esegue più il controllo completo di salute ogni minuto, l'elenco dei torrent usa un indice invece di riordinare tutta la tabella, e le righe di debug di gx-torrent non vengono più costruite quando il log è a WARNING. Nessuna impostazione da cambiare.

- **Il log dice perché un download finisce e che la copia in libreria resta.** Quando un season pack già archiviato finiva il seed e la sua copia di download andava nel cestino, il log scriveva soltanto che la cartella era stata spostata nel cestino: sembrava che fosse stato buttato via tutto il pack. Ora la riga dice quale limite ha chiuso il seed («seed time reached», «ratio reached»…) e che la copia in libreria resta, con il suo percorso. Se lo spostamento nel cestino fallisce, il messaggio dice comunque dove si trova la copia buona. Non c'è nulla da configurare.

- **I Weekly Pack arrivano appena sono scaricabili.** GetComics pubblica la pagina del pack prima dei link di download, e il controllo fumetti gira una volta a settimana: il pack restava in attesa fino al controllo successivo, cioè circa una settimana. Ora a ogni ciclo normale Gextto ricontrolla solo i pack già registrati ma non ancora scaricati e li accoda appena il link compare, senza rifare il controllo dei fumetti. Il pack della settimana del 21 ottobre, fermo dal 23, viene preso al prossimo ciclo.

- **I fumetti monitorati seguono la serie, non solo l'albo scelto.** Un monitor con il numero nel titolo veniva confrontato solo con quel numero: il giro automatico non trovava mai i numeri nuovi, che comparivano soltanto cercandoli a mano. Ora il controllo periodico scarica anche i numeri successivi della stessa serie; numeri precedenti, albi di altre serie e raccolte restano esclusi. Il post scelto in *Aggiungi fumetto* viene scaricato così com'è, e da lì in avanti la serie prosegue da sola.

- **La pagina Salute mostra solo le sorgenti davvero configurate.** Se toglievi un indexer dalle integrazioni, o lo disattivavi, la sua riga restava in *Salute → Stato provider* con l'ultimo errore, come se fosse ancora in uso (per esempio «mircrew indexerr»). Ora la tabella elenca solo gli indexer presenti e attivi e i feed configurati, e la riga di una sorgente rimossa viene dimenticata: se la riaggiungi riparte senza il vecchio backoff. Non c'è nulla da configurare.

- **«Cerca mancanti» non tiene più ferma la scheda della serie.** Il pulsante nella scheda di una serie avviava la ricerca e lasciava la pagina in attesa per minuti, tanto più a lungo quante più puntate mancavano. Ora avvia la stessa ricerca in background della scheda *Mancanti*: la pagina dice a che punto è, si può annullare e, quando finisce, elenca i risultati con **Scarica** restando sulla serie. Anche la ricerca avviata da un programma esterno o dal terminale risponde entro 90 secondi con le puntate già controllate (il campo `complete` dice se ha finito) invece di restare appesa, e una ricerca annullata segna come cercate solo le puntate che ha davvero guardato: il passaggio approfondito non salta le altre per un giorno.

- **La copia fuori profilo compare anche nella ricerca manuale.** Quando per un episodio mancante non esiste nessuna copia nel profilo qualità, l'elenco di *Cerca mancanti* e della singola puntata mostra ora la copia nella lingua richiesta con il badge «🆘 ultima risorsa»: è quella che Gextto prenderebbe da solo, invece di far leggere «nessun risultato» per un episodio che poi scaricava. Lo stesso vale per una copia che si trova solo nell'archivio locale: il ciclo la può prendere come ultima risorsa, prima restava invisibile.

- **Una serie «in produzione» che ha già trasmesso non perde più le puntate mancanti.** Gextto considerava senza buchi ogni serie con stato «In produzione» o «Pianificata», anche se una puntata era già andata in onda: i suoi mancanti sparivano dalla scheda *Mancanti*. Ora lo stato conta solo se la serie non ha mai trasmesso; una data di ultima messa in onda conosciuta lo scavalca.

- **Gextto passa alla serie 1.2.** Da questa build la versione è `1.2.<build>` (la prima è la 1.2.2065); il numero di build continua da dove era. gx-torrent resta nella sua serie. Il controllo degli aggiornamenti confronta commit e build, quindi un'installazione 1.1 vede la 1.2 come aggiornamento normale.

- **I download eD2k non finiscono più tra gli errori a ogni ciclo.** All'inizio di ogni ricerca Gextto confronta i suoi download con quelli del motore torrent e segna come rimossi quelli spariti. Lo faceva anche con i file eD2k, che stanno in aMule e non nel motore torrent: il 28 ottobre 38 episodi ancora in download in aMule sono finiti in errore «missing from session». Gextto smetteva di controllarne il completamento (il file finito sarebbe rimasto nella cartella incoming di aMule) e a ogni ciclo li riproponeva come mancanti. Ora i file eD2k restano ad aMule, e quelli già segnati tornano in coda da soli.
- **Un file che il motore o aMule hanno già non conta più come problema.** Il riepilogo del ciclo diceva «33 problems (see the warnings above)» senza nessun avviso sopra: erano file già in coda. Ora un doppione non è un problema.
- **Meno avvisi ripetuti sui pack.** Mentre gx-torrent sposta i dati di un torrent (sul RAM disk o fuori), le priorità dei file del pack non si possono cambiare: Gextto lo scriveva come avviso ogni 30 secondi (19 volte per uno spostamento di 10 minuti). Ora riprova senza avvisare.

- **La ricerca approfondita dei mancanti fa il giro di tutte le serie.** A ogni giro approfondito Gextto cerca online solo pochi episodi mancanti (5 per default). Li prendeva sempre dall'inizio dell'elenco, quindi con molti mancanti introvabili nelle prime serie in ordine alfabetico le serie più in basso aspettavano settimane il loro turno. Ora vengono prima gli episodi mai cercati, una serie alla volta e ripartendo dalla serie dopo l'ultima servita, poi quelli cercati da più tempo. Così ogni giro tocca serie nuove invece di ricominciare dalla A. Non c'è nulla da configurare.

- **Ultima risorsa per gli episodi mancanti.** Se per un episodio mancante l'unica copia nella lingua giusta ha una risoluzione fuori dal profilo, o non la dichiara (capita spesso con i file eD2k), prima Gextto la scartava e il buco restava aperto. Ora la prende come ultima risorsa, solo quando non c'è nessuna copia nel profilo. Lingua, sottotitoli, audio ed esclusioni valgono sempre. In *Scarico* il download porta il badge «🆘 ultima risorsa»; anche il log, il motivo del download e la notifica lo segnalano. Appena esce una copia nel profilo, l'upgrade la sostituisce come al solito. È attivo per default; si spegne in *Configurazione → Episodi mancanti → Ultima risorsa*. La copia viene presa anche quando lo stesso file arriva pure dalla ricerca normale del titolo (succede con DDUnlimited).

- **Il log dice «→ aMule» per i download eD2k.** Quando Gextto avviava un file eD2k (per esempio da DDUnlimited tramite indexarr), la riga «📥 Downloading» del log indicava la cartella dei torrent, anche «RAM disk», benché il file andasse ad aMule, che usa le sue cartelle. Ora la riga dice «→ aMule». Il download avveniva già nel posto giusto: cambia solo il messaggio. Non c'è nulla da configurare.

- **Le release eD2k di indexarr si scaricano anche in automatico.** indexarr manda per i link eD2k un numero di seed e peer pari a zero, perché sulla rete eD2k l'indexer non può contarli. Gextto li leggeva come «nessuno condivide il file» e, nella ricerca automatica, rifiutava ogni release eD2k di indexarr come morta: si poteva scaricarla solo a mano. Ora per un link eD2k quei numeri vengono ignorati e le fonti le trova aMule, come per i link che Gextto trova da solo. Non c'è nulla da configurare.

- **I download eD2k presi da indexarr compaiono nel suo Log → Download.** Prima Gextto leggeva il link diretto `ed2k` e lo accodava in aMule senza dire niente a indexarr: per indexarr quel download non era mai avvenuto e il contatore eD2k restava a zero. Ora, quando il risultato arriva da un indexer che espone anche un link di download HTTP (indexarr, o Jackett e Prowlarr con lo stesso schema), Gextto lo apre una volta quando il file è in coda, senza far aspettare il download: indexarr registra il download e la fonte che l'ha fornito. Il trasferimento resta il link `ed2k://` diretto; il rinvio non viene seguito e un errore dell'avviso non blocca il download. Con un link `ed2k://` diretto non cambia nulla.

## 27 ottobre 2026

- **I link eD2k arrivano anche da indexarr.** Se in indexarr aggiungi una fonte eD2k (per esempio il forum DDUnlimited), i suoi risultati ora arrivano a Gextto come link `ed2k://` e non vengono più scartati perché non hanno un magnet o un `.torrent`. Gextto li mostra come rete eD2k nella ricerca e nell'archivio, e al momento dello scarico li accoda in aMule come quelli che trova da solo; il client torrent non viene toccato. Non c'è nulla da configurare: basta che la fonte sia abilitata in indexarr e che l'indexer indexarr sia già fra le sorgenti di Gextto. Con indexarr la ricerca aspetta fino a 45 secondi (invece dei 5 delle altre fonti) per dare tempo alla fonte eD2k di aprire le pagine dei topic; Sonarr, Radarr e gli altri programmi non ricevono questa attesa e restano come prima.

- **La ricerca web non taglia più tutte le fonti insieme dopo 15 secondi.** Prima c'era un unico budget di 15 secondi per tutte le fonti web della ricerca, e comprendeva anche l'attesa in coda: con otto motori e quattro posti, la seconda metà poteva non partire nemmeno. Ora ogni motore ha i suoi 15 secondi da quando inizia, quindi nessuno viene escluso solo perché era in attesa; le risposte già arrivate restano. Un singolo motore più lento di 15 secondi viene ancora lasciato indietro per quella ricerca, ma non ferma gli altri e non riempie più il log di avvisi: il dettaglio finisce nel log di debug. Con più motori dei posti disponibili il tempo complessivo della ricerca può allungarsi: prima la seconda metà veniva comunque tagliata.

- **Una serie non ancora uscita non compare più fra i mancanti.** Se i metadati dicono che una serie è ancora «In produzione» (o pianificata), Gextto non considera mancanti le puntate annunciate: prima le ricavava dal numero previsto per la stagione e la scheda *Mancanti* mostrava serie che non erano ancora andate in onda. Lo stesso vale per una puntata con una data di messa in onda futura già conosciuta. I buchi veri restano quelli di sempre.

- **«Cerca mancanti» mostra cosa ha trovato.** La ricerca delle puntate mancanti di una serie diceva solo quanti risultati aveva trovato («Trovati 1 risultati per 15 puntate») e poi li buttava via: per vederne uno bisognava indovinare la puntata giusta e ricercarla a mano. Ora la scheda *Mancanti* elenca i risultati sotto la serie, con la puntata, il titolo, la sorgente e il pulsante **Scarica**. La ricerca non si ferma più alle prime 15 puntate: le cerca tutte, gira in background con l'avanzamento visibile e un pulsante per annullarla, e non scarica niente da sola. I risultati torrent restano anche nell'archivio locale, quindi ricercando la singola puntata li ritrovi subito.

- **Le tracce audio straniere si tolgono da sole dopo l'importazione.** In *Libreria e rinomina* c'è una nuova opzione, spenta di default: dopo che un film o un episodio è entrato in libreria, Gextto tiene solo le tracce audio nella lingua scelta (italiano per impostazione predefinita) e rimuove le altre. Non ricodifica niente: video e audio restano quelli di prima, HDR e sincronizzazione compresi. Tocca il file solo se c'è più di una traccia audio e almeno una è dichiarata in quella lingua; le tracce senza lingua non vengono considerate. Prima di sostituire il file controlla tracce, durata e sincronizzazione, e se qualcosa non torna lascia l'originale. Se ci sono più tracce nella lingua scelta puoi tenerle tutte, solo la migliore o solo la prima. Con i file ancora in seed il download non viene toccato: la copia in libreria diventa indipendente e il torrent continua a condividere. Servono ffprobe, ffmpeg e mkvmerge; per gli MKV Gextto usa mkvmerge, che conserva il contenitore esattamente. Nell'immagine Docker completa gli strumenti sono già installati; in un'installazione manuale vanno messi sull'host. L'opzione si attiva solo se ci sono tutti e tre: altrimenti la riga resta bloccata con un avviso e il log lo dice. La lingua si sceglie da una lista (o si scrive il codice a 2-3 lettere). Per ogni file pulito arriva una notifica sui canali configurati (Telegram, webhook, email) con il nome del file, quante tracce sono state tolte e lo spazio risparmiato. Non provoca riscarichi: il nome del file resta lo stesso e le uniche voci del punteggio che possono cambiare (dimensione e codec della traccia rimasta) restano sotto la soglia di upgrade di 200 punti.

- **Con indexarr una ricerca completa dura meno della metà, e non perde risultati.** Il giro delle 03:15 del 27 ottobre ha impiegato 12 minuti. Per ogni titolo Gextto mandava a indexarr 46 richieste, una per ciascuna fonte di indexarr, e aspettava la più lenta con due posti per fonte: 5.244 richieste per 104 titoli. In più apriva il link `.torrent` di ognuno dei circa 400 candidati, uno dopo l'altro, anche di quelli già in libreria. Ora indexarr riceve una sola ricerca per titolo, interroga da sé le sue fonti in parallelo e restituisce i risultati già senza doppioni. Gextto legge tutte le pagine della risposta, fino a 1000 risultati per titolo: misurato su 14 titoli reali, le release distinte trovate sono le stesse o di più (Reacher 702 invece di 567, CIA 872 invece di 773). Quando un risultato dichiara già il suo infohash, Gextto decide con quello e apre il link solo per la release che scarica davvero; quando non lo dichiara (i topic MirCrew, il cui magnet indexarr chiede al momento), Gextto lo giudica prima da titolo e qualità e apre il link solo se lo scaricherebbe. Lo stesso vale per i pack di stagione «in corso»: il controllo sui file contenuti si fa solo per il pack che verrebbe scaricato. Così indexarr conta come download solo quelli veri; prima contava ogni candidato. Con Jackett e Prowlarr la ricerca resta per singolo sito come prima, e anche lì i link si aprono solo per ciò che si scarica. Primo giro di prova: la ricerca negli indexer è passata da 3 min 40 s a 1 min 23 s, e l'intero giro da 12 a 5 minuti prima del rinvio dei link.

- **Due righe del log più leggibili.** Quando un indexer risponde con un errore Torznab, il log riporta il suo codice e la sua spiegazione («Torznab 100: site requires a login») invece dell'XML grezzo. E se un file migliore sostituisce due copie con lo stesso nome in cartelle diverse, il log scrive una riga sola («… (2 copies in different folders) are in the trash») invece della stessa riga due volte.

- **Con indexarr, gli errori di download indicano il sito giusto.** indexarr ora dice per ogni risultato da quale sito arriva, come già faceva Jackett, e Gextto lo mostra come «indexarr:MirCrew». Quando un sito dietro indexarr continua a fallire (per esempio un login MirCrew scaduto), il log non dice più «indexarr continua a fallire: accedi di nuovo o disattivalo», che portava fuori strada: dice quale sito fallisce e di controllarne il login dentro indexarr. Lo stesso vale per i siti dietro Jackett e Prowlarr. Gextto riconosce indexarr anche sulla sua nuova porta 8788 (la 8484 resta riconosciuta) e la procedura guidata propone il nuovo indirizzo. Se in indexarr attivi «Fai passare i download magnet da indexarr», Gextto continua a funzionare come prima: scarica dal link di indexarr, segue il rinvio al magnet e riconosce i doppioni dall'infohash; indexarr può così contare quali fonti ti hanno davvero dato i download.

## 26 ottobre 2026

- **La velocità Auto non resta più bloccata al minimo con la linea libera.** Ogni sei ore il riferimento del ping «a riposo» scade e Gextto fa una breve prova rallentando i download. Se il ping era già buono, la prova non poteva farlo scendere, e Gextto lo leggeva come «la linea è ancora intasata»: tagliava la velocità fino a 128 KB/s e la teneva lì per tutto il download, con il ping perfetto. Bisognava passare a mano su *Senza limite* e poi tornare su Auto. Ora una prova che parte da un ping buono e non lo abbassa restituisce la linea a Gextto, e il limite cade anche quando resta solo il traffico di fondo della rete DHT. In più, quando il ping peggiora, Auto ricorda la velocità alla quale è successo e si ferma appena sotto, riprovando una volta al minuto, invece di risalire subito oltre: su una linea satura il ping resta alto per una frazione del tempo di prima, con la stessa banda usata. Non c'è nulla da configurare.

- **gx-torrent spreca meno CPU mentre scarica.** Un profilo sotto carico reale (cinque ISO pubbliche insieme, circa 8,5 MB/s e 120 peer) ha mostrato due lavori inutili del motore, ora tolti. Il primo: azzerava ogni buffer prima di riempirlo comunque coi dati in arrivo, circa il 2 % della CPU. Il secondo: quando un torrent ha anche sorgenti HTTP (webseed), ricalcolava e riordinava da capo l'elenco dei buchi per ogni pezzo chiesto a ogni peer; ora la scelta costa un terzo e non alloca memoria. Il resto della CPU va nel traffico stesso (rete, cifratura del protocollo, verifica dei pezzi). I torrent con file di padding si scaricano come prima, e un nuovo test lo verifica. Non c'è nulla da configurare.

- **Gextto e gx-torrent usano meno CPU e memoria.** Il riavvio di Gextto non rilegge più tutte le traduzioni dell'interfaccia quando non sono cambiate: circa un secondo di CPU e 45 MB di allocazioni in meno a ogni avvio. La pagina *Serie* si apre con un terzo del lavoro (da circa 110 a 37 ms di CPU sul mini PC di prova) e l'aggiornamento automatico dei log, che parte ogni 5 secondi mentre la pagina è aperta, costa una frazione di prima. Il dashboard e lo stato letto dalla TUI riusano per 30 secondi il conteggio delle release viste. gx-torrent carica il filtro IP in poco più di metà del tempo e con un sesto della memoria, e Gextto smette di interrogare il database per ogni torrent in seed a ogni giro di controllo. Non cambia niente di visibile e non c'è nulla da configurare.

- **Il log si legge come frasi anche nelle righe rimaste tecniche.** Una sessantina di righe scriveva ancora un'etichetta seguita da campi («torrent link resolution failed · target: … · source: … · error: …», «managed gx-torrent exited unexpectedly · error: … · restarts: 1»). Ora dicono cosa è successo in una frase, per esempio «⚠️ Could not fetch the .torrent of an episode from mircrew: …» o «❌ gx-torrent stopped unexpectedly (…): Gextto restarts it». Riguarda link `.torrent` non scaricabili, ricerche manuali e su eD2k, arresti e riavvii di gx-torrent, filtro IP, test FTP, login dei provider, spostamenti nel cestino e sul RAM disk, pulizia dei download completati e dell'archivio delle release. Il log resta in inglese e ogni riga resta collegata alla cronologia del suo download.

- **gx-torrent non si riavvia più quando il disco di rete è lento.** Se la cartella dei download sta su una condivisione di rete (NFS, SMB) e il NAS impiega più di un minuto a confermare una scrittura, gx-torrent scambiava l'attesa per un blocco interno e si chiudeva, interrompendo tutti i download finché Gextto non lo riavviava. Ora aspetta che il disco risponda, scrive nel log che lo spazio di archiviazione è lento e, quando risponde, che il torrent ha ripreso normalmente. Un blocco vero viene ancora rilevato come prima.

- **Un 4K senza seeder torna in gioco quando lo swarm si riprende.** Quando Gextto rinuncia a un download perché nessuno nello swarm ha il file completo, la release restava in blocklist per sempre: se il 4K resuscitava una settimana dopo, non veniva più riprovato e restava il 1080p. Ora il blocco dura un periodo configurabile (7 giorni per impostazione predefinita, in *Torrent bloccati → Riammetti una release senza seeder dopo*; 0 la tiene bloccata per sempre) e alla scadenza la release torna fra i candidati: se nel frattempo ha trovato seeder viene scaricata come upgrade, mentre fino a quel momento la copia in libreria resta al suo posto. Nella pagina Blocklist la nuova colonna *Scade* mostra la data, oppure «per sempre» per i blocchi che decidi tu, e il log dice quando la release potrà essere riprovata.

- **qBittorrent 5.3: il motore torna a funzionare anche con le versioni 5.2 e 5.3.** Con qBittorrent 5.2 e 5.3 Gextto non riusciva più ad accedere alla Web API (la risposta di login è cambiata), quindi il motore risultava irraggiungibile; dalla 5.0 i torrent aggiunti in pausa partivano subito e i limiti di seed di un singolo torrent venivano rifiutati. Ora Gextto riconosce entrambe le generazioni della Web API e funziona con qBittorrent-nox 4.6 e successivi, compresa la 5.3 installata dalla modalità gestita. Non serve cambiare impostazioni: aggiorna Gextto e riavvia il servizio.

- **Il test di un indexer parla in modo naturale.** La riga nel log non è più una fila di campi tecnici («indexer test · kind: … · name: … · host: … · ok: … · status: …»): ora è una frase, per esempio «✅ The indexer «indexarr» at 192.168.1.161 answered the test (HTTP 200)», oppure «⚠️ The indexer «indexarr» … did not answer the test: the service is unreachable» quando il servizio non risponde.

- **indexarr è un tipo di indexer a sé.** Nella lista *Integrazioni → Indexer Torznab* e nella configurazione iniziale ora puoi scegliere **indexarr** come tipo, con l'URL base (di solito `http://host:8484`) e la API key; non serve più farlo passare per Jackett. Gextto usa l'endpoint Torznab nativo `/api` per la ricerca aggregata e i percorsi compatibili con Jackett per leggere l'elenco degli indexer e interrogare un tracker alla volta, come già fa con Jackett e Prowlarr. La verifica dell'indexer e il controllo di salute usano `t=caps` e riconoscono indexarr, quindi un problema di API key o di URL viene segnalato come per gli altri manager. Le righe già configurate come Jackett continuano a funzionare: per usare l'endpoint nativo basta cambiare il tipo in **indexarr**.

## 25 ottobre 2026

- **La verifica della libreria non rinomina più lo stesso episodio avanti e indietro.** Il controllo periodico dei nomi confrontava due modi di scrivere lo stesso audio (`EAC3` e `ddp`) e, a seconda di quale risposta arrivava per prima, rinominava il file ogni sei ore: due puntate della stessa serie cambiavano nome a ogni passaggio e il conteggio diceva «4 rinominati» per due file. Ora la verifica mantiene la grafia già presente nel nome e rinomina solo quando il codec è davvero diverso; un file già corretto non viene più rinominato una seconda volta nello stesso controllo. Vale anche per l'HDR (`DV` e `DV HDR10`) e per i film.

- **Meno righe ripetute nel log.** Il log di gx-torrent non ripete più «memory released: no active downloads for …» ogni dieci minuti: scrive la riga quando il riposo inizia e poi al massimo ogni sei ore, continuando a liberare la memoria ogni dieci minuti. Il promemoria «Searches need attention» per una sorgente che resta guasta arriva al massimo una volta ogni sei ore (prima ogni ora) e il riepilogo di un pack parzialmente utile compare una volta sola, non a ogni ricontrollo. Il motivo del login di mircrew, che il servizio restituisce in italiano, ora è in inglese nel log e nella notifica; il testo originale resta nel dettaglio.

## 24 ottobre 2026

- **Le copie della stessa release non si moltiplicano più, e una fa da riserva all'altra.** Lo stesso torrent arriva spesso da più fonti (feed e ricerca dello stesso indexer, Jackett, Prowlarr, indexarr) e i siti che danno solo il link `.torrent` lo facevano sembrare ogni volta una release diversa. Ora Gextto unisce le copie quando hanno lo stesso infohash (anche se dichiarato accanto al link `.torrent` o scritto in base32) e, quando l'infohash manca, quando nome e dimensione coincidono a meno dell'arrotondamento. Nel dubbio non unisce: due infohash diversi, o nomi che differiscono per gruppo, risoluzione, lingua, PROPER o episodio, restano release distinte. Della release unita usa la copia con lo swarm migliore e tiene le altre come riserva: se il link scelto non funziona o è in quarantena, prova subito la stessa release da un'altra fonte invece di aspettare il ciclo successivo. Nel log compare una riga «🔁 … trying the same release from …». Nella ricerca manuale ogni torrent compare una volta sola.

## 23 ottobre 2026

- **Anche i fumetti raccontano cosa fanno, nel log.** Il controllo dei fumetti non elenca più righe tecniche («comics: candidate posts · posts: 1», «title checked · queued: 0»): dice quante testate sta controllando, cosa scarica e perché («📥 Downloading «… #41 (2026)» — new comic on GetComics»), quando una testata è già aggiornata o non ha novità, e quando un albo non ha ancora il link di download. Anche gli errori sono frasi con il titolo e il motivo.

- **Weekly Pack: scaricato una volta sola e il log dice perché.** Prima di accodare un Weekly Pack, Gextto controlla nel suo storico se è già stato scaricato (e se è ancora nella lista dei download): se lo ha già preso — anche se dopo averlo letto hai cancellato la cartella o lo hai tolto dalla lista — non lo riscarica e lo segna come già gestito. La riga del log ora spiega il motivo: «📥 Downloading Weekly Pack 2026-10-14 — its download link is available now» quando il link arriva dopo che il pack era stato trovato senza, «… — new pack of the week» per un pack appena pubblicato, «Weekly Pack … was already downloaded on 2026-10-14: not downloading it again» quando lo storico dice che c'è già stato.

- **Il log parla la lingua di chi usa Gextto, sempre in inglese.** Le righe di avvio, ricerca, velocità e download sono ora frasi leggibili invece di elenchi di campi tecnici: «Gextto restarted (run #594) — 1.1.2013 · it was down for 6 seconds · web interface at …», «Speed: no speed limit (Auto)», «Searching: 25% done (26 of 104 titles)». Titoli, episodi e nomi dei file restano nella loro lingua; la frase intorno e gli errori tecnici sono in inglese. I dettagli tecnici restano nel log di debug, dove servono.

- **Un download da uno swarm già morto non parte più.** Se la fonte dichiara zero seeder e zero peer, la release viene scartata. Se è un upgrade e non ha seeder, Gextto non lo avvia e lascia la copia in libreria. Per le fonti che non dichiarano i seeder, una sonda di 15 minuti (30 per un magnet che non trova nessun peer) controlla se qualcuno risponde: se nessun peer si collega, il download viene scartato e la release messa in blocklist, così la ricerca successiva non la ripropone. I tempi di attesa scendono: swarm senza seeder 30 minuti (prima 72 ore), metadati 3 ore (prima 24), alternativa dopo 30 minuti (prima 6 ore).

- **Un riavvio si vede subito.** Prima della riga di riavvio c'è una riga divisoria evidente, così in un log lungo il punto in cui Gextto è ripartito si trova a colpo d'occhio. Il primo avvio non ha la riga divisoria.

- **Meno ripetizioni e meno decorazioni.** Un riavvio non occupa più sei righe (avvio, riavvio, prossima ricerca e velocità sono una riga sola), i separatori di trattini e di uguali sono spariti, l'avanzamento della ricerca compare a quarti invece che ogni minuto, e l'attesa della lista file di un torrent si ripete ogni sei ore invece che ogni ora.

- **Un episodio mancante non viene più chiamato «versione migliore».** Se la copia in libreria non c'è, il download dice «fills a missing episode» anche quando il confronto lo aveva classificato come upgrade. La riga del download dice ora «replacing» per la copia che rimpiazza, così non si confonde più con la sorgente da cui arriva.

- **Un solo racconto per un upgrade scartato.** Quando MediaInfo non conferma un miglioramento, il log lo dice con una riga sola che spiega il confronto e che la copia in libreria resta; i passaggi interni (rimozione dalla sessione, spostamento nel cestino) passano al dettaglio. La stessa riga non compare più due volte quando due controlli guardano lo stesso file sparito.

- **Meno rumore di manutenzione.** Le sessioni browser chiuse perché inattive non occupano più il log normale; le rimozioni dei download falliti dicono anche il motivo riportato dal motore. Il log di gx-torrent resta quello tecnico del motore, invariato.

- **Il controllo automatico tiene la regola.** Un test legge il codice e rifiuta un messaggio INFO, WARN o ERROR scritto con nomi interni o con campi tecnici dentro la frase: la leggibilità non dipende più dal ricordarsene.

## 22 ottobre 2026

- **«Modifica selezionati»: i requisiti dei film seguono il valore che qualificano.** Cambiando lingua o sottotitoli di più film, anche partendo da un profilo, viene aggiornato anche il requisito obbligatorio corrispondente: prima restava quello vecchio e i film non accettavano più nessuna release finché non si riapriva la scheda. L'interruttore si chiama ora **Permetti upgrade**, con **Sì**/**No**, come nel resto dell'interfaccia.

- **Profili di qualità: un nome doppio viene segnalato, non fa sparire una riga.** Salvando due profili con lo stesso nome (anche con maiuscole diverse) la modifica viene rifiutata con un messaggio nella sezione: prima la seconda riga spariva senza spiegazioni. Lo stesso avviso compare ora per qualsiasi salvataggio rifiutato degli elenchi strutturati.

- **Profili di qualità: «Parti da un profilo» non cancella le esclusioni dei titoli.** In **Modifica selezionati** il profilo riempie i campi ma lascia deselezionate le esclusioni, che sono di ogni titolo: si spuntano solo se le si vuole sostituire. Nella scheda del titolo restano sommate, come prima. Un profilo eliminato nel frattempo non riempie più i campi con un altro profilo: compare un avviso.

- **Profili di qualità: Jellyseerr non riceve più un numero inesistente.** L'elenco v3 dei titoli riporta il primo profilo salvato invece del numero 1 fisso; una richiesta con un profilo eliminato viene rifiutata (400) invece di applicarne un altro in silenzio, come già facevano le schede dei titoli.

- **Profili di qualità: riconosciuti anche con maiuscole e codici diversi.** La scheda del titolo preseleziona il profilo anche quando la qualità è scritta `1080P` o la lingua è `it` invece di `ita` (o `ita,eng` in ordine diverso): prima compariva **Valori attuali** per una differenza solo di scrittura.

- **Profili di qualità: l'editor spiega quando si generano.** Il testo della sezione dice ora che le combinazioni della libreria vengono aggiunte da sole **finché non salvi la lista**, e che togliendo tutti i profili vengono rigenerate; lingua predefinita ed esclusioni sono descritte come si comportano davvero.

- **TUI: «Permetti upgrade» anche per i film.** La scheda di modifica di un film ha ora lo stesso interruttore della serie, e il valore viene salvato.

- **Scheda serie: c'è anche l'ID TMDB.** La scheda di una serie mostrava solo **TVDB ID**, anche se TMDB è la fonte predefinita dei metadati quando la chiave TMDB è configurata. Ora ci sono entrambi i campi, con **TMDB** per primo: salvando un ID l'altro resta, e un ID sbagliato si può correggere senza toccare la libreria.

- **Upgrade: niente riscarico quando il nome della copia ha perso sorgente o gruppo.** Se la copia archiviata non dichiara la sorgente o il gruppo (per esempio un file importato o rinominato con il solo titolo), quei campi non vengono più accreditati alla release trovata: senza la prova che la copia sia peggiore non viene sostituita. `ffprobe` completa anche i canali audio quando il nome li ha persi. Un file già in libreria chiamato solo `[1080p][h264][EAC3]` non viene quindi riscaricato solo perché la release trovata è la stessa con il nome completo `WEB-DL DDP5.1 ...`.

- **Upgrade: il punteggio non fa più riscaricare file già in libreria.** La regola che chiede un miglioramento tecnico reale, non solo un punteggio più alto, ora vale in tutti i percorsi. I season pack già in download non scaricano più episodi presenti della stessa qualità, un file della stessa qualità non sostituisce quello archiviato dopo il download e i punteggi degli episodi di un pack usano la dimensione del singolo episodio, non quella dell'intero pack. **Permetti upgrade: No** viene rispettato anche quando la scansione dell'archivio non trova il file (per esempio una cartella spostata o un disco non raggiungibile), purché la libreria indichi dove si trova. Corretto anche il pannello *Impatto sulla libreria*, che ora guarda i file realmente presenti come fa il ciclo.

- **Profili di qualità: rinominare non cambia più il numero.** Il numero di un profilo è quello che Jellyseerr ricorda: prima, rinominando un profilo dall'editor (o salvando da una pagina aperta prima di una modifica) il profilo riceveva un numero nuovo. Ora ogni riga dell'editor porta con sé il proprio numero.

- **Profili di qualità: la scheda del titolo non aggiunge più esclusioni da sola.** Un profilo viene preselezionato solo se corrispondono anche le esclusioni: prima bastavano qualità, audio, lingua, sottotitoli e upgrade, e salvando un campo qualsiasi (per esempio le stagioni) potevano comparire le esclusioni del profilo senza averlo scelto. La voce **Valori attuali** mostra ora anche le esclusioni del titolo. Inoltre una lingua vuota in un profilo vale la lingua predefinita di Gextto, come nel modulo di aggiunta, invece di accettare qualsiasi lingua nella scheda e nell'API v3.

- **Profili di qualità generati: anche audio minimo e upgrade.** I profili creati automaticamente dalla libreria (quelli che vede anche Jellyseerr) ora riportano anche l'audio minimo e **Permetti upgrade** dei titoli da cui nascono, non solo qualità, lingua e sottotitoli; il loro nome dice anche l'audio richiesto. Così una richiesta aggiunta da Jellyseerr con quel profilo non perde più questi due vincoli.

- **Schede di serie e film: il profilo di qualità al posto dei singoli campi.** Nella scheda del titolo **Qualità**, **Lingua** e **Sottotitoli** lasciano il posto a **Profilo di qualità**, come nel modulo di aggiunta. È preselezionato il profilo che corrisponde ai valori del titolo, con il suo riassunto; se nessuno corrisponde compare **Valori attuali**, che salvando non cambia nulla. Scegliendo un profilo e salvando, qualità, audio minimo, lingua, sottotitoli e **Permetti upgrade** del profilo vengono copiati nel titolo e le esclusioni del profilo si sommano a quelle del titolo. Il profilo resta un modello: modificarlo in *Qualità e upgrade* non cambia i titoli già creati.

- **Scheda serie: via il campo Nome in sola lettura.** Ripeteva il titolo già scritto in testata e sembrava modificabile senza esserlo. Per correggere un nome sbagliato si cancella la serie e la si aggiunge di nuovo.

- **Schede di serie e film più ordinate.** I campi **Audio minimo** e **Permetti upgrade** non sono più nella scheda del titolo: arrivano dal profilo di qualità quando aggiungi una serie o un film, e si cambiano per più titoli insieme con **Modifica selezionati**. Il valore già impostato su ogni titolo resta e continua a valere. Nella scheda serie i campi sono riallineati (Escludi su tutta la riga, **Anime** come scelta Sì/No).

- **Scheda film: un campo per ogni cosa.** Spariscono **Lingue richieste** e **Requisiti sottotitoli**, che duplicavano **Lingua** e **Sottotitoli** (e se compilati vincevano in silenzio). Al primo avvio i valori vengono riportati nei campi che restano, senza cambiare le decisioni: un film con lingua «any» ma lingua richiesta «chi» ha ora Lingua `chi`. Al posto dei requisiti dei sottotitoli c'è **Sottotitoli obbligatori: Sì/No**. Il campo **Escludi** occupa tutta la riga.

- **Profili di qualità: lingua e sottotitoli da scegliere o scrivere, meno doppioni.** Nell'editor dei profili **Lingua** e **Sottotitoli** propongono le scelte comuni (ita, eng, ita,eng, multi, any) e accettano anche un valore scritto a mano. I profili generati in automatico dalla libreria (quelli che vede anche Jellyseerr) non nascono più dalle esclusioni di un singolo titolo né dalla qualità `any`, che equivale a nessun limite: niente più profili doppi come *any · ita* accanto a *Default*.

- **Profili di qualità: risoluzione da elenco.** Nell'editor dei profili (*Qualità e upgrade*) il campo **Qualità** è ora un menu con le risoluzioni che Gextto riconosce, le stesse della scheda dei titoli, invece di un testo libero. Un valore scritto a mano in passato resta selezionato finché non lo cambi.

- **"Niente upgrade" diventa "Permetti upgrade".** Nei profili di qualità, in **Modifica selezionati** e nella TUI l'interruttore si chiama ora **Permetti upgrade**, con **Sì** (predefinito: una release migliore sostituisce il file archiviato) o **No** (il file archiviato resta). È la stessa impostazione di prima, scritta al positivo: nessun titolo cambia comportamento.

- **Upgrade verificati con MediaInfo prima di entrare in libreria.** Quando un download completo deve sostituire un episodio o un film già archiviato, Gextto legge con `ffprobe` il file nuovo (ancora nella cartella di download) e la copia in libreria prima di spostare o copiare qualsiasi cosa. Se il nome prometteva più del contenuto reale (un «2160p» ingrandito da un 1080p, un «HDR» falso, un audio diverso da quello dichiarato) e la qualità vera non è un upgrade, il download viene scartato e messo in blocklist, e la copia in libreria resta. Senza `ffprobe` vale la decisione presa prima del download; i pack di stagione non sono ancora verificati così.

- **WEB-DL e WEBRip: decidi tu se sono la stessa qualità.** La regola che sostituiva sempre un WEBRip o un HDTV con un WEB-DL segue ora i pesi **Sorgente** di *Qualità e upgrade*: se WEB-DL e WEBRip hanno lo stesso peso, un WEB-DL non sostituisce più un WEBRip già in libreria. HDR, una risoluzione più alta, REPACK/PROPER e un punteggio migliore di almeno la differenza minima restano motivi validi.

- **Qualità e upgrade: verifica dell'impatto prima di riscaricare.** Il nuovo pannello **Impatto sulla libreria** ripassa le release già viste nei feed con le regole e i pesi attuali e dice quanti episodi archiviati verrebbero sostituiti nei prossimi cicli, con alcuni esempi, senza scaricare nulla. Serve dopo aver cambiato pesi o soglie: se il numero è alto, conviene rivedere la modifica prima che parta un riscaricamento di massa. Il risultato diventa un avviso quando supera il 5% degli episodi archiviati.

- **Aggiunta di serie e film: profili e "Permetti upgrade".** Il modulo di aggiunta chiede ora solo il **Profilo di qualità** (con un riassunto di cosa imposta): qualità, audio minimo, lingua, sottotitoli, esclusioni e **Permetti upgrade** del nuovo titolo vengono dal profilo, senza altri campi che possano contraddirlo. Resta un campo **Esclusioni in più** per le parole che valgono solo per quel titolo, sommate a quelle del profilo. Dopo l'aggiunta i singoli valori si cambiano nella scheda del titolo, dove **Permetti upgrade** c'è ora anche per i film. I profili di qualità hanno due campi nuovi, **Audio minimo** e **Permetti upgrade**, che vengono applicati anche ai titoli aggiunti da Jellyseerr e con **Modifica selezionati**, dove ora si può cambiare anche l'audio minimo di più titoli insieme. Corretto anche un errore per cui alcuni salvataggi delle schede (per esempio un interruttore sì/no) potevano a volte essere rifiutati.

- **Qualità e upgrade: pagina riorganizzata.** La sezione parte dalle **regole di upgrade** (differenza minima, soglia di stop, sostituzione delle versioni archiviate), seguite dagli esempi di punteggio, dal nuovo pannello **Prova una release** e dai **Profili di qualità**, spostati qui da *Sorgenti*. I pesi **Video**, **Audio** e **Correzioni release** (PROPER, REPACK, REAL) restano disponibili in schede chiuse, da aprire solo per cambiare le preferenze. **Prova una release** accetta il nome di una release e mostra il punteggio voce per voce, la copia già in libreria con il suo punteggio e se verrebbe sostituita, con il motivo, senza scaricare nulla. Corretto anche l'editor dei profili di qualità, che nell'interfaccia attuale non compariva.

- **Upgrade: niente più riscaricamenti della stessa qualità.** Un upgrade "per punteggio" richiede ora un miglioramento reale della qualità (risoluzione, sorgente, codec, audio, HDR/DV, correzioni, gruppo) di almeno la **differenza minima**: un file più grande, i sottotitoli o uno swarm più sano aiutano a scegliere tra release equivalenti, ma da soli non fanno più riscaricare un episodio o un film già in libreria. La ricerca degli episodi mancanti registra il punteggio della release senza il bonus di salute dello swarm. I punteggi della libreria vengono ricalcolati da soli anche quando un aggiornamento di Gextto cambia le regole di calcolo, non solo quando cambi i pesi (succede una volta, al primo avvio di questa versione). Corretto anche un errore per cui i **gruppi custom** salvati con lettere maiuscole (per esempio `Me7alh`, `TheBlackKing`) non davano mai il loro bonus e non si potevano eliminare: ora vengono convertiti in minuscolo e funzionano.

- **Audio e video: pesi raggruppati e preset per titolo.** In **Qualità e upgrade** i punteggi audio sono ora una scheda per gruppo: **Lossless** (TrueHD, DTS-HD, FLAC, PCM), **DTS / DD+** (con DTS-HD HRA), **AC3 / 5.1**, **AAC / Opus**, **MP3**, più una scheda separata per **canali e Atmos/DTS:X**. Anche il video è raggruppato in un'unica scheda **Video**, con i sottogruppi 4K, Full HD, HD, SD, Codec video, Sorgente e HDR/DV; **Audio** e **Upgrade e sostituzione** sono le altre due schede, con i rispettivi sottogruppi. Il parser distingue DTS-HD MA da HRA, riconosce FLAC, PCM, Opus e non confonde più `AAC 5.1` con il solo layout 5.1. Nelle schede di aggiunta e modifica di serie e film c'è un solo **Audio minimo** con preset che includono già i canali (`AAC / Opus 5.1+`, `AC3 / 5.1`, `AC3 7.1`, `DTS / DD+ 5.1+`, `DTS / DD+ 7.1`, `Lossless 5.1+`, `Lossless 7.1`, in ordine dal peggiore al migliore): con **Qualsiasi** (il default, anche per tutta la libreria già caricata) non viene imposto nulla; scegliendo un preset le release inferiori vengono scartate. I vecchi pesi audio rimasti ai valori predefiniti vengono allineati una sola volta, senza toccare i valori personalizzati.

- **Serie TV: niente falsi upgrade da audio o punteggio.** La decisione prima del download confrontava lo score del nuovo release con il solo score di qualità del file archiviato, mentre al completamento il confronto includeva anche dimensione e altri bonus: un file identico poteva quindi sembrare un upgrade e venire riscaricato, per poi essere scartato. Ora il file sul disco usa lo stesso `FileScore` del completamento, lo score di approvazione non include più il bonus di salute dello swarm, e un audio di qualità inferiore non può vincere grazie al solo punteggio né sostituire una traccia migliore già archiviata. Il parser riconosce anche `TrueHD`, `7.1`, `DD+` e le varianti comuni, evitando che un audio non riconosciuto falsi il confronto.

- **Film già sul NAS: niente download inutile.** Prima di scaricare un film, Gextto controlla ora anche il file presente nella cartella archivio, non solo la riga del database. Se il file esiste ed è uguale o migliore, il download non parte; se il database era stato ricostruito o la riga mancava, il file sul disco conta comunque. La stessa informazione compare nella spiegazione della decisione.

- **eD2k: import sicuro e ricerche più complete.** Gextto ora aspetta che aMule abbia davvero spostato un file completato nella cartella di arrivo, con la dimensione attesa e stabile per qualche istante, prima di importarlo: un file ancora in copia da un altro disco o da un mount Docker non viene più trattato come pronto. Le ricerche eD2k/Kad vengono normalizzate (accenti, apostrofi tipografici, parentesi) e, quando `SxxExx` non trova nulla, Gextto prova anche `1x05`, `1x5` e `01x05`. Con qBittorrent un completamento senza video importabile indica ora quali file sono stati saltati, e il limite di seed inattivo viene inviato come `inactiveSeedingTimeLimit`.

- **Identità client: ora si sceglie da una tendina.** In **Configurazione → Motore torrent** il campo dell'identità annunciata da gx-torrent mostra i preset (`gx-core`, qBittorrent, Transmission, Rain, anacrolix/torrent) in un menu a discesa, non più come testo libero; la versione resta un campo separato.

- **eD2k rispetta sposta/copia.** Anche i download completati con aMule seguono l'opzione **Sposta gli episodi/pack in archivio**: con l'opzione attiva il file viene spostato in libreria e rimosso dalla cartella di arrivo (aMule smette di condividerlo), con l'opzione spenta viene copiato o collegato e resta nella cartella di arrivo per la condivisione. Per eD2k lo spostamento avviene al completamento, perché il seeding è gestito da aMule e Gextto non ha un fine seed da attendere.

- **Docker: tutto lo stack in un comando.** Il nuovo `docker-full/docker-compose.yml` avvia Gextto con `gx-torrent`, Jackett, Prowlarr, FlareSolverr e aMule, con cartelle, porte e permessi già predisposti. L'immagine compila Gextto e `gx-torrent` dal checkout; la guida in italiano e inglese spiega prima configurazione, indexer, eD2k, aggiornamento, backup e problemi comuni. L'identità del client è stata verificata con qBittorrent 5.3.0 e Transmission 4.1.3 reali in Docker: vedono `gx-core 1.1.252` per default e `qBittorrent 5.2.3` con il preset.

## 21 ottobre 2026

- **Import manuale di un file già presente.** La nuova scheda **Importa file**
  di **Manutenzione** legge un video o una cartella già sul disco (da scrivere o
  da scegliere con **Sfoglia**, che mostra cartelle e video), propone serie,
  stagione ed episodio (o film e anno) e la destinazione; puoi correggerli e
  confermare. Gextto rinomina, sposta, registra l'episodio o il film e aggiorna
  Jellyfin/Plex. Se esiste già un file con lo stesso nome l'import si ferma,
  salvo la spunta **Sostituisci il file esistente** (la copia vecchia va nel
  cestino). I file multi-episodio vengono registrati per ogni episodio.
- **Profili di qualità nominati e API v3 per Jellyseerr.** In Configurazione →
  Sorgenti puoi dare un nome alle combinazioni di qualità, lingua, sottotitoli
  ed esclusioni; Gextto genera un profilo per ogni combinazione già presente
  nella libreria (il primo è **Default**). Jellyseerr trova i profili e può
  aggiungere serie e film da `/api/v3` (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup`, `POST series`,
  `movie/lookup`, `POST movie`, `queue`). Gextto segue solo le stagioni
  richieste; una richiesta su una serie già in libreria aggiunge le nuove
  stagioni, e un film già presente non viene duplicato. Ogni profilo mantiene
  il suo numero anche se ne togli o riordini altri, così Jellyseerr applica
  sempre quello scelto. Non è una compatibilità completa con Sonarr/Radarr.
  I profili sono **modelli**: i loro valori vengono copiati nel titolo, che
  resta libero di cambiare; cambiare un profilo non modifica i titoli già
  creati.
- **Modifica di più titoli insieme.** Nelle tabelle **Serie TV** e **Film**
  ogni riga ha una casella: spunti le righe (anche tutte quelle filtrate),
  apri **Modifica selezionati** e cambi qualità, lingua, sottotitoli,
  esclusioni, upgrade o stato su tutte insieme. Puoi partire dai valori di un
  profilo. Tutto il resto di ogni titolo non viene toccato.
- **Seed infinito: basta il rapporto o il tempo.** 0 vuol dire infinito sia nel
  rapporto sia nel tempo di seed, e ora **basta uno dei due** perché il seed non
  finisca mai: il torrent resta in seed e, con *Sposta* attiva, in libreria ne
  entra una copia invece di spostarlo. Prima un torrent con rapporto infinito si
  fermava comunque al limite di tempo e veniva spostato. Attenzione: chi imposta
  solo il rapporto e lascia il tempo a 0 ora ha un seed infinito; per fermare il
  seed servono rapporto **e** tempo maggiori di 0. Vale anche per i download
  eD2k, che seguono le regole di seed e i limiti globali: con seed infinito il
  file viene copiato e aMule continua a condividerlo.
- **Mancanti diventa una scheda di Serie TV, raggruppata per serie.** Al posto
  dell'elenco di puntate una sotto l'altra, la scheda **Mancanti** di Serie TV
  mostra un blocco per serie con una riga per stagione e le puntate come
  pulsanti. Un clic su una puntata apre i risultati da scaricare; **Cerca
  mancanti** mostra mentre lavora e scrive l'esito accanto alla serie (prima un
  clic su Cerca non dava nessun riscontro). C'è un filtro per serie; la voce di
  menu Mancanti non c'è più.
- **Sorgenti che non rispondono: ora il log dice quale e perché.** L'avviso
  di fine ricerca diceva solo «1 indexer». Ora nomina ogni sorgente che ha
  fallito, quante ricerche su quante e l'ultimo errore, ad esempio «indexer
  Jackett: 3 of 106 searches failed — timeout».
- **Episodi eD2k: sposta o copia come i film.** Con **Sposta gli
  episodi/pack in archivio** attiva, anche gli episodi scaricati con aMule
  vengono spostati in libreria al completamento, come già i film; prima
  potevano essere copiati e lasciati in aMule, perché un download eD2k
  sembrava impostato su «seed infinito» anche quando il seed configurato è
  finito. Con il seed infinito restano invece copiati e condivisi (vedi sopra).
- **Download eD2k nel log e nella storia.** I download eD2k avviati a mano
  (da «Scarica» in una ricerca o da un link incollato) ora compaiono nel log
  appena aggiunti, o con il motivo se aMule li rifiuta. Tutte le righe di un
  download eD2k portano il suo codice `acq:` e finiscono nella **Storia** del
  download, come per i torrent: prima la storia dei download eD2k restava
  vuota. Vale per i download nuovi.
- **Pulsanti allineati.** In Configurazione i pulsanti «Predefinito» e «Salva»
  delle opzioni con una nota sotto il campo (es. «0 = illimitato») ora stanno
  all'altezza del campo invece che più in basso. Sono allineati anche
  «Aggiungi» in Aggiungi torrent, le righe dei Gruppi custom e la barra del
  Log, dove «Ferma scorrimento», «Filtra» e «Copia» non vanno più a capo.
- **gx-torrent si presenta con la sua versione, e può usare un'identità compatibile.** Prima diceva sempre `gx-core 0.0.0` a peer e tracker. Ora annuncia la versione reale (`gx-core 1.1.xxx`) e, se vuoi, può presentarsi come qBittorrent, Transmission, Rain o anacrolix/torrent con una versione scelta. Cambia solo l'identità annunciata: alcuni tracker privati accettano solo client in whitelist, quindi controlla le loro regole prima di usarla.

- **Download con migliaia di torrent: la tabella resta fluida.** Oltre 500
  torrent la tabella mostra solo le righe vicine a ciò che stai guardando e
  carica le altre mentre scorri: filtri, ordinamento e selezione continuano a
  funzionare su tutta la sessione, senza il vecchio limite di 300 righe e senza
  un DOM enorme.
- **DLNA: ricerca per titolo e Xbox/Windows Media Player.** I lettori possono
  cercare un titolo (video o audio) e ricevono i risultati anche dalle cartelle
  della libreria; Xbox 360 e Windows Media Player accettano il server grazie al
  servizio che richiedono. Nella pagina di gx-torrent il pulsante **Cartella
  download** aggiunge alle cartelle DLNA quella di download già configurata.
- **Cartelle osservate: il testo dice cosa succede.** La sezione ora spiega che
  i `.torrent` trovati vengono **scaricati** e il loro contenuto aggiunto alla
  sessione, non solo «aggiunti automaticamente».
- **Feed DHT: ora puoi pubblicarli.** Dal menu **Strumenti** (o dall'API) firmi
  un feed BEP 46 con i torrent selezionati, o con tutti quelli della sessione, e
  ottieni l'indirizzo `dht://…` da dare a chi lo deve seguire; Gextto conserva
  la chiave e aggiorna la sequenza a ogni pubblicazione.
- **Numerazione: torna `1.1.<build>`, il secondo numero si alza a mano.**
  Gextto non sposta più da solo il secondo numero ogni 1000 build: la versione
  è `<serie>.<build>`, il terzo numero è sempre la build (`1.1.1961`) e la
  serie resta `1.1` finché non la si cambia di proposito in `version_series`
  (per esempio `1.2.2000`). Vale anche per gx-torrent, con il suo
  `gx_torrent.version_series`. La build e il commit restano visibili in fondo a
  **Manutenzione**.
- **Feed RSS: se IPv6 si blocca, Gextto riprova su IPv4.** Un host raggiungibile
  in IPv6 ma che non manda dati (è il caso di `extto.org` dietro Cloudflare)
  faceva scadere i feed uno dopo l’altro. Dopo il primo tentativo fallito i
  successivi passano da IPv4, così i feed di ext.to non spariscono più per un
  timeout.
- **Scarico: i download eD2k mostrano più dati e il badge è colorato.** Ogni
  riga eD2k ora mostra, oltre a stato, progressione, velocità e fonti, anche i
  **byte caricati**, il **tempo stimato** e il **rapporto** tra caricato e
  scaricato; il badge **eD2k** ha un colore suo, così si distingue subito dai
  torrent.
- **Banda: il ping accanto ad Auto e un autotune più attento all'upload.** Con
  la velocità su Auto, accanto alla velocità **↓ Download** nella barra in alto
  compare il **ping** misurato; quando la linea è
  asimmetrica l'upload conta come carico già a un decimo della capacità (tra 32
  e 256 KiB/s), quindi Auto lo limita prima che saturi la rete. Se l'upload
  comincia a caricare durante il controllo, viene limitato subito, senza
  aspettare un nuovo tentativo.
- **eD2k: tolta «Impostazioni consigliate».** Il pulsante che cambiava la
  preallocazione di aMule non c'è più; la preallocazione resta un interruttore
  esplicito in Configurazione → eD2k e Kad.
- **Hardlink: il log spiega il caso dello stesso NAS in mount separati.** Se
  download e libreria sono due mount NFS dello stesso server, un hardlink non
  può attraversarli: Gextto lo dice e indica di montare una cartella padre
  comune o di tenere la libreria nello stesso export, invece del generico
  «filesystem diversi».
- **qBittorrent: «Proponi» compila la mappatura dei percorsi.** Il pulsante
  legge da qBittorrent le sue cartelle di download e temporanea e propone le
  righe `percorso_gextto=percorso_qbittorrent`; si controllano e si salvano.
- **Tooltip su «attivo» e «ok».** Il pallino **attivo** in fondo al menu e il
  badge **ok** in alto ora dicono cosa indicano: il primo che Gextto sta
  lavorando davvero, il secondo lo stato di salute del servizio.
- **eD2k: i totali scambiati non sono più a zero e i file condivisi dicono
  l'ultimo caricamento.** La pagina eD2k mostrava **Sessione** sempre a `0 B`
  perché chiedeva ad aMule le statistiche a un livello che non le contiene; ora
  ci sono due righe, i byte scambiati **in questa sessione** e quelli
  **dall'inizio** (tutte le sessioni), sia nella pagina sia nella scheda
  **Salute**. Nei **File condivisi** richieste e accettate mostrano sessione e
  totale, con la velocità del file e l'**ultimo caricamento** (`—` se aMule non
  l'ha mai caricato): i contatori sono quelli di aMule, quindi la colonna della
  sessione riparte da zero a ogni riavvio di `amuled` mentre quella totale no.
- **La pagina Salute è tradotta per intero.** Le righe del pannello *Motore
  torrent* (coda, porta, router, uTP, cache, disco, totali di sessione…), le
  etichette dei percorsi, la memoria del processo e le intestazioni ordinabili
  delle tabelle restavano in italiano anche con l'interfaccia in un'altra
  lingua: ora seguono la lingua scelta (inglese, tedesco, francese, spagnolo,
  polacco). Lo stesso vale per la colonna *Connessione* nella scheda Peer.
- **«Cerca mancanti» ora usa davvero eD2k.** Prima eD2k entrava in gioco solo
  nel passaggio automatico del gap filling: *Cerca mancanti* e la ricerca del
  singolo episodio ignoravano aMule, anche con l'integrazione attiva. Ora, se
  **Usa eD2k quando i torrent non trovano nulla** è attivo e i torrent non
  trovano niente, la ricerca chiede l'episodio anche a eD2k/Kad. La ricerca di
  una serie fa **una sola interrogazione per stagione** (non una per episodio,
  che sarebbe molto più lenta): i risultati compaiono con la sorgente
  **eD2k/Kad** e il pulsante **Scarica** li passa ad aMule. Anche il fallback
  automatico ora scrive nel log quanti risultati trova, così si può verificare
  che venga usato; se non trova nulla non scrive niente. Le ricerche manuali su eD2k mostrano solo
  file della serie cercata: una serie con un nome simile (per esempio un titolo più lungo che inizia con la stessa parola) non compare più tra i risultati.
- **Interfaccia web più leggera, soprattutto su reti lente.** Le pagine ora
  viaggiano compresse: l'Archivio passa da circa 473 KB a 39 KB e la dashboard
  da 33 KB a 8 KB. I fogli di stile e gli script sono compressi e restano nella
  cache del browser con il numero di build: dopo la prima apertura non vengono
  più riscaricati a ogni pagina, e al controllo successivo il server risponde
  «non modificato» senza corpo. Gextto resta reattivo anche su collegamenti
  lenti o da telefono.
- **Archivio: il pulsante Scarica dei risultati eD2k non finisce più fuori
  schermo.** Con nomi di file lunghi la tabella dei risultati eD2k si allargava
  oltre la finestra e la colonna **Azioni** con **Scarica** restava fuori vista:
  serviva scorrere in orizzontale. Ora il nome viene accorciato con i puntini
  (il testo completo è nel tooltip) e **Scarica** resta visibile anche sul
  telefono. Aggiunto un test che verifica che il pulsante ci sia nei risultati
  dell'archivio.
- **Manutenzione → Backup: meno confusione tra salvare e fare il backup.** Il
  pulsante del pannello delle impostazioni ora si chiama **Salva impostazioni**
  e non più *Salva backup*: salva conservazione, programmazione, FTP, cartella
  cloud, percorsi extra e Telegram, senza creare nessun archivio. Per creare un
  backup al volo c'è **Backup ora**, ora presente anche sotto la tabella dei
  backup (oltre che tra le Azioni rapide). **Backup ora** e **Verifica** avvisano
  subito che stanno lavorando, si disattivano finché l'operazione non finisce e
  al termine dicono se il backup è stato creato o se l'archivio è integro.
  Corretto anche il nome interno `maintenance-t0` che poteva comparire nel
  titolo della pagina dopo un'azione avviata dalla tabella dei backup.
- **Configurazione → Le tue fonti: gli errori degli indexer portano a Salute.**
  Quando l'ultimo ciclo ha avuto errori, la voce della lista diventa **Errori
  indexer** e apre **Salute** con i dettagli, invece della gestione degli
  indexer (che resta in **Integrazioni**). Corretto anche il testo per un solo
  errore, che ora dice «1 errore» e non «1 errori».
- **eD2k e Kad: cartelle di aMule in chiaro.** In **Configurazione → eD2k e
  Kad** ora vedi la cartella di download (incoming) e quella temporanea usate
  da aMule, in sola lettura. Il tooltip spiega che si cambiano da
  aMule/amuled e che dopo la modifica va riavviato Gextto (o riaperta la
  pagina) perché usi il nuovo percorso.
- **Housekeeping: tooltip più utili.** Le opzioni dell'housekeeping ora dicono
  cosa puliscono e cosa non toccano: file, libreria, Archivio release e torrent
  attivi restano fuori.
- **Archivio release: la pulizia automatica ora funziona.** Le impostazioni
  «Pulizia automatica archivio», «Archivio — età massima» e «Archivio —
  mantieni almeno N voci» venivano salvate ma non eseguite. Ora girano con
  l'housekeeping periodico e rimuovono solo le righe del catalogo release più
  vecchie del limite: file scaricati, libreria, storico download e torrent
  attivi non vengono toccati.
- **Dettagli del torrent: tooltip che spiegano davvero.** Passando il mouse sui
  valori del tab **Generale**, sui pulsanti delle azioni e sulle intestazioni
  delle tabelle del dettaglio di un torrent ora compare una spiegazione di cosa
  significa il dato o cosa fa il pulsante, tradotta in tutte le lingue
  dell'interfaccia.
- **Memoria di gx-torrent restituita al sistema.** Dopo un periodo di download
  intenso gx-torrent poteva restare a centinaia di MB di RAM anche senza
  traffico. Ora, dopo 5 minuti con il traffico sotto la soglia dei torrent
  lenti, restituisce al sistema la memoria non più usata (al massimo una volta
  ogni 10 minuti); le cache si ricostruiscono da sole al trasferimento
  successivo.
- **Archivio gonfiato da release ripetute: corretto.** L'archivio poteva
  contenere la stessa release decine di volte quando l'indexer (per esempio
  Jackett) rigenerava il link `.torrent` a ogni ricerca: il link cambiava, ma
  il contenuto no. Ora la release viene riconosciuta dal suo identificatore
  stabile e resta una sola riga. In **Manutenzione → Manutenzione SQL
  dell'archivio** ci sono due operazioni in più: **Release recenti**, per
  vedere che cosa è appena entrato, e **Duplicati per titolo e fonte**, per
  ripulire le righe già accumulate tenendo la più recente. I magnet non vengono
  toccati. Una release con magnet resta riconosciuta dal suo torrent: se un
  feed pubblica un REPACK con lo stesso identificatore, entrano in archivio
  entrambi, ognuno con il suo titolo. Quando l'indexer offre il link
  `.torrent`, Gextto scarica quello anche se conosce l'hash (Jackett e Prowlarr): con gli indexer
  privati un magnet senza tracker non troverebbe nessuno.
- **Le notifiche di download dicono da dove arriva il torrent.** Il messaggio
  «Nuovo episodio in download» / «Film in download» (Telegram, email e gli
  altri canali) ora ha la riga **📡 Fonte** con il feed, l'indexer o l'archivio
  locale da cui è stata presa la release, per esempio «ExtTo - TheBlackKing»,
  «Knaben (Jackett)» o «ExtTo (archivio locale)».
- **Impostazioni backup e Simkl: «Salva» ora salva davvero.** In
  **Manutenzione → Backup** il pulsante *Salva backup* non registrava nessun
  campo (né *File e cartelle da includere* né gli altri), e lo stesso accadeva a
  *Salva Simkl*: la pagina si ricaricava come se fosse andato tutto bene. Ora i
  valori vengono salvati, e se un valore viene rifiutato (per esempio un
  percorso non assoluto) compare un avviso con il motivo. Chi aveva provato a
  cambiare queste impostazioni deve reinserirle una volta.
- **Molti torrent attivi, CPU sotto controllo.** Oltre al limite per singolo
  torrent, gx-torrent ora ha un totale di connessioni per tutta la macchina (75
  per core: 300 su un mini PC a 4 core), diviso tra i torrent in corso: chi sta
  scaricando ha la precedenza, un torrent in seed ne chiede la metà e il minimo
  (15) quando nessuno scarica da lui. Un torrent da solo ha sempre tutte le sue
  connessioni, quindi il totale entra in gioco solo con molti torrent insieme.
  Un torrent appena aggiunto ha subito il suo limite, invece di restare senza
  tetto per i primi 30 secondi, e un torrent che si muove a pochi byte al
  secondo non conta più come «in progresso». Nel log, quando il totale riduce
  un torrent, la riga lo dice («shared with 9 running torrents»).
- **Seed con seed: connessione chiusa subito.** Quando un torrent è in seed,
  gx-torrent chiude le connessioni con chi ha già tutto quello che abbiamo noi:
  non c'è niente da scambiare e tenerle aperte costava CPU e memoria per niente.
- **Log di Gextto meno ripetitivo.** La regolazione automatica della velocità
  (Autotune) scrive una riga quando cambia modalità e poi un riepilogo ogni 15
  minuti (quanti aggiustamenti, da quanto a quanto, valori attuali) invece di
  una riga al minuto; i singoli passi restano nel log di debug. «Ricerca
  interrotta» compare una volta sola quando Gextto si ferma, l'avanzamento della
  ricerca dei titoli non si ripete se nell'ultimo minuto non è cambiato niente,
  e il riepilogo degli errori di rete dei peer di gx-torrent arriva ogni 30
  minuti invece che ogni 10.

- **Connessioni decise dalla macchina, non da un numero fisso.** Gextto ora
  lascia che gx-torrent scelga quante connessioni tenere per ogni torrent in
  base ai core e alla RAM della macchina, e riduce quelle dei torrent che non
  progrediscono da un paio di minuti pur avendo molti peer: le connessioni
  inutili lasciano il posto a quelle in coda, la CPU resta bassa e la velocità
  non cala. Ogni cambiamento finisce nel log («automatic peer cap: 55
  connections per torrent …», «peer cap for …»), così si vede perché il numero
  si è mosso. In **Configurazione → Code e prestazioni → Connessioni** c'è **Connessioni
  automatiche (in base alla macchina)**, attiva di default; **Connessioni max
  per torrent** resta il tetto massimo (-1 = nessun tetto) e un limite scelto
  nella scheda *Limiti* di un singolo torrent ha sempre la precedenza. La stessa
  opzione è nel demone gx-torrent (*Limite peer automatico*). Chi preferisce un
  numero fisso spegne l'automatico.
- **Un'istanza non tocca più il motore di un'altra.** Se sulla porta
  configurata risponde il gx-torrent di un'altra installazione di Gextto, la
  nuova istanza si ferma con un messaggio chiaro invece di adottarlo: prima
  poteva ricaricargli il filtro IP e riavviargli la sessione, interrompendo i
  trasferimenti in corso. Le prove automatiche dell'interfaccia ora usano un
  motore proprio.

- **Filtro IP: la lista predefinita è ora eMule Security.** Gextto e gx-torrent
  partono con la lista di eMule Security
  (`https://upd.emule-security.org/ipfilter.gz`) invece di quella Aetherinox.
  La nuova lista è in formato P2P, si aggiorna ogni giorno e blocca meno
  indirizzi legittimi: non contiene più blocchi di interi provider o hosting
  come `158.173.0.0/16`, che fermavano anche peer disponibili. Chi ha già
  impostato un URL diverso lo mantiene; per passare alla nuova lista basta
  incollarlo in **Configurazione → Velocità e rete → Cifratura e filtro IP**, campo **IP filter (file/URL)**, e premere
  **Carica / aggiorna ora**.
- **Manutenzione: tab SQL per l'Archivio release, con scelta della lingua.**
  In **Manutenzione** c'è il tab **Manutenzione SQL dell'archivio**: un
  selettore in cima sceglie la lingua delle operazioni e l'elenco propone
  release più vecchie di 6 mesi o di 1 anno, release nella lingua scelta o
  senza quella lingua, titoli vuoti, più diagnostiche di sola lettura
  (distribuzione per età, release per fonte, stato dell'archivio, titoli
  duplicati). Ogni operazione che elimina mostra prima l'anteprima con il
  conteggio e le prime 200 righe, poi chiede conferma. Mentre la query è in
  corso compare un indicatore; al termine il pannello scorre al risultato. Non è
  una console SQL libera: esegue solo gli script inclusi e non tocca libreria,
  torrent attivi o impostazioni. La lingua è dedotta dal titolo, quindi le release senza
  marcatore rientrano tra quelle “non nella lingua selezionata”.
- **Versione: la terza cifra riparte ogni 1000 build, la build resta visibile.**
  Quando il contatore arriva a 2000 la versione passa da `1.1.1999` a `1.2.0`,
  poi `1.2.999` e `1.3.0`, e così via: il terzo numero non cresce all'infinito,
  e gx-torrent segue la stessa regola dalla sua build 1000. Per non perdere il
  riferimento esatto, dopo il rollover la versione mostra la **build** tra
  parentesi (`1.2.925 (build 2925)`, anche in `--version`, nell'API e
  nell'intestazione) e in fondo alla pagina **Manutenzione** una riga riporta
  versione, build completa e **commit** da cui è compilata (il commit breve;
  quello intero si vede passando il mouse). La versione attuale, `1.1.1925`,
  resta com'è.
- **Storico download: il nome del torrent resta leggibile.** Su schermi di
  larghezza media (telefono tenuto di lato o finestra stretta) la colonna
  **Nome** si riduceva a pochi pixel e il titolo andava a capo una lettera per
  riga, perché la tabella teneva comunque visibili tag, punteggio e sorgente.
  Ora quelle colonne si ritirano una alla volta man mano che lo spazio cala,
  così il nome ha sempre una riga utile (il nome completo resta nel suggerimento
  del passaggio del mouse). Su schermo largo nulla cambia.

## 20 ottobre 2026

- **Dashboard: riepilogo nella barra in alto e nomi più leggibili.** **Spazio
  libero** e **Magnet in archivio** sono passati dal corpo della pagina
  all'estrema destra della riga dei comandi della barra in alto, sotto
  l'orologio, e compaiono solo nella Dashboard; su schermo largo **Italiano**
  apre quella riga sotto il primo riquadro, quello di Gextto. Serie TV/Film e
  Motore non sono più mostrati in alto. In **Da controllare** il
  nome del download si ferma alla stagione/episodio (il nome completo compare
  passando il mouse), così il motivo ha spazio e la tabella non va più scorsa di
  lato.
- **Barra in alto: Aggiorna e stato accanto ad Aggiungi torrent.** Il pulsante
  **Aggiorna** e il tondino dello stato del servizio (il pallino verde **ok**)
  stanno ora nella prima riga, subito a destra di **Aggiungi torrent**, invece
  che nella riga dei comandi sotto. Sotto restano lingua, dimensione del testo,
  font e tema.
- **Avviso quando il login a MirCrew si rompe.** Se il login al forum del
  servizio `mircrew-indexer` resta rotto per più di 15 minuti arriva una
  notifica (Telegram, email o webhook, secondo la configurazione), con il motivo
  riportato dal servizio e cosa fare; un secondo avviso arriva quando il login
  torna a funzionare. Prima l'unico avviso possibile era il fallimento di due
  link `.torrent` nello stesso ciclo, che non scatta quando le ricerche
  rispondono ancora dall'indice locale.
- **Correzioni a eD2k e ai file con più episodi.** Un file con un trattino
  prima della risoluzione o dell'anno (per esempio `S01E01-1080p`) non viene più
  scambiato per un file che contiene centinaia di episodi, quindi rinomina e
  importazione scelgono sempre il file giusto. Il pulsante che riaggiorna i file
  condivisi di aMule mostra subito la lista letta dopo la richiesta, non quella
  di prima. Se la ricerca eD2k scade mentre aspetta di riprovare, Gextto lo dice
  invece di parlare di un'altra ricerca in corso, e riconosce prima i risultati
  di una ricerca avviata da un altro programma collegato ad aMule. Gli esiti
  delle azioni su aMule mostrano solo messaggi fissi e tradotti: in caso di
  errore i dettagli sono nel log. CPU e RAM di aMule e di gx-torrent nella barra
  in alto pesano meno sulla macchina e restano stabili anche con più schede
  aperte.
- **Backup: anche file e cartelle fuori dalla cartella dati.** Nella scheda
  backup di Manutenzione, alla voce **File e cartelle da includere**, puoi
  elencare uno per riga i percorsi assoluti di file o cartelle da mettere nello
  ZIP di backup, per esempio gli script Docker con cui installi i servizi.
  Nell'archivio compaiono sotto `extra/` con il loro percorso originale; un
  percorso che non esiste più viene saltato con un avviso nel log, senza far
  fallire il backup. Lascia vuoto per includere solo la configurazione di
  Gextto.
- **eD2k: filtro tra i risultati e Scarica sempre presente.** Sopra i risultati
  della ricerca eD2k (pagina eD2k e archivio) c'è un riquadro per filtrare per
  parole nel nome, fonti minime, dimensione minima e file già noti, con
  l'ordinamento per fonti, dimensione o nome: agisce sui risultati già ricevuti,
  senza rifare la ricerca. Il pulsante **Scarica** resta su ogni risultato,
  anche nella ricerca release, che non perde più il riquadro dei risultati dopo
  la prima ricerca.
- **Barra in alto più leggibile.** I badge di stato (Gextto, gx-torrent,
  aMule, velocità, torrent, peer e prossimo ciclo) hanno una riga propria e sono
  più larghi; **Aggiungi torrent** resta accanto a loro, mentre lingua,
  dimensione del testo, font, tema e **Aggiorna** passano nella riga sotto.
- **eD2k: risultati coerenti anche con un'altra ricerca in corso.** aMule ha
  una sola lista di risultati, condivisa con la sua interfaccia e con ogni
  altro strumento collegato: se un'altra ricerca la sostituisce mentre Gextto
  cerca, Gextto rifà la ricerca e, se il disturbo continua, lo dice invece di
  mostrare file non attinenti. Vale per la ricerca dalla pagina eD2k e per
  quella in archivio con **eD2k** spuntato.
- **eD2k: preallocazione di aMule nella scheda di configurazione.** In
  **Configurazione → eD2k e Kad** c'è il pannello aMule con l'interruttore
  **Prealloca l'intero file in aMule** e **Impostazioni consigliate**: il valore
  viene applicato subito ad aMule (è una sua impostazione, non una salvata in
  Gextto). Con aMule in Docker le cartelle vengono controllate dentro il
  container: se mancano lì Gextto le segnala ma non le crea da fuori.
- **eD2k: cartelle di aMule mancanti segnalate e ricreate.** Se la cartella di
  arrivo o quella temporanea di aMule non esistono (disco di rete smontato,
  cartella rimossa), la pagina eD2k lo dice e **Crea le cartelle** le ricrea:
  l'accumulo di *Failed to get file system statistics* nel log si ferma e
  l'import dei completati torna a funzionare.
- **eD2k e Kad hanno una scheda loro in Configurazione.** Le nove voci di
  aMule ed eD2k (collegamento, rete di ricerca, ripiego, download fermi) non
  stanno più dentro **Fonti**: c'è una scheda **eD2k e Kad** dedicata, e i link
  «Apri la configurazione» portano lì.
- **eD2k: totali di sessione, preallocazione e avvisi.** La pagina eD2k mostra
  i byte scambiati da aMule nella sessione e se aMule prealloca i file;
  **Impostazioni consigliate** disattiva la preallocazione completa (quella che
  blocca `amuled` sui dischi di rete). In **Salute** c'è una scheda eD2k con
  stato, velocità, totali e gli ultimi avvisi presi dal log di aMule.
- **Pezzi: la mappa si vede anche a torrent fermo.** Aprendo i dettagli di un
  torrent in pausa o parcheggiato, la scheda **Pezzi** diceva «metadata non
  ancora presenti» anche con il 99% scaricato. I pezzi verificati ci sono: la
  mappa ora li mostra, con i mancanti in grigio.
- **Ricerca: anche i risultati eD2k si possono accodare.** Nella ricerca
  manuale i risultati eD2k mostravano «Solo ricerca» senza **Aggiungi**, come se
  non fosse possibile scaricarli. Ora si accodano come i torrent: il link
  `ed2k://` passa ad aMule. Con l'integrazione spenta i risultati eD2k non
  compaiono, invece di restare lì senza un'azione.
- **Barra di ricerca: il pulsante torna alto una riga.** **Apri link eD2k** non
  è più alto quanto la casella del link: mantiene l'altezza di un pulsante
  normale.
- **eD2k: la spunta si chiama eD2k, e il pulsante dei link sta su una riga.**
  Nella ricerca di Scarico (e nelle altre caselle di ricerca) la spunta non si
  chiama più «Kad» ma **eD2k**: la rete usata è quella scelta in
  Configurazione → Fonti → eD2k e Kad (Kad, server o entrambi), come dice anche
  il suo suggerimento. Il pulsante **Apri link eD2k** non va più a capo: il campo
  del link occupa lo spazio disponibile e il pulsante resta su una riga.
- **Pagina eD2k a schede, e aMule nella TUI.** La pagina eD2k ora ha tre
  schede — **aMule e ricerca**, **File condivisi** e **Log di aMule** — invece
  dei pannelli in fondo alla pagina. Nella TUI, con eD2k attivo, la scheda
  **Stato** mostra la riga **aMule** (versione, server, ID alto/basso, Kad,
  upload e coda) e i download di aMule compaiono nella scheda **Torrent**:
  `Invio` apre il dettaglio con fonti, priorità, ultimo dato e link `ed2k://`,
  `y` copia il link, `p` mette in pausa o riprende, `d` lo toglie da aMule.
- **Barra in alto: CPU e RAM per processo.** I due riquadri CPU e RAM sono
  diventati un badge unico **Gextto** con `CPU% · RAM`, e accanto compaiono
  **gx-torrent** (il motore torrent, quando il demone è locale) e **aMule**
  (quando l'integrazione eD2k è attiva e `amuled` gira sulla stessa macchina).
  Ogni badge si aggiorna ogni 5 secondi come gli altri valori; per i processi
  su un altro host non compare, perché quei dati non sono leggibili da lì.
- **eD2k: i download completati entrano subito nello Storico download.** Un
  file eD2k completato e importato in libreria compariva nello Storico solo alla
  riconciliazione successiva, fino a sei ore dopo. Ora viene registrato come
  concluso appena la lavorazione finisce, come un torrent rimosso dopo il
  completamento.
- **eD2k: vedi cosa aMule sta caricando.** Nella pagina eD2k c'è il pannello
  **File condivisi**: per ogni file i byte caricati nella sessione e in totale,
  richieste e accettate, client in coda, priorità e il link `ed2k://` da
  copiare; sotto, i client a cui stai caricando con software, velocità e
  posizione in coda. **Ricarica file condivisi** fa rileggere ad aMule le sue
  cartelle, così i file aggiunti o tolti a mano compaiono subito; le cartelle
  condivise sono elencate in sola lettura.
- **Episodi multipli nello stesso file: il nome porta l'intervallo.** Quando un
  download copre più episodi contigui (per esempio `S01E01E02`), il file in
  libreria viene rinominato `S01E01-E02`: Jellyfin e Plex elencano entrambi gli
  episodi e la scansione degli archivi registra il file sotto ciascuno di essi.
  Un intervallo non contiguo resta nominato con il primo episodio.
- **eD2k: i film completati da aMule entrano in libreria.** Prima un film
  scaricato via aMule con la cartella libreria configurata non veniva importato:
  restava in un ciclo di spostamenti falliti. Ora viene copiato o linkato come
  gli episodi e passa dallo stesso controllo di qualità prima di toccare il NAS:
  se c'è già una versione uguale o migliore il download viene scartato (nel
  Cestino) e non copiato. Senza cartella libreria il film resta in quella di
  arrivo di aMule e risulta completato.
- **eD2k e Kad passano ad aMule.** Il demone `gx-ed2k` viene ritirato: per la
  rete eD2k/Kad Gextto ora comanda **aMule** (`amuled`), che va installato e
  avviato a parte con le connessioni esterne (EC) attive. Con aMule acceso puoi
  **cercare** su Kad o sui server eD2k, **incollare link `ed2k://`** (anche più
  di uno, mescolati a testo) e **scaricarli davvero**: i download di aMule
  compaiono in **Scarico** accanto ai torrent e ai download HTTP, con
  progressione, velocità, fonti e i pulsanti pausa, riprendi e rimuovi. Quando
  aMule completa un file, Gextto lo rinomina e lo importa in libreria come un
  torrent (con hardlink o copia, così aMule continua a condividerlo). La nuova
  pagina **eD2k** (menu Scoperta) mostra lo stato di aMule (server e ID
  alto/basso, Kad, velocità, rete), la lista dei server con il pulsante per
  collegarsi, i comandi per connettere e disconnettere eD2k e Kad, il download di
  `server.met` e `nodes.dat`, la ricerca e il riquadro per i link. **Cosa fare:**
  in Configurazione → Fonti → eD2k e Kad accendi **eD2k e Kad tramite aMule** e
  inserisci indirizzo, porta e password delle connessioni esterne di aMule (le
  vecchie opzioni URL e token di gx-ed2k e quella UPnP spariscono). Il ripiego
  automatico nel gap filling resta un secondo interruttore, spento di default.
  Un aggiornamento rimuove il vecchio binario `gx-ed2k`.
- **eD2k: completamenti, download fermi e link non video.** I file completati da
  aMule 3.x (che li toglie subito dalla coda) vengono riconosciuti e importati,
  anche dopo un riavvio di Gextto. Un download eD2k scelto da Gextto che non
  riceve dati per 7 giorni (opzione **Rinuncia a un download eD2k fermo dopo**,
  0 = mai) viene annullato in aMule, finisce in blocklist e l'episodio torna da
  cercare; quelli aggiunti a mano e quelli in pausa non vengono toccati. Un link
  a un file che non è un video resta solo un download di aMule e non finisce più
  in quarantena. Le vecchie impostazioni di extto e di gx-ed2k vengono cancellate
  (indirizzo, porta e password di aMule restano). Il manuale spiega come usare
  aMule in Docker e come aprire le porte con l'UPnP.
- **eD2k: un link già in aMule viene comunque seguito.** Se aMule risponde che
  il file è già in lista, o è lento a rispondere mentre crea un file parziale
  grande, Gextto controlla la coda e, se il file c'è, lo registra e lo importa
  in libreria al completamento. Con aMule su un disco di rete conviene
  `AllocateFullFile=0` in `amule.conf`: la preallocazione blocca aMule per
  decine di secondi.
- **eD2k: banda, dettagli, log, contatori e TUI.** aMule segue gli stessi limiti
  di banda dei torrent (regola, eccezione, programmazione). In Scarico ⓘ apre il
  dettaglio di un download eD2k, con fonti, ultimo dato ricevuto, link da copiare
  e la priorità in aMule. La pagina eD2k mostra il log di aMule; dashboard e
  menu contano anche i download eD2k, e la TUI li elenca con pausa, ripresa e
  rimozione. Il ripiego automatico cerca su Kad e sui server eD2k insieme.
  Corretto un errore che segnava come fallito un episodio eD2k appena importato
  in libreria e che impediva al ripiego automatico di avviare un download.
- **eD2k: Kad + server, Archivio e dashboard.** Nella pagina eD2k e in
  **Rete delle ricerche eD2k** c'è la scelta **Kad + server eD2k**, che fa
  entrambe le ricerche e unisce i risultati. In **Archivio** la casella
  **Cerca anche su eD2k** mostra i risultati di aMule sotto l'archivio, con
  **Scarica**. Nella dashboard il grafico del traffico ha linee tratteggiate
  per aMule, distinte da quelle dei torrent, e la barra in alto somma anche
  l'upload di aMule.

## 19 ottobre 2026

- **Ricerca eD2k/Kad, quando i torrent non bastano.** Arriva un provider di
  ricerca opzionale, spento di default, che cerca su Kad e sui server eD2k con
  il demone `gx-ed2k` (Go puro, nostro). Nella ricerca manuale i risultati eD2k
  compaiono accanto ai torrent con il badge della rete e il numero di fonti; un
  link `ed2k://` incollato viene verificato e mostrato con nome, dimensione e
  hash. In Configurazione → Sorgenti c'è il gruppo **eD2k e Kad**: accensione,
  indirizzo e token del demone, un secondo interruttore per usarlo nel gap
  filling solo quando i torrent non trovano nulla, soglia di fonti e timeout.
  Il fallback automatico è spento e accetta solo un episodio esatto con qualità
  riconosciuta. Nella pagina **Sorgenti** la voce **eD2k e Kad** mostra una spia
  della connessione (**aperta**, **limitata** da firewall/NAT, **sconosciuta**) e
  il demone prova ad aprire la porta Kad con **UPnP** (UDP 4672 e TCP 4662): se
  funziona i server eD2k assegnano un HighID. I nodi Kad imparati vengono salvati
  e riusati al riavvio. **Il trasferimento eD2k non c'è**: il demone dichiara di
  saper solo cercare e il pulsante per accodare resta disattivato, quindi la
  libreria non può riempirsi di file sbagliati. Nel pannello **Cerca una
  release** di **Scarico** c'è la casella **Kad**: spuntala per includere la
  rete anche in quella singola ricerca, oltre a DB, web e indexer. Se il
  provider è spento, la singola ricerca lo avvia senza cambiare
  l'impostazione globale. Il ripiego
  automatico interroga anche i **titoli alternativi** della serie (per esempio
  il titolo originale al posto di quello tradotto), così un titolo localizzato
  non nasconde i release con il nome originale.
- **gx-ed2k standalone, con aggiornamenti.** L'archivio Linux del demone di
  ricerca esce ora insieme alle altre release (stessa cartella di download) e
  la sua pagina ha **Controlla aggiornamenti**: confronta la build con
  l'ultima pubblicata e, se è più nuova, dà il link all'archivio giusto per
  l'architettura. C'è anche l'archivio **Windows** (`gx-ed2k-windows-<arch>.zip`)
  con `install-service.ps1`, che registra il servizio e apre UDP 4672/TCP 4662
  nel firewall di Windows. Su Linux il controllo può anche **installare**
  l'aggiornamento (checksum e versione verificati, vecchio binario conservato,
  riavvio automatico); su Windows resta il download dello zip. Una build
  compilata dal codice locale non propone nulla.
- **Aggiornamenti più sicuri.** Un aggiornamento, dal pulsante **Aggiorna** o
  rilanciando l'installer, è riuscito solo quando l'interfaccia della nuova
  versione risponde. Se la nuova versione cade dopo qualche secondo dall'avvio,
  torna subito quella precedente: prima veniva data per buona e il servizio
  restava fermo. Rilanciare l'installer tiene l'utente del servizio già
  installato, invece di passare all'account `gextto` quando lo si esegue da root
  senza `sudo`.
- **Grafico del traffico.** Nella Dashboard il tratto degli ultimi 30 minuti
  in cui non ci sono ancora dati resta vuoto, invece di una rampa che saliva
  da zero fino al primo valore. Le velocità sotto 1 KB/s valgono 0: è solo
  rumore di protocollo, e il grafico resta piatto invece di tremare.
- **La banda in Dashboard dice la verità.** Con la velocità **Auto** il
  riquadro **In sessione** mostra il badge **Auto**, invece di «Nessun limite»:
  la velocità non è libera, è Gextto che misura la linea e la adatta.
- **Log di avvio più chiari.** Le righe che ripristinano lo stato salvato
  (download fermi, attese dei metadati, spostamenti in sospeso) ora elencano i
  torrent coinvolti, non solo quanti sono: si capisce subito di cosa si parla.
- **Traduzioni.** Il contatore dei download dice «Torrent: 5 · HTTP: 0», giusto
  in ogni lingua; il riquadro del motore in Dashboard si chiama «Engine» in
  inglese e «Motor» in tedesco.
- **Velocità: una regola e un'eccezione.** In Configurazione → Velocità e rete
  una sola scelta, **Come si decide la velocità**, decide la regola: Auto, limiti
  fissi o programmazione oraria. Con la programmazione scegli anche se fuori dalle
  fasce vale Auto o i limiti fissi. Le impostazioni che avevi restano quelle: non
  devi rifare nulla. In Scarico il menu **Banda** dice cosa vale adesso e da dove
  viene, e fa un'**eccezione**: Auto o un limite, per 1, 3, 6 o 12 ore o finché
  non la togli. L'eccezione vince sempre sulla regola, programmazione compresa:
  un limite impostato non viene più scavalcato da una fascia oraria. Il
  meccanismo che misura la linea si chiama ovunque **Auto**.
- **Scarico più ordinato.** Filtro, tag e ordinamento stanno su una sola riga;
  **Pulisci completati**, **File orfani** e l'aggiornamento automatico sono nel
  menu **Strumenti**. Pausa, Riprendi, Ricontrolla, i tag e Rimuovi dalla sessione
  compaiono solo quando selezioni dei torrent, e la selezione non si perde più
  quando l'elenco si aggiorna da solo. «Elimina completati dopo il seed» non è più
  nella barra: si imposta in Configurazione → Seeding, e il menu Strumenti porta
  lì. «Sblocca pin» si trova nella scheda del torrent fissato, che nell'elenco ha
  il badge 📌.
- **Menu Banda.** I campi dei limiti temporanei in cima al pannello lasciano il
  posto a un pulsante **Banda** che mostra i limiti in vigore. Dal menu imposti al
  volo Auto o un limite di download o di upload (da 64 KB/s a 10 MB/s) e la sua
  durata, con un clic. I valori personalizzati restano in **Valori personalizzati…**.

- **Pack scartati senza lasciare tracce sbagliate.** Quando l'ispezione decide
  che un pack in download non serve, Gextto ripristina la versione precedente
  dell'episodio: la libreria non resta più con la qualità e il titolo della
  release scartata sopra il file vecchio.
- **Log delle decisioni molto più espliciti.** Il download di un aggiornamento
  dice quale qualità c'è già in libreria (`from: S01E01 480p h264 ...`); lo
  scarto di un pack mostra il confronto episodio per episodio; il pack
  parzialmente utile elenca gli episodi tenuti e quelli saltati.
- **Attesa dei metadati leggibile.** Il messaggio “nessuno ha fornito la lista
  dei file” ora riporta peer, byte ricevuti e ora di inizio; quando la lista
  arriva, una riga lo dice; se il contatore riparte, il log spiega perché.
- **L'attesa dei metadati sopravvive ai riavvii.** Se un magnet sta aspettando
  l'elenco dei file e Gextto viene riavviato, il conto riprende da dove era:
  dopo 24 ore senza elenco viene scartato come link morto, invece di ricevere
  una finestra nuova a ogni riavvio.
- **Qualità reale contro release registrata.** Se il file in libreria e il
  titolo registrato non concordano (per esempio un file 480p con un titolo
  1080p), il controllo qualità lo segnala e dichiara che vince il file.
- **Manuale e changelog online.** Le voci **Manuale** e **Changelog** aprono la
  documentazione aggiornata su GitHub in una nuova scheda, nella lingua
  dell'interfaccia: italiano per la UI italiana, inglese per le altre. Aggiornare
  manuale e changelog non richiede più di ricompilare Gextto; il vecchio
  indirizzo `/?view=manual` porta al manuale online.

## 18 ottobre 2026

- **Ispezione dei pack durante il download.** Gextto legge la lista dei file
  appena arriva, confronta gli episodi con la libreria e scarta un pack che non
  aggiunge nulla; con l'opzione dedicata scarica solo i file utili, non l'intero
  pack.
- **Link `.torrent` morti messi da parte.** Un link che fallisce in due cicli
  diversi viene messo in quarantena e non blocca più la ricerca; un magnet che
  l'indexer non serve ancora non viene trattato come link morto.
- **Auto speed più coerente.** L'interruttore Auto segue la configurazione e i
  timeout della ricerca sono diagnosticati senza riempire il log di avvisi.
- **Motore più leggero.** gx-core non abbassa più il limite dei descrittori e
  non tiene un pool di file non necessario.

## 17 ottobre 2026

- **Ricerca più ordinata.** Jackett e Prowlarr vengono interrogati un indexer
  alla volta; i link morti finiscono in quarantena; l'archivio è indicizzato per
  torrent (infohash) e non per il testo del magnet.
- **Regole di seed più fini.** Si possono fermare i seed per sorgente o
  categoria e impostare un limite di seed inattivi; lo stato sopravvive al
  riavvio.
- **API più osservabile.** Metriche Prometheus, stream di eventi legato alla
  sessione e chiamate compatibili qBittorrent 5 per gx-torrent.
- **Meno memoria sprecata.** Il motore rilascia la memoria del filtro IP e dei
  buffer uTP dopo l'uso; il peer reader non gira più a vuoto sulle connessioni
  uTP morte.
- **Diagnosi su richiesta.** Profili pprof opt-in solo su loopback, condivisi tra
  Gextto e gx-torrent.
- **Backup e ripristino verificati** end-to-end insieme ai pacchetti di release.

## 16 ottobre 2026

- **Dashboard più viva.** Nuovo grafico della banda degli ultimi 30 minuti,
  riquadro “In sessione” unico e avvisi “Da controllare” che restano finché il
  problema non è risolto, con pulsante Pulisci.
- **Auto speed e fascia oraria.** La modalità giorno ha tre stati, la fascia
  “Auto” è gestita dall'auto-tune e la velocità proposta tiene conto del picco
  osservato.
- **Configurazione più guidata.** Livelli Facilitata/Esperto, scheda “Gestione
  automatica” che si adatta, badge di provenienza dei valori e sistema di
  tuning che propone modifiche invece di applicarle di nascosto.
- **Pack di stagione solo se servono.** Un pack viene scaricato solo quando
  contiene almeno un episodio mancante o un vero aggiornamento.
- **Punteggio più onesto.** La salute dello swarm entra nella scelta del
  candidato; gli episodi archiviati vengono valutati dal file reale e il titolo
  nella lingua originale evita i titoli segnaposto.
- **Download più sicuri.** L'approvazione e l'aggiunta del torrent passano da un
  unico percorso crash-safe.

## 15 ottobre 2026

- **Libreria a locandine.** Le liste Serie TV e Film possono essere viste come
  card con locandina, stato e progresso, anche su telefono.
- **Dettaglio torrent più comodo.** Il dettaglio si apre dal nome e diventa un
  pannello in basso, con statistiche del motore e “riscarica” funzionante.
- **gx-torrent standalone più completo.** Menu More, Cercati con pack di
  stagione, cartelle dei feed, ricerca interattiva per episodio, manutenzione
  dell'archivio e controlli di accessibilità.
- **Notifiche più precise.** Un pack di stagione distingue gli episodi nuovi
  dalle copie aggiornate.
- **Script di supporto.** Pulizia intelligente di `/tmp` con timer systemd e
  `gx-core-test.sh` che usa una build locale accanto alla produzione.
- **Log utente in inglese** e meno rumore nel battito di stato.

## 14 ottobre 2026

- **Chiavi di accesso dalla UI.** Le chiavi API di Gextto e il token di
  gx-torrent si generano e si copiano dall'interfaccia.
- **Fallback motore.** Se il motore configurato non è disponibile, Gextto può
  usare qBittorrent-nox su richiesta; la build con libtorrent incorporato non
  viene più distribuita.
- **gx-torrent sempre più completo.** Pagina Statistiche, interfaccia
  alternativa VueTorrent, gestione automatica dei torrent (autoTMM), cartelle
  watch, torrent creator, cookie, limiti alternativi, API RSS e ricerca.
- **Media server DLNA/UPnP** opt-in per i contenuti completati, con cartelle
  della libreria e sottotitoli.
- **Aggiornamento in-app** anche per gx-torrent standalone.
- **Scheduler di banda per giorno** nella UI di Gextto e versione di gx-core
  visibile nell'intestazione.

## 13 ottobre 2026

- **Swarm Merging.** Il motore riusa i dati già presenti in altre release o
  layout dello stesso contenuto, anche quando un sibling compare più tardi.
- **Tracker WebSocket** (`ws`/`wss`) e **DHT feeds** (BEP 46).
- **uTP più reattivo.** RTO adattivo con pavimento di latenza minima e backoff
  guidato dalle perdite; contatori di ritrasmissione e SYN-drop nelle due UI.
- **Pipelining multi-pezzo**: un peer può scaricare due pezzi alla volta.
- **Disk guard**: i download si mettono in pausa se la cartella sta finendo lo
  spazio.
- **Nuova UI di gx-torrent** su design system condiviso, log standalone e
  CPU/RAM del processo.
- **Cercati più chiari**: stato Have/Missing e gestione episodi nel dialogo.

## 12 ottobre 2026

- **gx-torrent standalone utilizzabile dalla LAN**, con tab Cercati dedicata,
  controllo immediato dei feed e ricerca manuale.
- **FlareSolverr** per feed e indexer; l'archivio diventa l'indice principale e
  gli indexer si usano per i buchi.
- **Temi personalizzati** con colori scelti dall'utente e terzo tema “crema”;
  feedback quando si svuota il cestino.
- **Anti-SSRF**: web seed e sorgenti magnet non possono raggiungere la LAN.
- **Motore**: storage mmap opt-in con `fdatasync`, DHT ritentato ogni 15 secondi
  a freddo, priorità file graduate 0–7, Swarm Merging fase 1, BEP 44/46.
- **Download**: “Clean completed” non si blocca più su una copia enorme nel
  cestino; i limiti globali di seed sono rispettati.
- **UI**: dettaglio torrent a due colonne, provider status completo, indirizzo
  Torznab coerente tra Gextto e gx-torrent.

## 11 ottobre 2026

- **DHT più resistente**: limite opzionale di nodi per subnet (anti-Sybil),
  token legati all'infohash, concentrazione della routing table visibile.
- **NAT**: tipo di mapping dedotto dagli osservatori DHT e supporto PCP
  (RFC 6887).
- **Auto-speed sulla latenza** (anti-bufferbloat).
- **RSS più potente**: feed con scope, cartelle, filtri episodio, smart episode,
  REPACK/PROPER, regole disattivabili e “segui una serie”.
- **BEP**: 54 (`lt_donthave`), 53 (`ws=`), 21 (`upload_only`), 38, 31.
- **Standalone**: wizard, indexer, RSS, limiti e scheduler visibili nella UI.
- **Sicurezza**: hardening di create-torrent, login redirect e cookie di
  sessione.

## 10 ottobre 2026

- **Windows**: servizio nativo, pacchetto zip, junction al posto dei symlink,
  move tra volumi e wizard; gx-torrent scarica e compila anche su Windows.
- **BitTorrent v2** (BEP 52): metainfo, file tree, piece layers SHA-256, magnet
  e torrent v2-only.
- **IPv6** dual-stack: tracker UDP, `peers6`, PEX, DHT.
- **API qBittorrent** sempre più completa, RSS, notifiche webhook, scheduler di
  banda e modalità standalone con login/CSRF/LAN.
- **Sei lingue** per UI, TUI e notifiche; accessibilità e tooltip.
- **Osservabilità**: durata e sorgenti dei cicli, avvisi a soglia, pannello
  “Da controllare”.
- **Pack abbandonati**: gli episodi già completi vengono portati in libreria
  prima di rimuovere il torrent.

## 9 ottobre 2026

- **Installer più sicuro**: servizio con l'utente locale, update con rollback,
  uninstall, dry-run e self-test in CI.
- **Procedura guidata**: lingua, cartelle con Sfoglia, TVDB come alternativa a
  TMDB, test porta e banda.
- **Aggiornamento in-app** e pacchetto portabile.
- **Build di default in Go puro**, con libtorrent opt-in.
- **Impostazioni**: pulsante “Predefinito” su ogni campo, motore gx-torrent
  predefinito, opzioni non coerenti con il motore disabilitate.
- **Holepunching BEP 55** per gx-torrent.
- **TVDB** completo come alternativa a TMDB.
- **Notifiche** su riavvio/crash di Gextto e problemi di gx-torrent.
- **Prima/ultima parte** come impostazione globale; correzioni a rename,
  seed e pack di stagione.

## 8 ottobre 2026

- **Notifiche verso Discord, Slack, ntfy, Gotify e Pushover**, oltre a Telegram
  e webhook.
- **UI v2 più ordinata**: Configurazione a aree, header sticky, pannello job
  solo quando serve, Integrazioni raggruppate.
- **gx-torrent**: super-seeding BEP 16, streaming HTTP con Range, limiti per
  torrent, diagnostica per pezzo, web seed e tracker modificabili a runtime.
- **gx-torrent sopravvive ai riavvii di Gextto** e ha un proprio numero di
  build, log ruotato e pannello nella UI di Gextto.
- **Database**: niente riscrittura a ogni riavvio o tick di stall.
- **Sicurezza**: conversione interi e path hardening.

## 7 ottobre 2026

- **gx-torrent diventa il motore predefinito**, con parità rispetto a
  libtorrent: percorsi di salvataggio, banda, code, categorie, uTP, LSD,
  filtro IP, cache, porta singola, selezione file e gestione degli stall.
- **UI gx-torrent**: pagina web operativa, dettaglio torrent, statistiche,
  LAN, IP filter, token spostato nella scheda Accesso.
- **Login opzionale** per i client fuori dalla LAN.
- **Calendario iCal** di uscite, messa in onda originali e film.
- **Anime**: numerazione assoluta degli episodi.
- **Fumetti**: `ComicInfo.xml` anche nei CBZ scaricati direttamente.
- **Archivio**: hardlink dei file in seed verso la libreria invece di copie.
- **Aggiornamenti**: soglia di punteggio oltre la quale non si sostituisce più.
- **Jellyfin/Plex**: riscansione solo della cartella cambiata.
- **TUI**: nuova gestione di libreria, download, dettaglio e manutenzione.

## 6 ottobre 2026

- **TUI più completa**: libreria, download, dettaglio torrent e manutenzione,
  con layout adatto anche a terminali stretti e poco traffico.
- **Backup Telegram** diviso quando supera il limite dell'API.
- **Notifiche** senza duplicati e senza avvisi per pezzi danneggiati.
- **Cestino e pulizia**: i file orfani nella cartella temporanea finiscono nel
  cestino; i file sparsi restano sparsi.
- **Film**: cartelle con un solo video archiviate come file piatti; cartelle
  residue rinominate.
- **Stall**: lo stato di attesa sopravvive ai riavvii.
- **Log in linguaggio semplice** e indicazione dei file letti dal backfill
  MediaInfo.
- **Punteggi** ricalcolati quando cambiano i pesi.

## 5 ottobre 2026

- **Archivio**: corrispondenza degli episodi nelle sottocartelle, cestino come
  default, prova di scrittura e avviso di upgrade; finalizzazione post-seed.
- **Ciclo**: rollback del placeholder se il download viene saltato, budget del
  RAM disk rispettato, dry-run e cestino più coerenti.
- **UI**: ricerca nelle tabelle che resta dopo un'azione, niente sfarfallio dei
  form, stato Prowlarr visibile.
- **Stall**: stato di retry persistente e avviso dopo tre ore.
- **REPACK** preferiti nella sostituzione dell'archivio.

## 4 ottobre 2026

- **Libreria**: speciali preservati, sorgenti deduplicate, ricerca episodi in
  modale, indice FTS ricostruito se fuori sincrono.
- **Serie**: ordine alfabetico predefinito e azioni di ricerca nella pagina.
- **Dashboard**: titoli in arrivo collegati a TMDB/TVDB.
- **Salute**: riga del servizio mircrew-indexer e link ai provider.
- **Manutenzione**: integrità SQLite e indici.
- **Mobile**: navigazione inferiore più leggibile e layout download migliorato.
- **Dettaglio torrent**: tab più chiari e log degli stall.

## 3 ottobre 2026

- **Manutenzione**: VACUUM e ottimizzazione con toast di avvio/completamento,
  dimensioni database corrette e integrità verificata.
- **Archivio**: opzione “archivia e rimuovi”, notifiche di completamento e
  pulizia dei contenuti.
- **Filtri contenuto** applicati prima dell'archiviazione.
- **Punteggi**: gruppi personalizzati in fondo e modificatori corretti.
- **Installer** e supporto PayPal.
- **Log**: avvio dei singoli download e riepilogo del seeding.

## 2 ottobre 2026

- **Rimossi Trakt e il backend Anacrolix**: restano gx-torrent e qBittorrent-nox.
- **Mobile**: barra a tab in basso, topbar semplificata, torrent a card e Log
  sempre raggiungibile.
- **Sicurezza torrent**: salvaguardia nella rimozione, routing al cestino,
  pulizia dei file temporanei e lock sulla concorrenza.
- **Post-processing**: gestione dei doppioni inferiori, pack di stagione,
  fallback MediaInfo e cestino.
- **CodeQL** e selettore log v2 corretti.
- **Ricerca**: ricerca dei buchi e risultati torrent migliorati.

## 1 ottobre 2026

- **La UI v2 diventa l'interfaccia ufficiale**: dashboard, download, archivio,
  dettaglio serie/film, ricerca, log e configurazione in SSR + HTMX.
- **Dashboard**: orologio, metriche live, storico film scaricati, ricerca
  progressiva e prossime uscite.
- **Ciclo**: priorità ai buchi presenti in archivio, avanzamento delle ricerche
  e riepilogo dei monitoraggi manuali.
- **Provider e backup** più chiari nelle verifiche.
- **TUI**: scheda libreria accessibile e metriche ampliate.
- **Decine di correzioni UI** per allineamento, tabelle, modali e mobile.

## 30 settembre 2026

- **Sorgenti episodio** in modale nel dettaglio serie.
- **Impostazioni consapevoli del motore**: le opzioni non coerenti con il motore
  attivo vengono disabilitate.
- **Seed e archivio**: seed prima dell'archiviazione, `auto_remove_completed`
  rispettato, seed ratio e pulizia archivio effettivi.
- **Housekeeping**: ritenzione configurabile.
- **UI**: job manager per le operazioni lunghe, accessibilità delle tabelle,
  font selezionabili e calendario TMDB in cima a Esplora.
- **FlareSolverr**: sessioni scadute e orfane distrutte.
- **Mobile** più essenziale.

## 29 settembre 2026

- **Nuove lingue**: tedesco, francese, spagnolo e polacco.
- **UI**: tile prestazioni su mobile, paginazione dello storico, storico fumetti,
  azioni archivio, tooltip e diagnostica.
- **Installer**: self-test, aggiornamenti sicuri, rollback, uninstall e dry-run.
- **Indexer, FlareSolverr e RAM disk** migliorati.
- **MediaInfo backfill** più affidabile.
- **Log**: esito dei test di connessione a INFO, meno duplicati e dettagli
  tecnici.
- **Concorrenza, shutdown deterministico e integrità dati**.
- **Piano di sostituzione Sonarr/Radarr** documentato.

## 28 settembre 2026

- **Migrazione completa alla UI server-rendered**: tutte le 16 pagine, form,
  tabelle e azioni.
- **Mobile-first**: layout telefono, touch target, font e tema pastello.
- **Libreria**: form serie/film con qualità, lingue, sottotitoli, alias e
  percorso NAS; dettaglio con cast, prossimo episodio e stagioni.
- **Archivio ed Esplora**: ricerca TMDB, calendario, fonti episodio, badge
  “in libreria”.
- **qBittorrent gestito** con watchdog e fallback.
- **TUI**: tab archivio/mancanti/blocklist, modifica chiavi e rendering
  migliorato.
- **Shutdown** dei worker prima della distruzione della sessione.
- **Cloudflare/FlareSolverr** più robusto.

## 27 settembre 2026

- **Prima release pubblica di Gextto.**
- **Nuova UI Go server-rendered**: migrazione delle 16 pagine, editor i18n,
  libreria, archivio, blocklist, manutenzione e integrazioni.
- **Motori torrent**: qBittorrent selezionabile, backend Anacrolix, matrice
  delle capacità e controlli di connettività.
- **Sicurezza**: correzioni CodeQL, path contenuti, conversione interi e
  gestione dei segreti.
- **TUI bilingue** italiano/inglese.
- **Log e tooltip** più chiari.
- **Dipendenze** aggiornate e bundle UI ricostruito.
