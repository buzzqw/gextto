# Gextto terminal UI (TUI)

Terminal interface for a Gextto daemon that is already running. It speaks the
same six languages as the web UI (Italian, English, German, French, Spanish,
Polish), offers filtering and sorting of the downloads, details, SSE logs, live
metrics and non-intrusive notifications; it never reads the databases directly.

> [!NOTE]
> The TUI uses the daemon's HTTP API. The daemon must therefore be running and
> reachable at the given URL before the TUI is opened.

## Starting

```bash
/opt/gextto/gexttod tui                    # standard installation
/opt/gextto/gexttod tui --url http://host:5000 --lang en
GEXTTO_URL=http://host:5000 GEXTTO_LANG=it /opt/gextto/gexttod tui
```

With an installation other than the standard one, replace `/opt/gextto` with
the directory that contains `gexttod`.

The language is chosen in this order: `--lang`, `GEXTTO_LANG`, the daemon's
active language (`/api/i18n/active`), otherwise Italian. It can also be changed
from the settings panel (`g`).

If the daemon has protected access enabled and the TUI runs outside the local
network, pass the API key with `GEXTTO_API_KEY=<key>`: it is sent in the
`X-Api-Key` header of every request. From the local network it is not needed.

## Tabs

1. **Status** — dashboard: daemon (version, mode, uptime, PID, CPU/RAM), system
   (CPU, load, RAM, disk, trash), cycle (countdown, last start and counters),
   feeds, downloads by state, traffic with its trend, consumption, an
   **Attention** line with what needs action (torrents in error, unwritable
   paths, almost full disk, last error), the transfers in progress with bar and
   ETA and, anchored at the bottom, the **latest log lines**. If the window is
   short the upper part scrolls with `↑↓`/`PgUp`/`PgDn`.
2. **Torrents** — unified list of torrents and HTTP comic downloads with state,
   progress, bytes, speed and name; filter (`/` or `F`), sort (`o`/`O`),
   torrent details (`Enter`), pause/resume and removal of HTTP downloads. Several
   torrents can be marked (`Space`, `*` all/none) and paused, resumed, rechecked,
   tagged or removed together; `#` tags, `A` sets the auto-removal, `H` opens the
   history, `b` restarts. The header shows the bandwidth limits in force (base,
   scheduled or temporary with the minutes left). `T` sets a temporary limit as
   in the web UI: `DL UL [minutes]` in KiB/s, where `0` is unlimited and without
   minutes (or with `0`) the limit stays until you remove it; `off` removes it
   and restores the normal limits. The maximum duration is 1440 minutes.
3. **Logs** — latest lines, filter (`/`), warnings/errors only (`w`), follow
   (`f`), live SSE stream.
4. **Health** — CPU/RAM/disk, paths, disks, RAM disk, latest errors; `x` empties
   the trash (with confirmation).
5. **Archive** — archive of the releases, paged, with filter, sort, details
   and direct queueing.
6. **Missing** — missing episodes of the monitored library: `Enter` opens the
   series on the episode, `s` searches it on the indexers, `i` ignores it.
7. **Blocklist** — blocked releases, with interactive removal.
8. **Library** — monitored TV series, movies and comics. Series and movies are
   managed from here: adding by searching TMDB, editing the requirements with a
   field form (for series, among others, *No upgrades*), pause and deletion.
   The detail of a series shows one season at a time with the state of each
   episode (✓ present, ✗ missing, ↓ downloading, `·` upcoming, `-` ignored);
   from there seasons are switched on or off, releases are searched and
   queued, episodes are ignored or downloaded again, metadata is refreshed and
   files are renamed. The detail of a movie shows plot, history and the
   releases already in the archive. The list refreshes every 10 seconds;
   actions reload at once what they change. Comics stay read-only.
9. **Maintenance** — the maintenance actions of the web UI: backup, database
   check, compaction (VACUUM) and statistics (ANALYZE), inferior duplicates
   (preview, then `x` deletes), rename all series or one folder (preview with
   `Space` to include/exclude, `x` applies), scan all archives, refresh
   MediaInfo, database housekeeping, RAM disk and empty trash. Heavy operations
   ask for confirmation; a long job shows its progress and `Enter` cancels it.

Completions, errors, stalls and archivings arrive as a temporary message in the
bottom bar. The TUI also shows the same state changes for the HTTP comic
downloads.

The **settings panel** (`g`) changes the interface language, the time between
searches, the download/upload limits and test mode (dry-run); `c` turns colours
on or off and `h` switches the high-contrast theme.

## Keyboard shortcuts

