# gx-ed2k — the eD2k/Kad search daemon

`gx-ed2k` is Gextto's native eD2k/Kad client. Like `gx-torrent`, it is our own
Go code, it can run managed by Gextto or standalone, and it has its own page and
`/api/v1`. It started as a search-only client; the transfer plane is now
enabled for completed eD2k downloads, with AICH, upload queue/credits and
source exchange, while obfuscation and the server-callback path remain open.

`/api/v1/health` reports `"download": true` when the transfer plane and its
storage are available. Gextto can add an eD2k result as a native download and
receives progress/completion events like it does for torrents.

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
    `/api/v1/nodes`, `/api/v1/update`;
  - a server-rendered page in Italian, English, German, French, Spanish and
    Polish, with the shared design system (`theme.css`, `common.js`,
    `icons.svg`).

## What is deliberately not implemented

- obfuscation/RC4 and secure identification (`KadVersion` stays below 6, so
  peers answer in clear);
- server-originated callback (direct UDP and buddy callbacks are implemented;
  the server callback needs a source client ID that the current source record
  does not carry);
- a full LowID↔LowID relay through a third client;
- file publishing (`PUBLISH_*`) and the full `known.met` shared-file manager;
- per-file bandwidth limits and priorities.

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
   `ed2k_min_sources` sources. The query runs the configured series name and
   its aliases in parallel, so a localized title does not hide releases
   published under the original one.
3. The transfer router (`multi_engine.go`) is ready and tested: it merges rows,
   labels `TorrentView.Network`, routes by hash and persists the route. It is
   not installed while the eD2k download capability is unavailable.

## Standalone updates

`release.json` carries `gx_ed2k_build`/`gx_ed2k_version`, and the release
workflow publishes `gx-ed2k-linux-<arch>.tar.gz` next to the Gextto and
gx-torrent archives. The standalone page's **Check for updates** calls
`/api/v1/update`: it fetches the manifest for the stamped channel, compares the
build and links to the archive when a newer one exists. Binaries built from a
checkout have no channel and answer `local_build: true`, so they never offer an
update that may be older than the source.

On Linux the page also offers **Install update** (`POST
/api/v1/update/apply`): the daemon downloads the archive, verifies the published
SHA-256, checks that the extracted binary reports the version in the manifest,
keeps the old binary as `gx-ed2k.old`, replaces the executable and execs it with
the same arguments. Windows is deliberately excluded: a running `.exe` cannot
be replaced, so the page keeps the zip download link there.

## Packaging

- `scripts/package-gx-ed2k.sh` produces the Linux tarball;
  `scripts/package-gx-ed2k-windows.sh` cross-builds `gx-ed2k.exe` (amd64 and
  arm64) and packages a Windows service installer with the firewall rules for
  UDP 4672 and TCP 4662. CI also runs the real `gx-ed2k.exe` on
  `windows-latest` and checks `/api/v1/health`, so Winsock/path handling is
  tested and not only cross-compiled.
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

## Riferimenti di protocollo (eMule/aMule)

Il client resta codice nostro: il sorgente di eMule e delle sue mod è usato
solo come **riferimento di protocollo**, non copiato e non incluso nel
repository. In laboratorio è stato usato anche aMule come peer di
interoperabilità (mai come dipendenza a runtime).

Fatti verificati sul sorgente e bloccati dai test:

- header client `<protocollo><lunghezza 4 LE><opcode>`, dove la lunghezza
  include l'opcode; `0xE3` per i pacchetti eDonkey e `0xC5` per le estensioni
  eMule (EMULEINFO, queue ranking, parti compresse, source exchange);
- `OP_HELLO` inizia con il byte `16` (dimensione dell'hash) e termina con
  server IP (4 byte, ordine di rete) e porta (2 byte LE); `OP_HELLOANSWER` non
  ha il byte iniziale ma ha lo stesso suffisso;
- `OP_REQFILENAMEANSWER` usa una lunghezza nome a 16 bit;
- una richiesta file estesa aggiunge due word a 16 bit (stato parti e fonti
  complete) dopo l'hash;
- non si manda `OP_SETREQFILEID` prima di `OP_STARTUPLOADREQ`: eMule reale
  chiude la connessione, aMule la tollera; il test del fake peer lo impedisce;
- per i file multi-parte si legge `OP_FILESTATUS` e si chiedono solo le parti
  dichiarate dal peer;
- per le fonti LowID si invia `OP_DIRECTCALLBACKREQ` (UDP 0xC5/0x95) e, se la
  fonte espone un buddy, `KADEMLIA_CALLBACK_REQ` al buddy; in entrambi i casi si
  accetta la connessione TCP di ritorno solo quando la nostra porta è
  raggiungibile (HighID). Il relay LowID↔LowID tramite terzo client non è
  implementato.

I test `TestClientWireInvariants`, `TestKadWireFormat`, i test dei codec e il
fake peer in `internal/ed2k/transfer/download` bloccano questi invarianti.

