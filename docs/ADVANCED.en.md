# Gextto advanced operations

Use this guide for NAS storage, multi-machine deployments, reverse proxies and
recovery. For day-to-day use, see the [user manual](MANUAL.en.md).

> [!IMPORTANT]
> Before changing storage or a torrent backend, pause automatic cycles (or use
> dry-run), make a backup, validate paths as the service user, then test one
> change at a time.

## Paths and permissions

The service must be able to read and write downloads, the archive, trash and
the data directory. Check paths in **Health** before enabling downloads. A path
visible to the client but not to the daemon cannot be used.

For a NAS, verify UID/GID, mounts at boot and read/write permissions. After
moving a library, use **Maintenance → Scan archives** before starting upgrades
or missing-episode searches. The scan runs in the background: you can keep
working and see the outcome when it finishes.

## Network and security

Gextto has no built-in user authentication. Bind it to `127.0.0.1:5000` when
possible. For remote access, use a firewall and an authenticated HTTPS reverse
proxy.

Do not expose the administrative port directly to the Internet. See
[Security](SECURITY.md) for details.

## Torrent backends

- **Embedded libtorrent**: the default, with no external service;
- **qBittorrent-nox**: useful when the torrent engine should run separately;
  before using it in production.

A torrent belongs to one backend at a time. Pause downloads before switching,
run the backend preflight, and verify shared paths or NAS mappings.

## MediaInfo and ffprobe

`ffprobe` is optional but recommended. Gextto probes only real, regular video
files; stale database references are ignored. Detected data (codec, audio, HDR,
languages and resolution) is used for quality comparisons and upgrades.

If the backfill reports missing files, check the archived path, the NAS mount
and the service permissions. Do not create empty files to satisfy the database:
the probe must inspect the real media file.

## Backup and restore

A backup contains databases and configuration, not videos or the complete
torrent session state. Before restoring:

1. stop or pause automatic cycles;
2. keep a copy of the current database;
3. verify that library paths are available;
4. restore the backup;
5. scan the archive and check **Health**.

## Source updates

```bash
git pull --ff-only
./scripts/update.sh
```

The script rebuilds and restarts the service without changing data. For official
installations, use the installer described in the README instead.

## Quick diagnosis

Check these in order:

1. `curl -fsS http://127.0.0.1:5000/api/health`;
2. `curl -fsS http://127.0.0.1:5000/api/status`;
3. path status and permissions in **Health**;
4. sources and indexers with **Verify**;
5. logs filtered for `ERROR`, `WARN`, `torrent` or `ffprobe`;
6. service status with `systemctl` or `systemctl --user`.

Do not delete databases or torrent state to solve an error without making a
backup first.
