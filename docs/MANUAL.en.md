# Gextto user manual

This manual explains **how to use Gextto**: from installation to the first
download, from the series you follow every week to looking after the library.
It is organised by task — *add a series*, *find out why an episode did not
arrive*, *make a backup* — rather than by page of the interface. The full list
of every setting, field and button is in the appendices at the end.

The UI is available in Italian, English, German, French, Spanish and Polish;
this manual uses the English labels and is also the manual for German, French,
Spanish and Polish. The Italian version is [MANUAL.it.md](MANUAL.it.md). The
**Manual** entry in the interface opens this document online, in the chosen
language; **Changelog** opens the release notes. Neither is built into the
program: they are updated without updating Gextto.

> [!IMPORTANT]
> Start in **test mode** (dry-run): Gextto searches and decides, but downloads
> nothing. Check paths, sources and one test title before switching to real
> downloads. The web interface is an administration panel and is open by
> default: keep it on a trusted network or, for access from outside, turn on the
> login and use HTTPS (see [Access and security](#20-access-and-security)).

## Contents

**Part I — Getting started**

1. [What Gextto is and how it works](#1-what-gextto-is-and-how-it-works)
2. [Installing Gextto](#2-installing-gextto)
3. [The initial setup](#3-the-initial-setup)
4. [The first download, in test mode and then for real](#4-the-first-download-in-test-mode-and-then-for-real)
5. [Finding your way around the interface](#5-finding-your-way-around-the-interface)

**Part II — Everyday use**

6. [Following a TV series](#6-following-a-tv-series)
7. [Following a movie](#7-following-a-movie)
8. [Searching and downloading by hand](#8-searching-and-downloading-by-hand)
9. [Watching the downloads](#9-watching-the-downloads)
10. [Where the files end up](#10-where-the-files-end-up)
11. [Comics](#11-comics)
12. [eD2k and Kad with aMule](#12-ed2k-and-kad-with-amule)

**Part III — Configuring**

13. [Sources: feeds and indexers](#13-sources-feeds-and-indexers)
14. [Quality, scores and upgrades](#14-quality-scores-and-upgrades)
15. [The torrent engine](#15-the-torrent-engine)
16. [Speed and network](#16-speed-and-network)
17. [Library, NAS and space](#17-library-nas-and-space)
18. [Notifications](#18-notifications)
19. [Integrations with other programs](#19-integrations-with-other-programs)
20. [Access and security](#20-access-and-security)

**Part IV — Keeping everything in order**

21. [Checking that everything works](#21-checking-that-everything-works)
22. [Looking after the library](#22-looking-after-the-library)
23. [Backup and restore](#23-backup-and-restore)
24. [Updating Gextto](#24-updating-gextto)
25. [Using Gextto from the terminal (TUI)](#25-using-gextto-from-the-terminal-tui)

**Part V — When something goes wrong**

26. [Troubleshooting](#26-troubleshooting)
27. [Glossary](#27-glossary)

**Appendices — Reference**

- [A. Configuration, setting by setting](#appendix-a-reference--configuration)
- [B. Integrations](#appendix-b-reference--integrations)
- [C. Maintenance](#appendix-c-reference--maintenance)

---

# Part I — Getting started

## 1. What Gextto is and how it works

Gextto is a **single service** that runs on an always-on computer (a mini PC, a
server, a NAS): you tell it which series and movies you are interested in, and
it searches for them, picks the best version according to your rules,
downloads it, gives it a tidy name and puts it in your library. You control it
from the browser (`http://<host>:5000`) or from the terminal. No other
components are needed: the torrent engine, **gx-torrent**, is included and
Gextto starts it by itself.

### The path of an episode

Every download follows the same road. Knowing it tells you where to look when
something goes wrong.

1. **Followed title.** You add a series or a movie to the library, with a
   quality profile (resolution, language, subtitles, audio).
2. **Search cycle.** At regular intervals Gextto reads the feeds and queries
   the indexers (Jackett, Prowlarr, indexarr…) for the titles you follow. Each
   result is a **release**.
3. **Choice.** Every release goes through a series of checks: is it the right
   title? does it meet quality and language? is an equal or better copy already
   in the library? Among the valid ones, the release with the highest **score**
   wins.
4. **Download.** The chosen release goes to the torrent engine (or to aMule,
   for eD2k). The **Download** page shows its progress.
5. **Into the library.** When the download finishes, Gextto renames the file
   with your template and copies, moves or links it into the series or movie
   folder. If the library held a worse version, that one goes to the **trash**.
6. **Seeding.** The torrent keeps sharing until it reaches the seeding limits,
   then it leaves the session.

### The words you will use

| Word | What it means |
|---|---|
| **Release** | a result found by a feed, an indexer or a web engine |
| **Cycle** | one automatic pass of searching and choosing |
| **Test mode** (dry-run) | Gextto searches and decides, but starts no download |
| **Torrent session** | the torrents the engine is still handling (downloading or seeding) |
| **Library** | the final, tidy folders with series and movies (often on a NAS) |
| **Archive** | the local catalogue of the releases seen in the feeds, searchable |
| **Trash** | where replaced or removed files go, before they are deleted for good |
| **Upgrade** | replacing a file in the library with a better version |

Keep these four sets apart: **torrent session**, **library**, **archive** and
**trash**. Cleaning one does not touch the others: removing a torrent from the
session does not delete the file from the library, and emptying the archive
does not delete any video.

## 2. Installing Gextto

You need Linux with systemd, on x86_64 or aarch64. Gextto is written in pure
Go and compiles nothing on the server.

### With the installer (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

The installer downloads the latest published version, verifies its SHA-256
checksum and installs it:

- the program in `/opt/gextto`, the data in `/var/lib/gextto`;
- the `gextto.service` service, started right away;
- the base packages under the right names for Debian, Ubuntu, Fedora, openSUSE
  and Arch and, when the distribution provides them, `ffmpeg`, `mediainfo` and
  `mkvtoolnix` (used to read the real quality of the files, check their
  integrity and clean the audio tracks). `--no-media-tools` skips them.

The service runs as **the user who runs the installer**, so it can write into
your media folders. For a separate account use `--user NAME`, or `--user
gextto` for the isolated system account without login (with `--media-group` to
give it access to other users' media).

Other useful options:

| Option | Effect |
|---|---|
| `--channel stable` / `--release <tag>` | installs the stable version or a specific release instead of the latest |
| `--local-archive <file>` | installs from an archive already downloaded, without network |
| `--no-media-tools` | does not install ffmpeg, mediainfo and mkvtoolnix |
| `GEXTTO_REPO`, `GEXTTO_RELEASE` | choose another repository or another release |

An optional `/etc/gextto/gextto.env` file, when present, is loaded as the
service's environment variables.

### Without root, from a checkout

```bash
make build
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

This is a **user** systemd service. If it must stay on after you log out, run
`loginctl enable-linger "$USER"`.

### With Docker

Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr and aMule together in one
composition: see [Docker: full stack](DOCKER.en.md).

### Checking that it runs

Open `http://<host>:5000`. If the page does not open:

```bash
systemctl status gextto            # system installation
systemctl --user status gextto     # user service
/opt/gextto/gexttod --version      # installed version
```

Gextto accepts **one instance per data folder**: if another Gextto already uses
the same folder, or port 5000 is taken, the new process stops at once and says
so in the log, without touching the databases.

## 3. The initial setup

The first time you open the interface the **Initial setup** starts, in five
steps. You can skip it and reopen it whenever you like from `/?view=setup`; it
no longer appears on an installation that already has titles or an archive.

1. **Access.** Choose a password. You can leave access from the home network
   free and ask for it only from outside.
2. **Folders.** Set library, downloads, temporary folder and trash. **Browse**
   opens the server's folders (and creates new ones); **Verify** checks, as the
   service user, that each folder exists and is writable, and shows the disk
   type (NAS included), the free space and whether it sits on the same disk as
   the library.
3. **Sources.** An indexer (Prowlarr, Jackett or indexarr) and the **TMDB** key,
   recommended, for titles, posters and episodes. The **TVDB** key, with its PIN
   if you have one, also works as an alternative.
4. **First title.** Search for a series or a movie and add it.
5. **Activate.** Turn on the automatic cycle and choose whether to leave test
   mode (**Really download**). For the first round, keep test mode on.

### The folders and their role

| Folder | What it is for | Notes |
|---|---|---|
| **Downloads** | the data of the torrents in the session | do not delete it by hand while a torrent is active |
| **Temporary** | incomplete data and metadata | must be writable; can be on a fast disk |
| **Library** | the final files, sorted by series and movie | usually on the NAS |
| **Trash** | replaced or removed files | emptied according to the retention |
| **Watched folders** | `.torrent` and `.magnet` files to import | optional |

Two tips that avoid most problems:

- **Test the permissions as the service user**, not as yourself. A folder you
  can see from your shell may not be visible to the systemd service.
- **Put downloads and library on the same filesystem** if you can: Gextto can
  then use hardlinks, and a file still seeding enters the library at once
  without taking space twice (see [Where the files end up](#10-where-the-files-end-up)).

## 4. The first download, in test mode and then for real

Make one change at a time: if you change paths, quality and sources together, a
problem becomes hard to reproduce.

1. **Check the folders** in *Configuration → Archive and space* and in
   *Configuration → Library and renaming*.
2. **Add a single source** in *Configuration → Sources* and press **Verify**.
   Add the others only once this one works (see
   [Sources](#13-sources-feeds-and-indexers)).
3. **Add a test title**: a series with a single season, or a movie.
4. **Start a cycle** from the Dashboard (*Automatic search*, only the domain
   you care about: Series, Movies or Comics).
5. **Read the Log**: you should see the cycle, the sources queried and, for
   every release, why it was accepted or rejected.
6. **Run a manual search** for the same title and open **Why not this one?** on
   a result (see [Searching and downloading by hand](#8-searching-and-downloading-by-hand)).
7. **Switch to real downloads**: reopen the last step of the initial setup
   (`/?view=setup&step=5`; the **DRY-RUN** badge at the top reminds you that you
   are in test mode), tick **Really download** and press **Complete**. It takes
   effect at once, without a restart; in the TUI the same switch is under
   *Settings*. A Gextto started with `--dry-run` on the command line stays in
   test mode until it is restarted.

### Checklist before real downloads

- [ ] `http://<host>:5000` opens;
- [ ] **Health** reports no permission or space problems;
- [ ] the temporary folder is writable;
- [ ] the library (or NAS) is mounted and writable by the service user;
- [ ] at least one source answers **Verify**;
- [ ] a manual search returns sensible releases;
- [ ] the test-mode cycle produced no unexpected errors.

## 5. Finding your way around the interface

### The menu

| Group | Pages | What they are for |
|---|---|---|
| Overview | **Dashboard** | the state of everything at a glance, search, cycles |
| Download | **Download**, **Log** | the transfers in progress; the live log |
| Library | **TV Series**, **Movies** | the titles you follow, their episodes and what is missing |
| Discovery | **Explore**, **Archive**, **Comics**, **eD2k** | finding new titles, browsing the releases seen, comics, aMule |
| System | **Configuration**, **Integrations**, **Maintenance**, **Health**, **Blocklist**, **Manual**, **Changelog** | settings, connected services, clean-ups, diagnosis |

### The top bar

- **Add torrent**, on every page: opens the form for a magnet, a URL or a
  `.torrent` file (see [Adding a torrent or a magnet](#adding-a-torrent-or-a-magnet)).
- **Refresh**: reloads the page's data at once.
- The download and upload **speeds** (torrents plus aMule) and **CPU and RAM
  per process**: Gextto, gx-torrent and, when active and local, aMule. They
  refresh every 5 seconds.
- **Theme**, text size and **Font**. The **Theme** dialog has ready-made themes
  (Dark, Light, Cream, Nord, Dracula, Solarized, Gruvbox, High contrast and
  more, in light and dark variants); **Customize** builds your own theme from
  four colours (background, panels, text, accent) and warns you when the
  contrast is too low. Saved themes live on the server and follow you to every
  device; the active theme, text size and font remain a choice of each
  browser.
- The interface **language**.
- **Donate**, bottom right (on a phone it is in the *More* menu), and **Update
  available** when a new version is out.

### From a phone

Below 900 pixels wide the interface becomes compact: scrollable menu at the
top, metrics in two columns, full-width dialogs, tables that scroll inside
their panel. On the **Downloads** page a torrent name wraps in full instead of
stopping after the first characters: on a phone there is no mouse tooltip to
show the rest. Every action stays reachable by touch. Theme, text size, **Font**, language and **Donate** are in the *More*
menu, so the top bar keeps only the title, **＋** and **Refresh**. In the
**TV series** and **Movies** lists every title is a card: name and status on
one line, **Pause** and **Delete** side by side below. On the **Dashboard**,
when nothing was transferred in the last 30 minutes, one line says so instead
of the bandwidth chart. Pages travel compressed and scripts stay in the browser cache, so the
interface stays usable on a slow network too.

### Accessibility

The interface works from the keyboard and with assistive technologies:
controls have accessible names; dialogs manage focus, `Tab`, `Esc` and the
return to the control that opened them; sortable headers work from the keyboard
and announce the sort order; updates, errors and notifications use live
regions; the layout holds up on narrow viewports, zoom and 320-pixel reflow.
The automated checks (axe-core and Playwright) run from the checkout with
`cd uiweb/end2end && npm ci && npm run test:a11y`. They are not a WCAG
conformance statement: that also needs a manual review. Details in
[ACCESSIBILITY.md](ACCESSIBILITY.md).

---

# Part II — Everyday use

## 6. Following a TV series

### Adding a series

1. Open **Explore**, search for the title and pick the right result: check
   year, network and poster, because titles with the same name are common. You
   can also use **TV Series → Add**.
2. Choose the **Quality profile**. The profile gives the series its quality,
   minimum audio, language, subtitles, exclusions and «Allow upgrades»: the form
   has no other fields for these values, so they cannot contradict each other.
   Profiles are prepared in *Configuration → Quality and upgrades* (see
   [Quality profiles](#quality-profiles)).
3. Add, if needed, **Extra exclusions** for this series only: words that reject
   a release when they appear in its name. They are added to the profile's.
4. Choose the **seasons** to follow. Ranges are allowed.
5. Check the **folder**: it is already filled with *library folder + title*.
   With «Create folder» on it is created when you add the series; untick it to
   type it yourself. «Subfolders per season» puts the episodes in *Season 01*,
   *Season 02*…
6. Save and open the series page.

In folder and file names Gextto removes the characters that Windows, exFAT and
SMB shares reject (`\ / : * ? " < > |`) and a trailing dot or space: «Title:
Subtitle» becomes «Title Subtitle». The library stays readable from any
computer.

> [!TIP]
> Keep the seasons that have not aired yet enabled: the calendar and the search
> for missing episodes will keep working when they arrive.

### The series page

At the top you find poster, plot, cast (linked to TMDB or TVDB), year, network,
status, completeness and the library path. The actions:

| Action | When to use it |
|---|---|
| **Search missing** | to search for all missing episodes now, without waiting for the cycle |
| **Scan archive** | you copied files into the series folder by hand and want Gextto to recognise them |
| **Update from TMDB** (or TVDB) | new episodes are out, or titles and dates changed |
| **Rename preview** | see the *Old → New* names before renaming; **Force rename** also recomputes the files that look correct already |
| **Edit** | change name, seasons, folder, aliases, IDs, profile |
| **Download history** | reconstruct what happened to each episode |

The most important fields of the page:

| Field | Effect |
|---|---|
| **Name** | the title shown in the library. Changing it keeps episodes, their history, the missing-episode search and the exclusions linked to the series; the archive folder is not moved, so the files stay where they are |
| **Quality profile** | applies a profile's values to the series. The profile that matches the current values is preselected; if none matches you see **Current values**, which changes nothing when saved |
| **Seasons** | which seasons are followed |
| **Alias** | other names under which the series appears in releases |
| **Exclusions** | words that reject a release |
| **TMDB ID / TVDB ID** | identify the series in the two services; a wrong ID is fixed here. They are independent: saving one does not clear the other |
| **Series status** | *Automatic* lets the sources decide (see below); *Ended* or *Running* beats every source, for when you know something the sources have not recorded yet |

**Minimum audio** and **Allow upgrades** are not on the page: they come from
the profile and are changed with [Edit selected](#editing-several-titles-at-once).

### When a series has ended

Gextto marks a series as ended (🏁) by putting four signals together, strongest
first:

1. **your choice** in *Series status*, which beats everything else;
2. **a next episode with a date**: if TMDB (or TVDB) announces an episode from
   today on, the series is **▶ running**, whatever the status says;
3. **the TMDB status** (or TVDB's without the TMDB key): *Ended* or *Canceled*
   mark it as ended;
4. **TVmaze**, a free second opinion with no key, which often records
   cancellations sooner. If TVmaze says the series is over while TMDB still has
   it running, Gextto marks it as ended; if TVmaze is waiting for the renewal
   decision and no episode is announced, it shows **⏳ renewal uncertain**.

When TMDB and TVmaze give opposite answers, the page also shows **⚠ sources
disagree**: you decide with *Series status*. Hovering the label tells you who
decided it. The signals are refreshed once a day, together with the metadata, or
at once with **Update from TMDB**. TVmaze receives the TVDB and IMDb ids of the
series (and the name, when TVmaze is the source because the keys are missing);
turn it off with *Use TVmaze for series*, in *Settings → Access and services*.

It is only a label: an ended series is still searched like the others, because
missing episodes can always show up. To stop searching for it, pause it.

### The episodes

Episodes are grouped by season. On each one you can:

- **🔍 search for it**: the search opens in a panel below the episode and tells
  you how far it got (how many sources it is querying, how many answered, when
  it moves on to eD2k);
- **📜 see its history**: every event of that download (see
  [An episode did not arrive](#an-episode-did-not-arrive-where-to-look));
- copy the magnet, **ignore** it (it will no longer be searched; re-enabling it
  makes it a candidate again), force it, delete it;
- **↻ Redownload**: moves the current file to the trash and searches for the
  episode again, even when it would not normally count as missing (for example
  because a pack covers the season or later episodes are already there).

### Missing episodes

The **Missing** tab of *TV Series* gathers every episode that has aired and is
not in the library, grouped by series and season:

- **one click on an episode** searches for it, first in the local archive and
  then online, and shows the results with **Download**;
- **Search missing** searches for every missing episode of a series in the
  background: the tab shows the progress, you can cancel, and at the end it
  lists the results with **Download**. It downloads nothing on its own, and the
  list stays open after each download: you can take more than one without
  running the search again;
- **Filter by series** narrows the list.

Not missing: episodes that have not aired yet, episodes that already have a
download in progress (even added by hand or from a watched folder), and the
episodes of a season whose pack is downloading. If the download ends in an
error, the episode goes back to the missing list.

If no copy of an episode matches the profile, the list offers the only copy in
the requested language with the **🆘 last resort** badge: it is the one Gextto
would take on its own, and an upgrade will replace it as soon as a copy within
the profile comes out.

You do not have to do it all by hand: in the automatic cycles Gextto searches
for missing episodes on its own, first in the local archive and then online,
and — if you enable it — asks eD2k once per season when torrents find nothing.
**Search missing** only saves you waiting for the next cycle.

### Season packs and single episodes

- A **season pack** can fill many episodes, but the comparison with the library
  stays episode by episode: a pack does not replace an episode you already
  have in equal or better quality.
- When a single episode and a pack containing it have the same score, the
  release covering more episodes wins; then a REMUX; then the one with known
  seeds.
- Gextto does not download an old episode again when you already have later
  ones, unless it is a recognised gap, a real upgrade or your explicit request
  (↻ Redownload).
- With **pack inspection** on (*Configuration → Seeding and completion → Pack
  inspection*, off by default) Gextto reads the pack's file list as soon as it
  arrives: if the pack adds nothing it is dropped, if only some files are
  useful only those are downloaded. A pack that comes from a magnet, whose list
  is therefore not known yet, is added stopped: only its metadata is fetched, a
  few KB, and the download starts (with its log line and its notification)
  only if something useful is inside. Otherwise the log says what it held and
  the pack goes without having downloaded anything. It applies only to packs
  chosen by the cycle: a pack you start by hand is never dropped.
- If a download stays **stuck at 0 B/s or waiting for metadata** longer than
  the time set in *Configuration → Quality and upgrades → Stuck downloads*, the
  search can start an alternative for the same episode (or season) without
  removing the stuck one: if it recovers, the library keeps the better copy
  anyway.
- If a pack is **given up** because it was stuck, the episodes already complete
  still enter the library and only the missing ones are searched again (in the
  log: `🛟 … already complete, added to the library before giving up`).

## 7. Following a movie

### Adding a movie

1. Search for the movie in **Explore** and check year and original title.
2. Add it and choose the **Quality profile**; add, if needed, exclusions for
   this movie only.
3. Open its page and press **Search now** to see the results without waiting
   for the cycle.
4. Before queuing a borderline release, open **Why not this one?**.

A movie is recognised by title and year, and punctuation does not count: a
release matches the same way with or without a colon. When one entry's name is
contained in another's, the more specific one wins. Do not add the same movie
twice under different spellings: fix the metadata of the one you already have.

The **alternative titles** TMDB knows for the movie also count — the
international one and those used in other countries: a release published under
another name still arrives, and the page's archive matches are searched with
those titles too. The indexer searches use the names that matter: the monitored
name, the original title (the film's original language), the title in the
language chosen for the film and the English one. The other titles TMDB lists
are not queried one by one — they would multiply the requests — but they still
count for recognising releases and for the archive search. Working titles are
excluded, because they sometimes belong to another film.

The comparison is the one Sonarr and Radarr make: the **whole** title counts,
not a word found anywhere in the name. A release is recognised when its name
**opens** with the film title or with one of its alternative titles, word by
word, ignoring accents, capitals and punctuation; the tag an indexer puts before
the title (a language such as "ITA", a resolution, a group in square brackets) is
skipped. A one-word character name — which TMDB lists among the alternative
titles — therefore no longer takes a release of another film. Leading articles do
not count, and at the same length the film's own name wins over an alternative
title. The page's **archive matches** show only the releases Gextto would really
take.

### The movie page

It shows poster, plot, cast, the **best results** found in the sources and the
**archive matches**, each with **Why not this one?**. Under the plot the
**Alternative titles (TMDB)** line opens the list of names the film was released
under abroad: they are the ones Gextto uses to recognise releases and to search
the archive, and they stay saved with the movie. If the film was added by hand
and TMDB has not been asked yet, the line says **not read yet**: **Aggiorna
TMDB** reads them at once, together with the metadata. From the page you can
edit the movie, download it again or search for it now.

In the editor:

- **Language** takes one code or several separated by commas (`ita,eng`), even
  beyond the entries of the list;
- **Subtitles** with **Subtitles required**: *Yes* rejects a release without
  those subtitles, *No* treats them only as a preference;
- **Allow upgrades** and **minimum audio** are changed with *Edit selected*.

The **Movies** page has two tabs, **Monitored** and **Downloaded**, with
sortable columns. In the *Monitored* tab, next to *active* or *paused*, a **✓**
means a version of the movie has already been downloaded: hovering over the
mark shows the path of the library file, or the archived title when the move
was not recorded.

### List or posters

Above the *TV Series* and *Movies* tables, the **List / Posters** switch
changes the view: *Posters* shows one card per title with cover, quality,
language and status. The choice applies to both pages and is remembered.

### Editing several titles at once

To change the same values on many titles — for example moving ten movies to
`2160p` or pausing several series — use the *List* view:

1. **Tick the rows.** The box in the header ticks every visible row: combine it
   with *Filter the list…* to take, for example, every title in `720p`.
2. Press **Edit selected**. Tick only the fields to change (quality, minimum
   audio, language, subtitles, exclusions) and type the new value. A ticked,
   empty field is cleared; an unticked field stays as it is. *Allow upgrades*
   and *Status* change only if you pick a value other than «Leave as is».
3. **Start from a profile** copies a profile's values into the fields, except
   the exclusions, which belong to each title (tick them only if you want to
   replace them).
4. Press **Apply to the selected titles**.

Seasons, folder, aliases and the other values of the titles are not touched.
A movie's mandatory requirements (subtitles, required languages) follow the
value they qualify: changing language or subtitles updates the requirement
too.

## 8. Searching and downloading by hand

The automatic cycle does almost everything on its own. A manual search lets you
see **what is out there** before you change a setting, pick a specific
release, or download something you do not follow.

### Where to search

| From | What it searches |
|---|---|
| **Dashboard**, search box | local archive, indexers, web engines; eD2k if you tick **eD2k** |
| **Download**, *Search for a release* | the same, without leaving the downloads page |
| **Explore** | titles on TMDB (trending, popular, upcoming) to add; generic release search |
| **Archive** | the releases seen in the feeds, instantly; online only if you tick *Also search the web* |
| 🔍 on an episode, **Search now** on a movie | that exact title |

### Reading the results

1. Type a title (or a technical query) and wait for the sources to answer;
   slow ones are flagged at the timeout.
2. Compare title, source, quality, size and seeds/peers.
3. Narrow down with **Filter the already loaded results**: it works on the
   results received and does not search again.
4. Open **Why not this one?** on the interesting results.
5. Press **Queue** (or **Download**) when you are convinced: the results stay
   in the list, so you can add others without running the search again.

A manual search also shows releases the cycle would **not** pick, precisely so
you can inspect them: seeing one does not mean it would be downloaded.

### Where a download added by hand ends

| Where you add it from | Where it ends |
|---|---|
| A free search: **Dashboard**, **Download** (*Search for a release*), **Explore**, a pasted eD2k link, the TUI | it stays in the **transfer folder**, under its own name: it does not go to the library, is not renamed and does not count as an episode or film you have. It is shared within the seed limits and then stopped; you take it out of the session |
| **Download** on the results of a title you follow, from its page (🔍 on a missing episode, the archive matches of a film), **Queue** on the followed titles of the Dashboard | it goes to the **library**, next to the other episodes or films, like an automatic download |

In the reason column a download of the first row reads *✋ added by hand,
stays in the transfer folder*.

### «Why not this one?»

It is a read-only diagnosis of a release: it queues nothing and changes
nothing. It shows the outcome, the score with its parts and every check:

- **Passed** — the check passed;
- **Blocking** — enough on its own to reject the release;
- **Informational** — depends on the rest of the cycle (other candidates,
  delay, free space, gaps to fill).

At the end comes the comparison with the library: first download, possible
upgrade, or a copy no better than the one you have. The checks run in this
order: followed title, blocklist and duplicates, global and source filters,
release sanity (burnt-in subtitles, absurd size), the title's
quality/language/subtitles/exclusions, comparison with the library and active
downloads. The score only chooses among the releases that passed every check.

### Adding a torrent or a magnet

**Add torrent**, in the top bar of every page, takes a magnet, a `.torrent` URL
or a `.torrent` file. You can set the save folder, **Download now** (outside
the queue) and **Do not rename**. With gx-torrent there are also:

- **Skip check**: adds the torrent on files you already have, without checking
  them first; it starts seeding at once and every piece is verified the first
  time a peer asks for it. Use it only for data you know is complete;
- **sequential** download and **first/last piece** of the files;
- space **preallocation**, which can be turned off for a single torrent.

A magnet with `so=` downloads only the listed files; with `ws=` it adds web
seeds. An address that answers with a magnet is added as a magnet.

You can also copy `.torrent` or `.magnet` files into a **watched folder**
(*Configuration → Sources → Watched folders*): Gextto waits until the file stops
changing, adds it and then removes it or renames it `.imported`.

### The Archive

**Archive** is the catalogue of every release seen in the feeds, including
titles you do not follow. The search starts by itself as you type (from three
characters; for shorter ones press **Enter**) and is instant. `-word` excludes
the titles containing it. The same torrent appears only once. From each row you
can queue (⬇️), open **Why not this one?** (🔍), copy the magnet (🧲) or delete
the row (🗑️).

- **Also search the web** extends the search to indexers and online engines:
  slower, it starts only when you press **Search**.
- **Also search eD2k** (with aMule active) adds the eD2k results below.
- The **Seen in feeds** tab groups releases by title, with the number of
  versions and the best resolution and score: expand a group to queue one.

The releases of a **feed removed from Sources** (or commented out with `#` in
the list) stay in the catalogue, like those of a switched-off indexer: their
`.torrent` links are no longer opened, so Gextto no longer reports the error of
a feed that is gone. Putting the feed back makes them usable again on their
own; rows archived before this version behave as before.

### eD2k links

In **Explore** and on the **eD2k** page you can paste one or more `ed2k://`
links (even mixed with text): Gextto checks them, removes duplicates and hands
them to aMule with **Add all** or one by one.

## 9. Watching the downloads

The **Download** page shows everything being transferred: torrents, HTTP comic
downloads and aMule eD2k files (with the **eD2k** badge), in a single list that
refreshes every 5 seconds.

### Reading the list

Each row shows name, status, progress, speed, estimated time, peers and ratio.
The **swarm** (🌐 seeders / leechers) is the one reported by trackers and DHT:
with **0 leechers** there is nobody to upload to. Sort with **Sort by** (for the
whole list at once), filter by name and by tag, and pick a state with the
buttons above the list (*All torrents*, *Downloading*, *Seeding*, *Paused*,
*Completed*, *Waiting for seeds*, *Error*), each with its count; an empty
state is not shown. In the panel head, next to the counts, ↓ and ↑ say how much
the engine downloaded and uploaded since it started.

| Status | What it means | What to do |
|---|---|---|
| Queued | registered but not started yet | wait: the queue will start it |
| Downloading | transferring | check speed and peers |
| Stalled | no new bytes for too long; paused | wait for the automatic retry |
| Seeding | complete, sharing | let it finish according to the seeding limits |
| Error | the engine reported an error | read the reason before removing it |
| Archived | the file is in the library | nothing; do not delete the source while it seeds |
| 📌 pin | forced outside the queue | **Unlock pin** on its page puts it back in the queue |

### Acting on downloads

- **On one row**: pause/resume, recheck, remove.
- **On several rows**: tick the torrents and use the selection bar: **Pause**,
  **Resume**, **Check again**, **Tag** (assign, create or remove a tag),
  **Remove from session**.
- **Tools**: **Clean completed** (removes torrents that finished seeding),
  **Orphaned files…**, **Refresh every 5 s** and the link to **Removal after
  seeding**.

**Remove** takes the torrent out of the session and can ask whether to delete
the data too. Removing a torrent from the session does **not** remove the file
from the library. A torrent with infinite seeding is never removed by *Clean
completed*.

### A torrent's details

Click the name: the details open above the *Download history*, so the list
stays visible. The tabs:

| Tab | What you find |
|---|---|
| **General** | torrent data, copy magnet, **Export .torrent**, reannounce, pin, restart, mark as failed, **Super seeding**, web seeds |
| **Tracker** | add trackers (`tier|url` per line) or remove one |
| **Content** | the files, with **priority** (Skip, Normal, High, Maximum) and, with gx-torrent, **▶** for streaming |
| **Peers** | the connected peers; with gx-torrent the peer's **country** sits next to its address, from the DB-IP database gx-torrent downloads by itself |
| **Pieces** | the piece map: green downloaded, yellow in progress, grey missing. With gx-torrent, below it is the **availability among peers**: the fuller the bar, the more connected peers have the pieces you are missing; red marks pieces no connected peer has. With qBittorrent there is the map only, and the pieces of skipped files show as missing |
| **Speed** | that torrent's download and upload chart over the last 30 minutes, with average and peak. Gextto records the speed of the torrents that are transferring only, so the chart is already there when you open the tab |
| **Limits** | ratio, seeding days and bandwidth of that torrent: **-1** uses the global value, **0** removes the limit. With gx-torrent there is also **Peers and bandwidth share**: the **bandwidth share** (100 = normal, 200 = twice a normal torrent, 50 = half) sets how much that torrent takes when the line is limited, and **DHT**, **PEX** and **Local discovery** are turned on or off for it alone. A private torrent always keeps DHT, PEX and local discovery off |
| **Storage** | **Move storage** to another folder |
| **History** | every event of that download, from the choice to the library |

Hovering over a value or a button shows what it means. If you move the storage
to a folder that **already holds the data**, Gextto rechecks and reuses it
instead of downloading it again. Every save shows a notice at the top saying
what was saved; if something fails the notice is red, gives the reason and the
tab stays as it was, with the values you typed.

### Streaming and media server

With gx-torrent each file has **▶**: it opens **HTTP streaming**, and the pieces
of the part you are watching are downloaded first. You can start watching
before the download ends.

The same engine works as a **DLNA/UPnP media server** (*Configuration → Torrent
engine → DLNA/UPnP server*, off by default): TVs and consoles on the home
network see the completed media and the library folders, with the posters and
thumbnails already there (in the Jellyfin, Kodi and Emby format) and the
`.srt`/`.vtt` subtitles with the same name as the video. It has no
authentication, so it answers only devices on the local network.

### Bandwidth

The **Bandwidth** button, top right of the page, shows what limits the speed
right now and lets you make a temporary **exception** to the rule set in
*Configuration → Speed and network*:

- **Auto** — Gextto measures the line and adapts;
- **Limit download** / **Limit upload** — a value from the list, or **No
  limit**;
- **Duration** — 1, 3, 6, 12 hours or **Until I remove it**;
- **Custom values…** — download and upload in KiB/s and duration in minutes;
- **Remove the exception** — back to the rule at once.

The icon tells you what applies: ⚡ an exception (with the minutes left), ⏰ a
window of the schedule, otherwise the rule. With eD2k active **aMule follows
the same limits**. See also [Speed and network](#16-speed-and-network).

### Stalled torrents

When a torrent adds no bytes for a while it becomes **stalled**: Gextto pauses
it, so it does not take a queue slot, and every so often resumes and
reannounces it. If it does not restart before the final limit, it is given up
and another release is searched for. Having connected peers without receiving
data does not reset the timer.

The times are in *Configuration → Seeding and completion → Stalled torrents*:

| Setting | Default |
|---|---|
| Consider stalled after | 60 minutes |
| Retry stalled torrents every | 60 minutes |
| Remove stalled torrents after | 14 days (`0` = never) |

In the log: `is stuck at` (with the reason), `is still stuck` at each retry,
`is downloading again` when it restarts, `Gave up on` when it is given up. The
Dashboard lists stuck downloads under **To check**, with the next retry and
when they will be given up.

### Seeding limits: per-source rules and idle seeds

When a torrent stops sharing is decided by Gextto, with the same rule for
gx-torrent and qBittorrent. For each limit (ratio, time, inactivity) the first
level that sets it applies:

1. the limit of the **single torrent** (**Limits** tab);
2. the first matching **seed rule** (*Configuration → Seeding and completion →
   Seed rules*);
3. the **global** limit.

A **seed rule** names a **source** (part of the indexer's name is enough, case
does not matter: `mircrew` also matches `mircrew indexerr`) and/or a
**category** (TV Series, Movies, Comics, Other). This way a private tracker's
rules apply only to its torrents. An empty field uses the global value. A
torrent from Jackett's aggregate search carries no tracker name: category or
global apply to it.

**Infinite seeding.** `0` means infinite, for ratio as for time, and **one of
the two being infinite is enough** for seeding never to end: the torrent is not
stopped even for inactivity and is not moved into the library (a copy or a
hardlink goes there instead). To stop seeding, ratio **and** time must both be
greater than zero. The same applies to eD2k.

**Stop an idle seed after** stops a torrent that has sent no data for that many
minutes. Only time actively seeding counts (not queued or paused), and the
count restarts at every Gextto restart: a restart can only make seeding
longer.

At the end of seeding the torrent follows its path: archiving into the library
and, if enabled, removal from the session.

### The history

**Download history**, at the bottom of the page, lists finished downloads: name
(with the **NAS** badge when the file is archived), type, season and episode,
folder rule, score, library path and date.

## 10. Where the files end up

### What happens when a download finishes

Gextto recognises the content, renames it with your template and puts it in
the series or movie folder. The behaviour depends on the shape of the torrent
and on whether a library is configured:

| Content | With a library configured | Without a library |
|---|---|---|
| One episode, as a single file | archived and renamed according to the series rules | stays in the downloads folder |
| One episode inside a folder | the video enters the library, renamed; the folder stays for seeding and then goes to the **trash** | the folder stays intact in Downloads |
| A season pack | the episode videos (recognised by `S01E02`, `1x02`…) enter the library; the pack stays for seeding and then goes to the **trash** | the pack stays intact in Downloads |

Subtitles, NFO files and images of the pack stay in the source folder. A source
folder is never moved or deleted while it is seeding, and it goes to the trash
only when the trash is configured and removal after seeding is on; without a
trash, source files are never deleted.

### Copy, move or hardlink

- **Move** (*Move episodes/packs to the archive*): at the end of seeding the
  file is moved into the library.
- **Hardlink**: while the file seeds, Gextto tries to create in the library a
  **second name for the same data**. The file shows up in the library at once
  and does not take space twice. Deleting one of the two names does not touch
  the other: the data disappears only when both are deleted.
- **Copy**: if a hardlink is not possible, Gextto copies and says so once in
  the log, with the reason.

A hardlink works only if downloads and library are on the **same filesystem**.
Two folders of the same NAS mounted **separately** (for example
`nas:/volume1/Downloads` and `nas:/volume1/Video` as two mounts) are not enough:
mount a common root (`nas:/volume1`). If you use programs that edit videos **in
place** (some taggers, `mkvpropedit`), turn off *Configuration → Seeding and
completion → Hardlink instead of copy while seeding*: they would change the
seeding file too.

### Upgrades

When a better version of a file you have arrives, the new one enters the
library and the old one goes to the **trash**. An equal or worse version is
dropped (in the log: `discarded as inferior`). To freeze a title set **Allow
upgrades** to *No* with *Edit selected*.

### File names

With **Rename episodes** on, the name is chosen in *Configuration → Library and
renaming → Name composition*, with four formats and a preview:

| Format | Episodes | Movies |
|---|---|---|
| **Basic** (default) | series, season, episode and title | `Title (Year)` |
| **Standard** | plus resolution and codec | `Title (Year) [resolution][codec]` |
| **Complete** | plus audio, HDR and language | `Title (Year) [resolution][audio channels][HDR][codec]` |
| **Personalized** | your template, with the tokens `{Serie}`, `{Stagione}`, `{Episodio}`, `{Titolo}`, `{Source}`, `{Gruppo}`, `{Risoluzione}`, `{VideoCodec}`, `{Audio}`, `{Canali}`, `{HDR}`, `{Lingue}` | the movie template (below) |

With **Personalized** movies have a template of their own, in the field
*Custom template for movies*: the tokens are `{Titolo}`, `{Anno}`, `{Source}`,
`{Gruppo}`, `{Risoluzione}`, `{VideoCodec}`, `{Audio}`, `{AudioCodec}`,
`{Canali}`, `{HDR}` and `{Lingue}`, with their own buttons and preview. The
default, `{Titolo} ({Anno}) [{Risoluzione}]`, is the name movies had before,
so a library already renamed keeps its names: if you give the episode template
the quality tags, add them to the movie template too, which does not take them
on its own. A token with no value (an
unknown HDR, a missing year) disappears together with its brackets.

The technical tags are not taken from the release name alone: before renaming,
Gextto reads the file with `mediainfo` (or, when missing, `ffprobe`) and uses the
real resolution, codec, audio, channels, HDR/Dolby Vision and languages. A tag
already in the name stays unless the file contradicts it. A file holding
several consecutive episodes gets the range (`S01E01-E02`). Without an episode
title (no TMDB or TVDB key) the name uses «Episodio N» (the placeholder is always Italian).

### Importing a file you already have

For a video Gextto did not download — an episode recovered by hand, an old
season, a movie from another disk — use **Maintenance → Import file**:

1. type the path or press **Browse** (one click on a video picks it, **Select**
   takes the whole folder);
2. press **Analyze**: Gextto proposes series or movie, season, episode and
   destination folder;
3. correct it if needed and press **Import now**.

The file is renamed, moved into the library and registered, and Jellyfin or
Plex are refreshed. If a file with the same name already exists the import
stops: tick **Replace the existing file** to send the old one to the trash.

If instead you copied files straight into a series folder, **Scan archive** on
the series page is enough.

## 11. Comics

1. In **Comics → Add comic** type the title and press **Find**.
2. Pick the exact GetComics result and confirm. Gextto saves that post and
   uses it for the download, without confusing it with an issue of the same
   name.
3. A title with a number (for example «Title #41») follows the series **from
   that number on**: later issues are downloaded, earlier ones, other series and
   collections are not.

The page also has the list of followed comics, link extraction from a post,
the **weekly pack** settings and the history with resend, delete and force. A
weekly pack is downloaded only once; one found before its links were published
is checked again at every cycle and queued as soon as the link appears.

Direct HTTP downloads show up in **Download** like the others. In **CBZ** files
downloaded directly Gextto adds a `ComicInfo.xml` with series, number and year,
which Komga, Kavita and readers use to sort the issues; a file that already has
one, comics from torrents (they are seeding) and CBR files are not touched. An
issue that arrives as RAR is saved as `.cbr`.

## 12. eD2k and Kad with aMule

Gextto can also use the **eD2k/Kad** network through **aMule**: `amuled` does
the network work, Gextto drives it through **external connections (EC)**, the
same protocol as `amulecmd`. It is optional and off by default; with aMule off,
torrents do not change.

### Connecting aMule

1. Install aMule (`amule-daemon` on Debian/Ubuntu) and enable external
   connections in `amule.conf`:

   ```ini
   [ExternalConnect]
   AcceptExternalConnections=1
   ECAddress=127.0.0.1
   ECPort=4712
   ECPassword=<md5 of the password>
   ```

   `ECPassword` is the MD5 of the password: `printf '%s' 'secret' | md5sum`.
2. Start `amuled`.
3. In **Configuration → eD2k and Kad** turn on **eD2k and Kad through aMule**
   and enter address, port and the password **in clear**.
4. Open aMule's TCP and UDP ports on the router: you get a **High ID** and a Kad
   not behind a firewall, which means more sources.

Gextto must see aMule's **incoming folder** at the same path (it does when they
run on the same machine): that is where it imports completed files from. Use a
dedicated folder, because aMule shares everything in it. With aMule in
**Docker**, mount the downloads folder at the same path as on the host, set
`INCOMING_DIR` and `TEMP_DIR` there and, for UPnP, use `network_mode: host`.

### The eD2k page

- **aMule** — version, server, **High/Low ID**, Kad status, speeds, upload
  queue, bytes exchanged; the **Connect**, **Disconnect**, **Start/Stop Kad**
  and **Leave the server** buttons. If aMule does not answer, the page says
  what to check.
- **eD2k servers** — the server list, with **Connect**; from here you also
  download a new `server.met` or a `nodes.dat`.
- **Search eD2k and Kad** — pick the network and search; each result has
  **Download**, and **already known** marks a file aMule already has. The filter
  above the results (words, minimum sources, minimum size, hide known) does not
  search again.
- **Paste eD2k links** — one or more `ed2k://` links, then **Download with
  aMule**.
- **aMule log**, **aMule folders** (with **Create the folders** when they are
  missing) and **Shared files** (bytes uploaded, requests, queued clients,
  links to copy; **Reload shared files** after manual changes).

### What happens to an eD2k download

eD2k downloads appear in **Download** with the **eD2k** badge, with pause,
resume and 🗑️ (on a running download it cancels it; on a completed one it only
removes it from aMule's list). **ⓘ** opens the details, with sources, hash,
link and **priority in aMule**.

When aMule completes a video, Gextto waits until the file is stable in the
incoming folder, then treats it like a completed torrent: same quality check,
renaming, library, MediaInfo, notification. With *Move episodes/packs to the
archive* on the file is moved (and aMule stops sharing it); off, it enters with
a hardlink or a copy and stays shared. A file that is not a video stays in the
incoming folder. With infinite seeding the file is copied and stays shared,
with the «seed ∞» badge.

### eD2k as a fallback

With **Use eD2k when torrents find nothing** on, the search for missing episodes
asks eD2k/Kad when torrents and archive have no results: one query per season,
lasting as long as **eD2k search duration**. The results appear with the
**eD2k/Kad** source and **Download** hands them to aMule.

The complete guide — installation, Docker, common problems — is in
[amule.md](amule.md).

---

# Part III — Configuring

The **Configuration** page has four areas: **What to look for** (General,
Sources, eD2k and Kad, Quality and upgrades), **How to download** (Torrent
engine, Speed and network, Queues and performance, Seeding and completion),
**Where to store** (Library and renaming, Archive and space) and **System**
(Automatic maintenance, Notifications, Access and services, Diagnostics and
updates).

How to use it:

- the sections are in the left column (on a phone, in the *Section* drop-down);
  the search box finds a setting by name or by technical key;
- each row shows name, description, unit and special values (for example «0 =
  never»); settings that depend on another one **appear only when they are
  needed**;
- a value changed and not saved is marked **not saved** and the *Unsaved
  changes* bar appears, with **Save all** and **Discard changes**; leaving the
  page with pending changes asks for confirmation;
- **↺ Default** puts the default value back; **Modified only** shows only what
  you changed;
- settings the active torrent engine does not use are gathered under **Not used
  by the active engine**; rare ones under **For experts**.

This part explains the choices that matter. Every setting, one by one, is in
[Appendix A](#appendix-a-reference--configuration).

## 13. Sources: feeds and indexers

Gextto finds releases in three ways: **RSS feeds** (read at every cycle, they
fill the local archive), **Torznab indexers** (queried for the titles you
follow) and **web engines**. Everything is set in *Configuration → Sources*.

### Adding Jackett or Prowlarr

1. In Jackett (or Prowlarr) enable at least one indexer and copy the **API
   key**.
2. In Gextto press **+ Jackett** (or add a row to the *Torznab indexers* list
   and pick the type).
3. Enter **only the base address**, for example `http://jackett:9117` or
   `http://prowlarr:9696`, and the API key. Do not add the Torznab path: Gextto
   builds it.
4. Save and press **Verify**.
5. Check *Health → API status*.

Gextto reads the list of indexers from the manager and queries them **one by
one**, so each has its own timeout and health and the slowest does not hold up
the others. If the list cannot be read, it falls back to the aggregate search.
In the results the source shows as `jackett:TrackerName`.

A **switched-off** indexer is not queried, and the `.torrent` links of the
releases already collected from it are no longer opened: the releases stay in
the archive catalogue — nothing is deleted — but Gextto does not try to download
them and no longer reports the error of the service you turned off. Switching
the indexer back on makes them usable again.

### Adding indexarr

The same, with type **indexarr** and base address `http://indexarr:8788`.
indexarr is queried in one go: it merges its sources' duplicates itself and
says which site each result comes from. Its eD2k sources return `ed2k://`
links, which Gextto hands to aMule instead of the torrent engine. When a
download completes, Gextto tells indexarr, which marks the row as
**downloaded** on its Download page. There is nothing to configure.

### FlareSolverr

Sites behind Cloudflare need a working **FlareSolverr**: enter its URL in
*Configuration → Sources* and press the test button. With a proxy configured,
FlareSolverr is not used as a fallback, because it would download outside the
tunnel.

### Reachable does not mean working

An indexer can answer and still refuse the request (wrong API key, no indexer
enabled in Jackett). That is why *Health* tells them apart:

| Status | Meaning | What to do |
|---|---|---|
| **OK** | answers and accepts requests | nothing |
| **API error** | answers, but refuses the API key or the configuration | copy the API key again, check the enabled indexers and the base URL. Restarting Gextto does not help |
| **Unreachable** | no answer within the timeout | check DNS, container, port, firewall |

Behind a manager the table also shows each of its indexers, so a tracker in
error or paused is visible even when the manager answers.

### Backoff: when a source fails

A source that keeps failing is put in **backoff**: for a while Gextto does not
query it, with longer and longer pauses. If an indexer answers `429 Too Many
Requests` with `Retry-After`, the pause lasts at least the time asked. The state
is in *Health → Provider status*, with level, expiry and last error. Once you
have fixed the cause press **Reset** on that source; do not use it to hide a
wrong API key, or the backoff will start again.

Gextto warns you by itself (log and notification) when a source fails in three
of the latest searches, when a source's `.torrent` links fail on two different
releases in the same cycle (usually an expired login) and when everything is
back to normal.

### Copies of the same release

The same torrent often arrives from several sources. Gextto merges them into
one release when they have the same infohash or — when a source does not give
it — when name and size match. When in doubt it does not merge: a different
group, resolution, language, PROPER or episode stays separate. It uses the best
copy of the release and keeps the others in reserve: if a `.torrent` link does
not work, it tries the same release from another source at once.

## 14. Quality, scores and upgrades

### How Gextto chooses

1. **Filters.** A release that does not meet the title's requirements
   (quality, language, required subtitles, exclusions) or the sanity checks is
   rejected.
2. **Score.** Among the remaining releases the one with the highest score wins.
   The score adds up the weights of resolution, source, codec, audio, HDR/Dolby
   Vision, fixes (PROPER, REPACK) and group.
3. **Comparison with the library.** If you already have the file, the new
   release replaces it only if it is a **real quality improvement**.

The sanity checks are automatic and cannot be configured: burnt-in subtitles
(`HC`) and sizes that make no sense for the resolution are rejected. The
rejections are visible in the log at `DEBUG` level.

### Quality profiles

A profile is a **named template**: quality, minimum audio, language, subtitles,
exclusions and «Allow upgrades». They are managed in *Configuration → Quality
and upgrades → Quality profiles*.

- **Quality** is picked from a list (360p … 2160p, a minimum with «+», the
  `720p-1080p` range, or *Any*).
- **Language** and **Subtitles** offer the common choices (`ita`, `eng`,
  `ita,eng`, `multi`, `any`) but also take other codes (`fre`, `ita,eng,fre`).
  Empty means Gextto's default language.
- Until you save the list, Gextto generates a profile for each combination
  already in the library; the first is **Default**. Two profiles cannot share a
  name.

**Profiles are templates, not rules.** When you pick a profile for a title, its
values are **copied** into the title. Changing the profile later does not
change the titles already created: to update many at once use [Edit
selected](#editing-several-titles-at-once). The profile list is also what
Jellyseerr sees; each profile keeps its number even if you rename it, because
Jellyseerr remembers the number.

### Trying before changing

At the top of *Quality and upgrades*:

- **Examples with the current weights** — the scores of a few typical releases;
- **Impact on the library → Check now** — how many episodes in the library would
  be replaced with the current rules. Use it after changing weights or
  thresholds: a high number means the next cycles will download a lot again;
- **Test a release** — paste a release name: you see the score item by item
  and whether it would replace the copy in the library, with the reason.

None of them downloads anything.

### The weights

Below are the weights, in closed panels: **Video** (4K, Full HD, HD, SD,
codec, source, HDR/DV), **Audio** (Lossless, DTS/DD+, AC3, AAC/Opus, MP3,
channels, Atmos), **Release fixes** and **Custom groups** (a bonus or a penalty
for a release group, written like the release's final tag, for example `TBK`).
Higher means more preferred. After a weight change the library scores are
recalculated by themselves.

Language is not a bonus: it is a requirement of the title, so you can prefer
English as much as Italian. Preferred subtitles give a small bonus. A
**freeleech** release gets a separate bonus only when choosing among
candidates, never for an upgrade.

### When a file is replaced

| Setting | Effect |
|---|---|
| **Min score difference for upgrade** | how much better the new release must be. Only quality counts: size, subtitles and swarm health are not enough |
| **Stop upgrading above this score** | above this score the file is no longer replaced, except by a REPACK or PROPER |
| **Replace versions already archived** | turns on upgrades of files already in the library |

Before replacing a file Gextto reads both the new one and the one in the
library with `ffprobe`: if the name promised more than is there (a «2160p»
upscaled from 1080p, an «HDR» without HDR) the download is dropped and
blocklisted (in the log: `Upgrade not confirmed by MediaInfo`). A library file
that does not declare its source or group is not considered worse without
proof.

### Delay before downloading

In *Configuration → General → Delay before downloading* you can make Gextto
wait a few minutes after finding a release, to give a better version time to
show up. **Skip the delay from this score** starts excellent releases at once.
The titles waiting are in the **Title queue** panel of *Health*.

## 15. The torrent engine

Gextto always owns the database, queue, scores, renaming and library; the
**engine** only does the transfer. It is chosen in *Configuration → Torrent
engine*, and the **Download** page and the automations are identical with
either. The interface header shows the engine in use with its version.

### gx-torrent (default)

The included engine, in pure Go. Gextto starts and supervises it in a separate
process; there is nothing to install.

- **It stays on when Gextto restarts** (for example for an update): transfers
  are not interrupted. It is restarted only if its program or options changed,
  never during a file move. If Gextto stays off, it stops by itself after 15
  minutes.
- It has its own **web page** at its listen address (default `127.0.0.1:8890`,
  this computer only): magnets, pause, check, queue, IP filter, log. To open it
  on the network use `0.0.0.0:8890` **and a token**.
- The **disk cache** sizes itself from free memory and disk type. The **IP
  filter** from a URL is refreshed once a day.
- With a **proxy** configured, feeds, indexers and `.torrent` downloads go
  through the tunnel too; services on the local network stay direct.
- It supports streaming, DLNA, sequential download, v2 torrents and magnets
  with `so=`, `ws=` and `dht=`.
- In **Configuration → Torrent engine → gx-torrent** there are three native
  qBittorrent options: **recheck data when a download completes** (verify the
  files once before seeding them), **anonymous mode** (hide the client version
  from peers and public trackers, not the IP) and **announce to all trackers in
  a tier** (ask every tracker of a tier together). The first two are off by
  default, the third is on. After changing them, restart Gextto to apply them
  to the managed daemon.
- The same network section holds the peer rules. Three work by themselves from
  installation, with values Gextto sets and you can change: the **client
  filter** (refuses the clients known to download without giving anything
  back; the list is already filled), the **tracker list** (the "best" one of
  the *trackerslist* project, downloaded every day and added to new public
  torrents, never to private ones: for a magnet it waits until it knows whether
  it is private) and the **peer country** (the free DB-IP database, downloaded
  every month, without sending the peers' addresses anywhere). The **upload
  share for peers that give back** stays off until you choose a percentage:
  when the upload is limited, by Auto too, that share goes first to the peers
  that send you data for a torrent you are still downloading, the rest to
  seeding.
- In *Health → Torrent engine*, the **DHT** row says how many nodes gx-torrent knows, how
  many answer and how many lookups are running; **Incoming DHT queries** says
  whether any node reached it from outside in the last hour (if not, the UDP
  port may be closed on the router). The daemon's **Statistics** page also
  shows the node table by distance, the clients refused and the state of the
  tracker list.

If the engine cannot stay up (6 abnormal starts in 10 minutes), Gextto switches
by itself to qBittorrent-nox and, as a last resort, to a recovery state with
transfers stopped and the interface reachable. An update of gx-torrent that does
not start is first rolled back to the previous version. You get a notification
in every case.

### qBittorrent-nox

Gextto can drive a **qBittorrent-nox** (from 4.6 to 5.3) through its Web API:
set URL, user, password, category and tag. If the two programs see the folders
under different paths (Docker, NAS, another machine) you need **path mappings**
`local=remote`: **Propose** reads them from qBittorrent. If a path cannot be
translated the activation is refused, so a file cannot end up in the wrong
folder.

In **managed mode** Gextto downloads qBittorrent-nox, installs it next to
itself, starts and stops it with the service, updates it and restarts it if it
crashes. The full steps are in [qbittorrent-nox.md](qbittorrent-nox.md).

### Switching engine

A torrent belongs to one engine at a time: switching is a migration, not two
clients on the same data. The engine panel has a reachability and path
**test**: use it before switching. The state of the active engine (queue,
port, DHT, encryption, proxy, cache, disk I/O) is in *Health → Torrent engine*.

### On a small machine

Gextto runs well on a mini PC or a NAS. At rest you do not notice it; during a
transfer the cost is the engine's, which gives memory back when the download
ends. If resources are tight:

- turn on the **dynamic queue** and *Don't count stalled torrents in active
  slots* (*Configuration → Queues and performance*);
- do not set a cache limit: the automatic value is almost always the best.

## 16. Speed and network

### The speed rule

In *Configuration → Speed and network*, **How the speed is decided** offers
three rules:

- **Auto** — Gextto measures the line (the ping to public servers) and raises
  or lowers the speed so as not to saturate it. If you know your line's real
  speed, enter it in *Auto — maximum line speed*: Auto finds the right point
  faster.
- **Fixed limits** — one cap for download and one for upload.
- **Hourly schedule** — one row per day, with *Off*, *Limit* (your values) or
  *Auto*, plus start and end time. **Outside the hourly windows: Auto** decides
  what applies outside the windows.

Peers on your home network are not limited, unless you turn on *Apply the
limits to peers on the local network too*. For a temporary exception use the
**Bandwidth** button in Download (see [Bandwidth](#bandwidth)).

### Ports, VPN, encryption

- **Ports**: open the listen port on the router; UPnP and NAT-PMP can do it by
  themselves. **Test ports** only checks that the port opens on this computer,
  not the forwarding on the router.
- **VPN interface (killswitch)**: binds torrent traffic to one network
  interface (`tun0`, `wg0`); if the VPN drops, the traffic stops instead of
  going out in the clear. Applied after a restart.
- **Encryption and IP filter**: in *Speed and network → Encryption and IP
  filter*.

### RAM disk

To spare writes on a slow disk you can keep incomplete data in RAM
(*Configuration → Queues and performance → RAM disk*). Gextto lists the
available `tmpfs` and, if there is none, creates `/dev/shm/gextto`. Its content
is lost when the machine restarts and the folder has to be created again.

## 17. Library, NAS and space

### A library on a NAS

1. Mount the NAS **before** Gextto starts (for example with a systemd unit or
   `fstab`).
2. Give the service user read and write access.
3. Set a prudent **minimum free space** (*Archive and space → Disk space*):
   below that threshold Gextto starts no new downloads.
4. If you copied existing files, run **Scan archive**.
5. After the first download check the actual path in the *Download history*.

If the NAS is not mounted, do **not** replace the path with a local folder to
make it work: the files would end up in the wrong place. Fix the mount and let
the download wait: failed moves are retried after 1, 2, 4, 8, 16 and 32
minutes. An unmounted NAS deletes nothing: an empty mount point is treated as a
missing volume and the episodes stay registered.

### Trash

The **trash** (*Archive and space → Trash*) receives the files replaced by an
upgrade, the packs at the end of seeding and the duplicates. It is emptied
according to the retention; **Clean trash** in Maintenance empties it now.
Without a trash configured Gextto never deletes source files by itself.

### Language and metadata

- **Default language** (*Library and renaming*): the language series and
  movies get unless you choose another.
- **Metadata**: TMDB is the default source of titles, episodes and posters;
  TVDB is used when only its key is set. The keys go in *Access and services →
  Metadata services*.
- **Remove audio tracks in other languages**: after the import it keeps only
  the audio in the chosen language, without re-encoding, and checks the copy
  before replacing the file. It needs `ffprobe`, `ffmpeg` and `mkvmerge`.

## 18. Notifications

Gextto can notify you through **Telegram**, **e-mail**, **webhook**,
**Discord**, **Slack**, **Gotify**, **ntfy**, **Pushover**, **Apprise** or your
own **program**. It is set in *Configuration → Notifications*.

### Setting up a channel

1. Fill in the channel's fields (an empty field turns the channel off).
2. In the **Events** group choose what to receive.
3. Press **Send test notification**: it uses every configured channel and
   shows an error at once.
4. Try a real completion only with an unimportant download.

### Which events

| Group | What it covers |
|---|---|
| **Download notifications** | a download starts, is discarded (a pack holding nothing useful), completes or fills a missing episode |
| **Error notifications** | a download fails, a torrent or a comic goes into error, the service stops on an error |
| **Health and engine notifications** | problems with health, the cycle, the indexers, logins, the torrent engine, and the return to normal |
| **Maintenance notifications** | restarts, completed backups, audio track clean-up |

Completion notifications carry size, duration and average speed. For a season
pack they tell the **new** episodes apart from those that **replaced** a worse
version. **Anti-spam — minimum interval** avoids receiving the same event
several times; **Message language** chooses the language, independently of the
interface's.

Gextto also warns you about restarts (and abnormal stops, at the next restart:
a dead process cannot report its own crash), fatal errors, gx-torrent problems
and the switch to another engine.

### Webhook: which URL and which format

The **Webhook** channel sends events to an external service. Pick the
**Format**, paste the **URL** and put the credential in **Webhook token**, not
in the URL.

| Service | Webhook URL | Format | Credential |
|---|---|---|---|
| Your own endpoint | your URL | **Gextto (signed JSON)** | «Webhook secret» signs the body (HMAC SHA-256, header `x-gextto-signature`) |
| Discord | the channel's webhook URL | **Discord** | none |
| Slack | the incoming webhook URL | **Slack** | none |
| ntfy | `https://ntfy.sh/<topic>` | **ntfy** | «Webhook token» = Bearer token, if the topic is protected |
| Gotify | `https://gotify.example/message` | **Gotify** | «Webhook token» = application token |
| Pushover | `https://api.pushover.net/1/messages.json` | **Pushover** | «Webhook token» = application token, «Webhook user» = user key |

Each service also has **dedicated fields** (Discord, Slack, Gotify, ntfy,
Pushover, Apprise, Program), so several channels can be on at once. Every
channel receives the same event, and a slow one does not hold up the others.
The **Program** receives the data in the `GEXTTO_*` variables and runs without
a shell.

## 19. Integrations with other programs

No integration is needed to download and archive. Turn them on one at a time
and always use the test button. Fields and actions are in
[Appendix B](#appendix-b-reference--integrations).

### Jellyseerr (in place of Sonarr and Radarr)

Gextto answers Jellyseerr's requests as Sonarr and Radarr would:

1. in Jellyseerr add a Sonarr server and a Radarr server with Gextto's address;
2. as the key use Gextto's **API key** (*Configuration → Access and services*);
3. pick quality profile and folder from the lists Gextto exposes.

A requested series is followed only for the seasons asked; if you already
follow it, the seasons are added. A movie already there is not duplicated.
Jellyseerr's «search now» is not handled: the title is searched by the normal
cycle. It is not full Sonarr and Radarr compatibility: unhandled requests
answer 404.

### Jellyfin and Plex

Enter the server URL (reachable from Gextto's computer) and a token: Gextto asks
for a library refresh when a new file arrives.

### Simkl, calendar, hooks

- **Simkl**: complete the PIN flow, check the account and then import the
  watchlist.
- **iCal calendar**: upcoming episodes in a calendar you can add to your phone.
- **Event hooks**: a program run on events such as `download_started`,
  `torrent_completed`, `season_pack_completed`, `torrent_error`. The fields take
  placeholders (`{title}`, `{path}`, `{series}`, `{episode}`…) and receive the
  `GEXTTO_*` variables. It runs without a shell (no pipes or `&&`: use a
  script), with a default timeout of 60 seconds and at most four hooks at once.
  Try it first with a harmless program that writes a log.
- gx-torrent **metrics and events**: `GET /api/v1/metrics` for Prometheus and
  `GET /api/v1/events` for Home Assistant (see [API.md](API.md)).

## 20. Access and security

The interface is an **administration panel**: whoever opens it can change
everything. By default it is open, because Gextto is meant for a trusted home
network.

- **Home only**: leave it as it is, or restrict port 5000 with the firewall.
- **From outside**: in *Configuration → Access and services* turn on **Require
  login** and set a password. With **No login from the local network** (the
  default) you are not asked at home. Put Gextto behind an HTTPS reverse proxy
  or a VPN.
- **Scripts, TUI, calendar**: they use the **API key** (`X-Api-Key` header or
  `?apikey=`). **Generate** creates a random value, **Copy** copies it.
- **gx-torrent on the network**: if you open its page on the LAN, set its
  **token**.

Regardless of the login, Gextto refuses changes that the browser declares as
coming from another site: a malicious page cannot reconfigure it through your
browser. The full model is in [SECURITY.md](SECURITY.md); the keys in [Access
keys](api-keys.md).

---

# Part IV — Keeping everything in order

## 21. Checking that everything works

### The Dashboard

This is the page to look at every day. From the top:

- **In session** — torrents downloading, stuck and seeding, the bandwidth of
  the last 30 days and the **traffic chart** of the last 30 minutes (download
  and upload, with the peak and the limit in force; with eD2k, aMule as dashed
  lines);
- **To check** — stuck downloads (with reason and next retry), pending moves to
  the library, warnings and errors. A warning stays while the download is in
  the session and disappears by itself when it leaves with nothing pending;
  **Clean** marks them as read. When all is well it says «Nothing to check»;
- **Manual search** and **Automatic search** — the next run, the outcome of the
  last cycle and the buttons to start one (everything, or only Series, Movies,
  Comics);
- **Background operations** — renames, scans, MediaInfo, integrity check,
  searches for missing episodes: with the percentage and **Cancel**;
- **Upcoming**, **latest downloads**, **recent activity** and **latest found**.

The **DRY-RUN** or **ACTIVE** badge at the top tells you whether Gextto really
downloads.

### Health

*Health* (🩺) answers «does everything work?»:

- process, system and service state, folder permissions, disks;
- **API status** and **Provider status**: each indexer with its state and
  backoffs (see [Sources](#reachable-does-not-mean-working));
- **Searches**: the latest cycles, with duration, releases examined, downloads
  started, errors and sources that did not answer;
- **Title queue**: the titles held by the delay, and for how long;
- **Failed imports**: how many per day over the last 7 days (a growing number
  points to a disk or NAS refusing the moves);
- **Torrent engine** and, with eD2k on, **eD2k and Kad (aMule)**.

### The log

**Log** shows the live log, with filter, number of lines and follow/pause. The
lines are in English, in readable sentences. Every cycle prints a **SOURCE
REPORT** with the outcome of each source and the decisions taken. The file is
`<data>/gextto.log` (rotated at 5 MB); the engine's is
`<data>/gx-torrent/gx-torrent.log`, also readable from *Maintenance →
gx-torrent log*.

A line repeated in a burst — a file the disk refuses hundreds of times in a
second, for instance — no longer fills the log: the first copy is shown, the
others are counted and every half hour a summary says how many there were;
after a minute the line can appear again, so a problem that goes on is never
hidden. If the engine exits with an error while Gextto is replacing it (an
update or a restart), Gextto writes a line saying where the detail is: the file
`<data>/gx-torrent/gx-torrent.crash.log`.

### The history of a download

Every log line about a download ends with an **acquisition ID** (`acq:
7f3a2c`), the same from the choice of the release to its arrival in the
library. Type it in the log filter to see only that download. The same lines
are kept for 180 days as **history**: *History* tab in the torrent's details,
📜 on every episode, *Download history* on the series page. They stay readable
after log rotation and after the torrent is removed.

### Charts and activity

**Charts** shows CPU, RAM, network, disk and daily usage; **Activity** the
recent events.

## 22. Looking after the library

All these actions are in **Maintenance** (full list in
[Appendix C](#appendix-c-reference--maintenance)). Long operations run in the
background: you can keep using Gextto and cancel them.

> [!TIP]
> Always use the **preview** first, where there is one. Operations that move
> files send them to the trash, they do not delete them.

### Renaming

- **Rename preview** on a series, or **Rename all** for the whole library:
  first you see *Old → New*, then you apply.
- **Rename folder contents**: pick any folder; Gextto recognises series and
  movies from the names, matches them against TMDB/TVDB and proposes new names.
  You can pick an alternative result, accept or reject each proposal and then
  **Apply selected**. Conflicts are left unchanged; subtitles, images and NFO
  files follow the new name.
- **Restore source**: puts the `[WEB-DL]`, `[BluRay]`… token back in names that
  lost it, taking it from the original release. If it is unknown, the file does
  not change; if the folder already holds a file with the new name, the episode
  is skipped and counts as an error, and nothing is overwritten.

### Duplicates

*Duplicate preview* and *Clean duplicates* find clearly worse videos left next
to the better version in the same folder (the old 480p next to the new 1080p)
and move them to the trash. At equal resolution the version in the preferred
language is kept. Files of torrents in the session are protected.

### Real quality and file integrity

- **Refresh MediaInfo** reads with `ffprobe` resolution, codec, HDR, audio and
  languages of the library files. This data is used to compare upgrades with
  the file as it really is, not with its name. New files are read when the
  download finishes; for the others there is also the automatic backfill.
- **File integrity check** (*Configuration → Automatic maintenance*) decodes the
  videos a little at a time, in the background, and finds truncated or damaged
  files. The **Check status** panel lists the damaged or unreadable files with
  the reason; **Redownload** next to an episode takes it out of the library,
  blocklists that release and looks for a different one. With *File integrity
  check — re-download damaged files* on it does so by itself. It needs `ffprobe`
  and `ffmpeg`.

### Night maintenance

In *Configuration → Automatic maintenance → Night maintenance* pick a window
(default 01:00–07:00) for the heavy jobs: database optimisation and checks, the
report of files gone from disk, audio clean-up of the files already in the
library (if you enable it), integrity check and MediaInfo. Each job starts only
when Gextto is idle, one at a time. The panel shows what is running or why it is
waiting; **Run now** starts a pass at once.

### Database clean-ups

- **Housekeeping** keeps the database tidy (cycle statistics, old errors,
  expired backoffs, `VACUUM` when needed). It does **not** touch the library,
  the downloads or the Archive.
- **Release archive cleanup** (*Automatic maintenance*) removes old releases
  from the catalogue, if you enable it. It touches no file and no download.
- **Archive SQL maintenance** (in *Maintenance*) runs ready-made operations on
  the catalogue: releases older than 6 months or 1 year, in a language or
  without it, empty titles, duplicates. It first shows the preview with the
  number of rows, then **Delete permanently**. Make a backup before a large
  clean-up.

### What not to confuse

| Set | What it holds | What cleans it |
|---|---|---|
| **Torrent session** | the torrents in the engine | *Remove*, *Clean completed*, end of seeding |
| **Library** | your videos | *Clean duplicates*, upgrades (to the trash) |
| **Trash** | the files taken out of the library | retention, *Clean trash* (final) |
| **Archive** | the catalogue of the releases seen | *Release archive cleanup*, Archive SQL maintenance |
| **Download history** | the list of finished downloads | the history retention |

## 23. Backup and restore

### What a backup contains

A `gextto-backup-<date>_<time>.zip` file in `<data>/backups` with:

- the four databases: series, episodes, movies and downloads
  (`gextto_series.db`), settings and indexers (`gextto_config.db`), feed archive
  (`gextto_archive.db`), comics (`gextto_comics.db`);
- the other configuration files of the data folder, gx-torrent's state
  included;
- the external folders you list in *Files and folders to include*.

It does **not** contain the videos, the torrent session, the IP filter, the
caches, the logs, the trash or `/etc/gextto/gextto.env`: keep a copy of that
yourself.

### Scheduling backups

In *Maintenance → Backup* choose the retention and when to make them (by hand,
every N hours or at a fixed time). You can also send them to **FTP** (with
**Test FTP**, which tells you which step fails), to a synced folder or to
Telegram. The Dashboard button makes a backup at once.

### Restoring

1. Install Gextto with the same options as before, or stop the service:
   `sudo systemctl stop gextto`.
2. Set the current data folder aside:
   `sudo mv /var/lib/gextto /var/lib/gextto.old`.
3. Extract the backup and give it to the service user:

   ```bash
   sudo install -d /var/lib/gextto
   sudo unzip /path/gextto-backup-<date>.zip -d /var/lib/gextto
   sudo chown -R <user>:<group> /var/lib/gextto
   ```

4. Put `/etc/gextto/gextto.env` back if you had one, then
   `sudo systemctl start gextto`.
5. Open **Health**. With gx-torrent the downloads in progress are added again
   from the saved magnets, and the data already downloaded is rechecked and
   reused.

A restore does not move the videos. If the library changed place in the
meantime, fix the paths or run *Scan archive* before turning the searches back
on.

## 24. Updating Gextto

### From the interface

*Maintenance → Updates* shows the installed version, the channel, the latest
published version and what is new. Gextto checks by itself every 6 hours
(*Update check interval*); **Check for updates** checks now. When a new version
is out:

1. press **Update**: Gextto backs up the databases and asks the
   `gextto-update` system service for the update;
2. wait: Gextto finishes the copies to the library, restarts and the panel
   tells you by itself;
3. at the first visit a **Gextto updated** pop-up shows what is new.

If the new version does not start, the previous one comes back by itself. At
the bottom of the page you find the exact version: the third number is the
build (`1.2.2065`), with the commit it was built from.

### From the command line

```bash
sudo /opt/gextto/gexttod --update
```

It downloads the archive, verifies its checksum (mandatory), replaces program
and engine and restarts the service, keeping the previous versions
(`gexttod.prev`, `gx-torrent.prev`) and putting them back if the new ones do not
start. Data and configuration are not touched, and neither is the systemd unit:
your customisations stay. Options: `--channel stable`, `--release <tag>`,
`--archive <file>` (offline), `--force`, `--no-restart`.

On a source installation: `git pull` and `make build`.

### Other commands

| Command | Effect |
|---|---|
| `gexttod --version` | installed version, build and channel |
| `gexttod --help` | usage summary |
| `gexttod --config <file>` | uses a different configuration file |
| `gexttod --dry-run` | starts in test mode until the next restart |
| `gexttod tui` | opens the terminal interface |

## 25. Using Gextto from the terminal (TUI)

The TUI is a tabbed interface for the terminal, handy over SSH even from a
phone. It uses only the service's API, so it works from another computer too.

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang en
GEXTTO_URL=http://host:5000 GEXTTO_API_KEY=<key> /opt/gextto/gexttod tui
```

The tabs are Status, Torrents, Log, Health, Archive, Missing, Blocklist,
Library and Maintenance.

| Key | Action |
|---|---|
| `1`–`9`, `Tab` | change tab |
| `r` | refresh |
| `?` | help |
| `/` | filter (in the Log) |
| `f` | follow or freeze the log |
| `Enter` | open the details |
| `p` | pause or resume |
| `y` | copy the link |
| `d` | remove an eD2k download from aMule |

On a narrow terminal (under 60 columns) the interface becomes compact. The full
guide is in [tui.md](tui.md).

---

# Part V — When something goes wrong

## 26. Troubleshooting

### An episode did not arrive: where to look

Start from the series page and press **📜** on the episode:

| What you see | What it means | What to do |
|---|---|---|
| *No events* | Gextto never downloaded it | 🔍 on the episode and **Why not this one?**: is the release missing from the sources, or was it rejected by filters, quality, language or delay? Check *Health* too |
| *📥 Downloading* and then nothing | the download is running or stuck | open the torrent in *Download*: «⏸️ is stuck» means few people are sharing; «❌ Gave up» that it was given up and another version will be searched for |
| *🎉 downloaded* but no 📁 | downloaded but not in the library | look for «still cannot be moved» or «Could not move»: usually the NAS is unreachable, permissions or space. The retries resume by themselves |
| *discarded as inferior* | the library already had a version at least as good | nothing: this is the expected behaviour |
| *📁 added* / *♻️ updated* | it is in the library | if the name is not the expected one, see *A file is not renamed* below |

For the full detail open *Show in the log* from the torrent: the log is filtered
on that download alone.

### Nothing is downloaded

1. Does the badge at the top say **DRY-RUN**? You are in test mode (see
   [chapter 4](#4-the-first-download-in-test-mode-and-then-for-real)).
2. Is the *Automatic cycle* on (*Configuration → General*)?
3. Is the series or movie active, and are the seasons followed?
4. Are the sources **OK** in *Health*?
5. Does a manual search find releases? If so, open **Why not this one?**: it is
   usually a quality or language filter that is too strict.
6. Is there enough free space compared with the minimum set?

### A release is visible but not chosen

Open **Why not this one?** and look at the **Blocking** checks: followed title,
filters, sanity, quality/language, comparison with the library. If all pass,
look at the **Informational** ones: a better candidate, the delay, free space
or gaps to fill can change the choice.

### A source does not answer or shows «API error»

- Check *Health → API status* and press **Verify** on the source.
- Jackett: **base** URL (`http://host:9117`), API key copied again, at least one
  indexer enabled. Do not add the
  `/api/v2.0/indexers/all/results/torznab/api` path.
- indexarr: base URL (`http://host:8788`) and type **indexarr**.
- Sites behind Cloudflare: a working FlareSolverr is needed.
- After fixing it, **Reset** the source's backoff.

### A torrent is stalled

It is paused on purpose, out of the active queue: it will be resumed at the
next retry. You can also **Resume** or **Restart** it by hand. Look at the swarm
in the row: with 0 seeders it will probably not restart, and Gextto will look
for another version. The times are in [Stalled torrents](#stalled-torrents).

### The download is complete but not in the library

Look in the log for move, permissions and space. Temporary errors (NAS
unreachable, disk full, timeout) are retried after 1, 2, 4, 8, 16 and 32
minutes; only after the last one the torrent goes into error. A move
interrupted by a restart resumes by itself about a minute later. Do not delete
the source files until the library path shows up in the history.

If the download arrived **packed in RAR archives** (a set of `.part01.rar`,
`.part02.rar`… files, or `.rar`, `.r00`, `.r01`…) and holds no loose video,
Gextto extracts the video next to the archives before importing it, and says so
in the log ("arrived packed in RAR archives"). The archives stay, so the torrent
keeps being shared: while seeding lasts the space is used twice. Only
**uncompressed** archives (the form usually used for videos split into parts)
that are complete are extracted: a compressed archive, one protected by a
password, one with a missing volume or with data that does not match its
checksum stays as it is, and the log says why.

### A file is on the NAS but Gextto counts it as missing

Check the file name (it must contain season and episode, for example
`S01E02`), the series path and the permissions, then press **Scan archive**.

### An episode in the library cannot be downloaded again

Use **↻ Redownload** on the episode: it moves the file to the trash and searches
for the episode again even if a pack covers the season or you already have
later episodes. A special `SxxE00` does not cover the season.

### A file is not renamed

Check the *Library and renaming* settings and the TMDB key (or TVDB and its
PIN): without metadata the episode title becomes «Episodio N». Having
`mediainfo` installed helps.

### A watched folder does not import the file

The file must have a `.torrent` or `.magnet` extension. Gextto waits until size
and date are stable, then retries errors by itself. Check the log lines about
the watched folder.

### The interface does not open

- Check the service: `systemctl status gextto` (or `systemctl --user status
  gextto`).
- If the log says the data folder is already in use or the port is taken,
  another instance is running: stop it and restart.
- After a restart Gextto can take about a minute to be ready.

### The FTP backup fails

Use **Test FTP**: it tells you which step fails (connection, login, path,
upload, removal) and writes it in the log.

### Where the logs are

- Gextto: `<data>/gextto.log` (rotated at 5 MB × 4), or the **Log** page.
- gx-torrent: `<data>/gx-torrent/gx-torrent.log`, or *Maintenance → gx-torrent
  log*.
- aMule: *eD2k → aMule log*.
- Updates: `/var/log/gextto-update.log`.

## 27. Glossary

| Term | Meaning |
|---|---|
| **Alias** | another name under which a series appears in releases |
| **Archive** | the local catalogue of the releases seen in the feeds |
| **Backoff** | the growing pause in requests to a failing source |
| **Blocklist** | the releases that will not be downloaded again |
| **Cycle** | one automatic pass of searching and choosing |
| **Dry-run** | test mode: searches and decides, but does not download |
| **Forced gap** | the explicit request to download an episode again (↻ Redownload) |
| **Gap** | a missing episode among the ones you have |
| **Hardlink** | a second name for the same data, without taking space twice |
| **Indexer** | a service that searches releases on one or more sites (Jackett, Prowlarr, indexarr) |
| **Library** | the final folders with series and movies |
| **Placeholder** | the temporary row of a download not yet complete |
| **Quality profile** | a template of quality, language, subtitles and exclusions |
| **Release** | a result found by a feed, an indexer or a web engine |
| **Season pack** | a torrent with a whole season |
| **Seeding** | sharing a torrent after it completes |
| **Stalled** | a torrent that has received no data for too long |
| **Torrent session** | the torrents still handled by the engine |
| **Trash** | where files taken out of the library go before deletion |
| **Upgrade** | replacing a file with a better version |

---

## Appendix A. Reference — Configuration

The page is split into four areas, each with its sections: **What to look
for** (General, Sources, Quality and upgrades), **How to download** (Torrent
engine, Speed and network, Queues and performance, Seeding and completion),
**Where to store** (Library and renaming, Archive and space) and **System**
(Automatic maintenance, Notifications, Access and services, Diagnostics and
updates). On a computer the sections are in the left column, with the
number of settings; on a phone you pick them from the *Section* list. The page
address follows the open section, so it can be bookmarked. Links to the old
tabs (*Acquisition*, *Advanced*, *Translations*) open the section that took
over their content (*Translations* opens Diagnostics and updates). Entering Configuration hides the main menu bar; the **Menu** button at the top
left brings it back over the content (Esc closes it).

When a section gathers several groups of settings (for example **Search cycle**
and **Missing episodes** under *Automatic maintenance*), the group title stays
**stuck to the top** while you scroll, with a coloured accent bar.

Under the section title there is the **group index** (clickable chips) to jump
straight to a group. The **Modified only** checkbox, next to *Show technical
keys*, hides every setting still at its default value: handy to review only what
you changed. Rows highlight on hover.

How to read a row:

- **Name and description** on the left; the control in the middle with its
  **unit** (seconds, minutes, GB, KiB/s…) and, below it, the meaning of the
  **special values** (for example “0 = never”).
- Yes/no options are **switches**; times use the time picker. In **Speed and
  network**, **How the speed is decided** picks the rule: *Auto* (Gextto measures
  the line and adapts), *Fixed limits* (the two global limits) or *Hourly
  schedule*. With the schedule you get the table with one row per weekday, where
  every day has **one choice** among *Off*, *Limit* (your values, the only ones
  using the speed fields) and *Auto*, plus start and end, and **Outside the hourly
  windows: Auto**, which says whether Auto or the fixed limits apply outside the
  windows. The older single-window settings no
  longer appear on the page: until the table is saved it shows their values and
  they stay in use.
- Options that depend on a switch or a choice (for example the speed schedule or
  RAM disk fields) **appear only when they matter**, even before saving.
- A value different from the default shows **changed**, the *Default* value
  and the **↺ Default** button, which puts the default back in the field (it
  still has to be saved).
- The technical key (e.g. `refresh_interval`) is shown with **Show technical
  keys**; the search finds it anyway.
- Settings the active torrent engine does not use are collected, closed, in
  **Not used by the active engine** (or in the panel of the inactive engine):
  they stay saved and become active again when you switch engine. With
  qBittorrent-nox this includes the network, protocol, queue, cache and RAM disk
  options only gx-torrent reads: qBittorrent keeps its own configuration. Rare
  options are in the closed **For experts** panel.

Each row is saved with its own **Save**. A field changed and not yet saved is
marked **not saved** and the *Unsaved changes* bar appears at the bottom, with
**Save all** and **Discard changes**. Changing section or leaving the page with
pending changes asks for confirmation. The editors (feeds, filters, rules,
hooks) have their own *Save* button and also show **not saved** until you save
them.

### What to look for

#### General

*When and how often Gextto looks for new releases and missing episodes.*

**Search cycle**

| Setting | What it does |
|---|---|
| Automatic cycle (search and download) | The master switch of the cycle: when off, Gextto neither searches nor downloads on its own. On by default; with dry-run on it searches without downloading. |
| Automatic search interval | Interval between automatic series/movie searches, in seconds (21600 = 6 hours). *(Unit: seconds)* |
| Online title search in the cycle | Search titles on indexers during the cycle: auto (only when no feeds are configured), yes (always), no (never, use only feeds and the local archive). |
| Maximum release age | Ignore releases older than N days (0 = no limit). Releases without a date are treated as published today. *(Unit: days; 0 = no limit)* |

**Missing episodes**

| Setting | What it does |
|---|---|
| Search for missing episodes | Enable filling recognised gaps (missing episodes) from available releases. |
| Missing episodes per series in each cycle | Maximum gaps to search per series in one cycle (0 = unlimited). *(0 = unlimited)* Shown only while «Search for missing episodes» is on. |
| Deep search interval | How many hours between targeted live indexer searches for gaps. *(Unit: hours)* Shown only while «Search for missing episodes» is on. |
| Deep searches per cycle | Maximum number of live searches per cycle. The episodes never searched online come first, one per series in rotation (each pass starts after the series served last), then the ones searched longest ago. When the torrents have nothing, aMule answers once per season: one search fills every missing episode of that season, as **Search missing** does. Shown only while «Search for missing episodes» is on. |
| Last resort: accept a resolution outside the profile | On by default. When a missing episode has no copy within the quality profile, Gextto takes the one in the requested language even with a resolution outside the profile or unknown; this also covers copies found on eD2k, which often do not state the resolution in the name. Language, subtitles, audio and exclusions always apply. The download carries the «🆘 last resort» badge in *Downloads*, the log and the notification say so, and the upgrade replaces it as soon as a copy within the profile appears. Shown only while «Search for missing episodes» is on. |

**Delay before downloading**

| Setting | What it does |
|---|---|
| Delay for series | Delays the start of series downloads by this many minutes. 0 starts immediately. *(Unit: minutes; 0 = none)* |
| Delay for movies | Delays the start of movie downloads by this many minutes. 0 starts immediately. *(Unit: minutes; 0 = none)* |
| Skip the delay from this score | If a release reaches at least this score, the configured delay is ignored. *(0 = never)* |

#### Sources

*Where releases come from: RSS feeds, web engines, filters and watched folders.*

**Feeds and blacklist**

| Setting | What it does |
|---|---|
| Blacklist (one word per line) | Forbidden words, one per line: releases containing them are discarded. |
| Feed pages to read | How many listing pages to read per feed (3 is a good compromise). |
| Disable feeds after days of errors | After how many days without a success the feed is disabled automatically with a `#` in front of its line. It only fires when another feed worked on the same day; `0` turns the rule off. |

**Editors**

| Setting | What it does |
|---|---|
| RSS feeds | List of RSS feeds read on every cycle (one URL per line). Put `#` at the start of a line to exclude it without deleting it: `# https://…` or `#https://…`. |
| Web engines | Web search engines used by gap-filling when feeds and indexers find nothing. |
| Excluded content filters | Releases containing these words or scripts (e.g. [porno]) are excluded. |
| Source filters | Keywords to accept or reject for a single source, enabled per row. |
| Watched folders | The .torrent and .magnet files found in the given folders are **downloaded** and added to the session automatically (recursive, delete after). |

Below the editor there is the **Disabled feeds** frame: it lists the lines with `#` and says whether they were disabled by hand or automatically, with the date and the days of failure. The full activity — state, last success, last error, last results and attempts/successes/failures/releases taken over 1, 7 and 30 days — is on the **Health → Feed activity** page, with the last-24-hours summary. The panel tells feeds that share host and path apart through their parameters (for example ext.to's `filter=u=…`), masks credentials and, until a first attempt, says “waiting for the first cycle”. Removing the `#` and saving enables a feed again.

Many feeds can point at the same site (the uploader listings of ext.to or TorrentGalaxy). Gextto reads them spaced apart, not all together, and when the site answers “too many requests” it leaves that site alone: the pause starts at ten minutes and grows each time it happens again, up to six hours, while the other feeds of the same site wait for a later cycle. The pause clears by itself; you do not have to disable the feeds.

#### eD2k and Kad

*Connection to aMule, search on Kad and the eD2k servers, automatic fallback and stalled downloads.*

eD2k and Kad go through **aMule** (see [eD2k and Kad with aMule](#12-ed2k-and-kad-with-amule)):
Gextto drives an `amuled` that is already running, through external connections
(EC). It is off by default: without your consent Gextto does not contact the
network.

| Setting | What it does |
|---|---|
| eD2k and Kad through aMule | Connects Gextto to aMule: search, `ed2k://` links, downloads in Downloads and the eD2k page. Off by default. |
| aMule — address | Machine running `amuled`, usually `127.0.0.1`. Gextto must see aMule's incoming folder at the same path. |
| aMule — external connections (EC) port | `ECPort` in `amule.conf`, 4712 by default. |
| aMule — external connections (EC) password | The password in clear; aMule keeps its MD5 in `ECPassword`. Not displayed. |
| aMule folders | The incoming (download) and temporary folders are shown read-only: they belong to aMule and are changed in aMule/amuled. After the change restart Gextto (or reopen this page) so it uses the new path. |
| eD2k search network | **Kad** (default, no server needed), **Kad + eD2k servers** (both searches, results merged), **eD2k servers (global)** (every server) or **eD2k server (connected)**. Applies to the manual search; the automatic fallback always searches Kad and the servers. The same choice is on the eD2k page. |
| Use eD2k when torrents find nothing | A second, separate switch: when the torrent search for the episode and the season produces nothing, Gextto searches the same episode on eD2k/Kad (with the series name and every alias). It applies both to the deep gap-filling pass and to **Search missing** and single-episode search. Off by default. |
| Minimum sources for the fallback | eD2k source floor for accepting a result automatically. Manual search shows everything. *(Unit: sources; 0 = no minimum)* |
| eD2k search duration | How many seconds aMule collects results before Gextto shows them. *(Unit: seconds)* |
| Give up on a stalled eD2k download after | An eD2k download chosen by Gextto that receives no data for this many days is cancelled in aMule, the release is blocklisted and the episode is searched again. Downloads added by hand and paused ones stay. *(Unit: days; 0 = never)* |

The automatic fallback only accepts a result that matches the episode exactly,
has a recognised quality, a plausible size and at least the source floor. In
every other case the result stays visible in manual search and the gap stays
open: a missing episode is better than a wrong file in the library. If aMule is
unreachable, torrents continue unaffected.

On the **Sources** page the **eD2k and Kad** entry shows the connection sensor:
**open** (connected to a server with a high ID), **limited** (firewall/NAT: low
ID or Kad only), **not connected**, **aMule unreachable** or **unknown**. When
it is limited, open aMule's TCP and UDP ports on the router.

#### Quality and upgrades

*How releases are compared and when a better version replaces the one in the library.*

The page starts with what is needed most often and leaves the weights last:

1. **Upgrades and replacement** — the rules that decide when a better release
   replaces the one in the library (table below).
2. **Examples with the current weights** — reference scores (1080p WEB-DL,
   1080p H.265 DD+, 2160p Dolby Vision) and the stop threshold.
   **Impact on the library** — **Check now** replays the releases already seen
   in the feeds with the current rules and weights and says how many archived
   episodes would be replaced, with a few examples. Use it after changing the
   weights or thresholds: a high number means the next cycles would re-download
   a lot. It downloads nothing; series with “Allow upgrades” set to No do not count.
3. **Test a release** — paste a release name: Gextto shows the score item by
   item, the copy already in the library with its score and whether it would
   replace it, with the reason. It downloads nothing.
4. **Quality profiles** — the templates of quality, language, subtitles and
   exclusions (see *Quality profiles*); the list is the one exposed to
   Jellyseerr.
5. **Video**, **Audio**, **Release fixes** — the weights, in closed cards: open
   them only to change the preferences. For each: **higher = more preferred**.
6. **Custom groups**.

The **Custom groups** panel adds a bonus or a penalty to a release group (the name
must match the final tag of the release, e.g. TBK; case does not matter). The
library scores are recomputed automatically when you change the weights or when
a Gextto update changes the scoring rules, so a change is never mistaken for an
upgrade; *Maintenance → Recalculate scores* is still there to force it.

An upgrade **by score** needs a real quality gain: resolution, source, codec,
audio, HDR/DV, fixes and group must be worth at least the minimum difference
more. File size, subtitles and swarm health help choose between equivalent
releases, but on their own they never re-download an episode or a movie of the
same quality. The same rule holds for season packs and after the download: a
file of the same quality does not replace the archived copy just because its
score is higher. When the archived copy does not declare the source or the
group (for example a file imported or renamed with the title only), those
fields are not credited to the release found: without proof that the copy is
worse, it is not replaced. `ffprobe` fills resolution, codec, audio and channels
when the name has lost them.

The rule that prefers a WEB-DL source to a WEBRip or HDTV one follows the
weights too: it holds only while the WEB-DL **Source** weight is higher. Give
WEB-DL and WEBRip the same weight and Gextto treats them as the same quality: a
WEB-DL no longer replaces a WEBRip (HDR, resolution, REPACK/PROPER and the score
still count).

**MediaInfo check before entering the library.** When a completed download is
about to replace an archived episode or movie, Gextto reads with `ffprobe` the new
file, still in the download folder, and the copy in the library, before moving,
linking or copying anything. Resolution, video codec, audio, channels and
HDR10/HLG are taken from the real streams, not from the name. If the name promised
more than there is (a «2160p» that is an upscaled 1080p, an «HDR» without HDR, a
«DDP 5.1» that is stereo AAC) and the real quality is not an upgrade, the download
is discarded and blocklisted and the library copy stays as it is; the log explains
it with «Upgrade not confirmed by MediaInfo». Source, Dolby Vision and
REPACK/PROPER stay as named, because `ffprobe` does not tell them apart reliably.
Without `ffprobe`, or with the archived copy unreachable, the decision taken before
the download stands. Season packs do not go through this check.

**Upgrades and replacement**

| Setting | What it does |
|---|---|
| Min score difference for upgrade | Minimum score difference to replace a file with a better upgrade. Only quality counts: size, subtitles and swarm health are not enough to reach it. |
| Stop upgrading above this score | Upgrade ceiling: once the library file scores at least this much it is no longer replaced by better releases; only a REPACK or PROPER, which fixes a defective release, is still accepted. With the default scores: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. It adds to the minimum difference: that one says *by how much* a release must improve, this one *up to where*. To stop upgrades for a single title set “Allow upgrades” to No with *Edit selected* (or in the profile you add it with). “Why not this one?” says when a release is skipped because of this ceiling. *(0 = always upgrade: no ceiling)* |
| Replace versions already archived | Replaces lower versions already archived with better upgrades. |
| Min score difference for cleanup | Minimum score difference to replace an existing file with a better one (cleanup). |

**Stuck downloads**

| Setting | What it does |
|---|---|
| Try an alternative after (stall) | If a download has been stuck for at least these minutes — at 0 B/s or still waiting for its file list (metadata, typical of dead magnets) — the search may start an alternative version of the same episode or movie. The stuck download stays in the session and may still recover; archive deduplication will then keep the best copy. *(0 = never)* |
| Score drop for the alternative | How many points below the stuck release an alternative may score: only an adequate alternative is started, never a poor version. The stuck download is not removed. *(0 = disabled)* |

### How to download

#### Torrent engine

*Which engine downloads the torrents and how Gextto reaches it.*

**Torrent engine**

| Setting | What it does |
|---|---|
| Torrent engine | Active torrent engine (gx-torrent or qBittorrent-nox). |

**gx-torrent**

| Setting | What it does |
|---|---|
| gx-torrent — Web API URL | gx-torrent API URL (e.g. http://127.0.0.1:8890); Gextto drives the daemon there. |
| gx-torrent — listen address (LAN) | Listen address of the managed daemon. Default `127.0.0.1:8890`: the web page and API stay on this host only. To expose them on the LAN use `0.0.0.0:8890` and set `gxtorrent_token` (without a token Gextto starts the daemon in `-insecure`). The port is aligned with the URL. |
| gx-torrent — automatic management (cache and queue) | Turns on gx-torrent self-management: dynamic queue and adaptive cache (sized on available memory, active downloads/seeds and storage type). On by default. Turn it off to set cache and slots by hand. |
| gx-torrent — proxy (socks5:// or http://) | Proxy for peers, HTTP trackers, web seeds, the IP filter list, RSS feeds, Torznab indexers and .torrent downloads; localhost and the local network stay direct. With a proxy, DHT and UDP trackers are disabled and FlareSolverr is not used as a fallback (not shown). |
| gx-torrent — announced client identity | What peers and trackers see: `gx-core` with the real version (default) or a qBittorrent, Transmission, Rain or anacrolix/torrent preset. It changes only the announced identity, not the protocol: some private trackers only accept whitelisted clients, check their rules. |
| gx-torrent — announced client version | Version used with the preset; empty uses the preset's default. |
| gx-torrent — recheck data when a download completes | Verifies the files of a finished download once, before seeding them: a disk that returns a wrong block is caught at once instead of being seeded for hours. It costs one full read of the data. Applies to downloads completed after it is turned on. |
| gx-torrent — anonymous mode | Hides the client version from peers and public trackers: the peer id loses it, the handshake sends no version and public trackers receive no User-Agent. Private trackers keep the configured agent. It does not hide your IP address: a VPN does that. |
| gx-torrent — announce to all trackers in a tier | Contacts every tracker of a multi-tracker tier at once and merges the peers, as qBittorrent does (on by default). Turn it off to contact one tracker per tier and move to the next on failure. |
| gx-torrent — request timeout | Timeout in seconds for HTTP requests to gx-torrent. *(Unit: seconds)* |
| gx-torrent — polling interval | Minimum interval in milliseconds between torrent status reads (the daemon owns the queue). *(Unit: ms)* |
| gx-torrent — DLNA/UPnP server | Enables the daemon's DLNA/UPnP media server: the torrents' **completed** media is visible to TVs and consoles on the local network. Local network only, no authentication; off by default. Works with the gx-torrent engine. |
| gx-torrent — DLNA: HTTP bind | Address and port the media server listens on (e.g. `:8200` for all interfaces, `192.168.1.10:8200` for one). Empty = `:8200`. Discovery uses UDP 1900 on the same interface. |
| gx-torrent — DLNA: server name | Name shown to the DLNA players on the network. Empty = `gx-torrent`. |
| gx-torrent — DLNA: library folders | Folders (e.g. the archive on the NAS) the media server shows beside the torrents, browsable by folder; separate them with `;`. Empty = automatic: the archive folder and the folders holding the series archives. Read only; the trash, hidden folders and NAS system folders (`@eaDir`, `#recycle`) are excluded. |

**qBittorrent-nox**

| Setting | What it does |
|---|---|
| qBittorrent-nox — downloaded and updated by Gextto | Gextto downloads the latest qBittorrent-nox release itself, installs it in the application folder (next to gexttod), starts/stops it with the service and updates it (with backup and rollback). The engine in use is still chosen in “Torrent engine”: this option does not change it. |
| qBittorrent-nox — Web API URL | qBittorrent-nox Web UI URL (e.g. http://127.0.0.1:8080). |
| qBittorrent-nox — user | qBittorrent-nox Web UI username. |
| qBittorrent-nox — password | qBittorrent-nox Web UI password (not shown). |
| qBittorrent-nox — category | Category applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — request timeout | Timeout in seconds for HTTP requests to qBittorrent-nox. *(Unit: seconds)* |
| qBittorrent-nox — polling interval | Interval in milliseconds between torrent status reads. *(Unit: ms)* |
| qBittorrent-nox — path mapping | Needed only when qBittorrent sees the folders under different paths (Docker, NAS, another host): one line per path, local=remote form. **Propose** reads qBittorrent's download and temporary folders and fills the lines; check them and press **Save**. Same machine with the same folders — or managed mode —: leave it empty. |

**Actions**

| Setting | What it does |
|---|---|
| Apply engine | Checks the configured engine and tells whether a restart is needed (*Torrent engine* panel). |
| Install / Optimize qBittorrent-nox, qBittorrent-nox status, Connection test | In the *qBittorrent-nox* panel: install or update the managed binary, show where it is installed and whether an update exists, check the connection. |

#### Speed and network

*Bandwidth limits, time slots, ports and protocols of the torrent session.*

**Speed limits**

| Setting | What it does |
|---|---|
| How the speed is decided | The speed rule: **Auto** (Gextto measures the line and adapts, no cap), **Fixed limits** (the two limits below) or **Hourly schedule** (windows per day, each with limits or Auto). In Downloads, from the **Bandwidth** menu, you can always make a temporary exception, which beats the rule. It is not a setting of its own: it turns «Speed schedule enabled» and «Outside the hourly windows: Auto» on or off. |
| Global download limit | Global download limit in KiB/s (0 = unlimited). *(Unit: KiB/s; 0 = unlimited)* Shown with «Fixed limits» and «Hourly schedule». |
| Global upload limit | Global upload limit in KiB/s (0 = unlimited). *(Unit: KiB/s; 0 = unlimited)* Shown with «Fixed limits» and «Hourly schedule». |
| Speed schedule enabled | Set from «How the speed is decided» and not shown on the page; still available through the API. |
| Outside the hourly windows: Auto | With the hourly schedule: outside the windows the speed is Auto instead of the fixed limits. Shown only with «Hourly schedule». |
| Schedule — per day | One choice per day: **Off** (no window), **Limit** (the window with your download and upload values) or **Auto** (the window in which Gextto measures the line and adapts). Only «Limit» enables the two speed fields; the times stay editable. A window past midnight belongs to the day it starts on. *(a «Limit» day has its own values (0 = unlimited); «Auto» lets Gextto measure the line and adapt; a window past midnight belongs to the day it starts on)* Shown only while «Speed schedule enabled» is on. |
| Schedule — start time | Schedule start time (HH:MM). Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — end time | Schedule end time (HH:MM). Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — days | Active days: 0=Mon … 6=Sun (e.g. 0,1,2,3,4). *(no day = never active)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — download | Download limit in KiB/s during the schedule. *(Unit: KiB/s; 0 = unlimited)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — upload | Upload limit in KiB/s during the schedule. *(Unit: KiB/s; 0 = unlimited)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Auto respects latency | With the speed on Auto, Gextto measures the ping to Google and Cloudflare every 2 seconds while it transfers, every 30 seconds while the line rests (8.8.8.8, 8.8.4.4, 1.1.1.1, 1.0.0.1; the lowest time is used). While the ping is good it raises download and upload; when it rises it cuts 15% and goes down to 128/64 KiB/s. It remembers the speed at which the ping degraded and settles just below it, trying to pass it once a minute. If the ping stays good at full speed the cap is removed; when only background traffic (DHT) is left, the cap is dropped. The first time it applies a cap while downloading and the ping has risen, it writes it to the log at once; a check that only measures the line, or one that runs with only seeding, stays in the debug detail and does not appear in the normal log. After that it writes at most one line a minute, only when the speed changes by at least 10% and the trend confirms it. On an asymmetric line upload counts as load from a tenth of the capacity (between 32 and 256 KiB/s) and is capped before it saturates; if it starts loading during the control phase the cap starts at once. The measured ping appears next to the **↓ Download** speed in the top bar. On by default. |
| Auto — what measures the latency | `both` (default): the ping decides, the router counters are a diagnostic and a brake. `ping`: ping only, the router is not read. `router`: old behaviour, yield only to the other devices the router sees. In `both` and `router`, when the router sees another device active, Auto **takes no more room**: the caps stay where they are and do not drop. This is how it respects a call or a game, which build no queue and so do not move the ping; in `ping` this cannot happen by construction, because the router is not read. |
| Auto — host to measure latency | Host (or host:port) to measure the latency to with a TCP connection, only while Auto is on: every 2 seconds while Gextto transfers, every 30 seconds while the line rests. Empty = Google (8.8.8.8, 8.8.4.4) and Cloudflare (1.1.1.1, 1.0.0.1), port 443; the lowest time is used, so a distant or slow server cannot fake the measurement. |
| Auto — line maximum, download | The real maximum download speed of the line, in KiB/s (e.g. 9000 for 9 MB/s). 0 = unknown: Auto discovers it. If you enter it, Auto never goes above it and finds the right point sooner. |
| Auto — line maximum, upload | The real maximum upload speed of the line, in KiB/s (e.g. 800). 0 = unknown: Auto discovers it. If you enter it, Auto never goes above it and finds the right point sooner. |
| Apply the limits to peers on the local network too | Off (default, recommended): the peers of your home or office network (private addresses 10.x, 172.16–31.x, 192.168.x, link-local, loopback and local IPv6 fc00::/7) download and upload at full speed, as in qBittorrent and libtorrent: the speed limits are meant for the Internet line. On: they follow the global and per-torrent limits too. With a proxy configured every peer follows the limits (behind a proxy gx-torrent cannot tell where a peer is). gx-torrent only (qBittorrent has its own option). Changing it restarts gx-torrent. |
| Upload share for peers that give back | Percentage of the limited upload that goes first to the peers that send you data for a torrent you are still downloading (trading); the rest goes to the peers you only seed to. It matters only while an upload limit is in place — Auto's included — and when both groups want more than there is: what one group does not use goes to the other. 0 (default) = off, every peer gets the same share. Values from 1 to 99; 70–80 puts downloads first without stopping seeding. gx-torrent only. Changing it restarts gx-torrent. |

**Ports and interfaces**

| Setting | What it does |
|---|---|
| Min port | Minimum torrent session port (requires service restart). |
| Max port | Maximum torrent session port (requires service restart). |
| Listen interfaces | Where the engine accepts connections: 0.0.0.0:6881-6891 for all interfaces, 127.0.0.1:6881 local only, or wg0:6881/tun0:6881 for a VPN. The suggested value works in most cases. |
| Outgoing interface | VPN killswitch: interface used for all outgoing BitTorrent traffic. |
| Test ports | Checks that the peer port is listening and that the router forwards it (the message says "open", "listening but not forwarded" or "closed"). Available with the **gx-torrent** engine. |

**Peer and tracker discovery**

| Setting | What it does |
|---|---|
| DHT | Enable the DHT network to find peers without a tracker. |
| PEX | Peer Exchange: exchange peers with other clients. |
| LSD | Local Service Discovery: finds peers on the local network. |
| UPnP | Opens router ports automatically with UPnP. |
| NAT-PMP | Opens router ports automatically with NAT-PMP (PCP, RFC 6887, is tried too). |
| uTP | Enable the uTP (UDP) protocol in addition to TCP. |
| Holepunching (BEP 55) | Opens a direct connection to peers behind NAT through a relaying peer (BEP 55). Requires uTP. |
| DHT bootstrap nodes | Initial DHT nodes (host:port separated by comma). |
| Add the trackers of a public list | On by default: gx-torrent adds the trackers of the list below, at most 200, to **new public** torrents. The list is downloaded at start and then every day (through the proxy or the VPN interface when configured) and kept across restarts. A private torrent never gets it: for a magnet it waits until the metadata confirms it is not private. Torrents that were already there stay as they are. gx-torrent only. Changing it restarts gx-torrent. |
| Tracker list to add (URL) | Address of the list, one tracker per line (`http`, `https`, `udp`, `ws`). By default it is the "best" list of the *trackerslist* project (`https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt`), kept current by its authors; you can name another one. Empty = the default list. Shown only with the option above on. |
| Refuse the clients on the list | On by default: gx-torrent refuses the peers whose client is on the list below and does not dial them again for that torrent. Meant for clients that download without ever giving anything back (offline-download services and the like); a normal client that shares little is not touched. gx-torrent only. Changing it restarts gx-torrent. |
| Clients to refuse | Already filled with the suggested list: `-XL, -SD, -XF, -QD, -BN, Xunlei, Thunder, QQDownload, Xfplay`. Comma-separated or one per line. An entry starting with `-` is a peer ID prefix (for example `-XL`); the others are looked for in the name the client announces (case-insensitive). Empty = the suggested list. Shown only with the filter on. |
| Show the country of the peers | On by default: the *Peers* tab shows each address's country. gx-torrent downloads the free **DB-IP "IP to Country Lite"** database (about 5 MB) by itself at start and then once a month, and keeps it in the data folder; every lookup stays on this computer. The database is loaded into memory only when you open the *Peers* tab (about 6 MB) and released after 10 minutes unused. The data is DB-IP's, CC BY 4.0 licence: the tab says so. gx-torrent only. Changing it restarts gx-torrent. |
| Country database (CSV file) | Optional: your own address → country CSV file instead of the one downloaded automatically, in the DB-IP "lite" form (`first,last,COUNTRY` with addresses) or IP2Location LITE (with numbers), IPv4 and IPv6. Empty (default) = the DB-IP database updated every month. Shown only with the option above on. |

**Encryption and IP filter**

| Setting | What it does |
|---|---|
| Encryption | Encryption policy: 0 disabled, 1 enabled, 2 forced. |
| Apply IP filter | Apply the IP filter to HTTP trackers as well. It is off by default: the bundled list contains Cloudflare ranges, so trackers hosted there (many private ones) would stay unreachable and the *Trackers* tab would show "tracker IP is blocked". Peer filtering is always on. |
| IP filter (file/URL) | Local file or URL of the IP blocklist. The default is the eMule Security list (`https://upd.emule-security.org/ipfilter.gz`), in P2P format and updated every day. |

**Actions**

| Setting | What it does |
|---|---|
| IP filter → Load / update now | Downloads the list from the configured URL (or uses the local file) and applies it to the active engine right away, gx-torrent included. |

#### Queues and performance

*How many torrents stay active, connections, RAM disk and cache.*

**Automatic management**

| Setting | What it does |
|---|---|
| Adapt values over time | Adjusts over time the values the system chose (the minimum free space, for instance) when conditions change. It never touches values you set by hand; turn it off to keep them fixed. |

**Queue and slots**

| Setting | What it does |
|---|---|
| Automatic queue and resource management | Automatically adjusts how many torrents are active based on load. |
| Minimum dynamic download slots | Minimum number of dynamic downloads. The queue changes by at most one at a time. Shown only while «Automatic queue and resource management» is on. |
| Maximum dynamic download slots | Maximum number of dynamic downloads. Consecutive consistent samples are required before increasing the queue. Shown only while «Automatic queue and resource management» is on. |
| Don't count stalled torrents in active slots | Torrents that are not transferring data do not consume an active slot. |
| Active downloads | Base value for active downloads; with the dynamic queue it is adapted at runtime. |
| Active seeds | Base value for active seeds; with the dynamic queue it drops to 1 when downloads are queued. |
| Active torrents limit | Base value for the active torrent limit; with the dynamic queue it becomes max(base, downloads + seeds + 2). |

**Download mode**

| Setting | What it does |
|---|---|
| Sequential download | Download files sequentially instead of in scattered pieces. |
| First/last piece of files | Download the start and end of every file first in all new torrents, then continue normally: useful to watch a video while it downloads. Works with every engine; torrents already running are not changed. |
| Preallocate disk space | Reserves all disk space up front before starting the download. |

**Connections**

| Setting | What it does |
|---|---|
| Automatic connections (sized from the machine) | On (default): Gextto lets the engine decide how many connections to keep for each torrent from the machine's cores and RAM, and trims the ones on torrents that have made no progress for a couple of minutes despite many peers. With many torrents active together the engine also keeps a total for the whole machine (75 connections per core), shared among the torrents with downloads first; a torrent on its own always gets all of its connections. The value below stays the ceiling. Turn it off to use a fixed number. gx-torrent only. |
| Max connections per torrent | Ceiling on the connections per torrent (-1 = unlimited, the default). With automatic connections on it is the most the engine may use; with it off it is the fixed limit. It applies to every torrent without a limit of its own (*Limits* tab of the detail) and takes effect at once, running torrents included; gx-torrent only. |

**RAM disk**

| Setting | What it does |
|---|---|
| Use the RAM disk | Downloads to RAM the torrents that fit the threshold; larger ones go to disk. |
| RAM disk folder | RAM disk to use for in-progress downloads, if available. Shown only while «Use the RAM disk» is on. |
| Maximum size per torrent | Maximum size of a single torrent allowed on the RAM disk (GB). *(Unit: GB)* Shown only while «Use the RAM disk» is on. |
| Free margin to keep | Free space to leave on the RAM disk once the download completes (GB). *(Unit: GB)* Shown only while «Use the RAM disk» is on. |
| Minimum free space | Minimum free space in bytes required to use the RAM disk. 0 = use the configured margin. *(Unit: bytes; 0 = use the margin)* Shown only while «Use the RAM disk» is on. |

The choice is made as early as possible: a torrent whose size is already known
and exceeds the threshold goes straight to disk; one with an **unknown** size (as
with most magnets) stays on disk until the metadata arrives, and is then moved
onto the RAM disk **only if** the real size fits. An oversized file never touches
the RAM disk. The rule applies to every download — automatic and manually added —
and to every engine that uses the RAM disk; qBittorrent, which does not support
it, always downloads to disk.

Gextto also checks the machine's available memory: a `tmpfs` lives in RAM, so a
6 GB mount can look almost empty while the system has little memory left. Before
starting, the RAM disk's usable space is capped by the available memory
(`MemAvailable`): a download goes to the RAM disk only if both the `tmpfs` space
and the system memory can hold it, otherwise it stays on disk like oversized
files. When the memory cannot be read, only the `tmpfs` space counts, as before.

**For experts**

| Setting | What it does |
|---|---|
| Disk cache | Disk cache size in blocks (-1 = automatic). *(Unit: blocks; -1 = auto)* |
| Cache expiry | Seconds of inactivity after which a block leaves the cache. *(Unit: seconds)* |

**Actions**

| Setting | What it does |
|---|---|
| Optimize | Computes cache and buffers from the RAM. |
| Optimize for the system | Proposes the values that fit this hardware (memory, disks, network share, RAM disk), one row each with the reason; nothing is written until you apply the selected rows. A value you saved by hand is never proposed again. |

#### Seeding and completion

*How long to seed, what to do when a download finishes, how to inspect downloading packs and how to handle stalled torrents.*

**Seeds**

| Setting | What it does |
|---|---|
| Global seed ratio | Upload/download ratio after which seeding stops. 0 = infinite: this alone makes the seed never end and the file is not moved, even with a time limit. *(0 = infinite)* |
| Maximum seeding | Primary seeding limit in days; when greater than 0 it takes precedence over the limit in minutes. With days and minutes at 0 the time is infinite. *(Unit: days; 0 = use the limit in minutes)* |
| Maximum seeding (fallback) | Seeding limit in minutes, used only when Maximum seed (days) is 0. With days and minutes at 0 the time is infinite: the seed never ends and the file is not moved. *(Unit: minutes)* |
| Stop an idle seed after | Stops a seeding torrent that has sent no data for this many minutes (only time spent actively seeding counts). Like the other limits, the end of the seed is followed by archiving or removal. With qBittorrent the value is also sent as `inactiveSeedingTimeLimit`. *(Unit: minutes; 0 = off)* |

**Editor**

| Setting | What it does |
|---|---|
| Seed rules | Seeding limits (ratio, days, inactivity) by source/indexer and category; the first matching rule applies, an empty field uses the global limit, 0 removes the limit. |

**When a download completes**

| Setting | What it does |
|---|---|
| Remove completed items after seeding | On: at the end of seeding the completed torrent is removed from the session (like “Clean completed”). Off: at the end of seeding the torrent stays in the list as Completed and you remove it with “Clean completed”. It does not affect where files are moved. |
| Move episodes/packs to the archive (do not copy) | On: the downloaded source is moved into the library and removed from the download folder (for torrents at the end of the seed; for eD2k at completion, because seeding is managed by aMule). Off: the source is copied or linked and kept for sharing. |
| Hardlink instead of copy while seeding | On (default): a file that keeps seeding enters the library as a hardlink, without using the space twice; when downloads and library are on different filesystems it is copied. See “Hardlinks instead of copies”. |

**Pack inspection**

| Setting | What it does |
|---|---|
| Inspect downloading packs and discard useless ones | On: as soon as the engine knows the file list, Gextto checks whether at least one episode is really wanted (missing from the library or a real upgrade). If none is, the torrent is removed with its partial data and blocklisted, so it is not downloaded again. Off (default): the pack is downloaded in full and decided only when the download ends. |
| In useful packs, download only the files that are needed | On (with inspection): when the pack holds only some useful episodes, the engine downloads only those files and skips the others (priority 0); subtitles and files without an episode number stay active. Off: the useful pack is downloaded in full. |

**Stalled torrents**

| Setting | What it does |
|---|---|
| Consider stalled after | After this many minutes without progress the torrent is considered stalled. *(Unit: minutes)* |
| Retry stalled torrents every | Interval between reannounce attempts for stalled torrents. *(Unit: minutes)* |
| Remove stalled torrents after | After this period without progress, the torrent is removed automatically. Set 0 to completely disable automatic removal due to stalling. *(Unit: minutes; 0 = never)* |
| Remove torrents without seeders after | Stall with zero seeders (dead swarm): removed after this shorter period, and the episode is searched again right away. Set 0 to use the general threshold. *(Unit: minutes; 0 = use the general threshold)* |
| Let a release without seeders back in after | A release with an empty swarm — abandoned during the download or refused before starting because nobody was sharing it — stays blocklisted for this period, then it is back among the candidates: a 4K that has found seeders in the meantime can be downloaded as an upgrade, and the log stops repeating the same line at every search. A release that comes back and is still empty stays away four times as long, and twelve times as long the third time, up to three months; the count is forgotten after six months, and a download that succeeds starts it over. The copy already in the library stays meanwhile. Set 0 to keep it blocked forever. *(Unit: days; 0 = forever)* |

### Where to store

#### Library and renaming

*How files are named and organized in the library.*

**Rename**

| Setting | What it does |
|---|---|
| Rename episodes | Renames downloaded files using TMDB metadata (or TVDB without a TMDB key). |
| Movies as single files (flatten folders) | If Yes, a movie that arrives inside a torrent folder is moved to the movies folder as a single file, with its subtitles and artwork. If No, it stays in its folder. |
| Check renamed files every | How many hours between checks that archived/renamed files are still present. *(Unit: hours)* |
| Remove audio tracks in other languages | If Yes, after importing Gextto keeps only the audio tracks in the chosen language and removes the others, without re-encoding the video: quality, HDR and sync stay those of the original file. A file is touched only when it has more than one audio track and at least one is tagged in the chosen language. Before replacing it Gextto checks the copy: the same tracks in the same order, with the same codec, language and flags (a subtitle that was off stays off), the same duration, the same sync, and every packet of the video, of the kept audio and of the subtitles identical to the original, byte for byte. For MKV files it tries mkvmerge and, when the copy fails the check, makes it again with ffmpeg; when neither passes, the file stays as it is and the log says so. When the default track was among the removed ones, the track in the chosen language becomes the default. The temporary copy is written in the same folder: when there is not enough free space the file stays as it is. On network shares (NFS, SMB) the permissions are not touched: the share decides them. Files imported while the option is on are cleaned; those waiting or in progress when Gextto stops are cleaned after the restart. When the library file is a hardlink of a torrent still seeding, the cleaned copy is a new file: while seeding lasts the space is used twice. The option can be enabled only when ffprobe, ffmpeg and mkvmerge are installed; otherwise the row stays blocked with a warning. Every cleaned file sends a notification on the configured channels (Telegram, webhook, email) with the file, the removed tracks and the space saved. |
| Audio language to keep | Choose from the list (Italian, English, French…) or type the two or three letter code, for example ita or eng. The spellings of the same code are equivalent: it, ita, it-IT and Italian for Italian, pl and pol for Polish, and so on for every language in the list. Tracks declared in another language are removed; for untagged tracks, «Audio tracks without a language» decides. |
| When several tracks are in the chosen language | Keep them all, only the best one (more channels and bitrate) or only the first. |
| Audio tracks without a language | What to do with audio tracks that declare no language (no tag, und, mul…), which are often the original audio. **Keep them** (default): they stay in the file next to the tracks in the chosen language. **Remove them**: they are removed with the other languages. **Leave the file untouched**: a file with even one of them stays as it is. |

**Metadata languages**

| Setting | What it does |
|---|---|
| Default language | Default preferred language for series and movies (e.g. ita, eng). |
| TVDB language | Preferred language for TVDB metadata (e.g. ita, eng). |
| TMDB language | Language used for TMDB metadata (e.g. it-IT, en-US). |

**Editors**

| Setting | What it does |
|---|---|
| Name composition | Template editor with tokens and preview to compose file names. |
| Tag → folder rules | Associates a torrent tag with a temporary and a final folder. |

#### Archive and space

*Working folders, trash and minimum disk space.*

**Folders**

| Setting | What it does |
|---|---|
| Archive folder | Default archive folder for content without a dedicated path. |
| Download folder | Default download folder for every engine. |
| Temporary folder | Temporary folder for in-progress downloads. |
| Copy .torrent files to | Copy the .torrent files of downloads here (empty = no copy). Gextto keeps its own internal copy of the `.torrent` while the torrent is in the session and deletes it when you remove or archive it; this folder is left untouched. |

**Trash**

| Setting | What it does |
|---|---|
| Trash folder | Folder where replaced/duplicate files are moved (if left empty, uses the trash subfolder in the data folder). |
| What to do with replaced files | What to do with replaced files: move to trash or delete. |
| Trash retention | Retention days for non-forced cleanups; 0 deletes the whole trash content. Manual UI actions always empty the trash immediately. *(Unit: days; 0 = empty everything)* |

**Disk space**

| Setting | What it does |
|---|---|
| Minimum free space to download | Minimum free space (GB) on the download folder: below this threshold the cycle does not start downloads and gx-torrent pauses the running ones, which resume by themselves when space returns. *(Unit: GB; 0 = no cycle check, gx-torrent keeps its default of 1 GiB)* |

### System

#### Automatic maintenance

*Periodic cleanup of technical data, MediaInfo analysis and orphaned files.*

**Housekeeping**

| Setting | What it does |
|---|---|
| Periodic housekeeping enabled | Enables periodic cleanup of technical data and history. |
| Housekeeping — interval | Interval between two automatic housekeeping runs, in hours. *(Unit: hours)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — search-cycle statistics kept | Number of search cycle statistics to keep. Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — seen in feeds | Deletes feed-seen release history rows older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — download history | Deletes download history rows of removed torrents older than N days. *(Unit: days; 0 = keep forever)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — error cards | Deletes error torrent entries older than N days (minimum 1). *(Unit: days)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — gap search log | Deletes the missing-episode search log older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — upgrade backups | Deletes backups of files replaced by upgrades older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |

**Release archive cleanup**

| Setting | What it does |
|---|---|
| Automatic archive cleanup | Enables automatic archive cleanup on every housekeeping pass: it removes only release-catalogue rows older than the maximum age. Downloaded files, the library, download history and active torrents are untouched. |
| Archive — maximum age | Maximum age of archive releases, in days (0 = no limit). *(Unit: days; 0 = no limit)* Shown only while «Automatic archive cleanup» is on. |
| Archive — keep at least N entries | Minimum number of recent releases to always keep, even when older than the maximum age. Shown only while «Automatic archive cleanup» is on. |

**Night maintenance**

| Setting | What it does |
|---|---|
| Night window — start | Start time (HH:MM, 01:00 by default) of the window in which the heavy jobs run: one job at a time and only while Gextto is idle, that is no download transfers more than 100 KB/s, no download is being archived, the library check and the integrity check are not running and the system load is low. Seeding does not count: it goes on all night. |
| Night window — end | End time (HH:MM, 07:00 by default). After it no new job starts; the one in progress is completed. An end before the start makes the window cross midnight; the same start and end mean the whole day. |
| Integrity check and MediaInfo analysis only in the window | Yes (default): the integrity check and the MediaInfo analysis run at their interval but only inside the window and while Gextto is idle. No: they run at their interval at any hour, as before. The manual commands (**Check now**, **Refresh MediaInfo**) always start at once. |
| Database maintenance every night | Yes (default): once a night the databases are optimised (`PRAGMA optimize`) and compacted when at least 20% or 64 MB of them is free space; once a week a full check (`integrity_check`) reads every page and every index, and a problem sends a notification at once, without waiting for the summary. |
| Clean the audio of files already in the library | Applies **Remove audio tracks in other languages**, with the same language and choices, to the files already archived: one at a time, in the window, with the same checks as the cleanup after an import. Every file is examined once (Gextto remembers its size and date); a file still seeding through a hardlink waits for the end of the seed, so the cleaned copy does not use the space twice, and a file written in the last hour waits for the next night. A file that fails the check for a passing reason (a tool interrupted, a disk that does not answer) stays as it is and is tried again after a week; a file that cannot be cleaned without changing it — the track check does not match — is remembered and not tried again unless the file changes (size or date). Off by default; it works only with the audio cleanup on. |

**Night maintenance — panel**

Under the options, in the same group, the **Night maintenance status** panel shows the window, what is running or why it waits, and the outcome of each job.

| Item | What it does |
|---|---|
| Run now | Runs a maintenance pass at once, even outside the window; it still waits until Gextto is idle. |
| Database | Last optimisation, total size of the databases and space freed, last integrity check and the problems found. |
| Audio cleanup of the library | Files checked out of the total, files cleaned, space saved, files left as they were and the last check not passed. |
| Library | The last pass of the library check (correct files, renamed, duplicates in the trash, errors: the check runs by itself every *Check renamed files every* hours) and the night report of the files the database knows but the disk no longer has. A folder that cannot be read, for example a NAS that is not mounted, is listed apart and does not count as missing files; only the seasons the series still follows are listed, so the files of a season removed from the range, or of a disabled series, are not reported. When a download of that series is being archived or the audio cleanup of the library is rewriting one of its files, the check skips the series for that pass and the log line says which of the two is keeping it busy; the series is taken up again at the next check. |
| Mark as missing | Removes from the archive the episode whose file is gone, so the search downloads it again. It does nothing when the file is back or its folder cannot be read. |

When the window closes, the log sums up the night in one line; a notification on the configured channels, in the maintenance group, also arrives when the night found database problems, missing files, cleaned files or files left as they were, or freed at least 64 MB.

**MediaInfo analysis**

| Setting | What it does |
|---|---|
| Automatic MediaInfo backfill | Periodically analyses with ffprobe the files already present that have no MediaInfo yet. |
| MediaInfo backfill — interval | Minutes between two MediaInfo backfill passes. *(Unit: minutes)* Shown only while «Automatic MediaInfo backfill» is on. |
| MediaInfo backfill — files per run | Maximum number of files analysed in each MediaInfo pass. Shown only while «Automatic MediaInfo backfill» is on. |

**File integrity check**

| Setting | What it does |
|---|---|
| Automatic file integrity check | Periodically probes the library files with `ffprobe` and `ffmpeg`, one file at a time, to find the damaged ones. |
| File integrity check — interval | Minutes between two check passes. Shown only while «Automatic file integrity check» is on. |
| File integrity check — files per pass | Maximum number of files verified in each pass. |
| File integrity check — quick check for 4K files | For 4K files it reads the whole file but decodes only the keyframes: the check is much faster and still finds truncated or unreadable files, but it may miss a small defect between one keyframe and the next. It is on by default. |
| File integrity check — re-download damaged files | As soon as the check finds a damaged file it blocklists the release, moves the file out of the library and searches at once for a different release of the same episode or movie. It is off by default: without it the replacement stays manual, with **Redownload**. |

**File integrity check — panel**

Under the options, in the same group, the panel shows the state and the files it found.

| Item | What it does |
|---|---|
| Start check / Resume check | Starts the pass or resumes it from the first file not yet verified; files already checked and unchanged are skipped. It works with the automatic pass off too. |
| Stop | Stops the check after the current file; the position stays saved. |
| Start over | Starts again from the first file, clears the previous results and rechecks the whole library. |
| Damaged / Unreadable | The file could not be decoded, or it could not be read within the allotted time: an unreadable file is not treated as broken and is retried automatically in a later pass. |
| Redownload | Moves the damaged file out of the library, blocklists the old release and searches the same episode for a different release. With **File integrity check — re-download damaged files** on, Gextto does it by itself as soon as a file is reported as damaged. |

**Orphaned files**

| Setting | What it does |
|---|---|
| Move orphaned data in the temp folder to the trash | Every 30 minutes, moves to the trash the files and folders in the download temp folder that belong to no listed torrent and have not changed for at least the given number of days. Nothing is deleted: they stay in the trash until you empty it. |
| Orphaned data — idle for at least | How many days an item in the temp folder must stay untouched, with no listed torrent, before it is moved to the trash. *(Unit: days)* Shown only while «Move orphaned data in the temp folder to the trash» is on. |

#### Notifications

*Telegram, email, webhooks, Discord, Slack, Gotify, ntfy, Pushover, Apprise and programs to run on events. «Send test notification» sends a message on every configured channel.*

**Telegram**

| Setting | What it does |
|---|---|
| Telegram enabled | Send notifications to Telegram. |
| Telegram bot token | Telegram bot token (from @BotFather). Shown only while «Telegram enabled» is on. |
| Telegram chat ID | ID of the chat/channel where notifications are sent. Shown only while «Telegram enabled» is on. |

**Email**

| Setting | What it does |
|---|---|
| Email enabled | Send notifications by email. |
| SMTP | SMTP server as host:port (e.g. smtp.gmail.com:587). Shown only while «Email enabled» is on. |
| Email sender | Sender address for notification emails. Shown only while «Email enabled» is on. |
| Email recipient | Email recipients (comma-separated). Shown only while «Email enabled» is on. |
| Email password | SMTP password/app password (not shown). Shown only while «Email enabled» is on. |

**Webhook**

| Setting | What it does |
|---|---|
| Webhook URL | Webhook URL where events are sent. |
| Webhook secret | HMAC secret to sign webhook requests. |
| Webhook format | Format of the payload sent to the webhook URL (Gextto, Discord, Slack, ntfy, Gotify, Pushover). |
| Webhook token | Provider credential (ntfy, Gotify, Pushover); not shown. Not needed for Gextto, Discord and Slack. |
| Webhook user (Pushover) | Pushover only: the user key. Not needed for the other formats. |

**Discord**

| Setting | What it does |
|---|---|
| Discord webhook URL | Webhook URL of the Discord channel. Empty does not use Discord. |

**Slack**

| Setting | What it does |
|---|---|
| Slack webhook URL | Slack incoming webhook URL. Empty does not use Slack. |

**Gotify**

| Setting | What it does |
|---|---|
| Gotify URL | Address of the Gotify server (e.g. https://gotify.example); notifications are posted to `/message`. |
| Gotify token | Application token of Gotify, needed together with the URL. |

**ntfy**

| Setting | What it does |
|---|---|
| ntfy URL | Address of the ntfy server (e.g. https://ntfy.sh). |
| ntfy topic | Topic the notifications are published to. |
| ntfy token | Bearer token, only when the topic is protected. |

**Pushover**

| Setting | What it does |
|---|---|
| Pushover token | Application token of Pushover. |
| Pushover user key | User or group key of your Pushover account. |

**Apprise**

| Setting | What it does |
|---|---|
| Apprise URL | Address of an Apprise API server (e.g. http://192.168.1.10:8000); notifications are posted to `/notify`. |
| Apprise key | Optional key of one Apprise configuration; empty uses the default one. |

**Program**

| Setting | What it does |
|---|---|
| Program | Executable run on every event, with the data in the `GEXTTO_*` environment variables. |
| Program arguments | Arguments passed to the program, separated by spaces; quotes keep together the ones with spaces. |

**Events**

| Setting | What it does |
|---|---|
| Download notifications | Notifies when a download starts, is discarded, completes or fills a missing episode. |
| Error notifications | Notifies when a download fails or a torrent or comic goes into error. |
| Health and engine notifications | Notifies about health, search cycle, indexer, provider login and torrent engine problems. |
| Maintenance notifications | Notifies about restarts, completed backups and audio-track cleanup. |

**Anti-spam**

| Setting | What it does |
|---|---|
| Anti-spam — minimum interval | Within this many minutes the same event is not sent again. `0` disables the anti-spam window; the maximum is `10080` (seven days). |

**Message language**

| Setting | What it does |
|---|---|
| Message language | Language of the notifications; empty follows the interface language. |

**Editors**

| Setting | What it does |
|---|---|
| Event hooks | Runs a program on selected events (name, events, program, arguments, timeout). |

#### Access and services

*Login, API keys and credentials of external services.*

Gextto is meant for a trusted LAN: access is **open by default**. When you
reach it from outside (reverse proxy, port forwarding, VPN) you can require a
login from clients that are not on the local network.

**Login**

| Setting | What it does |
|---|---|
| Require login | Off (default): no checks. On: clients outside the local network must log in or use the API key. Until a password or a key is set everything stays open (and the log says so). |
| No login from the local network | On (default): 127.0.0.1, 192.168.x.x, 10.x.x.x, 172.16–31.x.x and local IPv6 addresses need no login. Behind a reverse proxy the real client address forwarded by the proxy counts (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`), so a client coming from the Internet through the proxy still has to log in. Shown only while «Require login» is on. |
| Username | Login username (default `admin`). Shown only while «Require login» is on. |
| Password | Stored only as a bcrypt hash; changing it closes every open session. Leave the field empty to keep it. Shown only while «Require login» is on. |

**Writes from another origin.** Regardless of the login, Gextto refuses
changing requests that the browser declares to come from another site
(`Origin`/`Referer`, `Sec-Fetch-Site: cross-site`): a malicious page cannot
reconfigure Gextto (settings, event hooks, torrent actions) through the browser
of someone who has the interface open. Reads (GET) and clients without a browser
(curl, the TUI) are untouched.

**API keys**

| Setting | What it does |
|---|---|
| API key (scripts, TUI, calendar) | For non-browser access: header `X-Api-Key: <key>` or `?apikey=<key>` in the URL. The TUI reads it from the `GEXTTO_API_KEY` variable. |
| gx-torrent — access token (page and API on the LAN) | Under the **Access** tab: shared secret required by the page and the API; mandatory when the daemon listens on the network (not shown). |

Both secrets are generated from the row itself: **Generate** creates a long
random value and saves it, **Copy** puts it in the clipboard. A new gx-torrent
token needs the daemon restarted to be applied. What they are and how to use
them is explained in [Access keys](api-keys.md).

**Metadata services**

| Setting | What it does |
|---|---|
| TMDB API key (recommended) | Free TMDB API key: the main source of titles, posters and metadata. When set, it takes precedence over TVDB. |
| TVDB API key (alternative to TMDB) | TheTVDB v4 API key: without the TMDB key it provides all the metadata. |
| TVDB subscriber PIN | TheTVDB subscription PIN, asked at login only by “user-supported” keys. |
| Use TVmaze for series | Once a day, asks TVmaze (free, no key) for the status of every series, as a second opinion after TMDB or TVDB. Without TMDB and TVDB keys it becomes the source of the series: seasons, episodes, dates, posters and status, with titles and plots in English and no movies. TVmaze receives the TVDB and IMDb ids of the series and, when it is the source, its name. |

#### Metadata: TMDB, TVDB or TVmaze

Gextto uses metadata to know how many episodes each season has (missing
episodes, season packs, completeness), the series status, the
calendar, episode titles in file names, posters and the series and movie
pages. One of the two keys is enough:

- **TMDB (recommended)** — free key from themoviedb.org → Settings → API. When
  it is set Gextto always uses TMDB, even if the TVDB key is set too.
- **TVDB (alternative)** — v4 key from thetvdb.com → Dashboard → API keys.
  Without the TMDB key it provides every feature listed above except the
  trending and popular lists of **Explore**, which exist only on TMDB.
  “User-supported” keys require a paid TheTVDB subscription: enter its PIN in
  *TVDB subscriber PIN*. Episode titles follow the *TVDB language* (e.g. `ita`);
  an untranslated episode keeps its original title.

Without either key Gextto switches by itself to **TVmaze**, free and with no
key: seasons, episodes and dates (so missing episodes, packs and the calendar),
the next episode, posters and the series status. The limits: episode titles and
plots are in English, the search by name finds the original title well but not
always the Italian one (if the series has a TVDB id, Gextto looks it up by that),
and there are no movies and no **Explore** lists. TVmaze can be turned off too,
with *Use TVmaze for series*: with no source at all Gextto still downloads, but
missing episodes stop at the last one already downloaded and files get generic
titles (“Episodio N”). TMDB and TVDB use different ids: a series added with TVDB keeps
its TVDB id, and if you later set the TMDB key Gextto finds the series on TMDB
by name.

A browser session lasts 30 days; `/logout` ends it. After five wrong passwords
from the same address further attempts are blocked for a minute and each failure
is logged. If you lock yourself out, start Gextto with the environment variable
`GEXTTO_AUTH_DISABLE=1`: access control is off while the variable is set and you
can fix the settings. For access from outside always use HTTPS (reverse proxy
with a certificate, or a VPN), otherwise the password travels in clear text.

#### Diagnostics and updates

*Detailed logs and the update check.*

**Diagnostics**

| Setting | What it does |
|---|---|
| Debug (detailed logs) | Enables detailed logs and periodic diagnostics for debugging. |

**Updates**

| Setting | What it does |
|---|---|
| Check for updates | Downloads the latest build information from GitHub on a timer (how often is set by «Update check interval») and, when a newer one exists, shows «Update available». It sends no data and never updates on its own: the update only starts from Maintenance. |
| Update check interval | How many hours apart Gextto checks whether a new build is out (1 to 720, default 6). The check downloads only a small file from GitHub; «Check for updates» in Maintenance checks at once. |

## Appendix B. Reference — Integrations

The page is grouped — **Services** (Simkl), **Media server** (Jellyfin, Plex),
**Sources** (indexers, FlareSolverr, source check) and **Links** (iCal calendar,
browser handlers) — with the group title stuck to the top while you scroll.

### Simkl

| Field / action | What it does |
|---|---|
| Client ID | Client ID of the Simkl app. |
| Calendar days | How many days ahead to show in the Simkl calendar. |
| Watchlist status | Status assigned to imported series: Plan to watch, Watching or Completed. |
| Mark as watched | Marks downloaded episodes as watched on Simkl. |
| Start access / Confirm | Starts the PIN flow and confirms the code shown by Simkl. |
| Revoke | Revokes access and removes the stored token. |
| Import watchlist | Imports the series from the Simkl watchlist into the library. |
| Watchlist / Calendar | Read-only tables with the watchlist and the upcoming releases. |

### Jellyfin

| Field / action | What it does |
|---|---|
| Jellyfin URL | Jellyfin server URL (e.g. `http://127.0.0.1:8096`). |
| Jellyfin API key | API key generated in Jellyfin → Dashboard → API Keys. |
| Jellyfin — path mapping | Only when Jellyfin sees the library under different paths (e.g. Docker): one line per folder, `gextto_path=jellyfin_path`. |
| Test connection | Checks that Jellyfin responds. |
| Refresh library | Asks Jellyfin to refresh the whole library. |

### Plex

| Field / action | What it does |
|---|---|
| Plex URL | Plex server URL (e.g. `http://127.0.0.1:32400`). |
| Plex token | `X-Plex-Token` used to access the library. |
| Plex — path mapping | Only when Plex sees the library under different paths (e.g. Docker): one line per folder, `gextto_path=plex_path`. |
| Test connection | Checks that Plex responds. |
| Refresh library | Asks Plex to refresh the whole library. |

After each import Gextto asks Jellyfin and Plex to rescan **only the folder that
changed** (the season or movie that just arrived), not the whole library: the NAS
is spared a full scan per episode and the file shows up in seconds. For Plex the
folder must be inside one of the folders of one of its libraries. When the
targeted request is not possible (unknown path, server refusal) Gextto asks for
the full refresh as before. When the server runs in Docker and sees the files
under another path, fill in the path mapping.

### iCal calendar

`/feed/calendar.ics` is a calendar to subscribe to from Thunderbird, Google
Calendar, Apple Calendar or a phone (link under *Integrations → iCal calendar*).
Gextto was born for an Italian audience, where series often arrive months or
years after the original broadcast, so the calendar tells three things apart:

| Entry | What it means |
|---|---|
| 📥 Series S01E04 | episode or movie that **arrived in the library**, on the day Gextto downloaded it (last 30 days). The only date that says when it is really available. |
| 📺 Series S02E03 · Title | **original broadcast** (TMDB, or TVDB) of the current season, from the last week to the next two months. It is not the date of a localised release; ✓ when the episode is already in the library. |
| 🎬 Movie | release of a monitored movie **in your country** (TMDB: digital, then home video, then cinema; the country follows the TMDB language, e.g. `it-IT`). When that country has no date yet the original one is shown, marked “(original release)”. With only the TVDB key the movie's first release is shown, always marked “(original release)”. |

A TMDB or TVDB key is required; the calendar is rebuilt at most every 30 minutes. With
access control on (see *Access*) add `?apikey=<key>` to the address.

### Torznab indexers

| Field / action | What it does |
|---|---|
| Name | Indexer label (e.g. `jackett` / `prowlarr` / `indexarr`). |
| Base URL | Base URL of the service; Gextto appends the Torznab path. |
| API key | Indexer API key. |
| Type | Auto-detected, Prowlarr, Jackett or indexarr. |
| Enabled | Enables or disables the indexer. |
| Test | Tests the indexer (for Jackett and indexarr it uses `t=caps`). |

A **direct** Torznab indexer (not managed by Jackett, Prowlarr or indexarr), for example the
`mircrew-indexer` service on the same machine or the LAN, is added here with
its base address (e.g. `http://192.168.1.10:9118`) or one ending in `/api`:
Gextto uses `<base>/api`, the same rule as gx-torrent, so the address to type is
identical in both. Automatic searches have a **100-second budget per title**: an indexer behind
Prowlarr may wait up to **90 seconds** (Prowlarr serves one search at a time per
indexer), a direct indexer or one behind Jackett up to **45**, indexarr up to
**90** (one search per title over the whole manager). A lane whose queue would
outlast the budget is skipped without counting as a failure, so one slow tracker
does not stop the cycle. When an
indexer on a local or private-network address drops the connection or does not
answer, Gextto does **not** retry through FlareSolverr (Cloudflare does not
protect a LAN service): the error stays visible as it is. For `mircrew-indexer`,
*Health → Provider status* shows whether the service answers and whether its
login to the MirCrew forum worked; if it did not, log in again from the
service's web UI.

Gextto reads the indexer list and its capabilities from the manager (the
Torznab *caps*; for Prowlarr the list of its indexers, re-read every 10 minutes)
and sends only the parameters that the single indexer declares. For example Jackett's "all" aggregate does not accept the
TMDB id in series and movie searches, and Prowlarr's public indexers cannot
search by id: in these cases the search uses title, season and episode instead
of failing or coming back empty. If an indexer still rejects an id, the search
is repeated right away without it; the error shown in *Health* carries the
indexer's own explanation (for example "HTTP 400: Torznab 100: Invalid API
Key") instead of the bare HTTP code.

### FlareSolverr

| Field / action | What it does |
|---|---|
| URL | URL of the FlareSolverr service used to bypass Cloudflare. |
| Test FlareSolverr | Checks that FlareSolverr responds. |

### Event hooks (Configuration → Notifications)

| Field / action | What it does |
|---|---|
| Name | Hook label. |
| Enabled | Enables or disables the hook. |
| Events | Events that trigger the hook (empty = all). |
| Program | Executable to run (without a shell). |
| Arguments | Arguments with placeholders `{title}`, `{hash}`, `{path}`, `{series}`, `{episode}`; each placeholder stays a single argument even with spaces. The same values are exposed as `GEXTTO_*` variables. |
| Timeout (s) | Timeout in seconds (default 60, maximum 24 hours; `0` = default). |

### Browser handlers and source check

| Item | What it does |
|---|---|
| Magnet / Torrent manager | Downloads the scripts to open magnets and `.torrent` files directly in Gextto. |
| Magnet / Torrent `.desktop` | `.desktop` versions for Linux desktop integration. |
| `install.sh` | Downloads the installation script. |
| Check sources / Refresh | Checks feeds, indexers and web engines; with a query it also measures results, without changing settings. |

### Browser extension (qBittorrent client)

Gextto answers a subset of the qBittorrent API (`/api/v2`) on the address of its
own interface, so an extension written for qBittorrent — *Torrent Control* for
Firefox, for example — sends magnets and `.torrent` files to Gextto: the torrent
enters the queue and the active engine downloads it (gx-core by default), as if
it had been added from the interface.

In the extension, configure a server with these values:

| Field | Value |
|---|---|
| BitTorrent client | qBittorrent (current version) |
| Server address | `http://<gextto address>:5000/` (the interface port, not the gx-torrent one) |
| Username / Password | the ones of the protected access, when it is on; otherwise they can stay empty |
| Download folder | empty to use the configured folder, or a path the engine accepts |
| Label / Category | empty: categories, tags and a content layout other than *Original* are refused with an explicit error, because in Gextto they would change the queue tags and the folders on disk |

When the protected access is on, the extension logs in with the same credentials
as the interface and receives a session cookie valid only for `/api/v2`: the rest
of the interface stays protected.

## Appendix C. Reference — Maintenance

### Quick actions

| Action | What it does |
|---|---|
| Backup now | Immediately creates a database backup snapshot. |
| Clean trash | Empties the trash completely (forced action: `trash_retention_days` does not apply). |
| Recalculate scores | Recomputes the score of archived releases with the current weights. |
| Scan archives | Re-reads the archive folders and updates the library. |
| Update MediaInfo | Analyses with `ffprobe` the archived files that have no MediaInfo. |
| Rename all | Renames every archived file using the configured format. |
| Housekeeping | Cleans technical data and history without touching the library. |
| Restart service | Restarts the Gextto daemon. |

### Rename folder content

| Item | What it does |
|---|---|
| Folder / Browse | Type or pick the folder to analyse from the server. |
| Scan and propose | Recursively analyses the videos, recognises series/movies with TMDB/TVDB and proposes new names (preview only), in the format chosen for episodes and movies. Episode tags come from the file name, movie tags from the file itself (MediaInfo); a movie you follow takes its TMDB id, otherwise the year in the name counts too. |
| Per-row result | Selects an alternative TMDB/TVDB match. |
| Accept / reject | Approves or discards the single proposal. |
| Accept all proposals | Approves every proposal at once. |
| Apply selected | Renames the selected files that really exist with a valid destination. |
| Rename progress | Progress of the background operation. |

### Library

| Item | What it does |
|---|---|
| Video duplicates — Preview duplicates | Lists inferior duplicates (lower resolution) without deleting anything. |
| Video duplicates — Clean duplicates | Moves the inferior copies found to the trash. |
| Database optimisation — VACUUM / ANALYZE | Compacts the databases and refreshes the query planner statistics. |
| Database optimisation — Refresh sizes | Re-reads size and status of the database files. |
| Database optimisation — Check integrity and indexes | Checks integrity, foreign keys and indexes (including the archive FTS). |
| RAM disk | Shows writable `tmpfs`/`ramfs` paths, lets you select one, create a dedicated one and refresh the list. |

### Cleanups

| Item | What it does |
|---|---|
| Trash — Open trash | Shows the trash items so you can delete them one by one. |
| Trash — Empty trash | Deletes every trash item and shows a notice with the removed files and the freed space. |
| Database cleanup — Cycles to keep | Number of cycle statistics to keep. |
| Database cleanup — Error days | Keeps error torrent entries for at least N days before removing them. |

### Log

| Item | What it does |
|---|---|
| gx-torrent log | Latest lines of the torrent engine log (`gx-torrent/gx-torrent.log`), with text filter, warnings/errors only, line count and a choice between the active file and the rotations. |

### Backup

| Item | What it does |
|---|---|
| Backup now | Immediately creates a backup archive, without waiting for the interval or the scheduled time. The same command is in Quick actions. While it works it shows a notice and stays disabled. |
| Save settings | Saves the backup settings of this section (retention, schedule, FTP, cloud folder, extra paths, Telegram): it does not create an archive. |
| Backups to keep | Number of local ZIP files to keep in the `backups` folder. |
| Interval (hours) | Automatic backup interval in hours (0 disables the interval). |
| Time (HH:MM) | Local daily time of the automatic backup; when set it takes precedence over the interval. |
| FTP host | FTP server host/address (empty = no FTP upload). |
| FTP user | FTP server user. |
| FTP password | FTP password (not shown; empty keeps the stored one). |
| FTP path | Remote destination folder for the ZIP. |
| Cloud folder | Local path of a folder already synced by a cloud service. |
| Files and folders to include | Absolute paths of files or folders to add to the ZIP (one per line). They end up in the archive's `extra/` folder with their original path; paths that no longer exist are skipped. |
| Send to Telegram | Sends a copy to Telegram using the configured bot. |
| Test FTP | Checks connection, login, remote path, a test upload and removal. |
| Available backups / Verify | Table of snapshots with label, name, size and date; the Verify button checks the archive, shows a notice while it works and confirms when the ZIP is intact. |
