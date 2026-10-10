# BitTorrent v2 (BEP 52) — progetto di implementazione

> Documento operativo per completare il supporto **BitTorrent v2** in `gx-core`
> (`internal/gxcore`). Lo stato corrente è in [`gx-torrent.md`](gx-torrent.md) e
> [`evoluzione.md`](evoluzione.md) (§4.4); il piano generale in
> [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md). Le fondamenta sono già in
> `main` (vedi *Stato*), qui c'è **cosa manca e come farlo**, con l'obiettivo di
> non toccare il comportamento v1 (che Gextto usa) se non dietro
> `meta version == 2`.

## 1. Stato attuale (fondamenta fatte)

- `internal/gxcore/internal/metainfo`: legge `meta version`, `file tree`,
  `piece layers`; calcola l'info-hash **SHA-256** (`Info.V2Hash`) e il troncato;
  `NewV2Info` accetta un info dict v2-only; `VerifyPieceLayers` valida i
  `piece layers` contro i `pieces root`; `ErrV2Only`/`ErrUnknownMetaVersion`.
- `internal/gxcore/internal/magnet`: legge la `btmh` (`V2InfoHash`).
- `internal/gxcore/internal/merkle`: alberi SHA-256 (blocchi 16 KiB, foglie
  mancanti = hash zero, `Layer`, `PieceLayer`, `Root`, `ZeroRoot`, `VerifyProof`).
- Il resto del motore è **v1-only**: i v2-only sono rifiutati; gli ibridi sono
  scaricati come v1.

## 2. BEP 52 in sintesi (le regole che contano)

- **Info-hash**: SHA-256 del *bencoded info dict*; per tracker e handshake si usa
  il **troncato a 20 byte** (il wire resta identico a v1).
- **`meta version: 2`** e **`file tree`**: albero di dizionari; una foglia ha la
  chiave vuota `""` con `length` e (per i file non vuoti) `pieces root`.
- **`pieces root`**: radice di un Merkle a branch 2 costruito sui **blocchi da
  16 KiB** del file; l'ultimo blocco può essere più corto; le foglie oltre la
  fine del file sono **hash di 32 byte a zero**.
- **`piece layers`** (dizionario **top-level**, fuori da `info`): chiave =
  `pieces root`, valore = hash concatenati di **un layer** scelto in modo che un
  hash copra `piece length` byte; gli hash che coprono solo dati oltre la fine
  del file **si omettono**. Il torrent è invalido se mancano o non combaciano.
- **Allineamento**: i file sono mappati nello spazio dei pezzi **allineati ai
  confini di pezzo**; l'ultimo pezzo di un file può essere più corto
  (gap di allineamento). I pezzi **non** attraversano due file.
- **Estensioni peer**: `hash request` (21), `hashes` (22), `hash reject` (23),
  per scaricare i `piece layers` quando non sono nel `.torrent` (magnet v2).
- **Ibridi**: un solo info dict con i campi v1 **e** v2; entrambi gli hash si
  calcolano su quel dict completo (v1 = SHA-1, v2 = SHA-256).

## 3. Decisione di progetto: modello file/piece v2

Il nodo del problema è che `piece.NewPieces` concatena i file e taglia in pezzi
(i pezzi possono attraversare i file). In v2 ogni pezzo appartiene a un solo
file e i file sono allineati a `piece length`, con padding implicito a zero.

**Scelta consigliata: padding sintetico (riuso della macchina v1).** Per un info
v2, `NewV2Info` costruisce `Info.Files` inserendo, dopo ogni file, un **file di
padding** (stile BEP 47) fino al confine di pezzo successivo. I padding non
esistono su disco: la loro area è a zero, che è esattamente il padding v2. Così
`NewPieces`, `allocator`, `filestorage`, selezione file, statistiche e streaming
continuano a funzionare, e i pezzi restano confinati a un file.

**Alternativa scartata**: riscrivere `piece.NewPieces`/`Piece.Data` per essere
consapevoli del "pezzo di un solo file": più pulita in teoria, ma tocca
allocazione, storage e picker in profondità, con rischio molto maggiore per v1.

Vincolo: **tutta** questa logica è attivata solo quando `meta version == 2`;
per v1 i percorsi restano identici (stesse funzioni, stesse foglie).

## 4. Incrementi (in ordine di dipendenza)

### I1 — Modello v2 in `metainfo` (M)
- `NewV2Info`: costruire `Info.Files` (file + padding sintetico), `PieceLength`,
  `NumPieces`, e `pieces` come concatenazione degli hash v2 (32 byte).
