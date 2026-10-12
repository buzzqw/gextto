# BitTorrent v2 (BEP 52) — implementation plan

> Working document to complete **BitTorrent v2** support in `gx-core`
> (`internal/gxcore`). Status in [`gx-torrent.md`](gx-torrent.md) and
> [`evoluzione.md`](evoluzione.md) §4.4; overall plan in
> [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md). Goal: enable everything
> **only** for `meta version == 2`, so v1 (used by Gextto) stays unchanged.

## 1. Current state (implemented)

- `internal/gxcore/internal/metainfo`: reads `meta version`, `file tree`,
  `piece layers`; computes the SHA-256 info-hash (`Info.V2Hash`) and the
  truncated one; `NewV2Info` accepts a v2-only info dict; `VerifyPieceLayers`
  validates the `piece layers` against the `pieces root`;
  `ErrV2Only`/`ErrUnknownMetaVersion`.
- `internal/gxcore/internal/magnet`: reads the `btmh` (`V2InfoHash`).
- `internal/gxcore/internal/merkle`: SHA-256 trees (16 KiB blocks, missing
  leaves = **zero hash**, `Layer`, `PieceLayer`, `Root`, `ZeroRoot`,
  `VerifyProof`).
- The engine is v1-only for non-v2 paths; **hybrids** are downloaded as v1.

**Done (2026-10-10):** **per-file piece** model (`NewPieces` v2 branch),
`Piece.VerifyV2` (Merkle node via `merkle`), SHA-256 verification in
`verifier`/`piecewriter`, truncated identity (`Info.Hash` = first 20 bytes of
`V2Hash`), enabling **v2 `.torrent`** (the daemon no longer rejects v2-only
from file), **persistence of `piece layers`** in the resume (v2 torrents
survive a restart and the **export/serve of the `.torrent` includes them**) and
**end-to-end tests** (multi-file seed/leech with `piece length` > 16 KiB).

**Done (2026-10-10) — I5:** **v2 magnets** (`urn:btmh:`) are supported. The
info dict arrives via BEP 9; the per-file `piece layers` are downloaded from
peers with the `hash request`/`hashes`/`hash reject` messages (BEP 52, ids
21/22/23, binary payload), verified against the `pieces root` (uncle hashes
included) and persisted in the resume. The **v2 reserved bit** (byte 7,
`0x10`) is announced in the handshake only for torrents with v2 identity. The
seed answers from its `piece layers`, without reading the data. End-to-end test
`TestV2MagnetTransfer` (seed and leech in-process).

**Done (2026-10-10) — piece layers in parallel and `base = 0`:**
- a file's `piece layer` is split into fixed chunks (≤512 hashes), each
  requested from a different peer; a failed, expired (30 s) or rejected chunk
  goes back into the queue for another peer. The `hash request`s start only
  towards peers that announce the v2 bit
  (`TestV2LayerChunksAreAssignedAndRetried`,
  `TestExpireV2HashRequestsReleasesStaleRequest`);
- the seed also answers requests for the **block layer** (`base = 0`), which a
  libtorrent leecher uses to verify blocks during the download: the tree is
  built from the verified pieces (`v2BlockTree`), **outside the torrent loop**
  (full read of the file: a `hash reject` means "not ready yet", the client
  retries), cached per-file with a **memory budget** and LRU eviction, only for
  a file entirely present and with the root **verified** against
  `pieces root`, otherwise `hash reject`
  (`TestAppendBlockLeavesMatchesContiguousHashes`,
  `TestV2BlockCacheEvictsLeastRecentlyUsed`; end-to-end interop with a raw TCP
  client in `cmd/gx-torrent/v2_blocklayer_test.go`).

## 2. BEP 52 in a nutshell (the rules that matter)

- **Info-hash**: SHA-256 of the info dict; for trackers/handshake the
  **20-byte truncated** one is used (wire identical to v1).
- **`pieces root`**: root of a branch-2 Merkle tree over the file's **16 KiB
  blocks**; the last block may be shorter (hash of the actual bytes); the
  leaves beyond the end of the file are **32-byte zero hashes**.
- **`piece layers`** (top-level, outside `info`): key = `pieces root`, value =
  hash of a layer chosen so that one hash covers `piece length` bytes; the
  hashes beyond the end of the file **are omitted**. The torrent is invalid if
  they do not match.
- **Alignment**: the files are mapped onto piece boundaries; **the last piece
  of a file may be shorter** than `piece length` (the "alignment gap" **does
  not** belong to any piece). The pieces **do not** span two files.
- **Peer extensions**: `hash request` (21), `hashes` (22), `hash reject` (23).
- **Hybrids**: an info dict with both fields; v1=SHA-1 and v2=SHA-256 over
  that complete dict.

## 3. Decision: per-file piece model (NOT concatenated)

**Attempt discarded and why.** A first attempt modelled v2 by reusing the v1
machinery with **synthetic padding files** (BEP 47) between the files, to align
the pieces. **It does not work**: in v2 a file's tail piece is *shorter* than
`piece length` and the missing part is a **zero hash**, not a block of zeros.
The concatenated model inserts a block of zeros **inside** the piece: the first
block of real data, then a block of zeros, whose hash is `SHA256(zeros)` and
not the expected zero hash. The verification never matches.

