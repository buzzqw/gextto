# Piano di miglioramento e correzione Gextto

## 1. Valutazione iniziale

Gextto è una buona base production per un’applicazione self-hosted utilizzata
nella propria LAN.

Verifiche eseguite:

- `go test ./...`: passato
- `go vet ./...`: passato
- `go test -race ./...`: passato
- `gofmt`: regolare
- coverage del package principale: circa 40%
- repository Git pulito

Le problematiche di sicurezza più severe sono relative a deployment esposti su
Internet. In una LAN fidata diventano principalmente problemi di affidabilità,
perdita o corruzione dati, consumo eccessivo di memoria, diagnosi difficile e
manutenibilità.

## 2. Priorità complessive

### P0 — Da correggere prima della distribuzione

Problemi con possibile perdita dati, comportamento errato o instabilità:

1. Errori SQLite ignorati durante le migrazioni.
2. Errori della blocklist ignorati.
3. Backup concorrenti non sicuri.
4. File temporanei di backup non sempre rimossi.
5. Risposte HTTP esterne senza limite.
6. Configurazioni JSON parziali che possono perdere i default.
7. Checkpoint e cleanup finali ignorati senza logging.

### P1 — Fortemente consigliata

1. Backup atomici.
2. Self-update con checksum obbligatorio.
3. Propagazione di `context.Context`.
4. Miglioramento delle interfacce tra componenti.
5. Aumento della coverage sui percorsi critici.
6. Rimozione dei warning di deprecazione libtorrent.

### P2 — Hardening opzionale per LAN

1. Permessi `0600` per database e backup.
2. Rifiuto del bind non-loopback senza API token.
3. Redazione dei segreti da `GET /api/config`.
4. Limitazione delle directory accessibili dalla UI.
5. Limiti sul numero di connessioni SSE.
6. Riduzione dell’ambiente ereditato dagli hook.

## 3. Piano dettagliato

## P0.1 — Gestione errori nelle migrazioni SQLite

### File coinvolti

- `database.go`
- `config.go`
- `sqlite.go`

### Problema

Sono presenti errori ignorati come:

```go
_ = ensureColumn(...)
_, _ = d.db.Exec(...)
```

e query che vengono semplicemente saltate se falliscono.

Un database danneggiato o un disco pieno può quindi produrre una configurazione
incompleta senza impedire l’avvio del daemon.

### Correzione

- Restituire gli errori di migrazione.
- Aggiungere il nome della tabella/colonna nel messaggio.
- Ignorare soltanto gli errori esplicitamente innocui, come “colonna già esistente”.
- Interrompere l’avvio se lo schema minimo non è disponibile.

### Test richiesti

- Errore `ALTER TABLE`.
- Database read-only.
- Database corrotto.
- Database con schema legacy incompleto.
- Migrazione già eseguita più volte.

### Criterio di accettazione

Una migrazione fallita deve essere registrata chiaramente, restituire un errore a
`OpenDatabase`/`LoadConfig` e impedire al daemon di continuare con dati parziali.

## P0.2 — Blocklist con comportamento fail-closed

### File

- `orchestrator.go:147`

### Problema

```go
blocklistedHashes, _ := db.BlocklistedHashes()
```

Se la lettura fallisce, la blocklist viene trattata come vuota.

### Correzione

```go
blocklistedHashes, err := db.BlocklistedHashes()
if err != nil {
    return nil, fmt.Errorf("load blocklist: %w", err)
}
```

### Test

- Database non leggibile.
- Query fallita.
- Verifica che il ciclo non avvii download se non può verificare la blocklist.

## P0.3 — Backup concorrenti

### File

- `backup.go`

### Problemi

1. Il nome del backup ha precisione al secondo.
2. Due backup simultanei possono usare lo stesso file.
3. Il file ZIP viene scritto direttamente nella destinazione finale.
4. La directory temporanea non viene sempre rimossa in caso di errore.

### Correzione proposta

- Nome con UUID o nanosecondi.
- Lock applicativo per impedire backup simultanei.
- File `.tmp` e successivo `os.Rename`.
- Cleanup con `defer`.
- Verifica finale dell’archivio ZIP.

### Test

- Due backup avviati contemporaneamente.
- Errore durante la copia di un database.
- Errore durante la creazione dello ZIP.
- Interruzione prima della rename.
- Verifica che non rimangano cartelle temporanee.

## P0.4 — Limiti sulle risposte HTTP esterne

### File

