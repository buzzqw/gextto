# Changelog di Gextto

Qui raccontiamo cosa cambia in Gextto dal punto di vista di chi lo usa, in ordine
di data. Le voci sono raggruppate per giorno e per effetto visibile: i commit di
sola build, test, documentazione o manutenzione interna non hanno una voce
separata, ma sono rappresentati nella voce della modifica a cui appartengono.
Per il dettaglio tecnico restano i commit e i documenti in `docs/`.

La versione installata è quella mostrata da Gextto (`<serie>.<build>`, per
esempio `1.1.1961`): il terzo numero è sempre la build, il secondo si alza a
mano quando si apre una nuova serie. Il manuale
aggiornato è online: [italiano](docs/MANUAL.it.md) ·
[English](docs/MANUAL.en.md). La versione inglese di questo changelog è
[changelog.en.md](changelog.en.md). Per segnalazioni:
[issue su GitHub](https://github.com/buzzqw/gextto/issues).

## 22 ottobre 2026

- **Upgrade verificati con MediaInfo prima di entrare in libreria.** Quando un download completo deve sostituire un episodio o un film già archiviato, Gextto legge con `ffprobe` il file nuovo (ancora nella cartella di download) e la copia in libreria prima di spostare o copiare qualsiasi cosa. Se il nome prometteva più del contenuto reale (un «2160p» ingrandito da un 1080p, un «HDR» falso, un audio diverso da quello dichiarato) e la qualità vera non è un upgrade, il download viene scartato e messo in blocklist, e la copia in libreria resta. Senza `ffprobe` vale la decisione presa prima del download; i pack di stagione non sono ancora verificati così.

- **WEB-DL e WEBRip: decidi tu se sono la stessa qualità.** La regola che sostituiva sempre un WEBRip o un HDTV con un WEB-DL segue ora i pesi **Sorgente** di *Qualità e upgrade*: se WEB-DL e WEBRip hanno lo stesso peso, un WEB-DL non sostituisce più un WEBRip già in libreria. HDR, una risoluzione più alta, REPACK/PROPER e un punteggio migliore di almeno la differenza minima restano motivi validi.

- **Qualità e upgrade: verifica dell'impatto prima di riscaricare.** Il nuovo pannello **Impatto sulla libreria** ripassa le release già viste nei feed con le regole e i pesi attuali e dice quanti episodi archiviati verrebbero sostituiti nei prossimi cicli, con alcuni esempi, senza scaricare nulla. Serve dopo aver cambiato pesi o soglie: se il numero è alto, conviene rivedere la modifica prima che parta un riscaricamento di massa. Il risultato diventa un avviso quando supera il 5% degli episodi archiviati.

- **Aggiunta di serie e film: profili e "Niente upgrade".** Il modulo di aggiunta chiede ora solo il **Profilo di qualità** (con un riassunto di cosa imposta): qualità, audio minimo, lingua, sottotitoli, esclusioni e **Niente upgrade** del nuovo titolo vengono dal profilo, senza altri campi che possano contraddirlo. Resta un campo **Esclusioni in più** per le parole che valgono solo per quel titolo, sommate a quelle del profilo. Dopo l'aggiunta i singoli valori si cambiano nella scheda del titolo, dove **Niente upgrade** c'è ora anche per i film. I profili di qualità hanno due campi nuovi, **Audio minimo** e **Niente upgrade**, che vengono applicati anche ai titoli aggiunti da Jellyseerr e con **Modifica selezionati**, dove ora si può cambiare anche l'audio minimo di più titoli insieme. Corretto anche un errore per cui alcuni salvataggi delle schede (per esempio un interruttore sì/no) potevano a volte essere rifiutati.

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
  file della serie cercata: una serie con un nome simile (per esempio «Lost
  Girl» cercando «Lost») non compare più tra i risultati.
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
  «Murdoch Mysteries» per «I misteri di Murdoch»), così un titolo localizzato
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
