# gx-torrent — Ottimizzazioni prestazionali, versione 2 (piano di lavoro)

Sostituisce e riordina `docs/gx-torrent-ottimizzazioni-prestazionali.md`. Quella
versione conteneva diagnosi in gran parte corrette come lettura del codice, ma
con tre difetti: non distingueva il codice upstream rain v2.4.2 da quello del
fork, presentava benchmark non riproducibili (nessuna funzione `Benchmark*`
esiste nel repo) e ordinava per priorità interventi irrilevanti o rischiosi.

Questa versione contiene **solo ciò che vale la pena fare**, in ordine di
priorità, con il *come* concreto, i rischi e la verifica. Ogni voce è pensata
per essere processata come singolo task/commit.

---

## 0. Criteri di priorità e regole di ingaggio

Ordine per **valore atteso = (impatto × certezza) / (sforzo × rischio)**.
Le voci 1–2 sono vittorie immediate a rischio nullo; la 3 è il progetto ad alto
impatto; 4–5 riducono allocazioni e memoria; 6–7 sono contorno; 8 è opzionale.

Regole valide per ogni voce (da `AGENTS.md`):

- Ogni modifica a `third_party/rain` è marcata nel codice con `// gextto fork` e
  registrata nell'inventario `third_party/rain/GEXTTO.md` (nuova riga/tabella).
- Un commit per voce, messaggi `tipo(area): …`, autore `buzzqw <azanzani@gmail.com>`.
  Mai `Claude`/`Anthropic` in messaggi, trailer o PR.
- Test mirati: `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`,
  `make test-rain`, `make test`.
- Invarianti: `scripts/check-ui-settings-index.sh`, `scripts/installer-selftest.sh`.
- Non committare `build_number`, `gx-torrent.build_number`, `bin/`, `data/`.
- Se cambia un comportamento visibile: aggiornare `README.md`/`README.it.md`,
  `docs/MANUAL.*` e `docs/gx-torrent.md`.

### Tabella riassuntiva

| # | Intervento | Impatto | Sforzo | Rischio | Codice |
|---|---|---|---|---|---|
| 1 | LSD senza `d.mu` e senza `t.Stats()` | Alto (latenza demone) | S | S | fork (cmd) |
| 2 | Streaming: check pezzi mirato `PiecesDone` | Medio (GC/coupling) | S | S | fork (cmd+rain) |
| 3 | Rimozione `O_SYNC` + invariante di durabilità | **Molto alto** (disco) | M/L | M | fork (rain) |
| 4 | MSE `StreamWriter` in-place | Medio (upload) | S | S | fork (rain) |
| 5 | `servedRequests` a finestra limitata | Medio (memoria) | S/M | M | fork (rain) |
| 6 | Piece cache: TTL lazy (+ sharding condizionale) | Medio (seed) | M | M | fork (rain) |
| 7 | `Bitfield.Count` con `math/bits` | Basso | S | S | fork (rain) |
| 8 | Opzionali: mappa in `findLocked`, ETag/304 | Basso | S | S | fork (cmd) |

`S` ≈ mezza giornata, `M` ≈ 1–2 giorni, `L` ≈ oltre.

---

## 1. LSD: non prendere `d.mu`, non interrogare i run loop

**Perché.** `dueHashes` (`cmd/gx-torrent/lsd.go:106-119`) acquisisce `d.mu` e,
tenendolo, chiama `t.Stats()` su **ogni** torrent. `Stats()` passa per `query`
(`third_party/rain/torrent/torrent_commands.go`) ed è una **query bloccante
senza timeout** al run loop del torrent. Se un run loop è fermo su I/O di
storage (mount di rete lento), il mutex globale resta bloccato e congela tutte
le API REST. Il demone ha già uno snapshot lock-free
(`snapshotViews`, `daemon.go:1056-1062`) creato proprio per evitare questo, e
`torrentInfo` espone già `Private` e `State`.

