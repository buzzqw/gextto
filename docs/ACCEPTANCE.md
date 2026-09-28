# Release checklist

Run this before publishing a build or a release tag. The automated smoke test is
[`scripts/acceptance.sh`](../scripts/acceptance.sh); this document is the full
manual checklist around it.

## 1. Source hygiene

- No data, logs, build artefacts or secrets are tracked:

  ```bash
    git ls-files | grep -Ei '(^|/)(data|bin)/|\.db($|-)|\.log|build_number|\.env|secret'
  # expected: no output
  ```

- `.gitignore` covers `data/`, `bin/`, `*.db*`, `*.log*`, `build_number`, `.env*`.
- No API keys, tokens or personal paths in tracked files.

## 2. Build

```bash
make build        # -> bin/gexttod (embedded web UI)
./bin/gexttod --version
```

The server-rendered web UI is embedded in the binary from `uiweb/templates` and
`uiweb/static`; no separate frontend build is required.

## 3. Tests

```bash
make test         # CGO_ENABLED=1 go test ./...
make test-real    # hermetic local seeder/tracker/leecher transfers
```

All tests must pass. The suite covers parser/quality/scoring, database and
migrations, archive search, import, RSS/listing parsing, MediaInfo handling,
filename building, log rotation, notifier formatting, config/API auth and a
smoke test of every registered GET route (no endpoint may return a server
error).

## 4. Runtime smoke test (dry-run)

```bash
scripts/acceptance.sh
```

Or manually:

```bash
GEXTTO_DATA_DIR="$PWD/data-acceptance" GEXTTO_ACTIVE=0 GEXTTO_DRY_RUN=1 \
  ./bin/gexttod --dry-run
curl --fail http://127.0.0.1:5000/api/status
curl --fail http://127.0.0.1:5000/api/health
```

Check, in order:

- the UI loads (dashboard, settings, health, logs);
- a manually triggered cycle runs and ends with a `CYCLE REPORT` line;
- *Configuration → Sources* shows the configured feeds/indexers;
- *Maintenance → Database* VACUUM/ANALYZE runs on **all** databases;
- *Maintenance → Backups* *Test FTP* reports every step;
- `/feed.xml` returns a valid magnet feed.

## 5. Service install

```bash
sudo ./install.sh
sudo systemctl status gextto.service
sudo journalctl -u gextto.service -f
```

The installer creates the dedicated service account, runtime directories, the
systemd unit and the initial databases. For a per-user install without root, use
[`scripts/install-user-service.sh`](../scripts/install-user-service.sh).

## 6. Enable real downloads

Only after the checks above, set `GEXTTO_ACTIVE=1` / `GEXTTO_DRY_RUN=0` and make
sure no other service is using the web/engine ports.

## 7. Command line and self-update

```bash
bin/gexttod --version   # daemon + build + libtorrent
bin/gexttod --help
```

`--update` must preserve the data directory and stay atomic. Test it against a
local archive and a throwaway install directory:

```bash
scripts/package-linux.sh --binary bin/gexttod \
  --output /tmp/gextto-linux-x86_64.tar.gz
mkdir -p /tmp/gextto-update-check
bin/gexttod --update \
  --archive /tmp/gextto-linux-x86_64.tar.gz \
  --install-dir /tmp/gextto-update-check --no-restart
/tmp/gextto-update-check/gexttod --version
```

The archive must contain `gexttod`, `lib/`, `run.sh` and `README.md`:

```bash
tar -tzf /tmp/gextto-linux-x86_64.tar.gz
```

Finally, extract it on a clean machine or container **without** libtorrent
installed and confirm `./run.sh --version` and a dry-run start work, proving the
bundled library and the embedded UI are complete.
