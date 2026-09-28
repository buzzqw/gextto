# Gextto

Gextto is a self-hosted daemon for automatically searching, downloading and
archiving TV series, movies and comics. It is managed through the web UI.

> Italiano: [`README.it.md`](README.it.md) · Full manual:
> [`docs/MANUAL.en.md`](docs/MANUAL.en.md)

## What Gextto is

Gextto automates the complete library workflow: it searches for releases,
selects the best one, downloads it, checks the file, renames it and archives it
in the configured location.

## Why choose Gextto

- **One service**: UI, database, search, torrent queue and archiving work
  together without an external orchestrator.
- **Embedded torrents**: libtorrent is included and needs no extra service;
  qBittorrent-nox and anacrolix are also supported.
- **Quality-based decisions**: resolution, source, codec, audio, HDR, languages
  and size are evaluated before downloads and upgrades.
- **Real file inspection**: `ffprobe`/MediaInfo check the actual file's codec,
  audio, HDR and languages instead of trusting only its release name.
- **TV, movies and comics**: TMDB/TVDB metadata, missing episodes, calendar,
  manual search, renaming and NAS paths.
- **Simple management**: responsive web UI in Italian and English, backups,
  logs, health checks and Trakt, Simkl, Jellyfin and Plex integrations.

## Installation

### Official Linux installation

On a 64-bit Linux server with systemd, run the installer as **root** (through
`sudo` or from a root shell):

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

The installer configures the service and keeps data and configuration in
`/var/lib/gextto`. The UI will be available at `http://<server>:5000`.

### Source installation

See [Development and local installations](docs/DEVELOPMENT.md).

## Updating

### Official installation

Run the installer command again:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

### Source checkout

```bash
cd /path/to/gextto
git pull --ff-only
./scripts/update.sh
```

The script builds the latest version and restarts the service. Databases,
configuration, downloads and archives are not deleted.

Check that the service is running:

```bash
curl -fsS http://127.0.0.1:5000/api/health
```

## First run

1. Open the web UI and complete the initial setup.
2. Configure paths, sources and TMDB/TVDB keys.
3. Keep dry-run mode enabled until the configuration is verified.
4. Enable active mode when you are ready to start downloading.

For detailed configuration, see the
[user manual](docs/MANUAL.en.md).

## Features

- TV series, movies and comics management;
- automatic and manual release search;
- torrents with embedded libtorrent or alternative backends;
- renaming, archiving and real media-file inspection;
- missing-episode search, calendar, backups and maintenance;
- Trakt, Simkl, Jellyfin, Plex and notification integrations.

## Documentation

- [English user manual](docs/MANUAL.en.md)
- [Manuale utente italiano](docs/MANUAL.it.md)
- [HTTP API](docs/API.md)
- [Migration from an existing installation](docs/MIGRATION.md)
- [Security policy](docs/SECURITY.md)
- [Terminal TUI](docs/tui.md)
- [Development and local installations](docs/DEVELOPMENT.md)

## License

See [LICENSE](LICENSE).
