# Changelog di Gextto

Qui raccontiamo cosa cambia in Gextto dal punto di vista di chi lo usa, in ordine
di data. Le voci sono raggruppate per giorno e per effetto visibile: i commit di
sola build, test, documentazione o manutenzione interna non hanno una voce
separata, ma sono rappresentati nella voce della modifica a cui appartengono.
Per il dettaglio tecnico restano i commit e i documenti in `docs/`.

La versione installata è quella mostrata da Gextto (`1.1.<build>`). Il manuale
aggiornato è online: [italiano](docs/MANUAL.it.md) ·
[English](docs/MANUAL.en.md). La versione inglese di questo changelog è
[changelog.en.md](changelog.en.md). Per segnalazioni:
[issue su GitHub](https://github.com/buzzqw/gextto/issues).

## 19 ottobre 2026

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
