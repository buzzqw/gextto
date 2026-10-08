# Rapporto tecnico Terra — miglioramenti proposti per Gextto

## Scopo

Questo rapporto nasce dalla lettura del sorgente, della documentazione e dai test
del repository. Elenca miglioramenti concreti; non afferma vulnerabilità
confermate in produzione e non modifica il comportamento del daemon.

## Stato dell'intervento

Sono stati implementati i punti 2, 3 e 5, oltre al primo passo del punto 7:
limite di concorrenza indexer, contesto radice di shutdown con attesa completa
dei worker, test di parità router/documentazione e registrazione UI isolata. Il
punto 1 è escluso su richiesta.

I punti 4 e 6 restano deliberatamente proposte progettuali: una cache archivio
senza strategia di invalidazione affidabile può accettare un upgrade errato; la
sostituzione delle API C++ richiede una matrice di versioni libtorrent realmente
supportate. Non vengono introdotte scorciatoie che cambierebbero silenziosamente
la correttezza delle decisioni o la compatibilità di build.

## Priorità

| Priorità | Area | Intervento |
|---|---|---|
| Alta | Esposizione HTTP | Separare o restringere il listener `EngineListen`. |
| Alta | Ricerca provider | Limitare la concorrenza degli indexer per richiesta. |
| Media | Arresto | Rendere deterministica la chiusura dei worker prima di libtorrent. |
| Media | Scalabilità | Togliere le scansioni complete dell'archivio dal ciclo critico. |
| Media | API | Generare e verificare automaticamente il riferimento API. |
| Bassa | Bridge C++ | Rimuovere le API libtorrent deprecate. |
| Bassa | Router | Modularizzare la registrazione delle route. |

## 1. Limitare l'esposizione del listener motore

**Evidenza.** In [`web_serve.go`](../../web_serve.go), `Serve` crea `webListener` e
`engineListener`, ma assegna a entrambi lo stesso `Router(state)` (righe
174–238). La porta `EngineListen` serve quindi UI e tutte le API, non solo una
superficie interna del motore.

**Rischio.** L'applicazione non ha autenticazione integrata. Se la porta motore
viene esposta involontariamente su una rete più ampia, diventa un secondo accesso
amministrativo per file, torrent, backup e configurazione.

**Proposta.** Definire lo scopo del secondo listener. Se è interno, vincolarlo a
loopback e usare un mux ristretto alle route indispensabili. Se è un'API
intenzionale, rinominarlo e applicare la stessa protezione della UI. Rifiutare un
bind non-loopback senza una configurazione esplicita di esposizione.

**Verifica.** Test con due porte: le route amministrative devono rispondere solo
sul listener autorizzato; test unitari del controllo di bind e documentazione
esplicita per entrambi gli indirizzi.

## 2. Applicare un budget di concorrenza agli indexer

**Evidenza.** In [`engine.go`](../../engine.go), `searchOneWithDB` avvia una goroutine
per ogni indexer abilitato (righe 411–438), senza semaforo. Il ciclo limita a due
le ricerche di titoli, ma ogni ricerca può interrogare contemporaneamente tutti
gli indexer configurati.

**Impatto.** Con molti provider, oppure con UI e ciclo attivi insieme, aumentano
socket, richieste, rate limit e pressione sui servizi Torznab.

**Proposta.** Introdurre un semaforo condiviso per gli indexer, configurabile con
un default prudente (4–6). Conservare timeout e ordine dei risultati; la
cancellazione deve impedire l'avvio del lavoro ancora in coda.

**Verifica.** Un server HTTP di test deve attestare il massimo di richieste
concorrenti. Esporre metriche o log strutturati di richieste attive, in attesa e
in timeout per provider.

## 3. Chiudere i worker senza una finestra di gara nativa

**Evidenza.** `stopBackgroundWorkers` in [`web_serve.go`](../../web_serve.go) attende
al massimo 15 secondi (righe 112–129), quindi prosegue anche se un ciclo è ancora
in corso. Il chiamante in [`cmd/gexttod/main.go`](../../cmd/gexttod/main.go) arresta poi
backend e sessione libtorrent.

**Impatto.** Cancellazione e lock sono già gestiti con cura, ma una operazione
lenta o non cooperativa può concorrere con la distruzione di una risorsa CGo.

