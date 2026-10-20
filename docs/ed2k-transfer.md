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
block/part tree (`AICHRootReader`, `AICHRootFile`, `AICHTree`): the download
session asks for `OP_AICHFILEHASHREQ`, requests `OP_AICHREQUEST` recovery data
per part, verifies every 180 KiB block before writing it, retries rejected
blocks and checks the master hash again at completion. The upload server
answers `OP_AICHFILEHASHREQ`/`OP_AICHREQUEST` from a lazily built tree.
`internal/ed2k/transfer/peer` adds
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
tests serves, queues and corrupts parts. The wire format was checked against a
real aMule peer (including a full 400 KB download whose MD4 matched): the
client header is protocol/length/opcode (length includes
the opcode), OP_HELLO has a leading hash-size byte and a six-byte server
IP/port suffix, the file-name length is 16-bit, and an extended file request
carries the part-status and complete-source words. Files above 4 GiB use the
I64 request/part opcodes. LowID Kad sources (type 3/5/6) are kept even without
an inbound TCP port: when our own port is open (HighID), the manager sends
OP_DIRECTCALLBACKREQ over UDP, waits for the source to connect back and runs a
server-side hello plus the normal download over that connection. If the source
advertises a buddy (Kad TAG_BUDDYHASH/SERVERIP/SERVERPORT), the manager also
sends KADEMLIA_CALLBACK_REQ to that buddy, which makes the source connect to us
on TCP. For a server LowID source (a `OP_FOUNDSOURCES` entry below 0x01000000)
the manager logs in to that source's server and sends `OP_CALLBACKREQUEST` with
the source client ID; the server asks the source to connect to us, and a
wildcard pending slot accepts the incoming connection. This is the
server-callback path. A full LowID↔LowID relay through a third client is still
out of scope. A queued transfer keeps retrying. Do not send OP_SETREQFILEID before
OP_STARTUPLOADREQ: real eMule peers close the socket after accepting the slot
when that packet arrives, while aMule tolerates it. With the packet removed, a
live download of the 4.2 MB eMule 0.70b installer completed, passed the final
MD4 and appeared in Gextto as a finished ed2k row. For multi-part files the
client reads OP_FILESTATUS and only asks for parts the peer actually has; a
partial source no longer causes requests for missing parts. eD2k 0xC5 extension packets
(EMULEINFO, queue ranking, compressed parts) use a separate header byte from
the 0xE3 standard packets. None of it is imported by
`cmd/gx-ed2k` yet.

The goal is a pure-Go eMule-compatible client inside `gx-ed2k`, so Gextto can
turn an eD2k result into a real download without aMule, `amulecmd` or the EC
protocol. aMule/eMule may appear only as interoperability peers in a lab.

## 1. Scope

In scope:

- eD2k server login and source discovery (the login/search part already exists);
- client-to-client protocol: hello, file request, hashset, part requests,
  compressed parts, queue, source exchange;
- `.part` and `.part.met` storage, block MD4, AICH master/recovery, resume and verification;
- a download manager (sources, retries, timeouts, bans, per-file queue);
- minimal upload and credits so peers do not penalise or ban us;
- LowID callback and the buddy path needed to download behind NAT;
- integration with Gextto: `download: true`, add from a search result, DB rows,
  progress/events, archive on completion.

Out of scope for the first usable version:

- secure identification with eD2k servers (server credits); the
  client-to-client RSA handshake is implemented;
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
  fetched as a master hash and per-part recovery data, verified block by block
  before a part is written, and checked again at completion; a rejected block
  is re-requested and a part that still fails is not trusted.
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
  behind NAT. Server LowID sources are handled with `OP_CALLBACKREQUEST` to
  their server. The existing reachability sensor already tells the UI whether
  this path is needed.
- **Obfuscation**: the client advertises crypt-layer support and implements the
  eMule RC4 handshake for outgoing connections when the source tag says the
  peer supports it, plus the receiving side for incoming callbacks/uploads.
  The keys follow aMule's magic bytes 34/203 and discard the first 1024 RC4
  bytes; the clear 0xE3/0xC5 header remains a valid fallback. The obfuscated
  outgoing path was verified against aMule 2.3.3 with the 400,000-byte lab
  file (hello, upload slot, AICH and final MD4 all succeeded).
- **Secure identification**: the client keeps a 384-bit RSA identity in
  `cryptkey.pem` (the size eMule and aMule use), advertises version 1 in the
  hello options and answers `OP_SECIDENTSTATE` with `OP_PUBLICKEY` and
  `OP_SIGNATURE`. The signature covers the peer key and the challenge the peer
  sent, so it cannot be replayed to another client. Downloads and uploads are
  fully symmetric. Identification with eD2k servers (`0x97`) is still missing,
  so server-side credits are not earned.
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