**Correct choice: per-file pieces.** For a v2 torrent the pieces are generated
**file by file**: file `f` occupies pieces `[k, k+ceil(len_f/pieceLength))`,
the tail piece has `Length = len_f mod pieceLength` (or `pieceLength`) and
**contains no padding**. After a file it restarts from a new piece index (the
address space is discontinuous). The current `Piece` model allows it
(`Piece.Length` can be `< pieceLength`); the **construction**
(`piece.NewPieces`) must be changed for the v2 case.

**Piece verification (v2).** The expected hash of a v2 piece **is not** a flat
hash of the piece: it is the **Merkle node** over the piece's 16 KiB blocks,
with the missing leaves (to reach `pieceLength/16KiB` leaves) equal to **zero
hash**. It must be compared with the hash from that file's `piece layers`.

Consequence: `piece.NewPieces` (or a v2 constructor) must receive the
**per-file** piece hashes (from the `piece layers`), and `Piece` must know that
it is v2 and how many leaves a piece covers (`pieceLength/16KiB`).

## 4. Increments (in dependency order, all done)

### I1 — Per-file piece model (L) — **done**
- `piece.NewPieces`: v2 branch that builds the pieces file by file (shorter
  tail, no padding), indexing them globally and taking each piece's hash from
  the file's layer.
- `Piece`: fields `V2 bool` and `V2Leaves int` (`pieceLength/16KiB`).
- Verification: new `Piece.VerifyV2(buf)` = Merkle node of the piece's blocks
  (missing leaves = zero hash) compared with `Piece.Hash`.
- **Tests**: full piece, shorter tail piece, partial last block; with a
  `pieceLength` > 16 KiB (to exercise the Merkle nodes, not just the leaves).

### I2 — v2 verification/verifier (M) — **done**
- `verifier`/`piecewriter`: if `Piece.V2`, use `VerifyV2`; otherwise
  `VerifyHash(buf, sha1.New())`. For v1 nothing changes.
- **Tests**: valid/corrupt v2 piece (Merkle node) + v1 regression.

### I3 — Identity and session (M) — **done**
- v2-only: identity = first 20 bytes of `V2Hash`; handshake/DHT/tracker
  unchanged.
- BEP 9 metadata with truncated SHA-256 for v2; `piece layers` persistence in
  the resumer.
- **Tests**: single-port routing with the truncated hash; reload from resume.

### I4 — Wiring up the piece layers (S/M) — **done**
- `metainfo.New`: for `meta version == 2`, build the v2 info (parse file tree),
  validate the `piece layers` (`VerifyPieceLayers`) and provide the per-file
  hashes to `NewPieces`.
- **Tests**: inconsistent layer → torrent rejected; valid → verification ok.

### I5 — `hash request`/`hashes` extensions (L) — **done**
- **Binary** codec (not bencode): messages 21/22/23, header
  `pieces root | base | index | length | proof layers` (32+4×4 bytes);
  `hashes` = header + requested hashes + uncle. Dispatch in `peerreader`,
  `WriteTo` serialisation, fuzzing on the header.
- Client side: `v2LayerFile`, requests in chunks of ≤512 hashes, verification
  of each response with `merkle.VerifyHashes` (rebuilding the root with the
  uncles), assembly of the layer and `AttachV2Pieces`, persistence in
  `piece layers`.
- Server side: `LayerTree` built from the hashes of the pieces already owned;
  `base = 0` (block layer) served from the verified pieces for a complete file;
  `hash_reject` when the layer cannot be served.
- **Tests**: codec round-trip + fuzzing; `TestLayerTreeRoundTrip` and
  `TestV2MagnetTransfer` (downloads the layers from a seed, validates them,
  verifies the pieces).

### I6 — Enabling and UX (S/M) — **done**
- Daemon: accept v2-only (`.torrent` with layers), model-based detection,
  `torrent_version` in the UI; remove the v2 blocklist/removal in Gextto.
- **Tests**: `add` v2 with a local seed/leech; update `TestV2OnlyDetection`.

## 5. End-to-end tests (key)

Build a deterministic v2-only torrent in the test: data, leaves
(`SHA256` of the 16 KiB blocks), `pieces root`, `piece layers`, info dict +
`.torrent`. Seed + leech in-process (as in `TestSharedPortTransfer`) and
comparison of the bytes. **Use `piece length` > 16 KiB** in at least one case
to exercise the Merkle nodes. The first attempt with `piece length = 2 blocks`
and synthetic padding failed right here (progress 0: wrong piece hash).

## 6. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| v1/Gextto regression | everything behind `meta version == 2`; `make test` (real v1 transfers) + race at every step |
| Wrong piece model | explicit per-file pieces; tests with a shorter tail and a partial block |
| Wrong piece hash | Merkle node with zero-hash padding, not a flat hash; `merkle` as reference |
| Truncated identity | 20-byte handshake/DHT/tracker; routing tests |
| Missing `piece layers` | downloaded from peers with `hash request` (I5); a silent peer expires after 30 s |
| Licences | algorithms rewritten (no anacrolix MPL); BEP 52 is public domain |

## 7. Out of scope (for now)

- WebTorrent/WebRTC; alternative storage; `share mode`.
