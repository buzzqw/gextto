# BitTorrent v2 (BEP 52) — progetto di implementazione

> Documento operativo per completare il supporto **BitTorrent v2** in `gx-core`
> (`internal/gxcore`). Stato in [`gx-torrent.md`](gx-torrent.md) e
> [`evoluzione.md`](evoluzione.md) §4.4; piano generale in
> [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md). Obiettivo: attivare tutto
> **solo** per `meta version == 2`, così v1 (usato da Gextto) resta invariato.

## 1. Stato attuale (fondamenta fatte)

- `internal/gxcore/internal/metainfo`: legge `meta version`, `file tree`,
  `piece layers`; calcola l'info-hash SHA-256 (`Info.V2Hash`) e il troncato;
  `NewV2Info` accetta un info dict v2-only; `VerifyPieceLayers` valida i
  `piece layers` contro i `pieces root`; `ErrV2Only`/`ErrUnknownMetaVersion`.
- `internal/gxcore/internal/magnet`: legge la `btmh` (`V2InfoHash`).
- `internal/gxcore/internal/merkle`: alberi SHA-256 (blocchi 16 KiB, foglie
  mancanti = **hash zero**, `Layer`, `PieceLayer`, `Root`, `ZeroRoot`,
  `VerifyProof`).
- Il motore è v1-only per i percorsi non v2; gli **ibridi** si scaricano come v1.

