# gx-ed2k — the eD2k/Kad search daemon

`gx-ed2k` is Gextto's native eD2k/Kad client. Like `gx-torrent`, it is our own
Go code, it can run managed by Gextto or standalone, and it has its own page and
`/api/v1`. Unlike `gx-torrent`, it is a **search client**: it finds files and
reports their identity, it does not move their bytes.

The design decision is intentional. The eMule client-to-client transfer
protocol (hello, file request, hashset, parts, compression, obfuscation, secure
identification, upload queue, credits, AICH) is a project on its own. Shipping a
half transfer engine would be worse than not shipping one: a result that cannot
be completed must stay a result, not become a stalled download. `/api/v1/health`
therefore answers `"download": false`, and Gextto keeps the "add" action
disabled for eD2k-only releases.

## What is implemented

- **eD2k links**: `ed2k://|file|name|size|md4|/` parse/format, MD4 validation,
  server links and server lists.
- **Kad (Kademlia2)**:
  - 128-bit IDs and the XOR metric, MD4 keyword hashes;
  - eMule wire layout: little-endian 32-bit fields, 128-bit values as four
    native-order chunks, IPv4 as a native-order word; `0xE5` datagrams are
    zlib-packed and inflated with a 1 MiB cap;
  - routing table with bounded contacts;
  - bootstrap from `nodes.dat` (version 1/2/3), downloaded or cached;
  - `BOOTSTRAP_REQ/RES`, `HELLO_REQ/RES`, `PING/PONG`, `REQ/RES`;
  - `SEARCH_KEY_REQ` / `SEARCH_RES` with bounded iterative lookups, 15 ms
    pacing between datagrams and one bounded retry when a round is silent; the
    request carries a restrictive AND expression tree (start position
    `0x8000`) so the peer filters the keyword bucket instead of returning
    every file indexed under the primary keyword;
  - answer to peers' routing/ping/hello requests, and an empty answer to search
    requests (this daemon does not index files).
- **eD2k servers**:
  - `server.met` download and parsing;
  - login (`OP_LOGINREQUEST`), `OP_IDCHANGE`/`OP_SERVERIDENT`;
  - keyword search (`OP_SEARCHREQUEST`) and `OP_SEARCHRESULT` parsing;
  - results merged with Kad by MD4 hash, higher source count wins.
- **Daemon**:
  - managed/standalone modes, token authentication, standalone Host/Origin
    guards;
  - `/api/v1/health`, `/api/v1/status`, `/api/v1/search`, `/api/v1/link`,
    `/api/v1/nodes`;
  - a server-rendered page in Italian, English, German, French, Spanish and
    Polish, with the shared design system (`theme.css`, `common.js`,
    `icons.svg`).

## What is deliberately not implemented

- the eMule transfer protocol, `.part`/`.part.met`, MD4 block trees, AICH;
- upload, sharing, credits, upload queue;
- LowID callbacks, buddy/hole punching, direct UDP callback;
- obfuscation/encryption (`KadVersion` stays below 6, so peers answer in
  clear), secure identification;
- file publishing (`PUBLISH_*`).

`capabilityLevels` in Gextto has no eD2k entry yet: the transfer plane is not
advertised until a real engine exists. The search plane is a provider, not a
backend.

## Bootstrap and reachability

Kad starts from a `nodes.dat` (default:
`https://upd.emule-security.org/nodes.dat`, cached in the data directory for a
week and rewritten with the contacts learned during use, so the next start does
not depend on the download). eD2k server search starts from `server.met`
(default: `https://upd.emule-security.org/server.met`) or an explicit `-servers`
list.

On start the daemon asks the router to map the Kad UDP port and the eD2k TCP port
with **UPnP IGD** (`-upnp`, default on). The eD2k TCP port is accepted and
closed: the daemon does not transfer files, but being connectable is what turns
the server-assigned LowID into a HighID. `/api/v1/health` exposes a
`reachability` sensor — `open` (Kad peers answered), `limited` (UPnP failed, a
LowID was assigned, or UPnP succeeded but no Kad peer answered, which points at
blocked outbound UDP) or `unknown` — and the page shows it next to the node
count. A host behind NAT
without UPnP can still search, but Kad may be firewalled and eD2k LowID, which
reduces the number and quality of sources. Port forwarding UDP 4672 and TCP 4662
is the manual fix.

## Configuration

Gextto passes these flags when it starts the managed daemon:

| Flag | Meaning |
|---|---|
| `-listen` | HTTP page and API address |
| `-data` | data directory (`DATA_DIR/gx-ed2k`) |
| `-token` | API token (also `GX_ED2K_TOKEN`) |
| `-mode` | `managed` or `standalone` |
| `-kad-listen` | Kad UDP bind (default `0.0.0.0:4672`) |
| `-tcp-port` | TCP port advertised to the network |
| `-nodes-url` | `nodes.dat` URL |
| `-serverlist-url` | `server.met` URL |
| `-servers` | explicit `host:port` server list |
| `-search-timeout` | whole search budget |
| `-lang` | page language |

The matching Gextto settings are `ed2k_enabled`, `ed2k_url`, `ed2k_token`,
`ed2k_fallback`, `ed2k_min_sources`, `ed2k_search_timeout_secs`; the daemon also
reads the API-only keys `ed2k_nodes_url`, `ed2k_serverlist_url` and
`ed2k_servers` when Gextto starts it.

## Search flow in Gextto

1. Manual search: when the provider is enabled, `SearchED2K` calls
   `/api/v1/search` and appends normalized `models.Release` values
   (`network: "ed2k"`, `ed2k_link`, `sources`). The UI labels them and disables
   "add" while `download` is false. A pasted `ed2k://` link is validated by
   `POST /search/link` and shown the same way.
2. Automatic fallback: only in the **deep** gap-filling pass, only after the
   torrent episode and season searches returned zero, only when
   `ed2k_fallback` is on **and** the daemon reports `download: true`
   (`ed2kFallbackEligible`). A candidate must match the configured episode
   exactly, have a recognised quality, a plausible size and at least
   `ed2k_min_sources` sources.
3. The transfer router (`multi_engine.go`) is ready and tested: it merges rows,
   labels `TorrentView.Network`, routes by hash and persists the route. It is
   not installed while the eD2k download capability is unavailable.

## Packaging

- `make gx-ed2k` builds `bin/gx-ed2k` with its own committed build number
  (`gx-ed2k.build_number`).
- `make build` rebuilds it only when its code changed, like gx-torrent.
- `scripts/package-gx-ed2k.sh` produces `gx-ed2k-linux-<arch>.tar.gz` (binary,
  systemd unit, install/uninstall scripts, README, licence).
- `scripts/build-release.sh` publishes that archive next to Gextto's and
  gx-torrent's.

## Tests

- `internal/ed2k`: link parser, tag codecs (Kad big-endian, server
  little-endian), nodes.dat/server.met parsers, routing, a fake Kad UDP node and
  a fake eD2k TCP server that answer real wire packets.
- `cmd/gx-ed2k`: API, auth, link validation, page rendering, i18n coverage and
  the option-tooltip guard.
- Gextto: `ed2ksearch_test.go` (provider, dedup, fallback gate, DB identity),
  `multi_engine_test.go` (merge and routing).

All tests are local and deterministic: they do not contact the live eD2k/Kad
network. Measuring live coverage is the Fase 0/Fase 1 gate in
[`../docs/evoluzione.md`](evoluzione.md).