**Come.** Riscrivere `dueHashes` leggendo dallo snapshot. Gli stati
"annunciabili" sono quelli di un torrent in esecuzione; `stateFor`
(`daemon.go:872-897`) produce `downloading`, `downloading_metadata`, `seeding`,
`checking_files`, `moving` per torrent attivi, e `paused`/`stalled`/`error` per
quelli fermi.

```go
// dueHashes lists the running public torrents to announce now.
func (s *lsdService) dueHashes(now time.Time) []string {
	// Snapshot lock-free (gextto fork): niente d.mu e niente t.Stats() sul run
	// loop, che potrebbe essere bloccato su storage lento.
	var running []string
	for _, v := range s.d.snapshotViews() {
		if v.Private {
			continue
		}
		switch v.State {
		case "downloading", "downloading_metadata", "seeding", "checking_files", "moving":
			running = append(running, v.Hash)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	live := map[string]bool{}
	var due []string
	for _, hash := range running {
		live[hash] = true
		if now.Sub(s.announced[hash]) >= lsdInterval {
			due = append(due, hash)
			s.announced[hash] = now
		}
	}
	for hash := range s.announced {
		if !live[hash] {
			delete(s.announced, hash)
		}
	}
	return due
}
```

**Opzionale nella stessa voce.** Anche `receiveLoop` (`lsd.go:199-205`) prende
`d.mu` per `findLocked` e poi chiama `t.Stats().Private` (fuori dal lock): si
può passare a uno snapshot, ma è a bassa frequenza e non è bloccante sotto
lock — valutare solo se si tocca comunque il file.

**File.** `cmd/gx-torrent/lsd.go`.

**Rischio.** Basso. Unica differenza di comportamento: un torrent in `error`
non viene più annunciato (prima poteva esserlo). È il comportamento corretto e
desiderabile.

**Verifica.** Test in `cmd/gx-torrent` che (a) un torrent `paused`/`stalled`
non compaia in `dueHashes`, (b) un torrent `downloading` pubblico sì, (c) un
torrent privato mai. In più un test che dimostri che `dueHashes` non prende
`d.mu` (es. chiamarlo con `d.mu` già detenuto da un'altra goroutine senza
deadlock, o asserendo il ritorno con `session == nil`).

---

## 2. Streaming: check pezzi mirato invece di `PieceStates()`

**Perché.** `piecesPresent` (`cmd/gx-torrent/stream.go:139-152`) chiama
`t.PieceStates()`, che alloca `make([]string, len(pieces))` e **scansiona tutti
i pezzi sul run loop** (`session_torrent.go:277-299`), ogni 200 ms finché la
finestra non è pronta. Per controllare 1–2 pezzi si alloca e si visita una
struttura lunga quanto il torrent, e si tiene occupato il run loop per tutta la
scansione. `PieceStates` (gextto fork) resta necessario per la mappa pezzi di
API/UI, ma non è adatto al polling.

**Come.** Aggiungere un metodo mirato al torrent, sempre in `session_torrent.go`:

```go
// PiecesDone reports whether every piece in [begin,end) is done or skipped
// (gextto fork, for the streaming wait loop). Runs on the torrent run loop and
// scans only the requested range.
func (t *Torrent) PiecesDone(begin, end uint32) bool {
	return query(t.torrent, func() bool {
		pieces := t.torrent.pieces
		if len(pieces) == 0 {
			return false
		}
		for i := begin; i < end && i < uint32(len(pieces)); i++ {
			if !pieces[i].Done && !pieces[i].Skip {
				return false
			}
		}
		return true
	})
}
```

e in `stream.go`:

```go
func piecesPresent(t *torrent.Torrent, begin, end uint32) bool {
	return t.PiecesDone(begin, end)
}
```

Nota: `waitForPieces` ritorna `true` prima di arrivare qui quando
`FilePieceRange` non è mappabile (nessun metadata), quindi il `len(pieces)==0`
sopra preserva esattamente il comportamento precedente (`PieceStates` dava
`ok=false` → `false`).

