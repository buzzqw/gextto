# Gextto

Gextto is a self-hosted daemon for automatically searching, downloading and
archiving TV series, movies and comics. It is managed through the web UI.

> Italiano: [`README.it.md`](README.it.md) · Full manual:
> [`docs/MANUAL.en.md`](docs/MANUAL.en.md)

## Installation

### Official Linux installation

On a 64-bit Linux server with systemd:

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
