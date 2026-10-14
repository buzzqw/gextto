# Configuring qBittorrent-nox for Gextto

Gextto can drive an **external qBittorrent-nox** through its Web API. Gextto
keeps its own database, queue, scoring, post-processing, renaming and archive:
only the transfer plane is qBittorrent. A torrent belongs to one engine at a
time, so switching backend is a migration, not two clients on the same data.

Use this backend when the torrent engine must run on its own (a separate
service, a different machine, a container) or when you already run
qBittorrent-nox and want Gextto to automate it.

> The addresses, ports and folders below are examples. Always check the real
> service user, the NAS mount and the permissions in your install.

## 1. Prepare qBittorrent-nox

- Enable the **Web User Interface** (qBittorrent-nox serves it by default).
- Set a **username and password**: Gextto authenticates with them.
- Note the **Web UI port** (default `8080`) and whether it answers on
  `127.0.0.1` only or on the LAN. If Gextto and qBittorrent are on different
  hosts, the Web UI must be reachable from Gextto's host.
- If you enable "Bypass authentication for clients on localhost" and Gextto
  connects from localhost, the credentials are not needed, but setting them is
  still recommended.

## 2. Configure Gextto

Open **Settings → Torrent engine → qBittorrent-nox** and set:

| Setting | Meaning |
|---|---|
| **Web API URL** | qBittorrent Web UI URL, e.g. `http://127.0.0.1:8080` |
| **User / Password** | qBittorrent Web UI credentials |
| **Category** | Category applied to every torrent Gextto adds (created if missing) |
| **Tag** | Tag applied to every torrent Gextto adds |
| **Request timeout** | Seconds for a Web API request (default 15) |
| **Polling interval** | Milliseconds between status reads (default 1500) |
| **Path mapping** | `gextto_path=qbittorrent_path`, one per line (see below) |
| **Downloaded and updated by Gextto** | Managed mode: Gextto downloads the latest qBittorrent-nox, starts/stops it with the service and updates it |

The **Test** button runs the preflight: it connects, reads the version, the
default save path and the number of torrents. **Activation is refused** when a
Gextto path the backend would receive is not covered by a mapping, so a move
can never target the wrong folder.

## 3. Paths: the part that matters

Gextto computes a save path for every torrent (the configured download folder,
or a per-title/per-rule folder). It hands that path to qBittorrent **in
qBittorrent's own filesystem namespace**. The two only agree when both see the
same folders.

### 3.1 Same host, same folders — no mapping

If qBittorrent runs natively on the same machine and the download folder is the
same, leave **Path mapping** empty. Gextto only checks that the folder exists
locally. Example:

```
Gextto download folder:      /mnt/media/downloads
qBittorrent sees the same:   /mnt/media/downloads   -> no mapping
```

### 3.2 Different path, same files — map it

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
| Native, same host, same folders | same paths | *(empty)* |
| Docker container, host `/mnt/media/downloads` | `/downloads` | `/mnt/media/downloads=/downloads` |
| NAS, Gextto mount `/mnt/nas/downloads` | `/volume1/downloads` | `/mnt/nas/downloads=/volume1/downloads` |
| Gextto RAM disk enabled | its path inside qBittorrent | map the RAM disk path too |

## 4. Managed mode

With **Downloaded and updated by Gextto** on, Gextto downloads the latest
qBittorrent-nox static release, installs it next to `gexttod`, and starts/stops
it together with the service; updates are applied with backup and rollback, and
a watchdog restarts it after an unexpected exit (after repeated crashes Gextto
runs the engine fallback chain). If qBittorrent is down at startup, Gextto
keeps retrying instead of failing.

In managed mode you do not set the URL to a service you started yourself: Gextto
owns the process, so the URL is the one it starts (still editable, keeping the
default local address and port).

## 5. Verify and diagnose

- **Health → Torrent engine** summarises the active engine (queue, speed, port,
  DHT/uTP/LSD, encryption, proxy, IP filter, cache, I/O).
- The **Test / preflight** on the backend tile shows the connected version, the
  default save path and the torrent count, and lists the mappings in use.
- `ErrPathMappingMissing` in the log (or "backend path … does not exist;
  configure qbittorrent_path_mappings") means a required path is not covered or,
  with no mapping, does not exist locally.
- The **Downloads** screen and every automation behave the same as with the
  other engines; only the transfer plane changes.
