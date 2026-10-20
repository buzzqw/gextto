# eD2k and Kad through aMule

Gextto reaches the **eD2k** and **Kad** networks through **aMule**. `amuled`
does the networking — servers, Kad, sources, queues, credits, transfers — and
Gextto drives it over aMule's **External Connections (EC)** protocol, the same
one `amulecmd`, `amuleweb` and the remote GUI use: it searches, sends
`ed2k://` links, lists and controls the downloads in **Downloads**, shows the
**eD2k** page and imports completed files into the library.

The integration is **optional and off by default** (`ed2k_enabled`). Gextto does
not ship an eD2k client: you install, configure and run aMule yourself. With
aMule off or unreachable the eD2k features are simply unavailable and torrents
are never affected.

- [Requirements](#requirements)
- [Install aMule](#install-amule)
- [Turn it on in Gextto](#turn-it-on-in-gextto)
- [Using it](#using-it)
- [Troubleshooting](#troubleshooting)
- [Reference](#reference)

## Requirements

- **aMule with `amuled`**, running and reachable from Gextto: on the same
  machine (the usual case) or on another machine of the network.
- **External connections enabled** in aMule, with a password (see below).
- **The incoming folder visible at the same path** on the machine where Gextto
  runs, if completed files must be imported into the library. When both run on
  the same machine this is automatic.
- **aMule's TCP and UDP ports open** on the router for a High ID and a Kad that
  is not firewalled, which means more sources. With the default ports that is
  TCP 4662 and UDP 4672 (Kad and eD2k share the UDP port).

## Install aMule

Install aMule from your distribution. On Debian and Ubuntu the package is
`amule-daemon`:

```bash
sudo apt install amule-daemon
```

On the other distributions install the aMule package that ships `amuled`.
Start it the way the distribution ships it (the package usually provides a
service), or run `amuled` yourself as the user that owns aMule's configuration
folder. Unless a different `--config-dir` is used, that folder is `~/.aMule`
and the file to edit is `~/.aMule/amule.conf`.

> [!NOTE]
> aMule rewrites `amule.conf` when it exits. Edit the file while `amuled` is
> stopped, then start it again, or the changes are lost.

### Enable external connections

In `amule.conf`:

```ini
[ExternalConnect]
AcceptExternalConnections=1
ECAddress=127.0.0.1
ECPort=4712
ECPassword=<md5 of the password>
```

- `ECPassword` is the **MD5 hex digest** of the password, not the password
  itself: `printf '%s' 'secret' | md5sum`. In Gextto the password is entered
  **in clear** (`amule_password`) and hashed by the client at login.
- `ECAddress` is the address aMule binds: `127.0.0.1` accepts connections only
  from the same machine. If Gextto runs on another host, bind to the interface
  or to `0.0.0.0` and restrict port 4712 with the firewall.
- `ECPort` is the port Gextto connects to (4712 by default); it must match
  **aMule — external connections (EC) port** in Gextto's settings.

### Docker

With the `ngosang/amule` image (or any aMule container):

- mount the download share at the **same path** inside the container and set
  `INCOMING_DIR` and `TEMP_DIR` to those paths, so the folder aMule reports is
  valid on the host too;
- use a **dedicated incoming folder**, because aMule shares everything it
  holds;
- to let aMule open its ports on the router by UPnP, run the container with
  `network_mode: host` and set `UPnPEnabled=1` in `amule.conf`. In bridge mode
  aMule would ask the router to forward the ports to the container's address,
  which does not work;
- publish the EC port (4712) and set `ECAddress=0.0.0.0` if Gextto is outside
  the container.

### On a network disk

Set `AllocateFullFile=0` in `amule.conf`: preallocating a multi-GB part file on
a NAS can block `amuled` (and its EC answers) for tens of seconds. Gextto copes
with a slow aMule — an add that times out, or that aMule refuses because the
file is already listed, is checked against the download queue and counts as
done when the file is there — but turning the preallocation off avoids the
wait altogether.

## Turn it on in Gextto

**Configuration → Sources → eD2k and Kad**:

1. switch on **eD2k and Kad through aMule**;
2. fill in **address** (usually `127.0.0.1`), **EC port** (4712) and the
   **password in clear**;
3. save, then open the **eD2k** page (**Discovery → eD2k**) and press
   **Connect** to join eD2k and start Kad.

The **Sources** page shows the eD2k and Kad sensor: **open** (connected to a
server with a High ID), **limited** (firewall/NAT: Low ID, or Kad only), **not
connected**, **aMule unreachable** or **unknown** (state not read yet). If the
sensor says *limited*, open aMule's TCP and UDP ports on the router.

When aMule does not answer, the eD2k page says so, explains what to check and
offers **Retry**. Nothing else in Gextto stops working.

## Using it

### Search

Three places, all through aMule:

- **Manual search** (the search box of **Downloads** and the search page):
  tick **Kad** to add eD2k/Kad results to the torrents.
- **eD2k page → Search eD2k and Kad**: pick the network and search; aMule
  collects results for a few seconds.
- **Archive**: tick **Also search eD2k** to get a block of eD2k results under
  the archived releases.

The network is the one chosen in **eD2k search network**:
**Kad** (default: a distributed network, no server needed), **Kad + eD2k
servers** (both searches, results merged by hash), **eD2k servers (global)**
(every server in aMule's list) or **eD2k server (connected)** (only the one
aMule is connected to). The automatic fallback always searches Kad and the
servers together.

Every result shows name, size, MD4 hash and sources (transferring / total),
is parsed like a torrent name (series, episode, quality) and has
**Download**; **already known** marks a file aMule already has. The eD2k page
also lists aMule's servers with users, files and ping, and can download a new
`server.met` or a `nodes.dat` for Kad from an address.

### Paste eD2k links

On the **eD2k page** or in the **Downloads** search row (the field under the
search box, **Open eD2k links**), paste one or more `ed2k://` links, even mixed
with text: Gextto validates them, shows them as results and **Download with
aMule** queues them all. A link to a file that is not a video (a pdf, a
program, an archive) is only an aMule download and stays in the incoming
folder, like a torrent added by hand.

### Downloads and completion

aMule's queue is listed in **Downloads** next to the torrents and the HTTP
downloads, with progress, speed, sources (transferring / total), pause /
resume / remove and a badge **eD2k**. ⓘ opens the details: state, sources,
last data received, hash, the `ed2k://` link to copy and the file's
**priority in aMule** (low, normal, high, automatic). The downloads count on
the dashboard and in the menu badge, appear in the TUI list and their traffic
is drawn on the dashboard chart as dashed lines, while the top bar sums
torrents and aMule.

When aMule completes a video, Gextto raises the same completion event a torrent
produces, so renaming, the library import, the MediaInfo check, the
notification and the media library refresh are shared. The quality gate runs
before anything is written to the library, movies included: if the library
already holds an equal or better file, the download is discarded (moved to the
trash, or deleted according to the cleanup action) and never copied. aMule 2.3 keeps a
finished file in its download queue; aMule 3.x drops it at once, so Gextto
finds it among the shared files, also after a restart. eD2k episodes are always
imported by **hardlink or copy** (never moved), so aMule keeps sharing the
original from its incoming folder; a hardlink costs no extra space when both
folders are on the same filesystem.

### Bandwidth

aMule follows Gextto's speed limits (rule, exception, schedule, Auto): they are
sent when they change and re-sent every 10 minutes. The eD2k page shows the
limits aMule has now.

### Automatic fallback

Second, separate switch (**Use eD2k when torrents find nothing**, off by
default): in the deep gap-filling pass, only when the torrent episode and season
searches found nothing, Gextto searches Kad and the eD2k servers (global
search) for the configured name and every alias, and starts a result only with
the exact episode, a recognised quality, a video file and at least the source
floor set in **Minimum sources for the fallback**.

### Downloads that stall

An eD2k download Gextto started by itself that receives nothing for as many
days as **Give up on a stalled eD2k download after** says (7, 0 = never,
measured on aMule's "last data received" time, so restarts do not reset it) is
cancelled in aMule, its release is blocklisted and the gap is searched again.
Downloads added by hand and paused ones are left alone.

### aMule's log

**eD2k page → aMule log → Load the log** shows the last lines of `amuled`'s log.
The same lines are available at `GET /api/ed2k/log`.

## Troubleshooting

| Symptom | What to check |
|---|---|
| **aMule unreachable** on the eD2k page or the Sources sensor | `amuled` is running; `AcceptExternalConnections=1`; `ECPort` and address match Gextto's settings; port 4712 is open in the firewall if aMule is on another machine; `ECAddress` is not `127.0.0.1` when Gextto connects from elsewhere. |
| **Wrong password** (the page reports `authentication failed`) | In Gextto the password is the **clear** one; `ECPassword` in `amule.conf` is its MD5. After changing it, restart `amuled`. |
| **Low ID** (badge on the eD2k page) | aMule's TCP port (4662 by default) is not reachable from outside: open it on the router, or enable UPnP in aMule. Fewer sources, slower queues. |
| **Kad behind a firewall** | aMule's UDP port (4672 by default) is not reachable: open it on the router. Kad works but finds fewer sources. |
| **Search returns nothing** | the chosen network (Kad only needs Kad to be connected); a search duration that is too short; a Kad firewalled. Kad and the servers index different files, so try **Kad + eD2k servers**. |
| **Download stays queued** | normal on eD2k: aMule waits for its turn in the sources' queues. Check sources and the ID; low ID means long queues. |
| **Files complete in aMule but never appear in the library** | the incoming folder is not visible at the same path from Gextto, or is not readable; the file is not a video (it stays in the incoming folder by design); aMule was still preallocating (`AllocateFullFile=0` on network disks). |
| **The eD2k page shows the integration as off** | the switch is off, or the settings were not saved. The page links straight to the setting. |

## Reference

### Settings

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
`amule_host`, `amule_port` and `amule_password` are kept. The API is documented
in [`API.md`](API.md) (`/api/ed2k/status`, `/api/ed2k/downloads`,
`/api/ed2k/limits`, `/api/ed2k/log`).

### The EC client (`internal/amule`)

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

### Gextto side

| File | Role |
|---|---|
| `ed2ksearch.go` | settings, shared client with a fail-fast backoff while amuled is down, cached status, search and gap-filling fallback, `GET /api/ed2k/status` |
| `ed2k_downloads.go` | download list, add/pause/resume/remove, the session that turns completed files into `torrent_finished` events, the copy-mode import configuration |
| `uiweb_v2_ed2k.go` | the eD2k page (status, servers, Kad, search, pasted links) |
| `web_background.go` | merges aMule's completion events into the torrent event loop, each handled by its own session |

### Tests

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