**Proposta.** Usare un contesto radice del daemon e un registro delle operazioni
in corso. In shutdown: fermare nuove richieste, cancellare, attendere la
quiescenza e solo dopo distruggere il backend. Se il limite systemd scade,
registrare chiaramente i worker residui e impedire loro l'accesso al backend.

**Verifica.** Test con ciclo e stream SSE artificialmente bloccati, `go test
-race` e ripetizione di shutdown/startup. Nessuna chiamata al backend deve
avvenire dopo l'inizio della sua chiusura.

## 4. Spostare l'indicizzazione dell'archivio fuori dal ciclo critico

**Evidenza.** `RunCycleDomain` chiama `IndexArchive` per ogni serie non ancora
in cache nel ciclo ([`orchestrator.go`](../../orchestrator.go), righe 806–829). La cache
evita duplicati nello stesso ciclo, ma una libreria NAS grande può essere riletta
ad ogni ciclo.

**Impatto.** La durata dell'acquisizione dipende da latenza del filesystem e
numero di serie candidate; su NAS lenti diventa difficile distinguere ricerca
lenta da disco lento.

**Proposta.** Persistire un indice di qualità incrementale nel database,
aggiornato da post-processing, scansione manuale e worker periodico. Il ciclo usa
l'ultimo indice valido con timestamp; la scansione sincrona resta fallback per un
indice assente o una richiesta esplicita.

**Verifica.** Benchmark con migliaia di file e mount simulato lento; le decisioni
di upgrade devono restare identiche a quelle con indice ricostruito.

## 5. Rendere l'API un contratto verificabile

**Evidenza.** Le route sono registrate manualmente in
[`web_router.go`](../../web_router.go) e `docs/API.md` è una tabella manuale. La
revisione ha trovato due route qBittorrent nel router ma assenti dalla tabella;
sono ora documentate, ma non esiste un controllo automatico nel repository.

**Proposta.** Aggiungere un test che confronti router e `docs/API.md`, oppure un
registro dichiarativo da cui produrre entrambi. Per le integrazioni esterne,
documentare request body, risposta, errori e capability; un OpenAPI parziale è
l'evoluzione consigliata.

**Verifica.** Il test deve fallire quando una route pubblica cambia metodo,
percorso o presenza senza aggiornare la documentazione.

## 6. Pianificare la compatibilità con le nuove API libtorrent

**Evidenza.** `make test` passa, ma [`libtorrent_bridge.cpp`](../../libtorrent_bridge.cpp)
genera warning per API deprecate: `half_open_limit`, cache, priorità, campi
tracker e flag di resume. Il bridge contiene già un commento relativo ai campi
deprecati in ABI v2.

**Proposta.** Creare una matrice delle versioni libtorrent supportate e sostituire
progressivamente le API deprecate con adapter per versione. In CI, trattare i
warning attesi come lista controllata, così i nuovi warning non passano inosservati.

**Verifica.** Build contro versione minima e più recente supportate, più test di
trasferimento locale, restore, tracker, limiti e coda.

## 7. Modularizzare il router senza cambiare il contratto

**Evidenza.** [`web_router.go`](../../web_router.go) registra centinaia di route in una
funzione, mentre gli handler sono già divisi per aree in più file.

**Proposta.** Introdurre registri per dominio (`registerTorrentRoutes`,
`registerLibraryRoutes`, `registerIntegrationRoutes`, ecc.) e mantenere uno
snapshot test delle route. I registri possono in futuro fornire metadata per API,
autorizzazione e capability.

**Verifica.** La rifattorizzazione non deve cambiare path, metodo o semantica
degli alias legacy; aggiungere smoke test per ogni gruppo.

## Piano consigliato

1. Correggere o rendere esplicita l'esposizione del listener motore.
2. Limitare la concorrenza indexer e aggiungere metriche.
3. Aggiungere il test di sincronizzazione API/documentazione.
4. Rafforzare il lifecycle di shutdown con test di concorrenza.
5. Progettare indice archivio incrementale e piano di compatibilità libtorrent.

Le prime tre attività sono circoscritte, osservabili e riducono rischio operativo
senza cambiare la logica di selezione delle release.
