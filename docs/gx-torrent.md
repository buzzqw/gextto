# gx-torrent: the alternative torrent engine in pure Go

`gx-torrent` is a small BitTorrent daemon written in pure Go on top of the
**gx-core** engine (`internal/gxcore`). It is the default engine of
Gextto (`torrent_backend = gx-torrent`): it does not require `libtorrent-rasterbar`,
runs in a separate process and is controlled via REST (`127.0.0.1:8890` when
Gextto manages it, `0.0.0.0:8890` in standalone), where it also exposes an
operational web page openable from the browser (managed: only from this server,
see *Web interface* to open it to the LAN; standalone: already reachable from
another PC on the LAN). The embedded
libtorrent engine remains selectable — on builds that include it — and acts as an
automatic fallback if gx-torrent does not start.

The gx-core engine lives in `internal/gxcore`. Its features are
described in `internal/gxcore/GEXTTO.md`: single port, outgoing interface,
proxy, IP filter, file selection and some fixes. The **state of
compliance with the BitTorrent specifications (BEP)** and the roadmap are in
[BEP compliance](bep.md).

Code:

| File | Role |
|---|---|
| `cmd/gx-torrent/main.go` | flags, startup, signals, clean shutdown |
| `cmd/gx-torrent/daemon.go` | gx-core session, persistent state, queue loop, operations |
| `internal/queue/queue.go` | queue scheduler (pure function) and dynamic queue |
| `cmd/gx-torrent/storage.go` | per-torrent symlink, safe move and deletion |
| `cmd/gx-torrent/api.go` | REST API with token |
| `cmd/gx-torrent/network.go` | port, interfaces, proxy, encryption, IP filter |
| `cmd/gx-torrent/portmap.go` | opening the port on the router (UPnP, NAT-PMP) |
| `cmd/gx-torrent/selection.go` | file selection |
| `cmd/gx-torrent/cache.go` | adaptive cache (available RAM, load, storage type) |
| `cmd/gx-torrent/stream.go` | HTTP streaming (Range) with piece readahead |
| `cmd/gx-torrent/ui.go` | daemon web page (summary, table, detail, actions) |
| `cmd/gx-torrent/i18n.go` | web page translations (it/en/de/fr/es/pl) |
| `cmd/gx-torrent/lsd.go` | local network discovery (BEP 14) |
| `cmd/gx-torrent/tracker_health.go` | removal of trackers that never responded |
| `cmd/gx-torrent/lifecycle.go` | rotated daemon log, orphan watchdog, Gextto log queue |
| `gxtorrent_engine.go` | Gextto-side `TorrentEngine` adapter |
| `gxtorrent_runtime.go` | startup and supervision of the daemon by Gextto |

## Index

