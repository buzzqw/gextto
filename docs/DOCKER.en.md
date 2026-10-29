# Docker: full stack with Gextto, indexarr, Jackett, Prowlarr, FlareSolverr and aMule

This guide runs **everything** in Docker: Gextto with its managed `gx-torrent`,
indexarr with its native sources, Jackett and Prowlarr for Torznab indexers,
FlareSolverr for Cloudflare-protected pages and `amuled` for eD2k/Kad. It targets a Linux host with Docker Engine and
the `docker compose` plugin; it does not replace the `install.sh` installation,
which remains the recommended one on a dedicated machine.

The ready-made file is:

```text
docker-full/docker-compose.yml
```

- [What you start](#what-you-start)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Folders and permissions](#folders-and-permissions)
- [First Gextto configuration](#first-gextto-configuration)
- [indexarr](#indexarr)
- [Jackett, Prowlarr and FlareSolverr](#jackett-prowlarr-and-flaresolverr)
- [aMule, eD2k and Kad](#amule-ed2k-and-kad)
- [gx-torrent and client identity](#gx-torrent-and-client-identity)
- [Paths seen by the containers](#paths-seen-by-the-containers)
- [Updating](#updating)
- [Backup](#backup)
- [Troubleshooting](#troubleshooting)
- [Security](#security)
- [Limits of this mode](#limits-of-this-mode)

## What you start

| Service | Image | Port | Role |
|---|---|---|---|
| `gextto` | built from `docker-full/Dockerfile` | 5000 | UI/API, daemon, managed `gx-torrent` |
| `indexarr` | built from the indexarr checkout (its `Dockerfile`) | 8788 | Torznab/Newznab indexer with native sources, eD2k included |
| `amuled` | `ngosang/amule` | 4712 EC, 4662 TCP, 4672 UDP | eD2k and Kad |
| `jackett` | `lscr.io/linuxserver/jackett` | 9117 | Torznab indexer |
| `prowlarr` | `lscr.io/linuxserver/prowlarr` | 9696 | Torznab indexer |
| `flaresolverr` | `ghcr.io/flaresolverr/flaresolverr` | 8191 | Cloudflare handling |

Gextto, indexarr and `amuled` use **`network_mode: host`**: aMule needs the host
network for UPnP and the eD2k ports, Gextto finds indexarr, Jackett, Prowlarr,
FlareSolverr and aMule on `127.0.0.1`, and indexarr finds FlareSolverr and
aMule's web API there too, exactly as in a normal installation. Jackett, Prowlarr
and FlareSolverr stay on the bridge network and publish their ports on
`127.0.0.1` only (see [Security](#security)).

## Requirements

- Linux with Docker Engine and Docker Compose v2 (`docker compose version`).
- At least 4 GB of free RAM and room for downloads, library and databases.
- Ports **5000** (UI), **6881 TCP/UDP** (gx-torrent), **4662 TCP** and
  **4672 UDP** (aMule), **4712** (aMule EC) free on the host.
- Port **8788** (indexarr) free, or another one chosen with `INDEXARR_PORT`.
- Ports 9117, 9696 and 8191 free if you do not already run those services.
- The **indexarr** source next to this repository (the image is built from it):
  `git clone https://github.com/buzzqw/indexarr.git ../indexarr`. Another
  location goes in `INDEXARR_SRC` in `docker-full/.env`.
- A user ID/GID that owns the mounted folders. The examples use `1000:1000`:
  change `PUID`/`PGID` in `docker-full/.env` if your user has a different ID
  (`id -u`, `id -g`).

> [!IMPORTANT]
> `network_mode: host` is not available on Docker Desktop for macOS and Windows.
> On those hosts use the native Gextto installation, or adapt the compose file
> to a bridge network (you lose UPnP and the convenience of `127.0.0.1`).

## Quick start

From the repository root:

```bash
cp docker-full/env.example docker-full/.env
# Edit .env: PUID, PGID, AMULE_EC_PASSWORD and, if you want, TZ.
```

Create the folders **before** starting the containers, so they are not created
as root:

```bash
mkdir -p docker-full/data/{gextto,indexarr,amule,jackett,prowlarr,flaresolverr} \
         docker-full/media/amule/{incoming,temp} \
         docker-full/library
sudo chown -R "$(id -u):$(id -g)" docker-full/data docker-full/media docker-full/library
```

Build the images with the version of the checkouts (for Gextto it reads
`build_number`, `version_series`, `gx-torrent.build_number` and
`gx_torrent.version_series`; for indexarr its `git describe`):

```bash
./scripts/docker-full-build.sh
```

Start the stack:

```bash
docker compose -f docker-full/docker-compose.yml up -d
```

Open:

```text
http://HOST-IP:5000
```

Follow the wizard. Logs are in:

```bash
docker compose -f docker-full/docker-compose.yml logs -f gextto
```

and in `docker-full/data/gextto/gextto.log` and
`docker-full/data/gextto/gx-torrent/gx-torrent.log`.

Stop everything without deleting data:

```bash
docker compose -f docker-full/docker-compose.yml down
```

## Folders and permissions

| Host | Inside Gextto | Use |
|---|---|---|
| `docker-full/data/gextto` | `/data` | databases, configuration, logs, gx-torrent state |
| `docker-full/media` | `/media` | downloads and media library |
| `docker-full/library` | `/library` | final archive (optional; you can use `/media`) |
| `docker-full/media/amule/incoming` | `/amule/incoming` | files completed by aMule, to import |
| `docker-full/media/amule/temp` | not mounted in Gextto | aMule temporary files |
| `docker-full/data/amule` | not mounted in Gextto | aMule configuration |
| `docker-full/data/indexarr` | `/data` in indexarr | indexarr database, configuration, logs and vault key |

aMule's path **must be the same** inside `amuled` and inside Gextto: that is why
`./media/amule/incoming` is mounted as `/amule/incoming` in both. If you change
it, change both mounts and the incoming folder in Gextto's settings.

Files created by Gextto belong to the UID/GID the container runs as
(`PUID`/`PGID`). If you see `permission denied`, fix the host folder owner and
restart.

## First Gextto configuration

In the wizard:

1. choose the language and set the password/API key if you want to protect the
   API;
2. use `/media` (or a subfolder such as `/media/downloads`) as the **download
   folder**;
3. use `/library` (or `/media/library`) as the **archive**;
4. leave **gx-torrent** as the engine: the binary is already in the image and
   Gextto starts and supervises it;
5. enable downloads when you are ready.

Do not set `gxtorrent_listen` to a public address without a token: the default
`127.0.0.1:8890` is the right one for this stack. The gx-torrent page is
reachable from inside the container; to expose it, use `gxtorrent_listen` and
`gxtorrent_token` carefully.

## indexarr

indexarr is a Torznab/Newznab indexer with its own native sources: it replaces
Jackett and Prowlarr for the sites it covers, and Gextto reads it like any other
indexer. Open its UI at `http://HOST-IP:8788` (or the `INDEXARR_PORT` you chose).

1. **Settings → Security**: set a username and password before anything else,
   because the UI listens on every interface of the host (host network). The
   same page shows the **API key**.
2. **Settings → Network**: FlareSolverr address `http://127.0.0.1:8191`, then
   **Test**.
3. **Indexers → Add**: add the sources you need from the catalog.
4. In Gextto: **Configuration → Sources → Add indexer**, manager **indexarr**,
   URL `http://127.0.0.1:8788`, the API key of step 1. Save and verify.

For eD2k sources, indexarr can send the links to the same `amuled`: in its
**App → Download clients** add aMule with the web API address
`http://127.0.0.1:4711` and the `AMULE_WEBUI_PASSWORD` password.

indexarr's own guides cover its sources, settings and API:
[README](https://github.com/buzzqw/indexarr/blob/main/README.md) and
[user guide](https://github.com/buzzqw/indexarr/blob/main/docs/USAGE.md).

The vault key that encrypts indexarr's passwords and cookies is created in
`docker-full/data/indexarr/.config/indexarr/master.key`. Without it the saved
secrets cannot be read: keep a copy apart from the backups.

## Jackett, Prowlarr and FlareSolverr

The three services are already on a network with Gextto. Open their UI **from
the host** (or through an SSH tunnel, see below):

- Jackett: `http://127.0.0.1:9117`
- Prowlarr: `http://127.0.0.1:9696`
- FlareSolverr: `http://127.0.0.1:8191`

### Jackett

1. Open Jackett, add the indexers you need and copy the **API key** from its
   home page.
2. In Gextto: **Configuration → Sources → Add indexer**.
3. Name: `jackett`; URL: `http://127.0.0.1:9117`; API key: Jackett's; manager:
   **Jackett**. Save and test the connection.

### Prowlarr

1. Open Prowlarr, add the indexers and copy the **API key** from
   *Settings → General*.
2. In Gextto add an indexer with URL `http://127.0.0.1:9696`, Prowlarr's API
   key and manager **Prowlarr**.
3. You do not need to configure Gextto as an "app" inside Prowlarr: Gextto reads
   Prowlarr's Torznab endpoint directly.

### FlareSolverr

In **Configuration → Sources → FlareSolverr** enter:

```text
http://127.0.0.1:8191
```

and press **Test**. FlareSolverr is used only for external indexers that need to
pass Cloudflare; a local indexer never goes through it.

### Opening the UIs from another computer

Ports 9117, 9696 and 8191 are published on the host's `127.0.0.1` only. From
another computer use an SSH tunnel:

```bash
ssh -L 9117:127.0.0.1:9117 -L 9696:127.0.0.1:9696 -L 8191:127.0.0.1:8191 user@host
```

then open `http://127.0.0.1:9117` and `http://127.0.0.1:9696` in your browser.
If you prefer to expose them on the LAN, replace `"127.0.0.1:9117:9117"` with
`"9117:9117"` in the compose file (and likewise for the others) and protect
them with a firewall and passwords.

## aMule, eD2k and Kad

`amuled` starts on the host network and generates its configuration on first
start. `AMULE_EC_PASSWORD` in `.env` is the **clear** External Connections (EC)
password; the image turns it into the MD5 aMule stores in `amule.conf`.

In Gextto:

1. **Configuration → eD2k and Kad**;
2. enable **eD2k and Kad through aMule**;
3. address `127.0.0.1`, EC port `4712`, password = `AMULE_EC_PASSWORD`;
4. save, open **Discovery → eD2k** and press **Connect**.

aMule's incoming folder is `/amule/incoming` both in the container and in
Gextto. Completed files appear in **Downloads** and are imported like the other
downloads.

> [!NOTE]
> If you change `AMULE_EC_PASSWORD` after the first start, aMule keeps using the
> old hash: the file is `docker-full/data/amule/amule.conf`. Stop `amuled`,
> update `ECPassword` (MD5 of the new password:
> `printf '%s' 'new' | md5sum`) and restart, or delete the configuration file
> and let the image regenerate it. Update the password in Gextto too.

The default eD2k ports are TCP 4662 and UDP 4672. Open them on the router (or
enable aMule's UPnP by editing `amule.conf`, which is generated with
`UPnPEnabled=0`). Without open ports aMule runs with a Low ID and finds fewer
sources.

## gx-torrent and client identity

The default engine is **gx-torrent**, already included in the image and started
by Gextto. It announces its real name and version:

```text
peer id  -GX1.1.<build>-…
BEP 10   gx-core 1.1.<build>
tracker  gx-core/1.1.<build>
```

In **Configuration → gx-torrent → announced client identity** you can choose a
compatibility preset:

- `gx-core` (default, real version);
- `qBittorrent` (5.2.3);
- `Transmission` (4.1.3);
- `Rain` (2.4.4);
- `anacrolix/torrent` (1.61.0).

The **version** field overrides the preset version. Only the announced strings
change (peer ID, BEP 10, HTTP/WebSocket tracker User-Agent, `.torrent` creator),
not the protocol behaviour. Some private trackers only accept whitelisted
clients: check their rules and do not use a preset to bypass a ban.

### How it was verified

The identity was tested with real clients in Docker, a local tracker and a test
torrent:

| Observing client | Gextto identity | What the client sees |
|---|---|---|
| qBittorrent 5.3.0 | `gx-core` | `client: gx-core 1.1.252`, `peer_id_client: -GX1.1.2` |
| qBittorrent 5.3.0 | `qbittorrent` preset | `client: qBittorrent 5.2.3`, peer id `-qB5230-` |
| Transmission 4.1.3 | `gx-core` | `clientName: gx-core 1.1.252` |
| Transmission 4.1.3 | `qbittorrent` preset | `clientName: qBittorrent 5.2.3` |

The test proves that the remote peer really receives the chosen identity. It
does not prove that a private tracker accepts it: that depends on its rules.

## Paths seen by the containers

Gextto sees files as:

- downloads: `/media`
- archive: `/library`
- aMule incoming: `/amule/incoming`

If Jellyfin or Plex run in another container and see the same library under a
different path, set the mapping in **Configuration → Jellyfin/Plex** (for
example `gextto_path=jellyfin_path`). The same applies to qBittorrent if you
ever use it instead of gx-torrent.

## Updating

```bash
git pull
git -C ../indexarr pull
./scripts/docker-full-build.sh
docker compose -f docker-full/docker-compose.yml up -d
```

`docker compose up -d` recreates only the containers with a new image; the
volumes in `docker-full/data`, `docker-full/media` and `docker-full/library`
stay. Do not use Gextto's internal update (`gexttod --update`) or indexarr's
`scripts/upgrade.sh` in this stack: the installers and systemd services are not
active in the containers. Update
Jackett, Prowlarr, FlareSolverr and aMule with:

```bash
docker compose -f docker-full/docker-compose.yml pull
docker compose -f docker-full/docker-compose.yml up -d
```

## Backup

Stop Gextto before copying the databases:

```bash
docker compose -f docker-full/docker-compose.yml stop gextto
cp -a docker-full/data/gextto /path/to/backup/gextto-$(date +%F)
docker compose -f docker-full/docker-compose.yml start gextto
```

indexarr the same way:

```bash
docker compose -f docker-full/docker-compose.yml stop indexarr
cp -a docker-full/data/indexarr /path/to/backup/indexarr-$(date +%F)
docker compose -f docker-full/docker-compose.yml start indexarr
```

The copy contains the vault key too: keep it somewhere safe. For a complete
backup also copy `docker-full/data/amule`,
`docker-full/data/jackett`, `docker-full/data/prowlarr` and, if you want,
`docker-full/library`. Downloads in `docker-full/media` are often large: skip
them or copy them with an incremental tool.

## Troubleshooting

| Symptom | What to check |
|---|---|
| Port 5000 is already in use | Another Gextto instance is running on the host. Stop it (`systemctl --user stop gextto`) or change `GEXTTO_LISTEN` in the compose file and the published port. |
| Port 8788 is already in use | indexarr is already installed on the host. Stop it (`systemctl --user stop indexarr`) or set another `INDEXARR_PORT` in `.env` and use that port in Gextto's indexarr URL. |
| `indexarr checkout not found` while building | Clone indexarr next to this repository or set `INDEXARR_SRC` in `docker-full/.env` (relative to `docker-full/` or absolute). |
| Gextto cannot see indexarr | The URL is `http://127.0.0.1:8788` (host network), not `http://indexarr:8788`; check `docker compose ... logs indexarr`. |
| `permission denied` on `/data` or `/media` | `PUID`/`PGID` in `.env` must match the folder owner; run `chown -R` on the host folders. |
| Gextto cannot see Jackett/Prowlarr | Gextto uses the host network: the URL is `http://127.0.0.1:9117` / `http://127.0.0.1:9696`, not `http://jackett`. Check the published ports. |
| FlareSolverr does not answer | Check `docker compose ... logs flaresolverr` and open `http://127.0.0.1:8191` from the host. |
| aMule unreachable | Gextto's EC password must be the clear `AMULE_EC_PASSWORD`; the port is 4712; `amuled` must be running. |
| aMule has a Low ID | Open TCP 4662 and UDP 4672 on the router, or enable UPnP in `amule.conf`. |
| No peers in torrents | Open 6881 TCP/UDP on the router/firewall; check that `gx-torrent` is running. Client identity has nothing to do with reachability. |
| The container does not start on macOS/Windows | `network_mode: host` is not supported by Docker Desktop: use Linux or the native installation. |
| Version shows `1.1.0` | You built with the default values. Use `scripts/docker-full-build.sh`, which reads the checkout's build files. |

## Security

- Change `AMULE_EC_PASSWORD` and `AMULE_WEBUI_PASSWORD` before the first start.
- Gextto listens on `0.0.0.0:5000` (host network): put it behind an HTTPS
  reverse proxy or restrict it with the firewall. Protect the API with its API
  key.
- indexarr listens on every interface on port 8788 (host network) and has no
  login until you set one in **Settings → Security**: do it at the first start,
  or restrict the port with the firewall.
- Jackett, Prowlarr and FlareSolverr are published on `127.0.0.1` only by
  default. FlareSolverr has no authentication: do not expose it on the LAN.
- Do not publish aMule's EC port 4712 on the Internet.
- The Docker group is equivalent to root on the host: restrict access to the
  Docker socket.

## Limits of this mode

- The stack is for **Linux**: host networking and aMule's UPnP do not work on
  Docker Desktop.
- There is no systemd, `install.sh`, `gexttod --update` or
  `gextto-update.path` service: update with `git pull` + compose.
- Jackett/Prowlarr/FlareSolverr/aMule have their own updates; `AUTO_UPDATE=true`
  for Jackett is convenient but can change version without your intervention.
- The image ships `ffprobe`, `ffmpeg` and `mkvmerge`: they serve the optional
  audio cleanup and do not need to be installed by hand.
- The images build from source: the first build takes a few minutes and pulls
  `golang:1.27` and Debian slim for Gextto, `golang:1.26-alpine` and Alpine for
  indexarr.

For the complete aMule integration (EC protocol, folders, diagnostics) see also
[eD2k and Kad through aMule](amule.md). For the engine and client identities see
[gx-torrent](gx-torrent.md).