**Realizzato (2026-10-10):** modello a **pezzi per-file** (`NewPieces` ramo v2),
`Piece.VerifyV2` (nodo Merkle via `merkle`), verifica SHA-256 in
`verifier`/`piecewriter`, identità troncata (`Info.Hash` = primi 20 byte di
`V2Hash`), abilitazione dei **`.torrent` v2** (il demone non rifiuta più i v2-only
da file), **persistenza dei `piece layers`** nel resume (i torrent v2
sopravvivono al riavvio e l'**export/serve del `.torrent` li include**) e **test
end-to-end** (seed/leech multi-file con `piece length` > 16 KiB).

**Realizzato (2026-10-10) — I5:** i **magnet v2** (`urn:btmh:`) sono supportati.
L'info dict arriva via BEP 9; i `piece layers` per-file si scaricano dai peer con
i messaggi `hash request`/`hashes`/`hash reject` (BEP 52, id 21/22/23, payload
binario), si verificano contro i `pieces root` (uncle hash inclusi) e si
persistono nel resume. Il **bit riservato v2** (byte 7, `0x10`) è annunciato
nell'handshake solo per i torrent con identità v2. Il seed risponde dalle sue
`piece layers`, senza leggere i dati; le richieste a livello blocco (`base = 0`)
sono rifiutate con `hash reject`. Test end-to-end `TestV2MagnetTransfer` (seed e
leech in-process).

## 2. BEP 52 in sintesi (regole che contano)

- **Info-hash**: SHA-256 dell'info dict; per tracker/handshake si usa il
  **troncato a 20 byte** (wire identico a v1).
- **`pieces root`**: radice di un Merkle a branch 2 sui **blocchi da 16 KiB** del
  file; l'ultimo blocco può essere più corto (hash dei byte effettivi); le foglie
  oltre la fine del file sono **hash di 32 byte a zero**.
- **`piece layers`** (top-level, fuori da `info`): chiave = `pieces root`, valore
  = hash di un layer scelto perché un hash copra `piece length` byte; gli hash
  oltre la fine del file **si omettono**. Il torrent è invalido se non combaciano.
- **Allineamento**: i file sono mappati sui confini di pezzo; **l'ultimo pezzo di
  un file può essere più corto** di `piece length` (lo "alignment gap" **non**
  appartiene ad alcun pezzo). I pezzi **non** attraversano due file.
- **Estensioni peer**: `hash request` (21), `hashes` (22), `hash reject` (23).
- **Ibridi**: un info dict con entrambi i campi; v1=SHA-1 e v2=SHA-256 su quel
  dict completo.

## 3. Decisione: modello pezzi per-file (NON concatenato)

**Tentativo scartato e perché.** Un primo tentativo ha modellato il v2
riusando la macchina v1 con **file di padding sintetico** (BEP 47) tra i file,
per allineare i pezzi. **Non funziona**: in v2 il pezzo di coda di un file è
*più corto* di `piece length` e la parte mancante è un **hash zero**, non un
blocco di zeri. Il modello concatenato inserisce un blocco di zeri **dentro** il
pezzo: il primo blocco di dati reali, poi un blocco di zeri, il cui hash è
`SHA256(zeri)` e non l'hash zero atteso. La verifica non combacia mai.

**Scelta corretta: pezzi per-file.** Per un torrent v2 i pezzi sono generati
**file per file**: il file `f` occupa pezzi `[k, k+ceil(len_f/pieceLength))`,
il pezzo di coda ha `Length = len_f mod pieceLength` (o `pieceLength`) e **non
contiene padding**. Dopo un file si riparte da un nuovo indice di pezzo (lo
spazio degli indirizzi è discontinuo). Il modello `Piece` attuale lo consente
(`Piece.Length` può essere `< pieceLength`); va cambiata la **costruzione**
(`piece.NewPieces`) per il caso v2.

**Verifica del pezzo (v2).** L'hash atteso di un pezzo v2 **non** è un hash
piatto del pezzo: è il **nodo Merkle** sui blocchi da 16 KiB del pezzo, con le
foglie mancanti (per arrivare a `pieceLength/16KiB` foglie) pari a **hash zero**.
Va confrontato con l'hash del `piece layers` di quel file.

Conseguenza: `piece.NewPieces` (o un costruttore v2) deve ricevere gli hash dei
pezzi **per file** (dal `piece layers`), e `Piece` deve sapere che è v2 e quante
foglie copre un pezzo (`pieceLength/16KiB`).

## 4. Incrementi (in ordine di dipendenza)

### I1 — Modello pezzi per-file (L)
- `piece.NewPieces`: ramo v2 che costruisce i pezzi file per file (coda più
  corta, nessun padding), indicizzandoli globalmente e prendendo l'hash di ogni
  pezzo dal layer del file.
- `Piece`: campi `V2 bool` e `V2Leaves int` (`pieceLength/16KiB`).
- Verifica: nuova `Piece.VerifyV2(buf)` = nodo Merkle dei blocchi del pezzo
  (foglie mancanti = hash zero) confrontato con `Piece.Hash`.
- **Test**: pezzo pieno, pezzo di coda più corto, ultimo blocco parziale; con un
  `pieceLength` > 16 KiB (per esercitare i nodi Merkle, non solo le foglie).

### I2 — Verifica/verificatore v2 (M)
- `verifier`/`piecewriter`: se `Piece.V2`, usare `VerifyV2`; altrimenti
  `VerifyHash(buf, sha1.New())`. Per v1 nulla cambia.
- **Test**: pezzo v2 valido/corrotto (nodo Merkle) + regressione v1.

### I3 — Identità e sessione (M)
- v2-only: identità = primi 20 byte di `V2Hash`; handshake/DHT/tracker invariati.
- Metadata BEP 9 con SHA-256 troncato per v2; persistenza `piece layers` nel
  resumer.
- **Test**: routing a porta unica col troncato; ricarica da resume.

### I4 — Collegare i piece layers (S/M)
- `metainfo.New`: per `meta version == 2`, costruire l'info v2 (parse file tree),
  validare i `piece layers` (`VerifyPieceLayers`) e fornire gli hash per-file a
  `NewPieces`.
- **Test**: layer incoerente → torrent rifiutato; valido → verifica ok.

### I5 — Estensioni `hash request`/`hashes` (L) — **fatto**
- Codec **binario** (non bencode): messaggi 21/22/23, header
  `pieces root | base | index | length | proof layers` (32+4×4 byte);
  `hashes` = header + hash richiesti + uncle. Dispatch in `peerreader`,
  serializzazione `WriteTo`, fuzz sull'header.
- Lato client: `v2LayerFile`, richieste a blocchi di ≤512 hash, verifica di ogni
  risposta con `merkle.VerifyHashes` (ricostruzione della radice con gli uncle),
  assemblaggio del layer e `AttachV2Pieces`, persistenza in `piece layers`.
- Lato server: `LayerTree` costruito dagli hash dei pezzi già posseduti;
  `hash_reject` quando il layer non è servibile (`base < piece layer`).
- **Test**: round-trip codec + fuzz; `TestLayerTreeRoundTrip` e
  `TestV2MagnetTransfer` (scarica i layer da un seed, li valida, verifica i pezzi).

### I6 — Abilitazione e UX (S/M)
- Demone: accettare i v2-only (`.torrent` con layer), rilevamento basato sul
  modello, `torrent_version` in UI; togliere la blocklist/rimozione v2 in Gextto.
- **Test**: `add` v2 con seed/leech locale; aggiornare `TestV2OnlyDetection`.

## 5. Test end-to-end (chiave)

Costruire in test un torrent v2-only deterministico: dati, foglie
(`SHA256` dei blocchi 16 KiB), `pieces root`, `piece layers`, info dict +
`.torrent`. Seed + leech in-process (come `TestSharedPortTransfer`) e confronto
dei byte. **Usare `piece length` > 16 KiB** in almeno un caso per esercitare i
nodi Merkle. Il primo tentativo con `piece length = 2 blocchi` e padding
sintetico ha fallito proprio qui (progress 0: hash del pezzo sbagliato).

## 6. Rischi e mitigazioni

| Rischio | Mitigazione |
| --- | --- |
| Regressione v1/Gextto | tutto dietro `meta version == 2`; `make test` (trasferimenti reali v1) + race a ogni passo |
| Modello pezzi sbagliato | pezzi per-file espliciti; test con coda più corta e blocco parziale |
| Hash del pezzo sbagliato | nodo Merkle con padding a hash zero, non hash piatto; `merkle` come riferimento |
| Identità troncata | handshake/DHT/tracker a 20 byte; test di routing |
| `piece layers` assenti | rifiuto esplicito finché non c'è I5 |
| Licenze | algoritmi riscritti (niente MPL di anacrolix); BEP 52 è pubblico dominio |

## 7. Fuori scope (per ora)

- WebTorrent/WebRTC; storage alternativi; `share mode`.
- Magnet v2 **senza** `piece layers` prima di I5.
