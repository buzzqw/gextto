# eD2k and Kad through aMule

Gextto reaches the eD2k and Kad networks through **aMule**. `amuled` does the
networking (servers, Kad, sources, queues, credits, transfers); Gextto talks to
it over aMule's **External Connections (EC)** protocol, the same binary protocol
`amulecmd`, `amuleweb` and the remote GUI use. The client is our own Go code in
`internal/amule`: aMule's sources were read only as a protocol reference.

Everything is optional and off by default (`ed2k_enabled`). With aMule off or
unreachable the eD2k features are simply unavailable; torrents are never
affected.

## What the user gets

- **Search** on Kad, on Kad + every eD2k server (both searches merged by hash),
  on every eD2k server (global) or on the connected server, from the eD2k page,
  from the manual search (the *Kad* box) and from the Archive (the *Also search
  eD2k* box, results in a block below the archive). Results carry
  name, size, MD4 hash and sources, and are parsed like torrent names (series,
  episode, quality).
- **ed2k:// links**: paste one or more (even mixed with text) on the eD2k page
  or in the search box of Downloads; they go to aMule and a video is registered
  like a manual download. A file that is not a video (a pdf, a program, an
  archive) is only aMule's download: it stays in the incoming folder, like a
  torrent added by hand, instead of failing the library import.
- **Queue in Downloads**: aMule's queue is shown next to the torrents and the
  HTTP downloads, with progress, speed, sources (transferring / total) and
  pause / resume / remove; ⓘ opens the details (last data received, hash, link
  to copy) and sets the file's priority in aMule. The downloads count on the
  dashboard and in the menu badge, and appear in the TUI download list.
- **Dashboard**: the traffic chart draws aMule's download and upload as dashed
  lines next to the torrents', from aMule's global rates; the top bar shows the
  sum.
- **Bandwidth**: aMule follows the torrents' speed limits (rule, exception,
  schedule, Auto), sent when they change and re-sent every 10 minutes. A completed file is only taken off aMule's list; an
  incomplete one is cancelled with its partial data.
- **Completion like a torrent**: when aMule completes a file, Gextto raises the
  same `torrent_finished` event a torrent produces (aMule 2.3 keeps a finished
  file in the download queue; aMule 3.x drops it at once, so Gextto finds it
  among the shared files, with its folder, also after a Gextto restart), so renaming, the library
  import, MediaInfo checks, notifications and the media library refresh are
  shared. eD2k episodes are always imported by **hardlink or copy** (never
  moved), so aMule keeps sharing the original from its incoming folder; a
  hardlink costs no extra space when both folders are on the same filesystem.
- **aMule's log** on the eD2k page (Load the log) and in `GET /api/ed2k/log`.
- **Connection and status** on the eD2k page: aMule version, eD2k server and
  High/Low ID, Kad state (firewalled or not), speeds and limits, network size,
  upload queue; connect / disconnect, start / stop Kad, connect to a given
  server, leave the server, download a `server.met` or a `nodes.dat` from an
  address.