**File.** `third_party/rain/torrent/session_torrent.go` (+ riga in
`GEXTTO.md`, tabella *Diagnostica pezzi*), `cmd/gx-torrent/stream.go`.

**Rischio.** Molto basso. `PieceStates` non viene toccato.

**Verifica.** Test del fork in `third_party/rain/torrent` se praticabile, o
test end-to-end dello streaming in `cmd/gx-torrent` (finestra pronta/di
bordo). `make test-rain`, `make test`.

**Follow-up (non in questa voce).** L'attesa resta un polling a 200 ms. La
sostituzione con una notifica a evento (`NotifyPieceComplete`) è più invasiva e
non necessaria finché il polling non alloca.

---

## 3. Rimozione `O_SYNC` + invariante di durabilità  ← il progetto

**Perché.** `filestorage.go:60` apre ogni file con `os.O_RDWR | os.O_SYNC`
(upstream rain). `O_SYNC` obbliga ogni `write(2)` a completarsi su storage
stabile: su HDD/network storage il throughput di download è limitato dalle IOPS
fisiche, e la goroutine di scrittura resta in D-state. È, con ogni probabilità,
il vincolo prestazionale maggiore. **Ma non è un quick win**: rain affida a
`O_SYNC` l'intera durabilità dei dati di pezzo, e l'alternativa va progettata.

**Contesto verificato.**
- `storage.File` è `io.ReaderAt/WriterAt/Closer` (`internal/storage/storage.go`):
  **non ha `Sync()`**. Il "`f.Sync()` mirato" richiede un'estensione.
- Il bitfield è persistito **solo** a torrent completato
  (`torrent_write.go:88`), allo stop (`torrent_stop.go:61`), dopo la verifica
  (`torrent_verification.go:40`) e dal loop statistiche ogni
  `ResumeWriteInterval` (`session_stats.go:148-175`); in gx-torrent è 2 minuti
  (`daemon.go:307`). **Non** è salvato "a ogni completamento di pezzo" come
  scriveva la v1.
- `session_stats.updateStats` scrive il bitfield dentro una transazione Bolt.

**Invariante da mantenere (la parte delicata).** *Un bit persistito nel bitfield
deve riferirsi a dati già fsyncati.* Se si fissa questo, su crash/power loss si
perdono al massimo gli ultimi `ResumeWriteInterval` di pezzi (che vengono
riscaricati), ma **non si corrompe**: il bitfield persistito non è mai "avanti"
rispetto ai dati su disco.

**Come.**

1. `third_party/rain/internal/storage/storage.go` — estendere l'interfaccia:
   ```go
   type File interface {
       io.ReaderAt
       io.WriterAt
       io.Closer
       // Sync flushes data and metadata to stable storage (gextto fork:
       // sostituisce l'O_SYNC per-scrittura).
       Sync() error
   }
   ```
   `*os.File` la soddisfa già. `storage.PaddingFile` (`internal/storage/padding.go`)
   va aggiornato con `func (f PaddingFile) Sync() error { return nil }`.

2. `third_party/rain/internal/storage/filestorage/filestorage.go`:
   ```go
   openFlags := os.O_RDWR            // gextto fork: niente O_SYNC, si sincronizza prima di persistere il bitfield
   openFlags = applyNoAtimeFlag(openFlags)
   ```

3. Nuovo helper di pacchetto (estraibile e testabile) in
   `torrent/torrent_pieces.go`, che sincronizza i file dei pezzi:
   ```go
   // syncPieces flushes every non-padding file referenced by pieces
   // (gextto fork). Deduplica per file.
   func syncPieces(pieces []piece.Piece) error {
       seen := make(map[filesection.ReadWriterAt]struct{})
       for i := range pieces {
           for _, sec := range pieces[i].Data {
               if sec.Padding {
                   continue
               }
               if _, ok := seen[sec.File]; ok {
                   continue
               }
               seen[sec.File] = struct{}{}
               if s, ok := sec.File.(interface{ Sync() error }); ok {
                   if err := s.Sync(); err != nil {
                       return err
                   }
               }
           }
       }
       return nil
   }
   ```
   (`FileSection.File` è tipizzato `filesection.ReadWriterAt`; il type-assert
   copre `*os.File` e ignora `PaddingFile`.)