- [Startup](#startup)
- [Web interface](#web-interface)
  - [Network exposure](#network-exposure)
- [Standalone use](#standalone-use)
  - [Feed archive](#feed-archive)
- [Network](#network)
- [uTP and LSD](#utp-and-lsd)
- [Holepunching (BEP 55)](#holepunching-bep-55)
  - [Verification](#verification)
- [File selection and priorities](#file-selection-and-priorities)
- [Sequential download (streaming)](#sequential-download-streaming)
- [BitTorrent v2 torrents](#bittorrent-v2-torrents)
- [Statistics](#statistics)
- [Piece diagnostics](#piece-diagnostics)
- [HTTP streaming (HTTP Range)](#http-streaming-http-range)
- [Disk cache, preallocation and mmap storage](#disk-cache-preallocation-and-mmap-storage)
- [RAM disk](#ram-disk)
- [On-disk layout and data safety](#on-disk-layout-and-data-safety)
- [Self-managed queue](#self-managed-queue)
- [Stalled: division of tasks](#stalled-division-of-tasks)
- [Seed](#seed)
- [Super-seeding (BEP 16)](#super-seeding-bep-16)
- [Writes to the state disk](#writes-to-the-state-disk)
- [Bandwidth limits](#bandwidth-limits)
- [REST API (v1)](#rest-api-v1)
- [API availability (snapshot and lock)](#api-availability-snapshot-and-lock)
- [Known limits (engine)](#known-limits-engine)
- [Tests](#tests)

## Startup

1. `make build` (or `scripts/build-daemon.sh`) compiles `bin/gx-torrent` alongside
   `bin/gexttod`; `make gx-torrent` compiles only the daemon. The package, `install.sh`
   and `gexttod --update` install `gx-torrent` alongside `gexttod`. The daemon has
   its **own build number** (`gx-torrent.build_number`), independent of
   Gextto's: it increases by one at every recompilation and stays unchanged
   when the build does not touch it. It is the `1.1.<n>` version reported by
   `gx-torrent --version`, by its web page and by `/api/v1/health`.
2. `gx-torrent` is the default engine of a new installation: there is no need to
   choose it. To use embedded libtorrent instead, set *Torrent engine* to
   `embedded` and restart. A configuration that has already saved an engine stays
   as it was: the new default applies only to new installations.
3. Gextto **always starts and supervises** the daemon when gx-torrent is the active
   engine:
   - data in `DATA_DIR/gx-torrent`;
   - log in `DATA_DIR/gx-torrent/gx-torrent.log`, written by the daemon itself and
     rotated at 5 MB keeping 4 files; errors of individual peers (expired
     handshakes, resets) and of failed announces are at debug level;
   - default download folder equal to libtorrent's;
   - the daemon is restarted if it exits;
   - **it stays up when Gextto restarts** (for example for an
     update): under systemd it starts in its own scope
     (`gextto-gx-torrent-*.scope`), so stopping the service does not affect it.
     On restart Gextto re-attaches to it if it has the same binary and the same
     options (`fingerprint` in `/api/v1/health`): peers, queue and transfers
     continue without interruption. If the binary or a startup option have
     changed, Gextto stops it with SIGTERM (45 seconds of grace, the engine saves the
     resume data) and starts a new one. `make build` replaces
     `bin/gx-torrent` only if its code has changed
     (`bin/gx-torrent.code-sha256`), so an update that touches only
     gexttod does not stop downloads;
   - if Gextto does not come back, the daemon stops by itself after 15 minutes without
     requests (`-orphan-timeout`); if the active engine is no longer gx-torrent,
     Gextto stops the daemon left running at startup. Gextto re-attaches to or
     stops only a daemon with its own data folder;
   - after 6 abnormal starts/stops in 10 minutes Gextto stops retrying.
     On a build with embedded libtorrent it falls back to the `embedded` engine and restarts
     the service; on a **pure Go** build (the default one) there is no
     fallback engine, so it continues with gx-torrent in **safe mode**
     (without uTP or holepunch) and growing waits between attempts (1 m, 5 m,
     15 m, 1 h). A clean shutdown (restart of Gextto or of the service, for
     example for an update) does not count: the budget considers only unexpected
     exits and failed starts; a daemon that stays up long enough
     resets it (10 minutes for the stop budget, 30 minutes to reset the
     growing waits).

   If a gx-torrent already answers on the URL (for example a systemd service of yours),
   Gextto uses that one.

Settings (*Torrent engine* tab, *gx-torrent* group):

| Key | Default | Notes |
|---|---|---|
| `gxtorrent_url` | `http://127.0.0.1:8890` | URL with which Gextto reaches the daemon |
| `gxtorrent_listen` | `127.0.0.1:8890` | listen address of the managed daemon: by default page and API stay only on this server. To open them to the LAN see *Web interface → Network exposure*. The port is aligned to that of `gxtorrent_url` (Gextto must be able to reach it) |
| `gxtorrent_token` | empty | `X-Gx-Token` header; mandatory if the daemon listens on the network |
| `gxtorrent_auto` | `true` | self-management of gx-torrent: dynamic queue and adaptive cache. Disable it to set slots and cache by hand |
| `gxtorrent_request_timeout_secs` | `15` | 1–300 |
| `gxtorrent_poll_interval_ms` | `1500` | minimum interval between two state reads (250–60000) |
| `gxtorrent_proxy` | empty | proxy for gx-torrent: `socks5://[user:password@]host:port` or `http://host:port`. Peers, HTTP trackers and web seeds go through the proxy; DHT and UDP trackers are turned off. It travels in the daemon's environment, not on the command line |

The queue and bandwidth settings remain those of the *libtorrent* section:

- active downloads and seeds, total limit;
- "don't count slow torrents";
- dynamic queue with minimum and maximum;
- standard, scheduled and temporary speed limits.

Gextto forwards them to the daemon.

## Web interface

At the address indicated by `gxtorrent_url` (default `http://127.0.0.1:8890`) the
daemon does not expose only the REST API: `GET /` (or `/ui`) opens an **operational
web page** intended for whoever opens that address from the browser, like qBittorrent's
Web UI. It refreshes by itself every 2 seconds without reloading the page.
The browser tab shows the daemon icon (a blue download arrow),
included in the page and also served by `GET /favicon.ico` without a token.
The page is **translated** (Italian, English, German, French, Spanish,
Polish): at startup Gextto passes the interface language to the daemon with
`-lang`, and `?lang=xx` overrides it for the single request.

- **Session summary**: at the top the tiles with the counts (torrents, downloading,
  seeding, stalled, paused, moving), peer port, IP filter rules, cache
  read/write, I/O operations and incoming connections; the live
  values (speed, downloaded/uploaded in the session, free space, DHT, port,
  encryption) are in the status bar at the bottom, always visible, with the
  *Test ports* button.
- **Add** from a single form: magnet, URL to a `.torrent` or uploaded local file,
  with destination, pause, "top of queue", sequential download,
  first/last part and super-seeding; seed and speed limits
  are set per torrent in the *General* detail.
- **Table** with two rows per torrent (name and folder; progress, status,
  size, speed, peers, seeds, share ratio, ETA), with search/filter by name, **filter by status** in the side
  bar, sortable columns, multi-selection and group actions
  (pause/resume/verify/reannounce/top/remove).
- **Per-torrent detail** in tabs: *General* (data, pieces, space,
  dates, copy magnet, export `.torrent`, pin, move, speed/connection/upload and seed
  limits, **web seeds**), *Files* (download/skip and
  **HTTP streaming** with ▶), *Peers* (connection flags, transport, encryption),
  *Trackers* (status, swarm, add and **remove**), *Pieces* (coloured map).
- **IP filter** from a URL or file; the field is pre-filled with the filter
  configured in Gextto.
- **Test ports**: from the status bar a button checks that the peer port
  is listening and that the router forwards it (the same check as Gextto's
  *Test ports* button, on top of `GET /api/v1/portcheck`). With DHT active the
  result also shows the **reflected port** that the DHT nodes see for us
  (`reflected_port` field, from the `ip` field of BEP 42) and the deduced **NAT
  type** (`nat_mapping`: endpoint-independent if the observers agree on the port,
  address/port-dependent if not): if the reflected port matches the local one
  the NAT preserves the port, otherwise it rewrites it.
- ***Gextto log* tab**: the last lines of `gextto.log` (200–2000), read
  only when you open the tab or press *Reload*, with text filter, DEBUG
  hidden by default and warnings/errors coloured. While it is open the torrent
  table does not refresh.
- **Large lists** (standalone only): the table renders at most `?rows=` torrents
  (default 300) and shows *Showing X of Y · Show more* to widen the
  window; in managed it always renders all torrents, as before.
- Shortcuts: `/` to search, `Esc` to close the detail.

The definitive command remains Gextto; the page is a convenience for
the operator.

### Network exposure

**By default page and API stay only on this server**
(`gxtorrent_listen = 127.0.0.1:8890`). To open them to the whole LAN set
`gxtorrent_listen = 0.0.0.0:8890` **and** a `gxtorrent_token`.

- The port of `gxtorrent_listen` is **aligned** to that of
  `gxtorrent_url`: Gextto must be able to reach the daemon, so a different port
  has no effect (it is reported in the log).
- A non-loopback listen requires `gxtorrent_token`: without a token Gextto starts
  the daemon in `-insecure` (consistent with Gextto's trusted-LAN default) and
  notes it in the log at debug level.
- With `gxtorrent_token`, the page asks for it on first access (it also accepts
  `?token=…`) and remembers it in a cookie; the API stays protected as before.

## Standalone use

```
gx-torrent [-listen 0.0.0.0:8890] [-data ~/.local/share/gx-torrent]
           [-download-dir DIR] [-token SECRET] [-allowed-roots /srv/media,/data]
           [-peer-ports 6881-6891] [-listen-interface IP|iface]
           [-outgoing-interface wg0] [-proxy socks5://host:port]
           [-encryption 0|1|2] [-no-dht] [-no-pex] [-no-utp] [-no-holepunch]
           [-no-lsd] [-no-upnp] [-no-natpmp] [-dht-bootstrap host:port,...]
           [-ipfilter FILE] [-ipfilter-trackers=true] [-lang it]
           [-mode managed|standalone] [-log-file FILE] [-insecure]
           [-debug] [-version]
```

`-mode` chooses how the daemon runs. Without the flag: **managed** if there is
`-fingerprint` (Gextto passes it), **standalone** otherwise. In managed the flags
passed by Gextto always take precedence and the `settings.json` file in the
data folder is **not** read (so the fingerprint stays stable); in standalone
`settings.json` provides the values that Gextto does not pass — language, `listen`,
`download-dir`, `peer-ports`, `auth-user`/`auth-password`, `local-bypass`,
`indexers` — and an explicit flag still wins.

**Listen address.** With no `listen` in `settings.json` and no `-listen` flag,
managed listens on `127.0.0.1:8890` and standalone on `0.0.0.0:8890`: standalone
is a client to drive from another machine (its web UI, *arr apps, mobile apps),
so it is reachable from the LAN by default, like `qbittorrent-nox`. Set
`-listen 127.0.0.1:8890` (or a specific interface IP) to keep it on one host
only; the packaged systemd unit already passes `-listen 0.0.0.0:8890`.

**Startup address.** At startup the daemon prints the address to open, not only
the wildcard it binds: `web UI: http://<lan-ip>:8890/` (the interface that
reaches the default gateway, so it works from another device) followed by
`web UI: http://127.0.0.1:8890/`; with a pinned `-listen` only that address is
listed. When the wizard is still to run, an additional `first run: open …`
line points at `/ui/setup`. A router that refuses the port mapping
(UPnP/PCP/NAT-PMP) is reported on a single line, e.g.
`router port 6881 not opened automatically (…): forward it by hand for incoming
peers`.

In standalone the page's POSTs (`/ui/*`) are accepted only if
`Origin`/`Referer` matches the request host (CSRF protection) and the request
host (`Host`) must be an IP or `localhost`, never a name that resolves
to this machine (DNS rebinding protection). The same CSRF protection applies to
the **POSTs of `/api/v2`** (which do not use a token): a client that does not send
`Origin`/`Referer` is still allowed. In managed the page behaves as before (the
v1 API POSTs stay protected by the token).

**Save paths.** In standalone **there is no path restriction**:
a `destination` (or a category save path, or `setLocation`) can be
any absolute path, as in qBittorrent, so the tool stays
fully usable with any folder. The optional flag
`-allowed-roots /srv/media,/data` restricts destinations to those folders, for
those who want to close the API to controlled paths; it is **not set by default**,
precisely so as not to cut the user out. The basic defence remains the login (and, on
Windows, the service runs as a dedicated account and not SYSTEM, so writing
is still confined to what that account can write).

**Platforms.** The daemon is pure Go and also runs on **Windows** (amd64 and
arm64): `GOOS=windows CGO_ENABLED=0 go build ./cmd/gx-torrent/`. System
reads use the Win32 APIs, so the cache and diagnostics stay complete:
memory (`GlobalMemoryStatusEx`) to size the cache, free space
(`GetDiskFreeSpaceEx`), network filesystem (`GetDriveType`), disk
class HDD/SSD (seek penalty via `IOCTL_STORAGE_QUERY_PROPERTY`) and default gateway
for UPnP/NAT-PMP (`GetBestRoute`, i.e. the route Windows actually uses even
with a VPN and LAN together, with a fallback to the `0.0.0.0` route at the lowest metric). The
default data folder is
`%LOCALAPPDATA%\gx-torrent`. The **library link** of each torrent
(`DataDir/<id>`) is a **junction** on Windows, not a symlink: the junction does not
require the `SeCreateSymbolicLinkPrivilege` privilege (nor Developer
Mode). `os.Readlink` reads it as a symlink, but since Go 1.23 `os.Lstat`
reports it as `ModeIrregular`: the daemon recognises it with `isDirLink`
(`link_*.go`). Removing the junction never touches the data it points to; to
re-point it after a move the new junction is built complete under a
temporary name and then swapped, so a concurrent read never sees the link
missing. A junction can however point only to a **local volume**: with a
download folder on a **network share** (`\\nas\media`, or a mapped drive
`DRIVE_REMOTE`) the daemon uses a **symlink** (the only link that can point to a
share) and, if the symlink privilege is missing, returns a clear error that
names the network folder instead of making the torrent addition fail. A
**service** runs in session 0 and does not see the user's mapped drives: for
network folders use a **UNC** path (`\\server\share\...`), not the drive
letter. On Unix the atomic symlink remains. Migration between volumes also recognises the Windows
error `ERROR_NOT_SAME_DEVICE`, so the copy fallback works as on Unix; without
the symlink privilege, a symlink of the payload pointing to a file is copied
as a file instead of making the move fail (on Unix it remains a symlink).
Only two I/O **optimisations** remain without a native equivalent — the
readahead (`fadvise`) and preallocation with `fallocate` — which use the
fallback (`Truncate`): they work, without the optimisation.

**Installation on Windows.** The daemon recognises that it is started by the
Service Control Manager (`svc.IsWindowsService`, `service_windows.go`) and runs
as a **native service**: a single command starts the HTTP server and the torrents, and
stopping the service stops them cleanly. The service runs as the **virtual
account** `NT SERVICE\gx-torrent` (not `LocalSystem`): it is created by the Service
Control Manager and has no rights beyond those granted by the installer — the
program folder read/execute and `%ProgramData%\gx-torrent` write. A **download
folder** chosen elsewhere must be granted separately
(`icacls D:\media /grant "NT SERVICE\gx-torrent:(OI)(CI)M"`), otherwise the
daemon cannot write. Running under a dedicated account (instead of SYSTEM)
also limits the writing of arbitrary files that an API client can
reach by setting `destination`. The package
`scripts/package-gx-torrent-windows.sh` produces
`gx-torrent-windows-<arch>.zip` with `gx-torrent.exe`, `install-service.ps1`
(`New-Service` + virtual account + `icacls`, run in elevated PowerShell),
`uninstall-service.ps1` and `README.txt` with the `sc create` alternative. Data and logs
in `%ProgramData%\gx-torrent`; the service runs in **session 0**, so it does not
open the browser: the guided installation remains the web wizard on `/ui/setup`.
Installer and systemd unit remain for Linux. The Linux package
`scripts/package-gx-torrent.sh` produces
`gx-torrent-linux-<arch>.tar.gz` with the binary, the unit, `run.sh` and **two
scripts**: `sudo ./install-service.sh` creates the system user `gx-torrent`,
installs in `/opt/gx-torrent` and enables the service; `sudo
./uninstall-service.sh` removes it leaving the data. The unit uses
`StateDirectory=gx-torrent`, so systemd creates and owns
`/var/lib/gx-torrent` before startup (the installer must not create it).

In short, on the Windows PC:

```powershell
# PowerShell with administrator privileges, in the folder of the extracted zip
.\install-service.ps1     # registers and starts the service (automatic startup)
Stop-Service gx-torrent    # stops
.\uninstall-service.ps1   # removes the service (data remains)
```

The zip is published in every release (`gx-torrent-windows-amd64.zip`,
`gx-torrent-windows-arm64.zip`, with `.sha256` checksum). The CI verifies it on
`windows-latest` at two levels: the `windows-platform` job runs the platform tests
(junction, memory, gateway, setup, cross-volume move, daemon lifecycle
`TestDaemonStartServeStop`); the `windows-service` job, non-blocking,
actually installs the service with the same `binPath` as the package,
starts it, waits for `/api/v1/health` and verifies its clean stop
(`scripts/gx-torrent-service-selftest.ps1`, reusable by hand too).

In standalone, if `settings.json` contains `auth-password` (bcrypt hash with the
`bcrypt:` prefix), the page asks for login (`/ui/login`, session in a
`gx_session` cookie) except from LAN sources when `local-bypass` is active
(default). Without a password the page stays open, so the first
configuration is reachable — and, since standalone listens on the LAN by
default, so is the wizard from another PC on the same network: set a password in
the wizard (or in *Settings*) if the LAN is not trusted. Other keys: `auth-user` (default `admin`),
`local-bypass` (`true`/`false`). The password also protects `/api/v1` and the
qBittorrent API `/api/v2` (same criteria as the page: no password, LAN bypass,
or `gx_session`/`SID` cookie)
and all state-changing requests on `/api/` are rejected if cross-site
(`Origin`/`Referer`). After 5 failed login attempts from the same address,
the IP is blocked for 15 minutes.

### Reverse proxy and allowed hosts

With a reverse proxy in front of the daemon, every request arrives from
`127.0.0.1` and, if `local-bypass` is active, would be considered LAN (that is,
without a password). Two keys of `settings.json` handle the case:

- `trusted-proxies`: list (comma or newline separated) of **trusted** addresses or
  CIDR networks (`127.0.0.1`, `10.0.0.0/8`). Only from these addresses is
  `X-Forwarded-For`/`X-Real-IP` believed to establish the real client; a
  request that arrives from an undeclared proxy with these headers is **not**
  considered LAN (so it requires login if there is a password). The path
  used is the last untrusted address of the chain.
- `allowed-hosts`: list of host names accepted in the `Host` header, besides
  literal IPs and `localhost`. It is needed when the proxy preserves the original name
  (`proxy_set_header Host $host`) or access is via a name like `nas.local`.
  Without this key a proxy that leaves `Host` as a non-IP name receives
  `403 host not allowed` (anti DNS-rebinding protection).

The proxy must pass the client address:
`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` (nginx) or
Caddy's equivalent.

At the first startup in standalone, until the setup is completed, the page
redirects to the **wizard** (`/ui/setup`): language, download folder and temporary
folder (with a **folder picker**: `GET /ui/browse-dir` lists the
server's directories and a *Browse* button lets you choose them, without typing the
path), peer port with **reachability test** (`/api/v1/portcheck`:
outcome, UPnP/NAT-PMP method, external IP), global **bandwidth limits**
(download/upload in KiB/s, 0 = unlimited), user and password, "LAN without
password" and optional indexers. The two folders are **pre-filled** with two
subfolders of the directory containing the daemon binary (the main folder
takes its name from the chosen language — `download` in English, `scarico`
in Italian, `telechargements`, `descargas`, `pobrane`; the temporary one is
always `download-temp`): they are only proposals, acceptable, editable or
clearable, so that an archive installation keeps the downloads next to itself. The
**Indexer** field is a drop-down of the already supported products (Prowlarr,
Jackett, MIRCrew, others): choosing Prowlarr or
Jackett pre-fills the URL with their usual local address and port
(`127.0.0.1:9696` / `127.0.0.1:9117`), still editable. The wizard writes
`settings.json` and marks the setup as completed; language and bandwidth limits apply
immediately (if the limits are not applied, the error is shown on the page and the
setup is not marked as completed), folder and port at the next startup (the
`peer-ports` key is read when the flag is not passed).

The daemon opens the browser on the wizard by itself, **after** the server is
listening and only if there is a graphical session: `rundll32` on Windows (skipped in
session 0, i.e. services and tasks without a logged-in user), `xdg-open` on Unix
with `gio`/`sensible-browser` fallback (only with `DISPLAY`, `WAYLAND_DISPLAY` or
`XDG_SESSION_TYPE`). In other cases the log still prints the URL to open.

In standalone the daemon also exposes a **qBittorrent-compatible API**
(`/api/v2`) for Sonarr, Radarr and mobile apps: `auth/login` and `auth/logout`
(`SID` cookie, the same login password), `app/version`,
`app/webapiVersion`, `app/preferences|setPreferences`, `transfer/info` and
`torrents/info|add|delete|pause|resume|recheck`, plus categories and tags
(`torrents/categories`, `createCategory`, `removeCategories`, `setCategory`,
`torrents/tags`, `createTags`, `deleteTags`, `addTags`, `removeTags`; category
and tag also travel on `torrents/add`), `torrents/properties|files|trackers`
and `torrents/reannounce|setLocation` (plus the 5.x aliases `stop`/`start`),
`torrents/toggleSequentialDownload|setSuperSeeding|addTrackers|removeTrackers|editTracker|filePrio|export|setForceStart|setAutoManagement|setDownloadLimit|setUploadLimit|setShareLimits`,
`transfer/downloadLimit|uploadLimit|setDownloadLimit|setUploadLimit`, `sync/maindata` (with incremental `rid`) and
`sync/torrentPeers` (which reports `rid: 0`), and `content_path` in
`torrents/info`, as Sonarr and Radarr read). The API uses the same access
criteria as the page: without a password it is open, otherwise the LAN bypass
is needed (if `local-bypass` is active) or a session (`SID` qBittorrent or `gx_session`
of the page). In managed it is **not** exposed: only Gextto commands.
Compatibility is verified by tests using Gextto's qBittorrent client
(`internal/qbittorrent`) against the daemon.

### Container

The standalone daemon can be run in a container: the `Dockerfile` in the root
compiles `gx-torrent` (pure Go) and packages it in `debian:12-slim` with the CA
certificates, in **standalone mode**, data in `/data`, UI on `0.0.0.0:8080`
and peer port `6881` (TCP+UDP); the process runs as an unprivileged user.

```sh
docker build -t gx-torrent .
docker run -d --name gx-torrent \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v gx-torrent-data:/data gx-torrent
```

On first open (`http://HOST:8080/`) the page redirects to the wizard
(`/ui/setup`).

In standalone the page also has a **Categories** filter in the side bar and,
in the *General* detail, the **Category** and **Tag** fields (writing a new
category creates it). In managed these controls do not appear and the endpoint
`/ui/category` rejects the request.

The **indexer search** (standalone) uses the `indexers` key of
`settings.json`, a JSON array of Torznab endpoints
(`[{"name":"Jackett","url":"http://host:9117","apikey":"…"}]`, so Jackett and
Prowlarr): `GET /ui/search?q=…` queries all of them, merges the results and sorts them
by seed. With at least one indexer configured the **search box** appears on the page,
with the results and an *Add* button to add the torrent. In
managed it is not available. **Jackett, Prowlarr and MIRCrew** are configured
in the same way, with the **base address** (an address ending in `/api` is also
accepted): Jackett gets its aggregate Torznab path
(`/api/v2.0/indexers/all/results/torznab/api`), a direct Torznab service such as
the `mircrew-indexer` gets `<base>/api` (for MIRCrew the port is chosen by hand, it
has no default), and **Prowlarr** (`:9696` or `prowlarr` in the URL) is queried
through its aggregate search `/api/v1/search` (`query`/`type`/`limit=100`), which
answers JSON: `internal/torznab` reads it (usenet results skipped, magnet built
from the infohash when missing) and the connection test uses
`/api/v1/system/status`. Gextto applies the same rule (`torznab_endpoint` in
`rss.go`), so the address to type is identical in both. The Torznab XML is read
**leniently**: a raw `&` or an HTML entity in a scraped title or URL no longer
makes the whole answer unreadable. An indexer can put the **magnet in the link or
in the enclosure** instead of in a `magneturl` attribute (this is MIRCrew's case):
it is recognised and added as a magnet, without trying to download it via HTTP.
An indexer that responds with an **error document** (e.g. invalid API key:
HTTP 200 with `<error code="100" description="…">`, as Jackett does)
is reported as an **error**, not mistaken for an empty feed: a wrong key no
longer passes silently.
Automatic searches in a search feed wait for each indexer **up to 45
seconds**, because MIRCrew may search the forum before responding.

**RSS** (standalone) uses the `feeds` key: a JSON array of feeds with the
rules, for example
`[{"name":"Serie","url":"http://…rss","folder":"serie","include":"1080p,ita","exclude":"cam","category":"tv","savepath":"/srv/tv"}]`.
Each feed can have an optional `folder` (group) to organise it in the
feeds box, which groups them.
A worker (every `feed-interval-secs`, default 15 minutes, editable from the
RSS page in minutes) downloads every active feed, applies `include`/`exclude` on the
title and adds magnets and `.torrent`s not seen yet (the list of already added
items is persisted, so a restart does not re-propose them). `disabled: true`
suspends a feed's automatic checks without deleting it (qBittorrent's *disable feed*;
the row's ↻ button still works). Every item read also ends up in the **feed
archive** (below). On the page appear a *Feeds* box with *Check feeds* and
`GET /ui/feeds`/`POST /ui/feeds/poll` for the status and an immediate check;
`POST /ui/feeds/poll` with `feed=<name>` checks
**a single feed synchronously** and responds with its status (`items`, `new` =
items never seen before, `added`, `last_error`, `last_check`), so the row
refreshes with the outcome; if a round is already in progress it responds 409. In managed it
is not available.

A feed can also be the **HTML page of a listing** instead of an RSS, as
in Gextto: an uploader or search page of **ext.to** (direct magnet links:
title from the text or from `dn`, dedup by info-hash) or of **TorrentGalaxy**
(`/get-posts/user:NAME/`: the rows point to `/post-detail/` pages read with 3
parallel requests and an in-memory cache, so a round costs one request
per new release). A page that advertises a feed (`<link rel="alternate"
type="application/rss+xml">`) is followed; an HTML page without torrent links
gives a clear error (*HTML page without torrent links, not an RSS feed*) instead
of the old `XML syntax error`. Requests use a browser User-Agent
(`internal/rss/listing.go`).

A feed can also be a **search**: instead of `url` a `search` (a
query) is used and the worker queries the configured Torznab **indexers**, treating the
results as feed items (same rules, dedup and actions). Example:
`[{"name":"cerca-serie","search":"Serie S01","include":"1080p"}]`.

With the `rules` key (JSON array) you switch to the **ordered rule set**, like
BiglyBT: the first rule that matches decides (`fail: true` discards, otherwise
it adds). Each rule has `name`, `feeds` (to limit it to some feeds; empty =
all), `fail`, `match` and `action`:

- `match`: `include`/`exclude` (substrings), `regex`/`not_regex`,
  `min_size`/`max_size` (bytes), `min_seeders`/`max_seeders`, `min_peers`,
  `max_age_days`, `require_episode`, `episode_filter` and `smart_episode`
  (adds an episode of a series only once, keeping track
  of the last one: like qBittorrent's smart episode filter; a **REPACK** or
  **PROPER** of an already taken episode is accepted only once);
- `ignore_days` (0 = off): real-time **cooldown** — after a match the rule
  no longer matches for N days, like qBittorrent's *ignore days* ("ignore
  subsequent matches for N days"); it does not depend on the publication date of the
  items, so an episode released two days after the previous one is not lost;
- every rule is active by default; `enabled: false` disables it without
  deleting it (the editor shows a *Disabled* label and an
  *Enable/Disable* button). A rule covers the feeds listed in `feeds` (empty =
  all); a feed **without any rule covering it** uses its own
  `include`/`exclude`, so adding a rule for a feed does not stop the
  others. Even a **disabled** rule covers its feeds: turning off the only
  rule of a feed (or an *once* rule that turns itself off) stops the downloads
  instead of falling back to the feed filter. A feed **without Include/Exclude and
  without rules** downloads nothing: it only grows the **archive** (its row
  reports it with *archive only*, and the log notes it once, when the feed is
  first polled — not at every round), unless
  the operator turns on the **Take all** flag. Adding a feed, then, does not
  mean pulling down what it contains: it means that material is there, and
  what to download is decided by the rules and the **Wanted** list.
- `action`: `savepath`, `category`, `tags`, `paused`, `sequential`, `first_last`,
  `top`, `once` (disables the rule after the first download; it is used by *Follow a
  title* for movies).

The **episode filter** `episode_filter` uses qBittorrent's syntax: `1x01`
(single), `1x01-05` (range within the season), `1x01-1x05` (spanning two
seasons), `1x01-` (open), `1x??`/`1x*` (whole season), separated by commas
or semicolons (`1x01, 1x03, 2x01-`); the `S01E01` forms are also accepted,
the range with tail `S01E01-E05` and the season alone `S01`/`s1`. The filter applies only
to titles with a recognisable episode; an invalid filter never matches
(the editor rejects it before saving).

Example:
`[{"name":"no-cam","fail":true,"match":{"regex":"(?i)cam"}},{"name":"shows","match":{"require_episode":true,"smart_episode":true},"action":{"category":"tv","savepath":"/srv/tv"}}]`.

The **`/ui/wanted`** page (tab *Wanted* in the navigation bar) is dedicated to managing
the titles the operator wants. Each title has type (series/movie),
seasons (`1+`, `2-4`, `1`), language, quality (selectable from a drop-down
among *Any*, *360p*, *480p*, *576p*, *720p*, *720p+*, *720p-1080p*, *1080p*,
*1080p+*, *2160p 4K*, *2160p+ 4K+*), excluded, episode filter, category
and path. You add it by hand, or you type a name and press **Search in
feeds**: the indexers and the already configured RSS feeds are queried and the page
proposes the **names found** (filtered by the language/quality already entered and by the
searched words: feeds deliver all their current content, not a
search, so their names unrelated to the query do not appear), to be
chosen with a click. The **title is required** (regex tolerant of separators,
so fuzzy results on common words do not enter) and language/quality are in
**AND**. The result also says **where the names come from**
(`Indexer: N · Feed: M · Archive: K`), or *No indexer configured* or the indexer's
error: so you can
immediately tell an absent or unresponsive indexer from the results of the
feeds alone.

At the top of the *Wanted* page, the **Search all wanted** button triggers an immediate
manual search across indexers, RSS feeds and the archive for all configured titles and
gap searches missing episodes, displaying real-time feedback with items added and status.
Each row in the table provides individual **Search**, **Edit**, **Pause/Resume** and
**Remove** actions, missing episodes ledger and real-time status.

The daemon, at every cycle, searches every title in the **indexers**, in the **RSS
feeds** and in the **archive** (where feeds deposit everything they find, even what
is no longer in the feed; indexers suffice if there are no feeds) and takes
the episodes that appear; it keeps a **record** (in the state file) of what has
appeared and what has been downloaded, shows the **missing** ones in the table and
**searches again** (gap search, with query `Title SxxEyy`) until it finds them.
For a **movie** it takes one release and does not look for others. The list does not create feeds or
rules: it is the engine that queries indexers and feeds for the title, so language and
quality apply in AND and feeds remain free for advanced rules.

The **`/ui/rss`** page (tab *RSS & indexers*) is dedicated to feeds, indexers and rule
definitions without cluttering the view with wanted titles. The **Check feeds** button
runs a complete check and provides immediate visual feedback (`✓ N feeds checked: X new · Y added`
or error indicators) rather than running silently in the background. Below: lists with
buttons to add, **edit** (the form is filled with the row's values; an empty API key keeps it)
**test** (Torznab request `t=caps`, outcome next to the row) and remove **indexers** (drop-down
Prowlarr/Jackett/Other, pre-filled URL; the name is a **link to the indexer's web page**, i.e.
scheme and host of the Torznab URL, opened in a new tab).
The **feeds** have a table with status (*active*/*disabled*, *adds paused*), **last check**
with items, new and added or the error, and per row the buttons **↻ Reload** (to the left of
*Remove*: reads that feed immediately and refreshes the row), **Edit** (the form is filled,
including the *Feed options*: folder, include/exclude, category, path, pause, disabled),
**Enable/Disable** and **Remove**. An **RSS options** box sets
the refresh interval (minutes) and the archive retention (days)
and shows the time of the next automatic check. The **rule editor** has
one rule per row with **Edit** (loads the rule into the form; saving keeps it in
its place in the order), **Enable/Disable**, arrows to change its order — the
first that matches wins — and *Remove*; the form also has *Download once*
(`once`) and **Match preview**: it tries the form's rule, saved or not,
on the last 5,000 items of the archive and shows those it would take (qBittorrent's
"matching articles" view, instantaneous and without network requests).
Regexes and numeric filters are under *Advanced filters*. Then follow the
advanced JSON textareas for feeds, rules and indexers (saved with validation) and,
for each feed, a button that immediately reads the current items with the rule
that would take them and a manual *Add*.
All controls, buttons and input fields of the standalone UI
have descriptive, localised tooltips. In the first-startup wizard
(`/ui/setup`), the *Test port* button checks the port typed in the field
(via `/api/v1/portcheck?port=...`).

The same results are obtained via
`POST /ui/rss-indexer`, `POST /ui/rss-feed` (op `add` — with `index` it edits in
place —, `toggle`, `remove`), `POST /ui/rss-rule` (op `add` — with `index` it edits
—, `toggle`, `remove`, `up`, `down`), `POST /ui/rss-rule-preview` (same fields
as the form, responds `{matches, scanned}`), `POST /ui/rss-settings` (`interval` in
minutes, `archive_days`) and `POST /ui/wanted`; the JSON textareas remain on
`POST /ui/rss-config`.

### Feed archive

Every item returned by a feed (RSS, HTML listing or search) is
kept in **`<data>/feed-archive.db`** (bbolt, already used for the resume: no
extra dependency), **deduplicated** by info-hash (or by `.torrent` link), with
title, link, size, seed/leech (updated when the feed re-proposes them),
origin feed, first and last appearance date and the *downloaded* flag (set
when a rule, the feed filter or the user adds it). The **`/ui/archive`** page
(*Archive* entry in the bar) searches it like Gextto's Archive:
words (all present, ignoring case and separators `. _ -`),
**phrases in quotes**, **`-words` excluded**, filter by feed and *downloaded
only*, sort by date, seed, size or title, pagination from 50,
resolution and episode badges, *in the session*/*downloaded*, and per row
*Add*, *Copy magnet* and delete; the *Cleanup* section deletes the results
of the current search (never the whole archive with an empty search) or the
items older than N days.

A **Search in indexers too** checkbox makes the configured Torznab **indexers**
(Jackett, Prowlarr) be queried *live* with the same query
(`GET /ui/search`, which reports in `X-Gx-Indexers` how many are configured) and
shows the results in a separate section with indexer and publication date and
an *Add* button: so also what the feeds have not yet
returned appears, i.e. the case where an indexer has the release but the feed's
RSS does not.

The automatic retention is `archive-days` (default 180, `0` = keep
everything), with a cap of 200,000 items; the pruning runs at the end of every complete
feed round and starts from the oldest items stopping at the first one to
keep, so it costs almost nothing. The database has a **compact index** (title
already normalised, feed, size, seed, flags) next to the JSON of the items:
the search scans only the index and decodes the JSON of only the rows of the
page — on 100,000 items **~7 ms and ~44 KB per search** (see
`docs/evoluzione.md`). The index rebuilds itself on opening if missing.

Endpoints: `GET /ui/archive/items?q=&feed=&added=1&sort=new|old|seeds|size|title&page=N`
(JSON with `items`, `total`, `page`, `pages`, `entries`, `size_bytes`),
`POST /ui/archive/add` (`id`), `POST /ui/archive/delete` (`id` repeatable, or
`all=1` with `q`/`feed`/`added`, or `days=N`).

The **bandwidth scheduler** (standalone) applies alternative global limits in a
daily time window, like qBittorrent: with `schedule-enabled` active and
`schedule-start`/`schedule-end` (`HH:MM`, also across midnight) the
daemon uses `schedule-download`/`schedule-upload` (KiB/s) inside the window and the
normal limits outside. The limits change on the fly, without reopening the session.
It is configured from the **`/ui/settings`** page (*Settings* link in the
bar of every page), which in **standalone** is the client's complete panel:
**Folders** (download, temporary), **Network** (listen address, peer port
range, incoming/outgoing interface, proxy, token), **IP filter** (below),
**Protocols** (encryption, DHT/PEX/uTP/holepunch/LSD/UPnP/NAT-PMP, bootstrap nodes,
DHT read-only), **Queue and cache** (active downloads/seeds, active limit, disk
cache, preallocation, sequential), **Optimisation** (below), **bandwidth** and
**scheduler**, **language** and **notifications** (`notify-url`). Bandwidth, scheduler and
queue limits apply immediately; folders, network and protocols apply **from
the next startup**. The **DHT read-only (BEP 43)** stays off by default: a
read-only node does not respond to queries and asks not to be entered into
others' tables, useful for nodes with little bandwidth or behind symmetric NAT,
but a complete client that uses the DHT must also serve it (it is also the choice of
libtorrent and qBittorrent).

The **IP filter** (`ipfilter`) is a local file or a **URL** (P2P, eMule or
CIDR format, plain or gzip). A URL is downloaded to `<data>/ipfilter.dat` at startup
(the session immediately uses the copy downloaded last time, if any) and again
every `ipfilter-refresh-hours` (default 24, `0` = never); a local file is
reloaded when it changes on disk (size or mtime, checked every 10
minutes). The box shows loaded rules, time and path of the load,
next refresh and any error; **Load now** immediately applies the
value of the field (even unsaved) without saving the rest of the page
(`POST /ui/ipfilter` with `return=settings`). Before this change a URL
written in the settings was not loaded at startup.

**Optimisation** is the equivalent of Gextto's *Optimise* button, divided
into three actions (`POST /ui/optimize` with `optimize=queue|cache|bandwidth`), each
of which changes only its own settings: **queue** = Gextto's base (3
downloads, 3 seeds, 5 active, dynamic queue between 1 and 10 slots, slow torrents not
counted, self-management active); **cache** = automatic again (sized
on RAM and disk type, TTL 300 s); **bandwidth** = unlimited download and upload
at **80% of the upload peak measured since startup** (a saturated upstream delays
the download ACKs), with auto-speed on latency active and a minimum of a tenth
of the limit. If not enough upload has been measured yet (less than 50 KiB/s)
the action refuses with an explanation; if the upload was already at the current limit the
line capacity is unknown: the limit stays and only auto-speed is activated.
The box shows the measured ↓/↑ peaks.

On the **Torrents** page in standalone the *cache r/w* and *I/O
ops r/w* tiles do not appear (diagnostics useful to Gextto, not to the client's
user) and the **peer port** tile has a **green** dot when the port is reachable —
UPnP/NAT-PMP/PCP mapping active or incoming connections already received — and **red**
otherwise. In **standalone `settings.json` is authoritative**: a value written here wins over
the startup flag (in *managed* the file is not read and Gextto's flags remain
sovereign).

Most flags have their own environment variable `GX_TORRENT_*`, for
example:

- `GX_TORRENT_LISTEN`, `GX_TORRENT_DATA`, `GX_TORRENT_DOWNLOAD_DIR`;
- `GX_TORRENT_TOKEN`, `GX_TORRENT_ALLOWED_ROOTS`;
- `GX_TORRENT_PEER_PORTS`, `GX_TORRENT_LISTEN_INTERFACE`,
  `GX_TORRENT_OUTGOING_INTERFACE`;
- `GX_TORRENT_PROXY`, `GX_TORRENT_ENCRYPTION`;
- `GX_TORRENT_NO_DHT`, `GX_TORRENT_NO_PEX`, `GX_TORRENT_NO_UTP`,
  `GX_TORRENT_NO_HOLEPUNCH`, `GX_TORRENT_NO_LSD`, `GX_TORRENT_NO_UPNP`,
  `GX_TORRENT_NO_NATPMP`, `GX_TORRENT_DHT_BOOTSTRAP`;
- `GX_TORRENT_IPFILTER`, `GX_TORRENT_IPFILTER_TRACKERS`;
- `GX_TORRENT_WEB_SOURCES_ALLOW_PRIVATE` (flag `-web-sources-allow-private`,
  key `web-sources-allow-private`, checkbox in *Settings → Network*): allows
  web seeds and magnet sources `xs=`/`as=` to reach private LAN addresses.
  Off by default, also in managed mode: those URLs come from the torrent's
  data (even from an untrusted feed) and without this limit they could make
  the client contact the router, the NAS or other internal services (SSRF). See
  `docs/bep.md`, *Revision of 2026-10-12*;
- `GX_TORRENT_LANG`, `GX_TORRENT_MODE`, `GX_TORRENT_LOG_FILE`, `GX_TORRENT_DEBUG`,
  `GX_TORRENT_NOTIFY_URL` (webhook for feed notifications);
- `GX_TORRENT_MMAP` (opt-in mmap storage, Linux: flag `-mmap`).

Some flags are internal (`-insecure`, `-version`, `-fingerprint`,
`-orphan-timeout`, `-ipfilter-source`, `-gextto-log`): only Gextto uses them to
start, re-attach and supervise the daemon.

Gextto, in managed mode, passes these values itself from the *libtorrent*
settings (ports, interfaces, encryption, DHT, PEX, uTP, LSD, UPnP, NAT-PMP,
IP filter, DHT bootstrap nodes) and from the `gxtorrent_proxy` setting. The global
connection limit (`libtorrent_connections_limit`) is split between the engine's
outgoing dials and incoming accepts. Proxy and token travel in the environment,
not on the command line. Changes apply from the restart of gextto.

In managed mode the daemon **has no path restrictions**: Gextto decides
the destinations, validates them and already limits them with its own rules. The flag
`-allowed-roots` exists only for the **standalone use** of the daemon (see below) and
Gextto never sets it.

The daemon **in managed** refuses to listen on a non-loopback address without a
token (unless `-insecure`); **in standalone** it is allowed, because access is
protected by login and LAN rules (or open if there is no password). The engine's internal
RPC server is disabled.

## Network

- **Single port, like libtorrent.**
  - The daemon uses the first free port of the range (`-peer-ports`,
    default 6881-6891): TCP for peers, UDP for the DHT.
  - All torrents share it. The handshake, even encrypted, chooses the
    torrent from the info hash.
  - `-peer-ports 0` returns to the original per-torrent port.
- **Opening on the router.**
  - UPnP (IGD v1 and v2), then **PCP** (RFC 6887, on the default gateway) and finally
    NAT-PMP, tried in this order; mappings are renewed every 20 minutes and
    removed at shutdown. PCP is a minimal MAP client, without external
    dependencies; on a router that does not speak it the request expires and it moves to
    NAT-PMP.
  - Outcome and external address are in `GET /api/v1/stats` (`port_mapping`).
  - If none of the three works, the log says to open the port by hand.
- **Listening interface**: `-listen-interface` accepts an IP or an interface
  name. From gextto it comes from `libtorrent_listen_interfaces`
  (e.g. `wg0:51413`), which takes precedence over `libtorrent_port_min/max`.
- **Outgoing interface (VPN killswitch).** With `-outgoing-interface wg0`
  peers, trackers, web seeds and DHT go out only from that interface. If it has no
  address, nothing goes out until it returns.
- **Proxy.**
  - `socks5://[user:password@]host:port` or `http://host:port`
    (CONNECT tunnel).
  - Peers, HTTP trackers and web seeds go through the proxy; the tracker names
    are resolved by the proxy.
  - DHT and UDP trackers are turned off, because they cannot go through a TCP
    proxy and otherwise would go out.
  - Incoming connections stay direct on the single port.
- **Encryption** (`libtorrent_encryption`):
  - 0: outgoing connections are in the clear;
  - 1: encryption is tried, with a fallback in the clear;
  - 2: mandatory encryption incoming and outgoing.
- **IP filter.**
  - Accepted formats: CIDR, ranges `a.b.c.d-e.f.g.h`, P2P
    (`name:start-end`) and eMule `ipfilter.dat`. eMule rules with level
    ≥ 128 allow traffic; IPv6 rules in CIDR form are applied
    (ranges/P2P/eMule remain IPv4 only).
  - The filter applies to incoming and outgoing peers, and to trackers if
    `libtorrent_apply_ip_filter` is active.
  - gextto's "Update IP filter" button also works with gx-torrent:
    it downloads the list if it is a URL and reloads it in the daemon.
  - The file is **updated at the startup of the service** and then **once a
    week** while the service stays active (besides the manual button).
- **DHT** (BEP 5), shared on the same UDP port as the uTP peers.
  - **IPv6 discovery BEP 32** dual-stack: v4 and v6 nodes kept separate
    (`nodes`/`nodes6`, `want: n4/n6`).
  - **Security extension BEP 42**: the node ID is derived from the external IP when it is
    known (the routable listen address from `-listen-interface`, or the IP
    of the outgoing interface), so no one can
    choose one close to a target info-hash; if the IP is not known at startup
    it is adopted from the `ip` field of DHT responses and the new ID is persisted.
    Local network addresses are exempt. The `ip` field is also **sent**
    in every response (ping, find_node, get_peers, announce_peer and the
    BEP 44 responses), so a peer behind NAT discovers its own reflected address;
    `dht_reflected_sent` counts the sends. An `announce_peer` with
    `implied_port: 1` uses the packet's source port instead of `port`.
  - **BEP 44** (key/value archive): immutable items (target = SHA-1 of the
    value) and mutable ones (ed25519 signature, monotonic `seq`, `cas`), with token, size
    limits and TTL 2 h; `get`/`put` queries and local API. There is also the
    **outgoing client**: `GetImmutable`/`GetMutable` read and
    `PublishImmutable`/`PublishMutable` publish to the closest nodes. It is the
    basis of **BEP 46**: a magnet `xs=urn:btpk:<pubkey>` (+ `s=<salt>`)
    resolves the info hash from the mutable item at addition.
  - **BEP 33** (DHT scrape): a `get_peers` with `scrape=1` receives the Bloom
    filters (`BFpe`/`BFsd`) and estimates seed/peer of the swarm from them; the client queries
    the closest nodes and aggregates. In the UI: **DHT swarm** button in the General
    tab (`GET /ui/dhtscrape`), besides `GET /api/v1/torrents/{hash}/dhtscrape`.
  - **BEP 43** (read-only): a node can not respond to queries and ask not
    to be entered into the routing tables (`ro`); it is activated with
    `-dht-read-only` (`GX_TORRENT_DHT_READ_ONLY`).
- **Trackers and BEP 31** (`retry in`).
  - A tracker that responds with `failure reason` and `retry in: N` postpones the
    next announce by N minutes (announce and scrape, HTTP and UDP).
  - `retry in: never` **discards the tracker from the tier**: it is no longer
    contacted, but the other trackers of the same tier continue to be
    used; only if the tier is exhausted does the torrent stop announcing to that
    tier.
- **BEP 38 hints** (`similar`/`collections`).
  - The `.torrent` hints (from the info dictionary and from the top level, merged)
    are shown in the General tab together with the **local** torrents that
    share a hint (data that could be reused), remain on
    `GET /api/v1/torrents/{hash}/collections` and are rewritten in the export of the
    `.torrent`.

## uTP and LSD

- **uTP** (`libtorrent_utp`, active by default) uses the same UDP port as the
  peers, shared with the DHT as in libtorrent.
  - Outgoing, uTP and TCP are attempted together and the first to connect wins.
  - Incoming, the handshake is routed as for TCP.
  - The peer list indicates which ones are connected over uTP.
  - With a proxy uTP is off; with the outgoing interface the UDP socket is
    bound to the VPN's IP.
- **LSD**, local network discovery (BEP 14, `libtorrent_lsd`).
  - Multicast announcements on 239.192.152.143:6771 (IPv4) and, if available,
    `[ff15::efc0:988f]:6771` (IPv6) every 5 minutes for every active non-private
    torrent.
  - LAN peers announcing the same torrents are added.
  - It is off with a proxy or outgoing interface, so as not to announce the torrents
    outside the tunnel.

## Holepunching (BEP 55)

A peer behind NAT that cannot receive incoming connections can still
be reached by going through a peer acting as a **relay** (`ut_holepunch`). It is the
BEP 55 standard, therefore interoperable with µTorrent, BitComet, qBittorrent and
libtorrent; it is not libtorrent's proprietary DHT-node relay.

- It is activated with the **Holepunching (BEP 55)** setting in *Configuration →
  Torrent engine → Peer and tracker discovery* (active by default). It requires uTP
  (`libtorrent_utp`): without uTP (or with a proxy) it is off, because the "hole" is
  opened on the UDP mappings. On the daemon the flag is `-no-holepunch`
  (`GX_TORRENT_NO_HOLEPUNCH=1`).
- When a direct dial fails, gx-torrent asks a connected peer
  (at most 8) to act as relay for that endpoint, once only; the relay,
  if it is connected to the target and the latter supports the extension, sends a
  `connect` to both and each starts a uTP connection towards the other.
- The target is searched among the connected peers by IP and port; protocol
  errors (`NoSuchPeer`, `NotConnected`, `NoSupport`, `NoSelf`) are
  reported as provided by the BEP.
- Known limit: as for `ut_metadata`/`ut_pex`, the incoming message is
  recognised via the local ID of the extension; a peer that assigns to
  `ut_holepunch` an ID other than 3 is not understood (pre-existing limit
  of the engine's extension architecture).

### Verification

The codec, the relay decision, the configuration forwarding and the source
mapping are covered by the tests (`make test-engine`,
`go test ./cmd/gx-torrent/`, `go test .`). For a **real** test on NAT there is a
root-only harness, `scripts/holepunch-netns-test.sh`:

```bash
make gx-torrent          # builds bin/gx-torrent
sudo scripts/holepunch-netns-test.sh
```

It creates two **distinct** client networks behind a **stateful firewall** (no NAT: the
clients keep peer IP and port), a minimal HTTP tracker and four daemons with
uTP and holepunch active: a public seed and a relay, on different IPs in `gx-lab`,
plus two clients (A behind gx-r1, B behind gx-r2). The firewall lets traffic out, DROPs
new incoming connections and accepts responses: it is the behaviour that
matters for BEP 55 (port-preserving "cone" filter). B enters as leecher,
downloads from the seed and completes while staying connected to the relay: an
**interested leecher** relay is needed for its data, because a seed discards uninterested peers when
it completes. Only then does A enter (leecher): A does not reach B directly, it asks
the relay for the introduction; the relay sends `connect` to both and the two dial over
uTP. Whoever dials first appears with origin **`holepunch`**, the other sees the
connection as `incoming`: the test passes if it appears on either one. The routers
refuse TCP towards the peer ports, so the hole is opened over uTP. `--keep` leaves
the topology active for debugging; the script reports what it observed (holepunch
/ direct / nothing) and, on failures, the `holepunch` lines of the logs (rendezvous
sent/received, `connect` received).

Why firewall and not NAT: Linux netfilter **does not emulate a port-translating
"cone" NAT**. A packet directed at the router's WAN IP (the leecher's direct dial,
before the rendezvous) creates a local conntrack entry that occupies the
peer's external port and prevents the target from creating its own mapping
when it receives the `connect`; with a single shared external IP the hole cannot be
opened. The stateful firewall, without address translation, reproduces the same
observable behaviour (unrequested inbound blocked, hole that opens only
after the crossed dial) and makes the test deterministic. The behaviour is
verified by the `source`/`incoming` criterion on `/api/v1/torrents/<hash>/peers`
(the daemon's web page shows the same origin).

Note: seed and relay must be on different IPs because the engine deduplicates peers by
IP (`connectedPeerIPs`); two daemons on the same IP would be seen as a single
peer. Both must be started with `-outgoing-interface` equal to the listen IP,
otherwise they announce to the tracker their "default" IP (wrong).

## File selection and priorities

You can download only some files of a torrent and give priority to the
others. The scale is that of libtorrent/qBittorrent: **0 = excluded**, **1..4 =
normal**, **5..7 = high** (high-priority files are downloaded first).
It is set from gextto's files tab or with
`POST /api/v1/torrents/{hash}/file-priorities` (`priorities=4,0,7`); the
qBittorrent-compatible API uses its 0/1/6/7 scale.

- Excluded files never appear in the destination folder. Pieces that
  span an included file are written to `DATA/parts/<id>`, like libtorrent's
  partfile.
- Progress, size and completion refer only to the chosen files:
  gextto receives "complete" when those are ready.
- Changing **which** files are excluded stops the torrent for a moment, moves the
  affected files between `parts` and the destination and starts it again; changing **only
  the priority levels** (in the same downloaded files) is instead applied to the
  torrent in progress, without stopping it or moving files.
- The engine computes the priority of each piece as the **maximum** between the files it
  touches: a piece spanning a normal and a high-priority file is high; the
  picker requests it right after the streaming/readahead window and before
  file-edge, allowed-fast and rarest.
- If the **magnet link** carries the `so=` parameter (BEP 53, *Select Only*, e.g.
  `so=0,2,4-7`), the engine immediately downloads only those files and as soon as the
  metadata arrives the daemon records the choice as normal priorities: the files tab
  shows the same excluded files and they can be changed as always.
- If the magnet carries `ws=` (BEP 53, web seed), the web seeds are added to the
  torrent and remain even after a restart.
- If the magnet carries `dht=` (bootstrap DHT node), the node is added to the
  session's DHT.
- If the magnet carries `xs=`/`as=` (exact/acceptable source, Magnet
  URI convention), the engine tries to download the `.torrent` from those URLs and uses it **only
  if the info hash matches**, so the metadata arrives immediately without a swarm;
  otherwise it falls back to downloading the metadata via peer/DHT.
- A **partial seed** (all chosen files complete, torrent not whole) declares itself
  as such to the peers with `upload_only` (BEP 21), so they stop offering it
  pieces it does not want, and reports it to the **trackers** with `event=paused` at every
  announce, so it is distinguished from an active downloader.

## Sequential download (streaming)

With `sequential` the torrent downloads pieces in index order instead of
"rarest-first", and first of all the edges of each file (first and last ~1%,
up to 8 MB): it is the mode designed for streaming, so a player can
start while the download continues. It is slower overall and worsens the
health of the swarm, so it remains **optional and off by default**.

- It is set at the moment of addition: from the web page (*sequential* and
  *first/last* checkboxes), with `sequential=1` / `first_last=1` on
  `POST /api/v1/add`, or from Gextto's `AddOptions.Sequential` /
  `AddOptions.FirstLast` fields (settings *Sequential download*,
  `libtorrent_sequential`, and *First/last part of files*,
  `libtorrent_first_last`, besides the add form checkboxes).
- `first_last` downloads the edges of each file first and then continues
  **rarest-first**: it is independent of the sequential order and useful for
  streaming without giving up the health of the swarm.
- The default value for torrents added afterwards is set with
  `POST /api/v1/config` (`{"sequential":true}`): Gextto does it when it changes
  the setting. **Changing this value on the fly also applies the order to torrents
  already in progress**, like libtorrent. `first_last` has no session
  default in the daemon: with *First/last part of files* active it is Gextto that
  adds `first_last=1` to every new torrent.
- the engine can change the order on the fly also on the single torrent
  (`Torrent.SetSequential`/`SetFirstLast` internally); the state is persisted and
  reported in `GET /api/v1/torrents` (`sequential`, `first_last`).

## BitTorrent v2 torrents

The engine downloads **v1** torrents, the v1 part of **hybrids** (v1+v2) and
**v2-only** torrents added from a `.torrent` file that includes the **`piece
layers`** (BEP 52). Pieces are built **file by file**: a file's tail piece is
shorter than `piece length` and does **not** contain padding; each piece is
verified with a **SHA-256 Merkle node** over the 16 KiB blocks (missing leaves
= zero hash). The torrent identity is the SHA-256 truncated to 20 bytes, so
handshake, DHT and trackers remain identical to v1.

**v2-only** torrents without `piece layers` (the v2 **magnets**, `urn:btmh:`)
are also supported: the info dict arrives via BEP 9 and the per-file **`piece
layers`** are requested from the peers with the `hash request`/`hashes`/`hash reject`
messages (BEP 52), verified against the `pieces root` and persisted in the resume. The
reserved **v2** bit (byte 7, `0x10`) is announced in the handshake only for a torrent
with v2 identity, so libtorrent peers accept the `hash request`. The seed
responds to the **piece layer** requests (`base = piece layer`) from its
`piece layers`, without reading the files. It also responds to the **block
layer** requests (`base = 0`), which a libtorrent leecher uses to verify
blocks while downloading them: the block tree is built from the data of the verified
pieces, **cached per-file** and only for a file **entirely present**
(otherwise `hash reject`, because a tree with missing leaves would not
anchor to the `pieces root`). Full plan in `docs/gx-torrent-v2.md`.

## Local data reuse (Swarm Merging, Phase 1)

If another torrent of the session has the **same content** with an identical layout
(same `piece length`, same length and same hashes at the same
indices), its **already verified** pieces are reused: at allocation the
new torrent reads the bytes from the other's storage, **re-verifies them against
its own hash** and writes them into its own, so it restarts only from the missing pieces and
can complete **even without peers**. It is useful for a reloaded episode or for
different `.torrent`s of the same content, and «resurrects» a torrent without a seeder
when the content is already present elsewhere. Merging is **disabled if one side is
a private torrent** (BEP 27): mixing private swarms violates the tracker
rules. It is independent of the **BEP 38** hints, which serve to *report* the
candidates (details in [`evoluzione.md`](evoluzione.md) §15.2).

## Statistics

gextto's **Health** page has a "Torrent engine" panel, refreshed
every 15 seconds, for all engines.

With gx-torrent it shows:

- version and torrents by status;
- speed and queue slots;
- port and opening on the router (in red if unsuccessful);
- DHT (nodes), uTP (uTP and TCP connections), LSD (peers found);
- incoming connections and disk I/O (jobs in queue, total reads/writes);
- encryption, proxy, IP filter rules;
- disk and cache, session totals.

With libtorrent it shows:

- torrents by status;
- DHT nodes, TCP and uTP peers;
- incoming connections, disk jobs, totals.

The same data are in `GET /api/v1/stats` of the daemon and in
`GET /api/libtorrent/session-stats` of gextto. Shared counters use the
**libtorrent names** also for gx-torrent (`net.recv_payload_bytes`,
`peer.num_tcp_peers`, `disk.num_read_ops`, …), so the endpoint is uniform; the
engine-specific counters (cache, disk speed, DHT traffic) remain
alongside. Compared to libtorrent:

- the **protocol overhead** is available (`protocol_overhead_bytes`: bytes
  exchanged on the peer connections minus the payload, encryption included);
- disk job times and failed pieces per peer are **missing**: the engine does not
  measure them and they are not implemented.

The *Test ports* button (in Gextto and on the daemon page) does not read
`/api/v1/stats`: it queries `GET /api/v1/portcheck`, which responds whether the peer port
is listening and whether the router forwards it.

## Piece diagnostics

`GET /api/v1/torrents/{hash}/pieces` reports the status of every piece as
compact intervals (`begin`/`end` inclusive, `state`): `have` (downloaded),
`downloading` (in progress), `skipped` (excluded file) or `""` (missing). The daemon's
web page has a **Pieces** tab with the coloured map; Gextto shows the
same map in the **Pieces** tab of the torrent detail. gx-torrent is the first
Gextto engine to expose piece diagnostics (libtorrent responds "not
available").

## HTTP streaming (HTTP Range)

Besides sequential download, the daemon can serve a file to a player while
downloading it: `GET /ui/stream?hash=<hash>&file=<index>` returns the file and
supports a single `Range: bytes=…` header (response `206` with
`Content-Range`, `Accept-Ranges: bytes`). The pieces of the requested window, plus
about 16 MB of readahead, are requested first; if a piece has not yet
arrived the daemon waits (up to 2 minutes) instead of serving zeros. Unsatisfiable
requests respond `416`.

Pieces are ordered on **two levels**: the window the player reads now
(`SetFileStreamWindow`) has priority over the **readahead** (`SetFileReadaheadWindow`),
which in turn precedes rarest-first. So the playhead does not stay behind the
prefetch.

Authentication is that of the web page (cookie, header or `?token=`), so
even an external player (VLC, mpv) can use the full token URL. From Gextto
`GET /api/torrents/{hash}/stream?file=<index>` redirects to the daemon adding
the token server-side; in the **Content** tab of the torrent detail every file
has a **▶** button, and the daemon page has it in the **Files** tab.

## Disk cache, preallocation and mmap storage

gextto's libtorrent settings also apply to gx-torrent, but gx-torrent's cache
is **adaptive**: the daemon recomputes it from the real load instead of
using fixed values.

- **`libtorrent_cache_size` = -1** (default): **adaptive cache**. The daemon
  recomputes it every 3 minutes from:
  - **available RAM** (`MemAvailable`, reclaimable memory), not the total
    RAM: on a busy server the cache deflates by itself;
  - **active downloads** (write buffer) and **active seeds** (read cache);
  - **storage class** of the download folder: on HDD or NFS the values
    double (more coalescing, fewer round-trips), on SSD/NVMe they stay low.
  - Limits: write 96 MB–1.5 GB, read 32–512 MB, never more than 1/4 of the RAM and
    1/8 of the available memory.
  - **Hysteresis**: it is reapplied only if the target changes by more than 25%, at
    most once every 10 minutes. The engine resizes read cache and write
    buffer on the session in progress: no reopening.
  - the engine does not have a write-back cache: the value is a **cap on the pieces in flight**,
    and the real write/coalescing cache is done by the kernel. The policy serves
    above all not to oversize when the load is low and to give more
    read cache during seeding.
- **`libtorrent_cache_size` > 0**: **manual override**; the indicated size
  (16 KiB blocks) applies to reading and writing and the adaptive policy is silent.
- **`libtorrent_cache_expiry`**: after how long a block in the cache expires.
- **`libtorrent_preallocate`**: new files are reserved in full with
  `fallocate`; files excluded from the selection remain sparse, and on tmpfs (RAM
  disk) they always remain sparse: reserving them would immediately take their whole
  size from the RAM.
- **`-mmap`** (experimental, Linux): the torrent's files are served from a
  **shared memory mapping** instead of with `read`/`write`. Piece writes
  become copies into the page cache (asynchronous kernel writeback) and
  reads avoid the syscall: `filestorage` benchmark on tmpfs ~7.0 GB/s versus
  ~5.9 GB/s for `pwrite`. It is **opt-in** (also `GX_TORRENT_MMAP=1`) because it maps
  every open file: with torrents of thousands of files it approaches the
  `vm.max_map_count` limit. Outside Linux it is ignored (normal files are used).
- **"Optimise settings"**: with gx-torrent it resets the cache to automatic and
  applies it immediately.
- The cache size changes on the fly; cache expiry and
  preallocation instead reopen the daemon session.
- The Health panel and `GET /api/v1/stats` show the effective cache, the
  chosen target (`cache_read_mb`, `cache_write_mb`), the **reason**
  (`cache_reason`), the storage class (`cache_storage`) and the available
  memory (`mem_available_mb`).

## RAM disk

gextto's RAM disk flow (download to tmpfs and move when there is no more
space or at the end of the download) works: the move between different filesystems
copies the files and resumes from the point it was at. It is proven by a test that moves
a download at 20% from `/dev/shm` to the disk.

- The `.torrent` copies in the folder chosen by the operator
  (`libtorrent_torrent_copy_dir`) take the torrent's name, as with
  libtorrent; the old `<hash>.torrent` copies are renamed at startup.
- A downloading torrent that does not yet have any verified piece is
  moved without copying its empty files: they are recreated in the
  destination, without writing gigabytes of zeros to the NAS and without the full
  check the engine would do finding files already present.
- A torrent with unknown size at addition (magnet) does not start on the RAM
  disk: it stays on disk until the metadata reveals the size, then it is
  moved to the RAM disk only if it falls within the threshold. A file too large never
  touches the tmpfs. The rule also applies to manual adds and to every engine
  with `ramdisk` support; qBittorrent (`ramdisk: none`) always downloads to disk.
- The "reserved" space on the RAM disk does not count the torrents already
  leaving it.

## On-disk layout and data safety

the engine saves every torrent in `DataDir/<id>` and at removal always runs
`os.RemoveAll(DataDir/<id>)`. For this reason gx-torrent:

- **always** creates `DATA/links/<id>` as a symlink towards the real
  destination folder. Files end up in `destination/<torrent name>`, with the
  same layout as libtorrent, so `CompletionPath` works unchanged;
- removing a torrent deletes only the symlink. Data is deleted only with
  `delete_files=1`, and only the torrent's file or folder, never the
  whole destination or other files;
- an old real folder in place of the symlink (previous layout) is
  moved to `DATA/orphaned/<id>` before removal, never deleted;
- destinations must be absolute paths; the flag `-allowed-roots`
  (standalone use only, Gextto does not set it) can restrict them.

**Durability.** Data files are not opened with `O_SYNC`: the engine writes
pieces in buffered mode and flushes in batch with `storage.File.Sync()` before
persisting the bitfield (order snapshot → fsync → write; `updateStats` in
two phases). A bit that survives a crash therefore always refers to data already
on disk, and at most the pieces of the last few minutes are re-downloaded. On HDD and NFS
this removes one fsync per piece write (measurements in `evoluzione.md`
§5.4/§5.12).

**Move**:

1. the torrent is stopped;
2. the daemon waits for the engine to close the files;
3. the content is moved, with `rename` or with copy and removal if
   the filesystem changes (via `.gxpart` files);
4. the symlink is re-pointed atomically;
5. the torrent starts again.

Gextto receives `storage_moved` when the new path appears in the list, and
`storage_move_failed` on error. `AssociateStorage` re-points without
moving and re-verifies.

The daemon's own state is in `DATA/state.json`, written atomically.
For each torrent it contains:

- path, queue position, user pause, park for stall, pin;
- probe in progress, rotation, seed limits;
- swarm size from the last scrape;
- completion date and error.

It survives restarts together with the engine database (`DATA/session.db`).

## Self-managed queue

the engine does not have a queue: the daemon itself manages it, every 3 seconds (and immediately
after every command), with the same policy that Gextto applies to libtorrent.

- **Slots.** Active downloads (`active_downloads`) start in order of
  queue position. Seeds (`active_seeds`) have their own slots.
- **Slow torrents** (`dont_count_slow`, active by default). A torrent that after
  `slow_after_secs` (120 s) transfers less than `slow_rate` (2 KiB/s) keeps
  running but frees its slot, so the next one starts. A dead swarm never
  blocks healthy downloads. Just started, a torrent has a grace period.
- **Total limit** (`active_limit`). It is a hard cap on the running
  torrents, slow ones included. When it is exceeded, the daemon:
  1. does not restart the torrents just set aside;
  2. stops the slow seeds, then the slow downloads, from the bottom of the queue;
  3. only afterwards gives up starting healthy torrents.

  The stopped slow torrents are **rotated** to the bottom of the queue and retry after
  `slow_rotate_secs` (30 min). So every blocked download is retried
  periodically while the healthy ones proceed, without continuous starts and stops
  between one turn and the next.
- **Never touched by the queue.** The queue never starts:
  - torrents paused by the user (or by Gextto at the end of seeding);
  - those parked as stalled by Gextto;
  - those in error;
  - those moving.
- **Pin and probe.** Torrents with pin and those in probe (see below) always
  run and do not occupy slots.
- **Dynamic queue** (`dynamic_queue`, with minimum and maximum). It is the same logic
  as the libtorrent bridge:
  - it narrows the slots if the global download limit stays saturated (≥ 90%)
    for ten minutes;
  - it widens them quickly if the bandwidth stays unused (≤ 70%) and there are
    torrents waiting;
  - with downloads queued for 5 minutes, the seeds drop to one.
- **Move to top.** `QueueTop` at addition, or the `top` action.

Statuses reported to Gextto:

| Status | Meaning |
|---|---|
| `downloading` | running (allocation too) |
| `downloading_metadata` | magnet waiting for metadata |
| `checking_files` | check in progress |
| `seeding` | complete, sharing |
| `paused` | `auto_managed=true`: queued, waiting for a slot. `auto_managed=false`: user-paused or stopped at the end of seeding |
| `stalled` | parked by Gextto, or set aside by the queue because it was not downloading anything. It stays `stalled` until it restarts, so Gextto's stall clock does not reset |
| `moving` | move in progress |
| `error` | engine error or failed move; a `resume` or a `verify` resets it |

The `auto_managed` flag (with `parked` and `pinned`) is what Gextto summarises in the
**Auto-managed** row (*Auto-gestito* in Italian) of the torrent detail, naming *who*
controls the scheduling, translated in every interface language: *managed by the
queue* (*gestito dalla coda*) when the queue manages it, *paused* (*in pausa*) when
it is stopped outside the queue, *parked (stalled)* (*parcheggiato (in stallo)*)
when it is stalled and has been set aside, *forced outside the queue* (*forzato
fuori coda*) when it is force-started. It is therefore not a simple yes/no. The
same values are used for every engine: with qBittorrent-nox the forced case is
`force_start` and the queue owns every torrent that is neither force-started nor
stopped (qBittorrent's `auto_tmm` is the automatic management of save paths, not
of the queue, so it does not count); a torrent outside the queue that is not
transferring (stopped, checking, in error) reads *paused*. The TUI shows the same
row (`sched.*` keys of its catalog).

## Stalled: division of tasks

The long-term management of stalls remains in Gextto (`MonitorStalled`),
identical for all engines:

1. after `libtorrent_stall_after_min` without progress Gextto calls
   `MarkStalled`, i.e. the `park` action (stopped, out of the queue, `stalled` state);
2. the attempts follow the backoff 1 h → 3 h → 6 h → 12 h → 24 h and use
   `Restart`, i.e. the `restart` action: the torrent runs out of the queue for a
   15-minute probe window and is re-announced to trackers and DHT;
3. if bytes arrive, Gextto calls `ClearStalled` (`unpark`); otherwise
   it parks it again;
4. once the give-up has expired, Gextto removes the torrent and looks for another release.

The daemon adds **short-term** management: slow torrents out of the
slots and rotation at the total limit. Parking survives restarts of the
daemon.

For diagnosis (`dead_swarm`, `no_connected_seed`, ...):

- swarm seeders and leechers come from the last successful scrape of the trackers
  and remain known even with the torrent stopped;
- "connected seeds" are the peers from which one is actually receiving.

## Seed

The per-torrent limits (`SetLimits` with ratio and days) are saved by the
daemon and reported in the list (`-1` = global, `0` = infinite). Gextto's
`EnforceSeedPolicy` applies them, as for the other engines: pause at the end of seeding,
then move or remove.

## Super-seeding (BEP 16)

On a **completed** torrent super-seeding can be activated: the engine does not
announce the whole bitfield, but **one piece at a time** per peer, and serves only the
pieces offered to that peer. It is for *initial seeding*: it pushes the swarm to
exchange the data instead of downloading it all from the seed, at the price of a
**deliberately lower** seed upload.

- `POST /api/v1/torrents/{hash}/super-seeding` with `enabled=1/0`, or the checkbox
  on the daemon page; Gextto activates it from the **General** tab of the detail.
- It applies only to a completed torrent: activated earlier, it comes into effect at
  completion; on a downloading or stopped torrent it is inert.
- It is a **seeding strategy**: it does not touch the queue, slots, seed policy or bandwidth
  limits. When Gextto's seed policy (ratio/days) stops the torrent,
  super-seeding stops with it.
- The flag is persisted in the daemon's resume, so it survives a restart.
- Like libtorrent, `have` and `not-interested` advance the announces; the pieces
  offered are not repeated and, when they are finished, the peer is released with the
  full bitfield.

Gextto reports the capability `super_seeding` for gx-torrent (`full`).

## Writes to the state disk

The daemon's state (`DATA_DIR/gx-torrent`: `session.db`, `state.json`, logs)
stays on the local disk: `session.db` is a bbolt database (mmap and lock), not
suitable for NFS. Writes are contained: the engine saves statistics and bitfield
every 2 minutes (the daemon raises to 2 minutes the 30 s default of
`ResumeWriteInterval`), with a fsync of the data before every bitfield
write: about 100 MB per day at rest; the log rotates at
5 MB × 4.

## Bandwidth limits

Global limits change on the fly (scheduling, temporary limit): the engine
uses a limiter whose rate updates while the peers use it,
so no reopening of the session and no peer lost. Gextto sends the
configuration only when it changes. Only the peer limits
(`max_peer_dial`/`max_peer_accept`), the cache expiry and the
preallocation reopen the session.

Every torrent has its **own speed limit** (download and upload, KiB/s):
`-1` inherits the global, `0` is unlimited, a positive value is the limit. The
torrent's peers use a dedicated limiter which, when the torrent inherits,
forwards to the session limiter: a change applies immediately, without reconnecting, and
the global limit continues to apply to the sum of the torrents that inherit it.
It is set with `POST /api/v1/torrents/{hash}/seed-limits` (fields
`download_limit`, `upload_limit`) or from the daemon page; Gextto does it from the
**Limits** tab of the torrent detail. Gextto keeps the values between one restart
and the next and the daemon reapplies them to the reloaded session.

Every torrent also has its own **connection limit** and **upload
slots** (`POST /api/v1/torrents/{hash}/conn-limits`, fields `max_connections` and
`max_uploads`; `-1` and `0` both mean no per-torrent cap — the global
budget `max_peer_dial`/`max_peer_accept` remains). The connection limit applies
to the connections established; if it is lowered below the current number,
the excess ones are closed immediately. The upload slots change the
number of peers the unchoker keeps unblocked.

### Auto-speed on latency (anti-bufferbloat)

**Standalone** option (settings.json, like the bandwidth scheduler), inspired by
BiglyBT's *Ping Auto-Speed*. A probe measures every second the **round-trip
latency** with a TCP connection towards the **gateway** (ports 80, 443,
53) or towards a configured host; the lowest value of the last 60 s is the
**resting baseline**. When the latency exceeds `baseline × 1.5` **and**
`baseline + 20 ms`, the daemon **lowers the upload** by 15% with respect to the limit in
force (never below the configured minimum); when the line returns to rest it
raises it again by `limit/20` at a time. It reduces **only** the effective upload: the global
or scheduled limit remains the cap, and an unlimited limit (or the absence of
measurements) disables the clamp.

Keys in settings.json: `autospeed-enabled` (default `false`),
`autospeed-target` (host or `host:port`; empty = gateway), `autospeed-min`
(KiB/s, default 50). A **positive upload limit** (global or
scheduled) is needed for auto-speed to act; the page enforces it. The decisions
appear in the log with reason `auto-speed` (`effective speed limits …`). The
settings page shows the measured latency and the current cap.

## REST API (v1)

All requests require `X-Gx-Token` (or `Authorization: Bearer`) if the
token is set.

| Method and path | Description |
|---|---|
| `GET /api/v1/health` | status and version |
| `GET /api/v1/stats` | counters: downloading, seeding, queued, stalled, slow, speeds, peers, effective slots |
| `GET /api/v1/portcheck` | port test: the peer port is listening and the router forwards it |
| `GET /api/v1/torrents` | full list (progress %, sizes, speeds, peers, swarm, status, path, seed limits, queue flags) |
| `POST /api/v1/add` | form fields: `magnet`, `destination`, `paused`, `top`, `sequential`, `first_last`, `super_seeding`, `stop_at_metadata`, `seed_ratio`, `seed_days`. Responds `{hash, existing}` |
| `POST /api/v1/add-file` | multipart `torrent` plus the same fields |
| `POST /api/v1/create-torrent` | JSON `path`/`name`/`trackers`/`webseeds`/`private`/`piece_length`/`comment`: creates a `.torrent` from a file or folder of the server and returns it as a download (mktorrent-style). `piece_length` (multiple of 16 KiB, 0 = automatic) is limited to 16 MiB so as not to turn an arbitrary value into a huge allocation |
| `DELETE /api/v1/torrents/{hash}?delete_files=1` | removal |
| `POST /api/v1/torrents/{hash}/{action}` | see list below |
| `GET /api/v1/torrents/{hash}/files\|peers\|trackers\|pieces\|torrent-file\|scrape\|dhtscrape\|collections` | inspection (files include `priority`; `pieces` reports the status intervals; `scrape` queries the trackers for seeders/leechers, BEP 48; `dhtscrape` estimates the swarm from the DHT, BEP 33; `collections` reports the BEP 38 hints — `collections`, `similar` in hex and the local torrents that share a hint) |
| `POST /api/v1/pins/clear` | removes all pins |
| `POST /api/v1/ipfilter` | reloads the IP filter (optional `path`) |
| `GET /api/v1/config` | configuration and effective slots |
| `POST /api/v1/config` | partial JSON: unknown keys rejected |
| `GET /ui/events` | SSE of session events (`torrent_added`, `torrent_completed`, `torrent_stopped`, `torrent_error`, `torrent_piece`, `peer_connected`, `peer_disconnected`, `tracker_announce`); the page uses them to refresh immediately |

Actions available on `POST /api/v1/torrents/{hash}/{action}`:

- `pause`, `resume`, `verify`, `reannounce`;
- `restart` (with `probe_secs`), `park`, `unpark`;
- `pin` (with `pinned=1/0`), `top`;
- `move` and `associate` (with `destination`);
- `seed-limits` (`seed_ratio`, `seed_days`, `download_limit`, `upload_limit` in KiB/s: -1 global, 0 unlimited);
- `conn-limits` (`max_connections`, `max_uploads`: -1 and 0 = no per-torrent cap);
- `trackers` (with `urls`, one per line);
- `set-trackers` (with `urls`, one per line: **replaces** the list, an empty list clears it);
- `webseeds` (with `urls`, one per line, and `remove=1` to remove them);
- `super-seeding` (with `enabled=1/0`: BEP 16 on the completed torrent);
- `file-priorities` (with `priorities`, comma-separated; 0 = skip, 1..4 normal, 5..7 high).

Keys accepted by `POST /api/v1/config`:

- `active_downloads`, `active_seeds`, `active_limit`;
- `dont_count_slow`, `slow_rate`, `slow_after_secs`, `slow_rotate_secs`;
- `dynamic_queue`, `dynamic_min`, `dynamic_max`;
- `speed_limit_download`, `speed_limit_upload` (KiB/s);
- `max_peer_dial`, `max_peer_accept`;
- `cache_mb`, `cache_ttl_secs`, `preallocate` (cache and preallocation: `cache_mb`
  ≤ 0 = adaptive);
- `auto` (self-management: dynamic queue and adaptive cache);
- `sequential` (default for torrents added afterwards).

## API availability (snapshot and lock)

Gextto queries `GET /api/v1/torrents` at every poll (1.5 s) with a timeout of
15 s: if the response does not arrive, the daemon is considered unreachable.
The list is therefore served from a **snapshot** published by the queue tick and
by the commands that change the state, **without taking `d.mu` and without touching the
run loop of any torrent**. A torrent blocked on storage I/O (for example
a long copy to a network mount) cannot therefore make the API seem dead.

The tick works in three phases, so as never to hold `d.mu` during an operation that
can block:

1. **Sampling** (without lock): for each torrent it reads stats, peers and trackers
   from its run loop and classifies the storage (the `statfs` on the network mount can
   block for a long time).
2. **Application** (with `d.mu`): it updates meta and queue from the samples,
   computes the plan and publishes the snapshot. No call to the run loop here.
3. **Actions** (without lock): it runs the `Start`/`Stop` decided by the plan; if the run
   loop is busy, the action is simply retried at the next tick.

The snapshot can be at most one tick old (3 s default); every
command that changes the state immediately restarts a tick, so the UI and Gextto
see the change almost immediately.

The **addition** of a torrent also calls the engine without `d.mu`: a magnet can
first resolve a DHT item (BEP 46, up to 10 s) or download the `.torrent` from
an `xs=`/`as=` source, and during that wait API, page and queue must stay
responsive. The id being added is in `d.adding`, so the tick does not start the torrent
before its state (pause, position, category…) is recorded; if in the
meantime the session has been rebuilt, the addition fails with a clear
error. Test `TestAddDoesNotHoldTheLockWhileResolving`.

## Known limits (engine)

Operations that the engine does not support respond with an explicit capability
error (`ErrCapabilityUnavailable`), never with a false success:

- v2 as **seed**: the `hash request` requests of the block layer
  (`base = 0`) are served only for a file **entirely present** and are
  built by reading the data (once per file, then cached); for an incomplete
  file they respond `hash reject` (see *BitTorrent v2 torrents*);
- IPv6: **partial support** (see *IPv6* below).

**Sequential download** and **first/last part** priority are supported
(see the dedicated section): the engine downloads first the edges of
every file (~1% of the size, up to 8 MB), so players immediately find
the index. The first/last part is **independent** of the sequential
order.

The upload slots and the per-torrent connections are **not** a limitation: they are
set with `conn-limits` (and the bandwidth limits with `seed-limits`), as seen
in *Bandwidth limits*.

### IPv6

The single-port listener (TCP) and the UDP socket shared by uTP and DHT are
**dual-stack**: if the listen host is unspecified (`0.0.0.0`, `::` or
empty) a single socket is opened that accepts both IPv4 and IPv6 and, if the machine
has no IPv6, it falls back to IPv4. A specific address remains single-family.

IPv6 peers are discovered from the trackers (`peers6` field, BEP 7), from the PEX
exchange (`added6`/`dropped6`, BEP 11) and from **DHT discovery over IPv6** (BEP 32): the DHT
shares the dual-stack socket, keeps v4 and v6 nodes separate in the fields
`nodes`/`nodes6` and requests both families (`want`). Peers, HTTP and
**UDP** trackers, and web seeds are also resolved over IPv6 (with a preference for IPv4
when the name has both). The extended handshake `yourip` field is
accepted in both IPv4 and IPv6 form, the **IP filter** accepts IPv6 CIDR rules
(the range/P2P/eMule forms remain IPv4) and the engine accepts IPv6 DHT
peers (18-byte contacts).

Only the LSD announcement over **IPv6** is missing in networks where v6 multicast is not
available: if the v6 listener does not open, the LSD stays active on IPv4. In the
other cases the LSD also announces and listens on the group `[ff15::efc0:988f]:6771`
(like anacrolix).

## Tests

- `go test ./cmd/gx-torrent/` covers:
  - the scheduler (slots, slow, rotation with cooling, hard cap,
    forced, dynamic queue);
  - a real lifecycle on a gx-core session without network (addition, verification,
    user pause respected by the queue, move, removal that preserves the
    data, targeted deletion, parking and probe, persistence after the restart
    of the session);
  - token and configuration;
  - the web page (page, live fragments, actions, token, favicon) and the
    translations of its language;
  - the origin of the peers (including `holepunch`), the holepunch tied to uTP and the
    port test;
  - the LSD discovery, the piece diagnostics and the streaming with `Range`;
  - real transfers between two local daemons:
    - through the single port, in the clear and with forced encryption;
    - with file selection and reactivation of an excluded file;
    - via SOCKS5 and HTTP proxy;
    - with a move from RAM disk at mid-download;
    - uTP only, with the DHT on the same UDP socket;
    - with the peer found via LSD (real multicast);
  - loading of the IP filter.
  - (opt-in, `GX_MEASURE=1` / `make measure-seeding`) the seeding/choking measurement
    on a local swarm, with bytes and times per peer: method and metrics in
    `docs/evoluzione.md` (§6).
- `go test ./internal/blocklist` in `internal/gxcore` covers the formats of the
  IP filter.
- `make test-engine` also covers the deterministic choking harness
  (`internal/unchoker/sim_test.go`) and super-seeding
  (`torrent/torrent_superseed_test.go`).
- `go test -run GxEngine .` covers the adapter:
  - statuses and events;
  - cache on interruption;
  - park, probe and unpark;
  - sending of the queue policy;
  - operations and token;
  - `storage_moved`.