- **Stalled downloads**: an eD2k download Gextto started by itself that
  receives nothing for `ed2k_stall_days` (7, 0 = never; aMule's "last
  received" time, so restarts do not reset it) is cancelled in aMule, its
  release is blocklisted and the gap is searched again. Downloads added by
  hand and paused ones are left alone.
- **Automatic fallback** (`ed2k_fallback`, off by default): in the deep
  gap-filling pass, when the torrent episode and season searches found
  nothing, Gextto searches Kad and the eD2k servers (global search) for the
  configured name and every alias, and
  starts a result only with the exact episode, a recognised quality, a video
  file and at least `ed2k_min_sources` sources.

## Setting up aMule

aMule is installed and started on its own (`amule-daemon` package on
Debian/Ubuntu). In `~/.aMule/amule.conf` (or the `--config-dir` in use):

```ini
[ExternalConnect]
AcceptExternalConnections=1
ECAddress=127.0.0.1
ECPort=4712
ECPassword=<md5 of the password>
```

`ECPassword` is the MD5 hex digest of the password (`printf '%s' 'secret' |
md5sum`). In Gextto the password is entered **in clear** (`amule_password`):
the client hashes it for the login.

In Docker (`ngosang/amule`), mount the download share at the **same path** inside
the container and set `INCOMING_DIR`/`TEMP_DIR` to those paths, so the folder
aMule reports is valid on the host; use a dedicated incoming folder, because
aMule shares everything in it. With `network_mode: host` and `UPnPEnabled=1`
aMule opens its eD2k/Kad ports on the router by UPnP (in bridge mode it would
ask the router to forward to the container's address, which does not work).

On a network disk set `AllocateFullFile=0`: preallocating a multi-GB part file
blocks amuled (and its EC answers) for tens of seconds. Gextto copes with it:
an add that times out, or that aMule refuses because the file is already
listed, is checked against the download queue and counts as done when the file
is there.

For completed files to be imported, Gextto must see aMule's incoming folder
(`IncomingDir`) at the same path, which is the case when both run on the same
machine. Opening aMule's TCP and UDP ports on the router gives a High ID and a
non-firewalled Kad, hence more sources.

## Settings

| Key | Meaning |
|---|---|
| `ed2k_enabled` | master switch (off) |
| `amule_host` | host of `amuled` (`127.0.0.1`) |
| `amule_port` | EC port (`4712`) |
| `amule_password` | EC password, in clear (secret) |
| `ed2k_search_network` | `kad` (default), `both` (Kad + eD2k servers), `global` or `local` |
| `ed2k_search_timeout_secs` | how long aMule collects results (5–120 s, default 20) |
| `ed2k_fallback` | automatic gap-filling fallback (off) |
| `ed2k_min_sources` | source floor of the fallback (3) |
| `ed2k_stall_days` | give up on an automatic download idle for this many days (7, 0 = never) |

The settings of the earlier integrations (extto's `amule_*` service options,
gx-ed2k's `ed2k_url`, `ed2k_token`, `ed2k_upnp`, …) are deleted at load;
`amule_host`, `amule_port` and `amule_password` are kept.

## The EC client (`internal/amule`)

- Framing: `flags:uint32 BE | length:uint32 BE | payload`. The payload is
  `opcode:uint8 | tagCount:uint16 | tags`; a tag is `name<<1|hasChildren:uint16
  | type:uint8 | length:uint32 | [childCount | children] | data`. A tag's length
  counts its data plus every child with a fixed 7-byte header (+2 when the child
  has children), independent of the number encoding.
- The client never advertises `EC_TAG_CAN_ZLIB` or `EC_TAG_CAN_UTF8_NUMBERS`,
  so amuled answers with plain packets; the decoder still accepts zlib payloads
  and UTF-8-encoded numbers.
- Login (protocol `0x0204`, aMule 2.3.x): `EC_OP_AUTH_REQ` → `EC_OP_AUTH_SALT`
  → `EC_OP_AUTH_PASSWD` with `MD5(hex(MD5(password)) + hex(MD5(uppercase hex
  of the salt)))` → `EC_OP_AUTH_OK`.
- One connection, requests serialised; a transport error closes it and the
  request is retried once on a fresh login, so an amuled restart is invisible.
- Operations: status (`EC_OP_STAT_REQ` with the connection state), server
  list, connect/disconnect, server connect/disconnect, `server.met`/`nodes.dat`
  from URL, Kad start/stop, download queue, add link, pause/resume/delete,
  clear completed, shared files, search (start, results, progress, stop), preferences
  (incoming/temp folders, bandwidth limits) and the log.
- aMule keeps **one** search result list: a new search replaces the previous
  one. Gextto serialises searches (`ed2kSearchMu`), so the alias queries of the
  gap filler run one after the other. A Kad search ends when the core reports
  `0xfffe` and the result count is stable; a server search is read until the
  configured duration elapses.

## Gextto side

| File | Role |
|---|---|
| `ed2ksearch.go` | settings, shared client with a fail-fast backoff while amuled is down, cached status, search and gap-filling fallback, `GET /api/ed2k/status` |
| `ed2k_downloads.go` | download list, add/pause/resume/remove, the session that turns completed files into `torrent_finished` events, the copy-mode import configuration |
| `uiweb_v2_ed2k.go` | the eD2k page (status, servers, Kad, search, pasted links) |
| `web_background.go` | merges aMule's completion events into the torrent event loop, each handled by its own session |

## Tests

- `internal/amule`: codec (round trip, tag length rule, UTF-8 numbers, zlib,
  malformed input), login answer, client against the fake core
  `internal/amule/amuletest`.
- `TestLiveCore` runs against a real amuled when `GEXTTO_AMULE_EC=host:port`
  and `GEXTTO_AMULE_PASSWORD` are set: status, preferences, servers, add /
  pause / resume / delete of a link, Kad search, limits, log and a refused
  wrong password. It is skipped in normal runs.
- Gextto: `ed2ksearch_test.go` (search, refusal, fallback gate, wrong password,
  aliases, link add and actions, completion events, copy-mode import) against
  the fake core.

## History

Gextto first shipped its own eD2k/Kad daemon, `gx-ed2k`. Its protocol layer
worked, but the download scheduler (sources, queues, UDP reasks) never reached
the level of a real eMule client, and the measured Kad coverage of missing
episodes was 7–13% (see [`measurements.md`](measurements.md)). It was retired in
favour of aMule; the code is kept in the git history, at commit `9f4e2b9`.