- Aggiungere a `Info` la lunghezza dell'hash (`PieceHashLen`, 20 v1 / 32 v2) e
  far usare a `PieceHash` quella lunghezza.
- **Test**: file non multiplo della piece (coda paddata), file esatto, albero
  annidato; conteggio pezzi e mappa pezzo→file; round-trip con `verify` su file
  esistenti.
- **Rischio**: basso (funzione nuova; `NewInfo` v1 invariata).

### I2 — Verifica SHA-256 (M)
- `verifier` e `piecewriter`: ricevere una `func() hash.Hash` (default
  `sha1.New`); il torrent la sceglie da `meta version` (`sha256.New` per v2).
- **Test**: unit (selezione dell'hash, verifica pezzo valido/corrotto su un pezzo
  costruito con hash SHA-256) + regressione v1 (i trasferimenti reali esistenti).
- **Rischio**: medio (percorso condiviso), mitigato dal default sha1 e dai test.

### I3 — Identità e sessione (M)
- Per un v2-only: `infoHash` = primi 20 byte di `Info.V2Hash`; il digest pieno
  resta a parte. Handshake/MSE/DHT/tracker continuano a usare i 20 byte.
- Metadata BEP 9: verificare l'info dict con **SHA-256 troncato** per v2.
- Persistenza nel resumer: salvare i `piece layers` (chiave nuova).
- **Test**: routing a porta unica con hash troncato; ricarica da resume.
- **Rischio**: medio (identità diffusa `[20]byte`).

### I4 — `piece layers` → verifica pezzi (S/M)
- Su add, validare i `piece layers` con `VerifyPieceLayers` (già fatto); usare la
  concatenazione dei layer come `pieces` v2 (fatto in I1).
- **Test**: layer incoerente → torrent rifiutato; layer valido → verifica ok.

### I5 — Estensioni `hash request`/`hashes` (L)
- Necessarie solo per magnet v2 (o `.torrent` senza `piece layers`). Codec
  bencode dei tre messaggi (id 21/22/23), dispatch, integrazione con `merkle` e
  persistenza dei layer.
- **Test**: round-trip + fuzz dei messaggi; scenario "scarica i layer da un seed,
  validali, verifica i pezzi".
- **Rischio**: alto (protocollo nuovo).

### I6 — Abilitazione e UX (S/M)
- Demone: **non** rifiutare più i v2-only con `errV2Only`; rilevamento basato sul
  modello (`NewV2Info`) invece dell'euristica sui byte; `torrent_version` in UI.
- Gextto: togliere la blocklist/rimozione dei v2 (e `v2_replacement.go`).
- **Test**: `add` di un magnet v2 e di un `.torrent` v2 con un seed/leech locale;
  aggiornare i test esistenti (`TestV2OnlyDetection`), non cancellarli.

## 5. Test end-to-end (chiave)

Costruire **in test** un torrent v2-only deterministico: generare i dati, le
foglie con `merkle.LeafHashes`, il `pieces root` con `merkle.Root`, i
`piece layers` con `merkle.PieceLayer`, e comporre l'info dict + `.torrent`.
Poi seed + leech **in-process** (come `TestSharedPortTransfer`) e verificare che
il contenuto scaricato coincida. Questo prova l'intero percorso v2 (parsing,
modello, verifica SHA-256, handshake troncato, storage) senza dipendere da
torrent reali.

## 6. Rischi e mitigazioni

| Rischio | Mitigazione |
| --- | --- |
| Regressione v1/Gextto | tutto dietro `meta version == 2`; `make test` (trasferimenti reali v1) a ogni passo; race |
| Modello storage sbagliato | padding sintetico riusa la macchina v1 già testata; test pezzo→file mirati |
| Hash sbagliato (padding/livelli) | vettori costruiti con `merkle` e calcolo indipendente; `VerifyPieceLayers` |
| Identità troncata | handshake/DHT/tracker restano a 20 byte; test di routing |
| `piece layers` assenti | per ora rifiutare (richiedono I5); messaggio esplicito |
| Licenze | algoritmi riscritti (niente codice MPL di anacrolix); BEP 52 è pubblico dominio |

## 7. Fuori scope (per ora)

- WebTorrent/WebRTC; storage alternativi.
- `share mode`; altri algoritmi di choking.
- v2 su magnet **senza** `piece layers` prima di I5.
