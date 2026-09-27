# Migrating an existing installation to gextto

gextto stores its databases and torrent state under its own names
(`gextto_series.db`, `gextto_archive.db`, `gextto_config.db`,
`gextto_comics.db`, `gextto_torrents_state/`). The `migrate` command copies a
previous installation into this layout **without touching the source**, so the
operation is reversible.

## What the migration does

`gexttod migrate --from <old-data-dir> --to <new-data-dir>`:

1. finds `<prefix>_series.db`, `<prefix>_archive.db`, `<prefix>_config.db`,
   `<prefix>_comics.db` in the source directory (any prefix);
2. copies each one with `VACUUM INTO` from a read-only connection, producing a
   consistent, self-contained `gextto_*.db` (safe even while the old daemon is
   running; falls back to a byte copy + checkpoint if needed);
3. copies `*_torrents_state/` to `gextto_torrents_state/` (fastresume and
   `.torrent` files, plus `seed_limits.json`);
4. copies a single `*.json` configuration file to `gextto.json` when present;
5. prints a JSON report with the paths and any warnings.

The source directory is only read.

## Procedure

### 1. Stop the old daemon

```bash
sudo systemctl stop <old-service>
# or, when running in the foreground: Ctrl-C
```

### 2. Migrate the data

```bash
gexttod migrate --from /old/data --to /new/data
```

### 3. Trial run (no downloads)

```bash
GEXTTO_DATA_DIR=/new/data GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 gexttod
curl --fail http://127.0.0.1:5000/api/status
curl --fail http://127.0.0.1:5000/api/health
```

Check that the library, the archive and the torrent session are restored, then
stop the trial (`Ctrl-C`).

### 4. Go live

```bash
GEXTTO_DATA_DIR=/new/data GEXTTO_ACTIVE=1 GEXTTO_DRY_RUN=0 gexttod
```

### 5. Install as a service

A system unit (replace the paths and the user):

```ini
[Unit]
Description=Gextto media acquisition and archiving daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gextto
Group=gextto
WorkingDirectory=/var/lib/gextto
ExecStart=/opt/gextto/gexttod
Environment=GEXTTO_DATA_DIR=/var/lib/gextto
Environment=GEXTTO_LISTEN=0.0.0.0:5000
Environment=GEXTTO_ENGINE_LISTEN=127.0.0.1:8889
Environment=GEXTTO_ACTIVE=1
Environment=GEXTTO_DRY_RUN=0
Environment=GEXTTO_LOG=info
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
TimeoutStopSec=90

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now gextto.service
```

A per-user unit (`~/.config/systemd/user/gextto.service`) works too, but it only
starts at login unless lingering is enabled: `sudo loginctl enable-linger <user>`.

## Verification

```bash
systemctl status gextto.service            # or: systemctl --user status gextto
curl --fail http://127.0.0.1:5000/api/status
curl --fail http://127.0.0.1:5000/api/health
journalctl -u gextto.service -f            # or: journalctl --user -u gextto -f
```

The status payload must report `"name": "gextto"`, `"active": true` and the
expected `seen` counts; the torrent session should list the migrated torrents
(`/api/torrents`).

## Rollback

Because the source was never modified:

```bash
systemctl --user stop gextto.service       # or: sudo systemctl stop gextto.service
sudo systemctl start <old-service>         # the old installation resumes
```

The old databases, configuration and torrent state are intact.

## Notes

- Make sure only one daemon binds the UI and engine ports at a time.
- The archive database can be large; the migration needs free space roughly
  equal to its size. `VACUUM INTO` also compacts it.
- Archive/NAS paths stored in the configuration are copied unchanged; adjust
  them in *Configuration* if the mount points differ.
- Run one verification cycle before deleting anything: keep the old data
  directory as a backup.
