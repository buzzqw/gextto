# eD2k/Kad transfer plane — design (gated)

**Status: archived after the gate, with isolated T0/T1 spikes.** The Phase 0
gate came back at **7–13%** real-fallback coverage in two independent samples
(4/30 and 2/30 with ≥3 sources), so the transfer engine is **not started**. The
design stays here for a future decision; the part-file and packet codecs remain
isolated from the daemon. The decision criteria are at the end.

Spike (19 October 2026): `internal/ed2k/transfer/partfile` implements the
9,728,000-byte part geometry, streaming MD4 whole-file/per-part hashing and the
non-splitted `.part.met` reader/writer (0xE0/0xE2, tags via the shared codec),
plus a `PartFile` that creates the sparse `.part`, verifies each part before
writing it, rebuilds the bitmap on reopen and verifies the whole file before
completion. It is not imported by `cmd/gx-ed2k`; the remaining peer protocol is still to
do. The splitted 0xE1 metadata variant with the zero marker is read and written
(the older non-zero marker is rejected). AICH is implemented as the eMule SHA-1
block/part tree (`AICHRootReader`, `AICHRootFile`) with small-size tree tests. `internal/ed2k/transfer/peer` adds
the 0xC5 TCP framing, the hello exchange and the file/hashset/part packet
codecs (round-trip and malformed-packet tests), plus a framed `Conn` with a
bounded payload (16 MiB, one compressed part) and a deadline-aware hello
handshake that rejects self-answers and zero identities. `internal/ed2k/transfer/source`
adds the per-file source list with deduplication, failure backoff and expiry.
`internal/ed2k` now also has the source-discovery codecs: `OP_GETSOURCES` /
`OP_FOUNDSOURCES` for servers and `SEARCH_SOURCE` requests/results for Kad,
with the two IPv4 wire orders normalised in one place. `internal/ed2k/transfer/download`
is the first end-to-end transfer path: hello, file name, hashset (file hash +
part hashes, as eMule writes it), queue rank, upload acceptance, three-block
part requests, compressed parts, per-part MD4 verification, resume from
`.part.met` and a final whole-file MD4 gate. A `Manager` drives source
discovery and the per-peer retry/queue state; a deterministic fake peer in the
tests serves, queues and corrupts parts. None of it is imported by
`cmd/gx-ed2k` yet.

The goal is a pure-Go eMule-compatible client inside `gx-ed2k`, so Gextto can
turn an eD2k result into a real download without aMule, `amulecmd` or the EC
protocol. aMule/eMule may appear only as interoperability peers in a lab.

## 1. Scope

In scope:

- eD2k server login and source discovery (the login/search part already exists);
- client-to-client protocol: hello, file request, hashset, part requests,
  compressed parts, queue, source exchange;
- `.part` and `.part.met` storage, block MD4, AICH, resume and verification;
- a download manager (sources, retries, timeouts, bans, per-file queue);
- minimal upload and credits so peers do not penalise or ban us;
- LowID callback and the buddy path needed to download behind NAT;
- integration with Gextto: `download: true`, add from a search result, DB rows,
  progress/events, archive on completion.

Out of scope for the first usable version:

- full obfuscation/RC4 and secure identification;
- preview/streaming, shared-file publishing beyond what upload requires;
- a second UI: the transfer state appears in Gextto's existing download list.

## 2. Why now (and why not yet)

The search plane can find files but cannot move a byte. The Phase 0 sample says
how often Kad has the right episode with enough sources: if the useful share is
too low, the 6.5–10 month transfer engine is not justified. The design is kept
ready so the decision is about data, not about missing groundwork.

## 3. Architecture

```
cmd/gx-ed2k
  ├─ search plane (today)          internal/ed2k
  └─ transfer plane (this design)  internal/ed2k/transfer
        ├─ partfile   .part / .part.met, MD4 blocks, AICH
        ├─ peer       eMule client protocol over TCP
        ├─ source     server + Kad + source exchange
        ├─ download   per-file state machine, scheduler
        └─ upload     queue, credits, sharing
```

`internal/ed2k/transfer` must not import Gextto. The daemon exposes a
`Transfer` interface and a `/api/v1/transfers` surface; Gextto talks to it over
HTTP like it talks to `gx-torrent`. The existing `multiEngine` router already
labels rows by network and routes by hash, so Gextto needs no second download
manager.

### Daemon surface

- `POST /api/v1/transfers` — add an `ed2k://` link (name, size, hash) and a
  destination; returns the transfer id.
- `GET /api/v1/transfers` — rows: id, name, size, done, rate, sources,
  state (`queued`, `downloading`, `verifying`, `completed`, `error`).
- `GET /api/v1/transfers/{id}` — detail: parts, sources, errors, AICH state.
- `POST /api/v1/transfers/{id}/pause|resume|remove`.
- `GET /api/v1/transfers/events` — long-poll/SSE for progress, same shape as
  `gx-torrent` events.
- `health.download` becomes `true` only when the transfer plane is enabled and
  the storage is writable.

## 4. Part file and metadata

- **Part size**: eMule `PARTSIZE` = 9,728,000 bytes (190 KiB × 51.2? verify in
  eMule `Defines.h`; do not guess in code).
- **`.part`**: the raw file, preallocated or sparse; a bitmap of completed parts
  lives in `.part.met`, not in the data.