4. `torrent.writeBitfield` — **prima lo snapshot del bitfield, poi il sync, poi
   la scrittura** (ordine critico: se si sincronizzasse prima, un pezzo marcato
   dopo il sync verrebbe persistito senza dati durevoli):
   ```go
   func (t *torrent) writeBitfield() error {
       buf := append([]byte(nil), t.bitfield.Bytes()...) // snapshot
       if err := syncPieces(t.pieces); err != nil {      // poi flush
           t.log.Errorf("cannot sync piece files: %s", err)
           return err
       }
       err := t.session.resumer.WriteBitfield(t.id, buf) // infine persisti
       if err != nil {
           t.log.Errorf("cannot write bitfield to resume db: %s", err)
       }
       return err
   }
   ```

5. `session_stats.updateStats` — ristrutturare in due fasi, per non tenere la
   transazione Bolt aperta durante gli fsync e per rispettare l'invariante:
   - **fase 1**: per ogni torrent, sotto `mBitfield.RLock()` copiare i byte del
     bitfield, poi `syncPieces(t.torrent.pieces)` (fuori dal lock);
   - **fase 2**: aprire la transazione e scrivere i bitfield copiati + i
     contatori.

**File.** `internal/storage/storage.go`, `internal/storage/padding.go`,
`internal/storage/filestorage/filestorage.go`, `torrent/torrent_pieces.go`,
`torrent/session_stats.go`, `GEXTTO.md`, `docs/gx-torrent.md`.

**Rischio/effetti collaterali.** Su crash o power loss si riscaricano fino a
2 minuti di pezzi (accettabile e da documentare). Aumenta la dirty page cache
del kernel (monitorare su RAM ridotta). Va verificato che nessun altro percorso
si affidi all'immediatezza di `O_SYNC`. Se si vuole ridurre la finestra, si può
abbassare `ResumeWriteInterval` (costo: più transazioni Bolt).

**Verifica.**
- Unit test di `syncPieces` con un `filesection.ReadWriterAt` finto che
  registra le chiamate `Sync` (e verifica la deduplica e l'esclusione dei
  padding).
- Test che `writeBitfield` chiami il sync prima della scrittura sul resumer
  (es. storage finto che registra l'ordine; il resumer è concreto, quindi
  verificare il sync e l'assenza di panic, più ispezione dell'ordine nel codice).
- Test manuale: avviare un download, `kill -9` del demone, riavviarlo e
  verificare che non compaia corruzione e che rain riscarichi gli ultimi pezzi.
- `make test-rain` (include `internal/storage/filestorage` e `torrent`),
  `make test`.

---

## 4. MSE: `StreamWriter` in-place (zero allocazioni sul wire)

**Perché.** Verificato il percorso: `peerwriter.messageWriter`
(`peerwriter.go:246`) scrive su `p.conn`, che è un `*countingConn` sopra
`*mse.Conn`: quando la crittografia è attiva (default `-encryption=1`), si
arriva a `cipher.StreamWriter.Write`, che fa `make([]byte, len(src))` **per ogni
blocco** (la stdlib non può cifrare in place). In upload sono decine di MiB/s di
spazzatura heap.

**Come.** In `third_party/rain/internal/mse/mse.go`, sostituire `s.w` con un
writer in-place (gextto fork). I chiamanti non riusano il buffer dopo la
scrittura: `peerwriter` scrive da un array locale riutilizzato ma ricostruito da
zero a ogni messaggio (`buf := bytes.NewBuffer(b)` con `b := a[:0]`), e gli
usi nell'handshake (`writeBuf`) sono locali e scartati.