| Scope | Keys |
| --- | --- |
| Global | `1`-`9`/`Tab` tabs · `l` Library · `g` settings · `r` refresh · `?` help · `q` quit |
| Adding | `a` magnet/URL · `t` `.torrent` file · `c` cycle · `s` search · `e` events |
| Torrents | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `Enter` torrent details · `p` pause/resume · `b` restart · `d`/`D` remove (torrent without or with files; HTTP from the list) · `X` clean completed · `k` recheck · `R` reannounce · `n` no-rename · `i`/`u` pin/unpin · `L` global limits · `T` temporary limit · `Space` mark · `*` mark all/none · `#` tag · `A` auto-remove · `H` history · `o`/`O` sort · `/` or `F` filter |
| Details | `1` general · `2` trackers · `3` files · `4` peers · `↑↓` scroll · `F` discard and find another version · `Esc`/`Enter` back; files: `Space` skip/download, `+`/`-` priority; trackers: `a` add, `d` remove |
| Logs | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` · `/` filter · `w` warnings/errors only · `f` follow/pause |
| Status | `↑↓`/`PgUp`/`PgDn`/`Home`/`End` scroll |
| Health | `↑↓`/`PgUp`/`PgDn` scroll · `x` empty trash (with confirmation) |
| Terminal | `Ctrl-L` redraws everything · pasting a magnet/URL opens "add" already filled in |
| Archive | `↑↓` select · `PgUp`/`PgDn` page · `Enter` queue · `d` details · `o`/`O` sort · `s` search |
| Missing | `↑↓` select · `Enter` open the series · `s` search the episode · `i` ignore · `r` refresh |
| Blocklist | `↑↓` select · `d` remove |
| Library | `←→` series/movies/comics · `↑↓` select · `s`/`/` filter · `Enter` details · `a` add from TMDB · `e` edit · `p` pause/resume · `d` delete · `m` search (the series' missing episodes / the movie) |
| Series | `←→` season · `Space` season on/off · `↑↓` episode · `Enter` sources in the archive · `s` search online · `i` ignore · `R` download again · `y` copy magnet · `m` search missing · `M` TMDB metadata · `n` rename (preview + confirmation) · `e` edit · `p` pause · `d` delete · `Esc` back |
| Movie | `↑↓` releases in the archive · `Enter` queue · `s` search online · `y` copy magnet · `e` edit · `R` download again · `p` pause · `d` delete · `Esc` back |
| Maintenance | `↑↓` select · `Enter` run (heavy operations ask for confirmation) |
| Settings | `↑↓` select · `Enter` edit · `c` colours · `h` contrast · `Esc` close |
| Forms | `↑↓` field · `Enter` edit (yes/no: `Enter` or `Space`) · `s` save · `Esc` cancel; `*` marks the changed fields |

Confirmations (removals, trash, cleanup) accept only `s`/`y`: `Enter` cancels
and pasted text is ignored, so a repeated `Enter` on a slow link cannot delete
a torrent together with its files.

## Use over SSH

The TUI is designed for an SSH session:

- **Bandwidth.** Each frame sends only the lines that changed, and of a line
  only the final part that changes; when the log scrolls the terminal scrolls
  the region and only the new lines arrive. Frames are synchronised updates (no
  flicker). Health and statistics are polled every 10 seconds instead of on
  every refresh. Bottom right the TUI shows the bandwidth it is using, for
  example `TUI ↓7.2KB/s ↑230B/s · 1.5 req/s`: ↓ is what it receives (daemon
  answers and keys), ↑ what it sends (screen and requests), `req/s` the
  requests to the daemon per second.
- **Word wrap.** Logs, details, health and dashboard wrap instead of being cut;
  in the lists the selected line shows the whole title. Widths are computed in
  columns, so titles with ideograms or emoji do not break the layout.
- **Safe text.** Escape sequences and control characters in torrent names,
  feed titles or logs are removed before they reach the terminal.
- **Paste.** Bracketed paste is on: a pasted magnet arrives whole in the field,
  and outside a field it opens "add" instead of running commands.
- **Copy (`y`).** It uses OSC52, which copies to the clipboard of the computer
  you connect from. In tmux you need `set -g set-clipboard on` (or
  `allow-passthrough on`); the TUI also sends the sequence in the tmux and GNU
  screen passthrough form.
- **Locale.** With a non-UTF-8 locale (e.g. `LANG=C`, frequent when SSH does not
  forward `LC_*`) the TUI draws in ASCII only. It can be forced with
  `GEXTTO_TUI_ASCII=1` or `GEXTTO_TUI_ASCII=0`.
- **Slow links.** If the arrows turn into letters, raise the wait for escape
  sequences: `GEXTTO_TUI_ESCDELAY=250` (milliseconds, default 100).
- **Disconnection.** If the connection drops (SIGHUP or terminal closed) the TUI
  exits by itself. Resizing the window redraws at once.
- Without an interactive terminal the TUI stops with a message: remotely use
  `ssh -t host /opt/gextto/gexttod tui`.

Other variables: `NO_COLOR` (or `TERM=dumb`) turns colours off,
`GEXTTO_TUI_THEME=high-contrast` turns the high-contrast theme on.

## Architecture

`internal/tui`:

- `i18n.go` — six-language catalog, `Translator`, language resolution.
- `client.go`, `client_torrents.go`, `client_library.go`,
  `client_maintenance.go` — typed HTTP client of the daemon APIs.
- `format.go` — formatting of bytes, durations, optional numbers.
- `text.go` — column width, removal of control characters, wrapping, ASCII
  fallback.
- `bandwidth.go` — measurement of the bandwidth the TUI uses (footer).
- `model.go` / `update.go` / `render.go` (with `render_library.go`,
  `library.go`, `downloads.go`, `maintenance.go`) — pure state, transitions
  from keys and rendering into lines with semantic style (testable without a
  terminal).
- `keys.go` — decoding of the byte stream into keys (complete CSI sequences with
  modifiers, bracketed paste, UTF-8).
- `term.go` — raw mode, alternate screen and incremental renderer.
- `run.go` (with `run_downloads.go`, `run_library.go`, `run_maintenance.go`) —
  main loop: polling, SSE stream, execution of the actions.

The TUI talks **only** to the daemon's HTTP API and never touches the
databases.

Text prompts also support `←→`, `Home`/`End`, `Delete`, `Ctrl-A`/`Ctrl-E`
(start/end), `Ctrl-U` (clear), `Ctrl-K` (delete to the end) and `Ctrl-W`
(delete the previous word).

## Tests

```bash
go test ./internal/tui/...
go test -race ./internal/tui/...
go test . -run TestTUILibraryClientAgainstTheRouter   # client against the real API
```

They cover the i18n catalog, formatting, the client (with a test HTTP server
and against the daemon's real router), key decoding and every transition of the
model.