- **`.part.met`**: versioned header (`0xE0`/`0xE1` family), file hash, name,
  size, part hashes (MD4 per part), AICH hashset, source list, tags. The parser
  and writer are the first code to write: they are deterministic, testable and
  independent of the network.
- **Hashing**: MD4 over the whole file and over each 9,728,000-byte part; the
  ed2k link hash must match the finished file exactly. AICH (SHA-1 tree) is
  verified per part when available; a part that fails is re-downloaded, not
  trusted.
- **Resume**: on start, verify the `.part` against the part hashes; a corrupt
  part is marked missing. Never rename/import a file before the full hash
  matches.
- **`known.met` / shared files**: only the minimum needed for credits/upload;
  the search daemon does not index third-party files.

## 5. Client protocol

The wire codecs go in `internal/ed2k/transfer` with table-driven tests and
fuzzers. Exact opcode numbers and edge cases are checked against the eMule
source (the project's reference, not a dependency), as required by the analysis
note.

- **Hello**: exchange client hash, TCP port, tags; reject incompatible
  versions and ourselves.
- **File request**: ask for a file by MD4; answer with name, size, hashset
  availability and queue state. A peer without the file is remembered and not
  asked again for that hash.
- **Hashset**: download the part hashes before requesting parts; verify the
  root hash against the file hash.
- **Parts**: request ranges of blocks; accept compressed parts when offered;
  enforce a per-peer request window and timeouts; never write outside the
  requested range.
- **Queue**: respect `QUEUEFULL`/queue position; back off; do not hammer.
- **Sources**: server source query, Kad source search (`SEARCH_SOURCE_REQ`) and
  source exchange; merge by IP/port, cap per file, expire stale sources.
- **LowID/callback**: if the server assigns a LowID, ask a HighID peer to
  callback; the buddy path is required for a useful download success rate
  behind NAT. The existing reachability sensor already tells the UI whether
  this path is needed.
- **Credits**: implement the eMule credit identity and upload accounting at
  least well enough not to be treated as a leech; the exact formula is in the
  eMule source.

## 6. Download state machine

```
queued → connecting → requesting sources → downloading → verifying → completed
             ↘ error (retry with backoff)
```

- One scheduler owns the global connection/bandwidth budget; each file has a
  source set and a part bitmap.
- Priorities and limits reuse Gextto's existing settings where possible
  (max active downloads, speed limits, free-space floor, temp dir).
- Events: source count, rate, ETA, part progress, hash failures, queue
  position, final verification. Gextto consumes them like torrent events.
- Completion: full MD4 verification, then the existing post-process/rename/
  hardlink pipeline; an eD2k row is marked `network: "ed2k"` and routed by MD4.

## 7. Upload and sharing

Without upload peers penalise the client (credits, queue priority) and some
refuse to serve. The first version needs a minimal upload: serve completed
parts of files we are downloading plus a configurable shared folder, with a
queue and a global upload cap. Full community features (friends, comments,
ratings) stay out.

## 8. Security and correctness

- **Names are claims; the MD4 hash is identity.** Never trust the name, size or
  tags until the part/file hash matches.
- **Fake/poisoned sources**: per-source failure counters, bans, a source floor
  and the existing IP filter; AICH catches corrupted parts early.
- **Path safety**: no `..`, absolute paths, reserved names or symlink escapes
  when creating `.part`/final files.
- **Resource caps**: max peers, per-peer buffers, part request size, disk
  preallocation, free-space floor.
- **Legal**: eD2k has no editorial filter; the provider stays opt-in and the
  automatic fallback stays off unless the operator enables it explicitly.

## 9. Testing

1. **Codecs**: table-driven + fuzz tests for `.part.met`, hello, file request,
   hashset, parts, compressed parts, tags; malformed input must return errors,
   never panic.
2. **Fake peer**: a deterministic TCP peer that serves one file, drops parts,
   sends corrupt data, queues, and exercises LowID callback.
3. **Storage**: create a `.part`, kill/restart, resume, corrupt a part, verify
   recovery, check the final MD4.
4. **Interoperability lab**: aMule/eMule as peers on a closed test network
   (never a runtime dependency): download a known file in both directions,
   compare hashes, test queue/credits.
5. **End-to-end**: Gextto search → add → daemon transfer → completion →
   post-process, with a fake engine first and a real lab peer second.

## 10. Phases and effort

| Phase | Deliverable | Estimate |
|---|---|---|
| T0 | `.part`/`.part.met`, MD4/AICH, storage tests | 3–4 weeks |
| T1 | Hello, file request, hashset, parts, fake peer | 4–6 weeks |
| T2 | Source manager, scheduler, resume, UI/API rows | 4–6 weeks |
| T3 | Upload/credits, LowID callback, interop lab | 6–10 weeks |
| T4 | Hardening, fuzzing, rate limits, packaging | 3–4 weeks |

Total for a usable MVP: **6.5–10 months** for one developer who knows the
codebase, matching the analysis. The work is only started if the gate says the
coverage justifies it.

## 11. Decision criteria

Start T0 only when all of these hold:

1. the stratified Phase 0 sample (with the broad retry and aliases) shows a
   useful share of gaps with an exact episode, parsable quality, plausible size
   and at least the configured source floor;
2. the manual search flow has been used on real gaps and the results are not
   dominated by fake/renamed files;
3. the operator explicitly wants the transfer plane and accepts the legal and
   maintenance cost.

If the sample stays low, Kad remains a manual search provider and this document
stays a design, not a commitment.