```go
// inPlaceStreamWriter cifra nello stesso buffer del chiamante (gextto fork):
// il chiamante non deve riusare src dopo Write. Elimina l'allocazione per
// blocco di cipher.StreamWriter.
type inPlaceStreamWriter struct {
	s cipher.Stream
	w io.Writer
}

func (w *inPlaceStreamWriter) Write(p []byte) (int, error) {
	w.s.XORKeyStream(p, p)
	return w.w.Write(p)
}
```

In `initRC4`: `s.w = &inPlaceStreamWriter{s: cipherEnc, w: s.raw}` (al posto di
`&cipher.StreamWriter{...}`). In `updateCipher`, usare `&inPlaceStreamWriter{s:
plainTextCipher{}, w: s.raw}` anche per `PlainText` (lì `XORKeyStream` è un
`copy`, quindi in place è un no-op).

**File.** `third_party/rain/internal/mse/mse.go`, `GEXTTO.md`.

**Rischio.** Basso, ma è un cambio di semantica di un wrapper generico
(`Stream.Write` consuma l'input). Da verificare che in `internal/mse` nessun
altro punto si aspetti l'immutabilità del buffer passato a `s.w.Write`. Gli usi
attuali sono solo `peerwriter` e l'handshake.

**Verifica.** Nuovo `third_party/rain/internal/mse/mse_test.go` con
`testing.AllocsPerRun` che asserisce **0 allocazioni** per la scrittura di un
blocco (dopo l'handshake) e correttezza del round-trip encrypt/decrypt.
Aggiungere `github.com/cenkalti/rain/v2/internal/mse` alla lista pacchetti di
`make test-rain` (Makefile).

---

## 5. `peerwriter.servedRequests`: finestra limitata

**Perché.** `servedRequests` (`peerwriter.go:30,48,200-205`) è una mappa che
cresce **indefinitamente** con i blocchi serviti: su seed longevi la memoria
cresce proporzionalmente ai GB caricati. Inoltre rifiuta per sempre il
re-request dello stesso blocco, anche legittimo (ritrasmissione dopo blocco
corrotto in transito). Entrambe le cose derivano dal fatto che non c'è mai
eviction.

**Come.** Sostituire la mappa con una struttura a finestra (mappa + FIFO), così
la dedup copre gli ultimi N richieste e la memoria è limitata. N ≈ 1024 tiene
il comportamento anti-abuso immediato senza crescere.

```go
// gextto fork: finestra limitata invece di una mappa illimitata.
type servedWindow struct {
	m map[peerprotocol.RequestMessage]struct{}
	q []peerprotocol.RequestMessage
}

func newServedWindow(n int) *servedWindow {
	return &servedWindow{m: make(map[peerprotocol.RequestMessage]struct{}), q: make([]peerprotocol.RequestMessage, 0, n)}
}

func (w *servedWindow) seen(r peerprotocol.RequestMessage) bool {
	if _, ok := w.m[r]; ok {
		return true
	}
	w.m[r] = struct{}{}
	w.q = append(w.q, r)
	if len(w.q) > cap(w.q) {
		delete(w.m, w.q[0])
		w.q = w.q[1:]
	}
	return false
}
```

Nel loop: `if p.served.seen(pi.RequestMessage) { msg = peerprotocol.RejectMessage{...} }`.

**File.** `third_party/rain/internal/peerconn/peerwriter/peerwriter.go`,
`GEXTTO.md`.

**Rischio.** Medio-basso: con una finestra troppo piccola si servono di nuovo
blocchi vecchi (banda sprecata) invece di rifiutarli. Con N=1024 il caso non si
presenta in pratica; il valore può diventare una costante.

**Verifica.** Unit test in
`third_party/rain/internal/peerconn/peerwriter/peerwriter_test.go`: dedup entro
la finestra, eviction oltre, nessuna crescita illimitata. Aggiungere il
pacchetto a `make test-rain`. Il contatore `currentQueuedRequests` e il resto
del loop restano invariati.

---

## 6. Piece cache: TTL lazy (e sharding solo se serve)

**Perché.** La piece cache è usata **solo** per servire blocchi ai peer in
upload (`cachedpiece` ← `torrent_messagehandler.go`, `RequestMessage`). Non è
sul download né sullo streaming locale di `stream.go` (che usa
`os.Open`/`ReadAt`). Quindi conta solo per il seeding. Due costi reali
(upstream):
- un `time.Timer` per pezzo in cache, con `Reset` a ogni accesso
  (`cache.go:166-173,184`): migliaia di timer riarmati;
- `getItem` prende il lock esclusivo anche su cache hit, e `updateAccessTime`
  lo riprende (`cache.go:107,177`).

**Come.**
1. **TTL lazy** (parte consigliata): sostituire `i.timer` con
   `i.expireAt time.Time`; su hit aggiornare `expireAt`; aggiungere alla Cache
   un ticker di sweep (es. 30 s) che, sotto lock, elimina gli item scaduti.
   `Close()` ferma il ticker. Questo toglie tutti i timer per-item.
2. **Sharding** (solo se il profiling mostra contesa sotto forte seeding):
   partizionare in 16 shard con mutex/LRU propri. È un refactor più ampio di un
   file upstream; non farlo "a naso".

**File.** `third_party/rain/internal/piececache/cache.go`, `GEXTTO.md`.

**Rischio.** Medio: la logica TTL/LRU va mantenuta coerente (eviction su sweep
e su `makeRoom`), e lo sharding cambia `Len`/`Size`/metriche.

**Verifica.** Test esistenti del pacchetto + nuovi: item scaduto rimosso dallo
sweep, hit che rinnova la scadenza, eviction per dimensione invariata. Aggiungere
`internal/piececache` a `make test-rain`. Prima/dopo con
`docs/gx-torrent-misure-seeding.md` per lo sharding.

---

## 7. `Bitfield.Count` con `math/bits`

**Perché.** `Count()` (`bitfield.go:108-114`, upstream) somma byte per byte su
una tabella da 256 byte. È un micro-ottimizzo pulito ma **non** su un percorso
critico per-pick: `Count()` è usato in `torrent_stats.go:177,219,285` e in
`All()` (`torrent_peer.go:190`). Impatto basso, ma il cambio è banale e sicuro.

**Come.** (gextto fork) sostituire l'implementazione e rimuovere `countCache`:

```go
func (b *Bitfield) Count() uint32 {
	var total int
	bytes := b.bytes
	for len(bytes) >= 8 {
		total += bits.OnesCount64(binary.LittleEndian.Uint64(bytes))
		bytes = bytes[8:]
	}
	for _, v := range bytes {
		total += bits.OnesCount8(v)
	}
	return uint32(total)
}
```

**File.** `third_party/rain/internal/bitfield/bitfield.go`, `GEXTTO.md`.

**Rischio.** Nullo (l'ordine dei bit non influenza il conteggio).

**Verifica.** Property test contro l'implementazione a tabella su bitfield
casuali, lunghezze multiple di 8 e non. Aggiungere
`github.com/cenkalti/rain/v2/internal/bitfield` a `make test-rain`.

---

## 8. Opzionali (solo se avanza tempo)

1. **`findLocked` O(1)** (`daemon.go:441-452`): oggi `ListTorrents()` alloca e
   scansiona, calcolando `InfoHash().String()` per elemento, a ogni chiamata
   API per hash. Con molte decine di torrent è comunque trascurabile. Se fatto:
   mantenere `byID map[string]string` (id→hash) e risolvere prima per ID
   (`Session.GetTorrent`, O(1)) e poi per hash; ricostruire la mappa in
   `openSessionLocked`/`reconcileLocked` e sugli add/remove. **Non** è una
   priorità.
2. **ETag/304 su `/api/v1/torrents`**: `handleList` (`api.go:159-164`) serve già
   lo snapshot precompilato (`snapshotViews`), quindi il costo è solo il
   `json.Marshal` per richiesta. Un ETag basato su revisione aiuta solo quando
   il contenuto è stabile (seed inattivi): durante attività i tassi cambiano a
   ogni tick e il 304 non scatta. Guadagno marginale; fattibile tenendo in
   `publishViewsLocked` una revisione e i byte serializzati, e gestendo
   `If-None-Match` lato `gxtorrent_engine.do`. Da fare solo con misura che lo
   giustifichi.

---

## 9. Esclusi, con motivazione

- **Rarity buckets / refactor del picker**: tecnica corretta (libtorrent,
  anacrolix), ma il costo reale del pick è ignoto — `slices.SortFunc` è pdqsort
  adattivo e l'array è quasi ordinato tra due pick; servirebbe un profilo con
  pprof su uno sciame reale prima di un refactor pesante (mantenere i bucket in
  `HandleHave`/`Cancel`/`removeHaving` e preservare il rilevamento endgame).
  Da rivisitare **solo con dati**.
- **Cursore watermark in `pickSequential`**: incompleto da solo; in modalità
  sequenziale/preferenza-bordi il costo O(N) dominante è `pickFileEdge`
  (`piecepicker.go:478-486`), aggiunto dal fork. Se si tocca, va sistemato
  anche quello; priorità bassa.
- **Bitmask in `PieceDownloader`**: le mappe sono allocate solo per i download
  *concorrenti* (≈ n. peer), non per tutti i pezzi; il guadagno è trascurabile,
  e la bitmask a 64 bit non copre pezzi da 2–4 MiB. Non conviene.
- **`clear(b.Data)` in `bufferpool`**: il pool è condiviso
  (`peerreader`, `urldownloader`, `piecewriter`, `piecePool`); "ridondante al
  100%" non è dimostrato — alcuni percorsi potrebbero leggere meno di
  `len(Data)`. Micro, da fare solo con audit di tutti i consumatori.
- **`FADV_RANDOM`**: la giustificazione principale della v1 (penalizza lo
  streaming HTTP) **non vale in gx-torrent**: `stream.go` apre il file con
  `os.Open` e legge direttamente, senza passare dallo storage di rain. Restano
  verifica e miss di cache in upload, impatto modesto. Al più, `FADV_SEQUENTIAL`
  mirato durante la verifica.
- **`saveLocked` fuori da `d.mu`**: non è sul percorso di polling (solo
  mutazioni e tick-quando-dirty, ~1/min/torrent). Il codice proposto dalla v1
  azzera `d.dirty` **prima** della scrittura e non lo ripristina sull'errore →
  perdita silenziosa di stato. Se si facesse, va corretto. Basso impatto.
- **`/proc/meminfo` sotto `d.mu`** (`stats()`, `daemon.go:1707`): decine di µs,
  irrilevante. Al più spostarlo fuori dal lock, ma non è una priorità.

---

## 10. Correzioni sostanziali alla v1 (per riferimento)

- La maggior parte delle voci (10 su ~16) è **codice upstream rain v2.4.2
  invariato**, non lavoro di gextto: `O_SYNC`, `FADV_RANDOM`, sort del picker,
  mappe di `PieceDownloader`, `servedRequests`, timer della cache, `mse`,
  `bufferpool`, `bitfield` (confermato via sha256 vs upstream).
- `O_SYNC` **non è un quick win**: tocca la durabilità; `storage.File` non ha
  `Sync()`.
- "Il bitfield è salvato a ogni pezzo" è **falso** (solo a completamento, stop,
  verifica e ogni `ResumeWriteInterval`).
- I moltiplicatori "15.500x / 65.000x / 600.000x" derivano da microbenchmark
  eterogenei, non da guadagni end-to-end; nel repo **non esiste** alcuna
  funzione `Benchmark*`. Se servono numeri, vanno rigenerati come benchmark
  versionati.
- La piece cache è sul percorso di **upload**, non di download/streaming
  locale.
- `handleList` serve già uno snapshot: non "clona i torrent" né interroga i run
  loop per la lista.