- `httpx.go`
- `rss.go`
- `web_handlers_g0.go`
- `web_handlers_g4.go`
- `web_handlers_g7.go`
- `update.go`

### Problema

Diverse chiamate usano `io.ReadAll(response.Body)` senza limite.

In una LAN il rischio principale non è necessariamente un attacco remoto, ma un
feed configurato male, un server locale che restituisce dati inattesi, un archivio
compresso enorme o memoria esaurita durante una ricerca o un aggiornamento.

### Correzione

Creare un helper centralizzato:

```go
func readLimitedBody(r io.Reader, limit int64) ([]byte, error)
```

Con limiti diversi per:

- JSON API: 8 MiB;
- RSS/HTML: 8–16 MiB;
- file torrent: 32–64 MiB;
- aggiornamenti: limite configurabile;
- contenuti decompressi: limite separato.

### Test

- Risposta esattamente al limite.
- Risposta oltre il limite.
- Risposta gzip oltre il limite dopo decompressione.
- Content-Length assente.
- Stream infinito.

## P0.5 — Default per configurazioni JSON parziali

### File

- `config.go:825-854`
- `config.go:2225-2249`

### Problema

Quando `gextto.json` esiste, il parsing parte da uno `Config` vuoto anziché da
`DefaultConfig()`.

Un file come:

```json
{}
```

può lasciare vuoti campi come `data_dir`, `listen`, `engine_listen`,
`refresh_secs` e directory libtorrent.

### Correzione

Inizializzare prima i default e poi sovrascrivere soltanto i valori presenti nel
JSON.

### Test

- Config vuota `{}`.
- Config con solo `data_dir`.
- Config con solo `listen`.
- Config con `libtorrent` parziale.
- Config con valori nel database e valori JSON sovrapposti.

Una configurazione JSON parziale deve comportarsi come una configurazione
completa con i default applicati.

## P0.6 — Errori di startup e shutdown

### File

- `cmd/gexttod/main.go:76`
- `cmd/gexttod/main.go:121-124`
- `cmd/gexttod/main.go:167-170`

### Problema

Vengono ignorati errori di cleanup iniziale, checkpoint SQLite e chiusura finale
dei database.

### Correzione

Distinguere tra:

- errori critici: restituire errore;
- errori non critici: registrare almeno a `WARN`;
- cleanup best effort: loggare senza bloccare lo shutdown.

Valutare inoltre la chiusura esplicita di database, archivio, comics e i18n.

### Test

- Checkpoint fallito.
- Database read-only.
- Chiusura con WAL attivo.
- Errore durante il cleanup iniziale.

## 4. Migliorie P1

### P1.1 — Backup atomici anche per copie esterne

### File

- `backup.go:222-237`

`copyFile` scrive direttamente sul file destinazione.

Scrivere invece in un file `.partial`, eseguire `fsync`, chiudere e rinominare
atomicamente nella destinazione finale.

### P1.2 — Self-update più rigoroso

### File

- `update.go:242-275`

Attualmente il checksum remoto mancante viene accettato.

Piano:

- rendere obbligatorio il checksum per release pubblicate;
- valutare firme digitali per release stabili;
- limitare la dimensione dell’archivio;
- scrivere il download direttamente su file temporaneo;
- verificare che l’archivio non contenga percorsi pericolosi.

### P1.3 — Propagazione dei context HTTP

### File

- `web_handlers_g3.go`
- `web_handlers_g4.go`
- `web_background.go`

Alcuni handler usano `context.Background()` invece di `r.Context()`.

Usare `r.Context()` per operazioni legate alla richiesta e `context.Background()`
soltanto per worker indipendenti.

### P1.4 — Interfacce per i servizi esterni

### File

- `appstate.go`
- `web.go`
- handler web
- `engine.go`
- `libtorrent.go`

Gli handler dipendono direttamente da implementazioni concrete.

Introdurre interfacce piccole e locali al consumer, ad esempio:

```go
type Searcher interface {
    SearchQuery(context.Context, *Config, string) []models.Release
}

type TorrentManager interface {
    List() []models.TorrentView
    Add(...)
    Remove(...)
}
```

Non creare un’unica grande interfaccia generale.

### P1.5 — Coverage sui percorsi critici

Coverage package principale: circa 40%; `cmd/gexttod`: 0%.

Test da aggiungere:

1. Startup completo con directory temporanea.
2. Configurazione JSON parziale.
3. Migrazioni fallite.
4. Backup simultanei.
5. Backup interrotto.
6. Risposte HTTP troppo grandi.
7. Errori blocklist.
8. Errori checkpoint.
9. Shutdown ordinato.
10. Self-update senza checksum.
11. Test dei path configurati.
12. Fuzz test per parser release, magnet, URL, nomi file e template di rinomina.

### P1.6 — Warning libtorrent

### File

- `libtorrent_bridge.cpp`

Sono presenti warning per API libtorrent deprecate, tra cui `half_open_limit`,
`cache_size`, `cache_expiry`, `torrent_status::error`, `set_priority` e
`file_priorities`.

Verificare la versione minima supportata, sostituire le API deprecate e rendere
i warning visibili in CI.

## 5. Hardening opzionale per LAN

### P2.1 — Permessi file

Anche in LAN, su un server multiutente è consigliabile:

- directory dati: `0700`;
- database: `0600`;
- backup: `0600`;
- log: `0600`;
- file contenenti token: `0600`.

Non è un blocco per una macchina personale con un solo utente fidato.

### P2.2 — API token

Comportamento consigliato:

- bind loopback senza token: consentito;
- bind LAN senza token: warning;
- bind Internet senza token: rifiutato oppure modalità esplicitamente forzata.

### P2.3 — Segreti nella configurazione

`GET /api/config` potrebbe restituire soltanto flag come:

```json
{
  "tmdb_configured": true,
  "telegram_configured": true,
  "email_password_configured": true
}
```

### P2.4 — Directory accessibili

Gli endpoint `browse_dir` e `mkdir` sono appropriati per un’app amministrativa
locale, ma si può aggiungere una modalità opzionale con radici consentite:

```text
allowed_roots:
  - /mnt/media
  - /srv/downloads
```

## 6. Migliorie di leggibilità

### P2.5 — Naming Go

Il codice contiene molti nomi portati dal progetto originale:

```go
config_path
last_cycle
torrent_events
fetch_with_flaresolverr
gh0_*
gh1_*
```

Per il codice nuovo sarebbe preferibile usare `configPath`, `lastCycle`,
`torrentEvents` e `fetchWithFlareSolverr`.

Per i nomi pubblici, preferire `TMDBClient` e `TVDBClient` a `TmdbClient` e
`TvdbClient`.

### P2.6 — Pulizia commenti

Da aggiornare:

- commenti con riferimenti al porting;
- commenti obsoleti;
- typo come `supimplements`;
- riferimenti a collisioni di nomi ormai risolte;
- commenti duplicati o poco naturali.

## 7. Sequenza consigliata di implementazione

### Fase 1 — Affidabilità

1. Error handling delle migrazioni.
2. Blocklist fail-closed.
3. Config JSON parziale.
4. Checkpoint e shutdown.
5. Test relativi.

### Fase 2 — Backup e aggiornamento

1. Lock dei backup.
2. Backup temporaneo e rename atomica.
3. Cleanup garantito.
4. Self-update con streaming e limite.
5. Checksum obbligatorio.
6. Test di interruzione e concorrenza.

### Fase 3 — Limiti risorse

1. Helper per body HTTP limitati.
2. Limiti decompressione.
3. Limiti archivio torrent/update.
4. Context legato alle richieste.
5. Test di memoria e timeout.

### Fase 4 — Qualità architetturale

1. Interfacce locali.
2. Riduzione dell’accoppiamento di `AppState`.
3. Test di startup e shutdown.
4. Fuzz test parser.
5. Pulizia naming e commenti.

### Fase 5 — Hardening LAN opzionale

1. Permessi `0600/0700`.
2. Protezione configurabile delle directory.
3. Redazione dei segreti nella Config API.
4. Fail-closed per bind non-loopback, se desiderato.

## 8. Criteri finali per la distribuzione

Il progetto può essere considerato pronto quando:

- tutti i test e i race test passano;
- una migrazione fallita interrompe chiaramente l’avvio;
- la blocklist non può essere bypassata per errore DB;
- i backup concorrenti producono sempre archivi validi;
- nessun file temporaneo rimane dopo un errore;
- le risposte esterne hanno limiti;
- una configurazione JSON parziale applica correttamente i default;
- lo shutdown segnala gli errori importanti;
- gli aggiornamenti remoti verificano l’integrità;
- CI copre almeno la piattaforma distribuita;
- i warning libtorrent sono stati rimossi o documentati.

Per lo scenario LAN, la priorità raccomandata è:

**affidabilità dati → backup → limiti risorse → configurazione → test → hardening di sicurezza**.
