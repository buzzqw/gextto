# Open items

The maintained list of work that is not done. One item per line: what to do and what
unblocks it. When an item lands it leaves this file; when a new one is accepted it is
added here. There is no history and no design essay: the state of the product is in
[`gx-torrent.md`](gx-torrent.md), [`gx-core.md`](gx-core.md) and the manuals, the numbers
in [`measurements.md`](measurements.md).

**Rule.** No speculative work: an item starts only when its gate is met. An item with no
gate is ready to pick up. Every idea proposed by the operator or by the assistant in a
working session is written here in the same cycle: under the matching area once accepted,
under [Proposals](#proposals) while it is still a decision. A rejected idea is not
recorded: this file holds only work that will be done. An idea must not stay only in the
chat. Do not add a diary: a sentence that would still be true after the item lands does
not belong here.

## Engine (gx-core, gx-torrent)

Nothing open: the engine items are proposals below.

## Gextto, acquisition and UI

- **Release parsing CPU** — the quality/language scans, the subtitle rewrite and
  the common episode shapes are in place; the x/season-word/in-progress matchers
  and the absolute-episode parser still use regexes, under 10 % of a cycle. Gate: a
  profile where the regexp share climbs back above 20 % of a cycle; then replace the
  next pattern and re-measure on the same corpus.
- **Cycle allocation churn** — the film match is fixed; what is left of a cycle's
  allocations is the indexer and feed replies (`encoding/xml` tokenizer, HTML parser,
  response bodies, about half) and the release score (10 %). Gate: a reply parser that
  reads the items without copying every token, measured on the same corpus.
- **Functions too long to review** — the search scheduler (`cycleWorker`) and the rest of
  the candidate pass (`candidatePass.try`, still about 300 lines around the closures that
  resolve the link and check an in-progress pack): split them by phase, as the
  completion, the library import, the rename pass, the configuration load, the event
  worker and the decision tail of the candidate pass were. Gate: for the scheduler, an
  injectable clock and sleep first; for the closures, a test of a `.torrent` link that
  fails and falls back to a mirror, with `cycle_candidate_behaviour_test.go` green.

## Measurements

- **The documents after a round** — the round of 2026-11-07 is in
  `docs/measurements.md`, `README.md`, `README.it.md`, `docs/README.md` and the
  benchmark rule in `AGENTS.md`; every future round replaces those rows and the
  tables, and nothing else. Gate: a round finished.

Designs rejected on a measurement are not tracked here: they stay in
[`measurements.md`](measurements.md#closed-designs), with the number that closed each one.
Do not reopen one without new data.

## Proposals

Ideas proposed but not decided yet. An accepted proposal moves to the matching area with
its gate; a rejected one is removed from this file.

- **Autospeed, a quieter signal** — probe the first router past the home line (TTL-limited
  ICMP echoes on unprivileged ping sockets, the first public hop) instead of a TCP connect
  to public resolvers, and use the shared one-way delay of busy uTP peers as a free second
  signal; learn the idle baseline at half rate until the lowest round trip stops falling;
  remember the learned cap per network (gateway hardware address). Gate: a bloated-line
  test (network namespaces, a token-bucket uplink with half a second of buffer) measuring
  ping and rate with autospeed off, as today, and with the change.

- **A client-wide cap on unverified data** — anacrolix/torrent holds at most
  `MaxUnverifiedBytes` (64 MiB, `config.go`) of received bytes that are not yet hashed and
  written; gx-core bounds a piece and a torrent, not the client. That is why the library's own
  memory stays at 11 MiB at full speed ([measurements](measurements.md)). Gate: a run with
  several torrents downloading at once where the hashing backlog is the peak.

- **IP address reported to trackers** — send an operator-chosen address in the announce
  (HTTP `ip=` and the UDP equivalent). Gate: a NAT or VPN setup where the tracker records
  the wrong address.
- **Incomplete file extension** — append `.!qB` while a file is incomplete and rename it
  at completion, with resume, recheck, move and streaming kept consistent. Gate: a
  workflow that reads the folder during a download.
- **Bundle more languages** — the pack loader is in place (`<data>/lang/*.json`, English
  fallback, partial packs allowed); the six compiled catalogs are still the only complete
  ones. Gate: a translator for each language.
- **Piece buffers in segments** — a piece now takes its buffer at its first block, but the
  piece being filled and the ones waiting to be written still hold the whole piece: taking
  256 KiB segments as blocks land would hold only the bytes received. It touches the hash
  (v1 and v2), the write, web seeds and the local block merge. Gate: a measurement where
  those pieces dominate the memory, with the full-speed download still at 290 MB/s.
