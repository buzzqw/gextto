# Configuring qBittorrent-nox for Gextto

Gextto can drive an **external qBittorrent-nox** through its Web API. Gextto
keeps its own database, queue, scoring, post-processing, renaming and archive:
only the transfer plane is qBittorrent. A torrent belongs to one engine at a
time, so switching backend is a migration, not two clients on the same data.

Use this backend when:

- the torrent engine must run on its own: a separate service, a different
  machine, a container;
- you already run qBittorrent-nox and want Gextto to automate it;
- you prefer qBittorrent's transfer behaviour and want to keep it.

You do not get two clients: while qBittorrent is active it moves every byte,
and the gx-torrent engine is not running.

> The addresses, ports and folders below are examples. Always check the real
> service user, the NAS mount and the permissions in your install.

- [1. Prepare qBittorrent-nox](#1-prepare-qbittorrent-nox)
- [2. Configure Gextto](#2-configure-gextto)
- [3. Paths: usually nothing to do](#3-paths-usually-nothing-to-do)
- [4. What Gextto drives, and what stays in qBittorrent](#4-what-gextto-drives-and-what-stays-in-qbittorrent)
- [5. Managed mode](#5-managed-mode)
- [6. Switching engines](#6-switching-engines)
- [7. Verify and diagnose](#7-verify-and-diagnose)
- [8. Reference](#8-reference)

## 1. Prepare qBittorrent-nox

### 1.1 Install it

On Debian and Ubuntu:

```bash
sudo apt install qbittorrent-nox
```

Start it the way your distribution ships it (the package provides a service),
or run `qbittorrent-nox` yourself as the user that should own the downloads.
On a fresh profile the Web UI listens on `http://127.0.0.1:8080` with the user
`admin`; current releases print a temporary password in the log (older ones
use `admin`/`admin`). **Set a real password** in *Tools → Options → Web UI*
before exposing anything.

The configuration lives in `~/.config/qBittorrent/qBittorrent.conf` for the
user that runs it (a different `--profile` or a container image moves it).

### 1.2 Enable the Web UI

Gextto talks to qBittorrent **only through its Web API**, which is part of the
Web UI component: with the Web UI off there is no API and Gextto cannot
connect at all. So:

- Keep the **Web User Interface enabled** (it is on by default in
  `qbittorrent-nox`; the switch is under *Tools → Options → Web UI*). In the
  configuration file that is `WebUI\Enabled=true` under `[Preferences]`.
- Set a **username and password**: Gextto authenticates with them. Set the
  password from the Web UI, not by hand: qBittorrent stores it hashed (PBKDF2
  in current releases), so a plain value written in the file does not work.
- Note the **Web UI port** (default `8080`) and whether it answers on
  `127.0.0.1` only or on the LAN. If Gextto and qBittorrent are on different
  hosts, the Web UI must be reachable from Gextto's host.
- If you enable "Bypass authentication for clients on localhost" and Gextto
  connects from localhost, the credentials are not needed, but setting them is
  still recommended.
- **HTTPS** works, but the certificate is validated: a self-signed certificate
  is refused. Use `http` on localhost or the LAN, a certificate from a trusted
  CA, or a reverse proxy with a valid certificate. Behind a reverse proxy with
  a sub-path, put the whole prefix in the URL (`https://host/qbt`).

### 1.3 In Docker

Run qBittorrent-nox in a container as usual (`linuxserver/qbittorrent` or the
official image), then:

- **publish the Web UI port** (8080) and set the Web UI address to `*` (or to
  the container's interface) if Gextto is outside the container;
- **mount the download folder at a fixed path** inside the container
  (`/downloads` in the examples below): that path is what qBittorrent reports
  to Gextto, and it is the one you map in §3.2;
- keep the container's user able to write the download folder, otherwise
  completed files cannot be imported.

## 2. Configure Gextto

Open **Configuration → Torrent engine** and choose **qBittorrent-nox**. The choice
of the engine is separate from the settings below: ticking *managed* does not
switch the engine by itself.

Then fill in the **qBittorrent-nox** tile:

| Setting | Meaning |
|---|---|
| **Downloaded and updated by Gextto** | Managed mode: Gextto downloads the latest qBittorrent-nox, starts/stops it with the service and updates it (see §5). Needs nothing else on this page |
| **Web API URL** | qBittorrent Web UI URL, e.g. `http://127.0.0.1:8080` |
| **User / Password** | qBittorrent Web UI credentials |
| **Category** | Category applied to every torrent Gextto adds (created if missing) |
| **Tag** | Tag applied to every torrent Gextto adds |
| **Request timeout** | Seconds for a Web API request (default 15) |
| **Polling interval** | Milliseconds between status reads (default 1500) |
| **Path mapping** | `gextto_path=qbittorrent_path`, one per line — leave empty in the common case (see §3) |

The **Test** button runs the preflight: it connects, reads the version, the
default save path and the number of torrents. **Activation is refused** when a
Gextto path the backend would receive is not covered by a mapping, so a move
can never target the wrong folder.

**Category and tag are the ownership boundary.** With both empty, the
qBittorrent instance is treated as **dedicated to Gextto** and every torrent it
holds is synchronized: handy for a fresh instance, risky if you use the same
qBittorrent for something else. Set a category or a tag (or both) and Gextto
will only see and manage its own torrents.

## 3. Paths: usually nothing to do

Gextto computes a save path for every torrent (the configured download folder,
or a per-title/per-rule folder). It hands that path to qBittorrent **in
qBittorrent's own filesystem namespace**. The two only agree when both see the
same folders — which is the common case, and then there is nothing to fill in.

### 3.1 The common case — leave Path mapping empty

- **Managed mode** (§5): Gextto starts qBittorrent itself on the same machine,
  so both see the same folders. Nothing to map.
- **Same host, same folders**: qBittorrent runs natively on the same machine
  and the download folder is the same. Leave **Path mapping** empty; Gextto
  only checks that the folder exists locally. Example:

```
Gextto download folder:      /mnt/media/downloads
qBittorrent sees the same:   /mnt/media/downloads   -> no mapping
```

An empty field is always safe: if a path Gextto would hand over is not
covered, **activation is refused** with
`path "…" is not covered by backend_path_mappings` naming the offending path —
a download never lands in the wrong place silently.

### 3.2 Only for Docker, NAS or another host — map it

When the same folder has a **different path** in qBittorrent (a container, a
different mount point, another host), tell Gextto how to translate. One entry
per line, **`gextto_path=qbittorrent_path`**; `#` starts a comment.

**qBittorrent in Docker** — the host folder `/mnt/media/downloads` is mounted as
`/downloads` in the container:

```
/mnt/media/downloads=/downloads
```

If you also use a separate temporary/incomplete folder, map it too:

```
/mnt/media/downloads=/downloads
/mnt/media/incomplete=/incomplete
```

**qBittorrent on a NAS** — Gextto mounts the NAS at `/mnt/nas/downloads`, while
qBittorrent (running on the NAS) sees `/volume1/downloads`:

```
/mnt/nas/downloads=/volume1/downloads
```

Rules Gextto enforces:

- the **longest** matching prefix wins, so a general mapping plus a specific one
  is fine;
- **overlapping** Gextto roots are refused (translation would be ambiguous);
- every Gextto path that can be handed over — the download folder, the
  temporary folder if set, and the RAM disk if enabled — **must be covered**;
  otherwise activation is refused with
  `path "…" is not covered by backend_path_mappings`.

The translation goes **both ways**: when qBittorrent reports a torrent's save
path back, Gextto shows the path in its own namespace, and a move translates the
destination before asking qBittorrent to relocate the data.

### 3.3 Typical situations, at a glance

| Setup | qBittorrent sees | Path mapping |
|---|---|---|
| Managed by Gextto, or native on the same host with the same folders | same paths | *(empty)* |
| Docker container, host `/mnt/media/downloads` | `/downloads` | `/mnt/media/downloads=/downloads` |
| NAS, Gextto mount `/mnt/nas/downloads` | `/volume1/downloads` | `/mnt/nas/downloads=/volume1/downloads` |
| Gextto RAM disk enabled | its path inside qBittorrent | map the RAM disk path too |

## 4. What Gextto drives, and what stays in qBittorrent

The settings below live in Gextto's own *Configuration* tabs — there is
nothing to set twice. While this backend is active Gextto pushes its policy to
qBittorrent and reads the results back:

- **Speed limits** (*Speed and network*): the global download/upload caps,
  including the scheduled windows and the temporary limit, are re-applied to
  qBittorrent every minute.
- **Queue** (*Queues and performance*): the active-download slots are enforced
  by pausing and resuming; Gextto switches qBittorrent's own queueing off
  while it runs and restores the previous value on shutdown, so the two never
  fight.
- **Seed** (*Seeding and completion*): the global ratio and max seed time
  travel with every torrent, together with the per-torrent limits set in
  Downloads.
- **Per torrent**: pause/resume, recheck, reannounce, move, sequential and
  first/last pieces, super-seeding, file priorities, tracker add/remove, and
  the category/tag from §2.
- **Optimize** (*Queues and performance → Optimize*): applies a disk-cache
  profile sized from the machine's RAM — 128 MiB below 2 GB, 256 MiB below
  4 GB, 512 MiB below 8 GB, 1 GiB below 16 GB, 2 GiB above.

What stays configured **in qBittorrent itself** (*Tools → Options*), because
Gextto does not manage it:

- the **listening port** (and UPnP/NAT-PMP), **DHT, PeX and LSD**,
  **encryption mode**, **proxy** and **connection limits**;
- the **IP filter**: Gextto does not push its blocklist to this backend, so
  set it in qBittorrent (*Connection → IP filtering*) if you use one;
- **Alternative Rate Limits** and their scheduler: leave them off if you want
  Gextto's limits to be the only ones, otherwise qBittorrent caps below them
  during the alternative window;
- qBittorrent's own **RSS and search**: Gextto never reads them — a rule left
  enabled there downloads on its own, outside Gextto's library.

## 5. Managed mode

With **Downloaded and updated by Gextto** on, Gextto downloads the latest
qBittorrent-nox static release from
[`userdocs/qbittorrent-nox-static`](https://github.com/userdocs/qbittorrent-nox-static)
(libtorrent 2.x builds), verifies its SHA-256 against the digest published by
GitHub and installs it next to `gexttod`; the installed tag, name, asset and
hash are recorded in `install.json`. It then starts and stops qBittorrent with
the service; updates are applied with backup and rollback, and a watchdog
restarts it after an unexpected exit (after repeated crashes Gextto runs the
engine fallback chain). If qBittorrent is down at startup, Gextto keeps
retrying instead of failing. No path mapping is needed: both see the same
folders.

In managed mode you do not set the URL to a service you started yourself: Gextto
owns the process, so the URL is the one it starts (still editable, keeping the
default local address and port).

Nothing else needs to be installed on the host: the binary comes from the
release, and a manual qBittorrent installation is left untouched.

## 6. Switching engines

The engine is chosen in **Configuration → Torrent engine** and the switch is a
coordinated migration: Gextto exports a manifest of the managed torrents,
validates the target in dry-run, shuts down cleanly and restarts with the new
engine, which re-imports the manifest. Nothing is moved and no second engine
runs meanwhile.

Two things to know:

- **A torrent belongs to one engine at a time.** The data stays where it is and
  the target re-checks the existing files; nothing is downloaded twice.
- If gx-torrent cannot start, Gextto falls back to qBittorrent-nox by itself —
  using the managed one if no external instance is configured — so the
  transfer plane keeps working.

## 7. Verify and diagnose

- **Health → Torrent engine** summarises the active engine. With this backend
  it shows speeds, torrent counts and session totals — the port, DHT, cache
  and I/O detail is a gx-torrent page.
- The **Test / preflight** on the backend tile shows the connected version, the
  default save path and the torrent count, and lists the mappings in use.
- The **Downloads** screen and every automation behave the same as with the
  other engines; only the transfer plane changes.

Common problems:

| Symptom | What to check |
|---|---|
| **Connection refused / timeout** | qBittorrent is running; the URL host and port are right (Web UI port, not the BitTorrent port); the Web UI listens on the interface Gextto reaches (`WebUI\Address`); the firewall; in Docker the published port. |
| **`authentication failed: banned or too many attempts`** | qBittorrent bans an IP after repeated wrong logins: wait for the ban to expire, or clear it from the Web UI ban list, then fix the credentials in Gextto. |
| **`authentication failed`** (other) | wrong user or password; the password was written by hand in the configuration file instead of being set from the Web UI; "bypass authentication on localhost" is on but Gextto connects from another host. |
| **Activation refused: `path "…" is not covered by backend_path_mappings`** | add the missing mapping in §3.2 — including the temporary folder and the RAM disk, if used. |
| **"backend path … does not exist"** | with no mapping, Gextto checks the folder locally: create it, fix its permissions, or map it if qBittorrent sees a different path. |
| **Torrents Gextto did not add show up** | **Category** and **Tag** are both empty, so the instance is treated as dedicated (§2). Set one of them. |
| **Limits or the queue differ from Gextto** | qBittorrent's own **Alternative Rate Limits** or its scheduler are on; Gextto re-applies its limits every minute but does not touch those. |
| **TLS error with `https://`** | the certificate is not trusted: use a CA-signed certificate or a reverse proxy with one. |
| **Speeds, port, DHT, PeX, encryption look wrong** | they belong to qBittorrent, not Gextto: set them in *Tools → Options* (§4). |
| **A qBittorrent RSS rule downloads on its own** | Gextto never reads qBittorrent's RSS: disable the rule there. |

## 8. Reference

### Settings

| Key | Default | Meaning |
|---|---|---|
| `torrent_backend` | `gx-torrent` | active engine; `qbittorrent` selects this backend |
| `qbittorrent_managed` | `false` | managed mode (§5) |
| `qbittorrent_url` | `http://127.0.0.1:8080` | Web API base URL (may include a path prefix) |
| `qbittorrent_username` | `admin` | Web UI user |
| `qbittorrent_password` | *(empty)* | Web UI password, not displayed |
| `qbittorrent_category` | *(empty)* | category applied to added torrents; also an ownership filter |
| `qbittorrent_tag` | *(empty)* | tag applied to added torrents; also an ownership filter |
| `qbittorrent_request_timeout_secs` | `15` | request timeout (1–300 s) |
| `qbittorrent_poll_interval_ms` | `1500` | status polling interval (250–60000 ms) |
| `qbittorrent_path_mappings` | *(empty)* | `gextto_path=qbittorrent_path` lines |

The API is documented in [`API.md`](API.md) (`GET`/`POST /api/torrent-backend`,
`/api/torrent-backend/preflight`, `/api/torrent-backend/test`). The errors
`ErrPathMappingMissing`, `ErrAuth` and `ErrUnavailable` in the log map to the
rows above (the log names the offending path).

### What this backend does not do

The full matrix is in *Health → Torrent engine* (and in `capabilityLevels`,
the single source of truth in `torrent_engine.go`). Against gx-torrent:

| Capability | qBittorrent-nox |
|---|---|
| RAM disk | not available (its path still needs a mapping if the option is on) |
| Web seeds | not available |
| Holepunching (BEP 55) | not available |
| Per-torrent connection limits | not available (global only, in qBittorrent) |
| Super-seeding | partial |
| Seed policy | partial (ratio and time travel per torrent) |
| IP filter | partial: Gextto's blocklist is not pushed |
| Session totals | partial: what qBittorrent reports |
| Piece diagnostics and fast-resume | not available (gx-torrent only) |
