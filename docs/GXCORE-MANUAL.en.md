# gx-core — user manual

**gx-core** is the complete BitTorrent client inside `gx-torrent`: the same
application Gextto uses as its engine, but **without Gextto**. It opens in the
browser, is configured in a few minutes and already does everything a client
needs: torrents, queue, categories, tags, RSS, Series & movies, archive,
search, statistics, streaming, DLNA and a qBittorrent-compatible API.

> [!TIP]
> This manual is illustrated with a real **Docker** environment: four legal
> torrents (Big Buck Bunny, Sintel, Tears of Steel, Cosmos Laundromat),
> categories, tags, one followed title and transfers in progress. The images
> come from the real interface, not from mockups.

## 1. Start in three minutes

```bash
docker build -t gx-torrent .
docker run -d --name gx-torrent \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v gx-torrent-data:/data \
  gx-torrent
```

Open `http://YOUR-SERVER:8080/`: the first visit shows the wizard.

![Setup wizard](screenshots/gx-core/en/setup.png)

Choose the language, the download folder, the peer port and, if you want, a
password. The same wizard can add a Torznab indexer (Prowlarr, Jackett,
indexarr or another endpoint). Everything else already has sensible defaults.

## 2. The Torrents page

The main screen is a dense but readable list: state, size, progress, seeds and
peers, speed, ETA, ratio, category and tags. The top bar shows the traffic; the
status bar has the global controls.

![Torrents](screenshots/gx-core/en/torrents.png)

- **Add** opens magnets, `.torrent` URLs and files, with destination, category,
  tags, pause, sequential, first/last piece, limits and content layout.
- **Pause all / Resume all** stop or restart the whole session.
- Columns can be chosen and resized; the choice is remembered.
- The sidebar filters by state, category, tag and tracker.
- The context menu has queue, limits, move, rename, verify, reannounce,
  super-seeding and remove.

### Torrent details

Click a name to open the panel below. **General** brings together state,
pieces, space, dates, limits, seeding and web seeds.

![Details](screenshots/gx-core/en/detail-general.png)

**Files** shows per-file progress, priority (Skip, Normal, High, Maximum),
grouping by folder, rename and **HTTP streaming**.

![Files](screenshots/gx-core/en/detail-files.png)

**Peers** shows client, transport, encryption and flags; from here you add peers
and ban an address. **Trackers** shows status, swarm and next announce;
**Pieces** is the coloured piece map.

![Peers](screenshots/gx-core/en/detail-peers.png)

## 3. Add and create

The add dialog takes several magnets or URLs at once; dropping a `.torrent`
anywhere on the page opens it already filled in.

![Add](screenshots/gx-core/en/add-dialog.png)

From **Tools → Create .torrent** you build a torrent from a file or folder on
the server, with piece size, private flag, comment, trackers and web seeds.

![Create torrent](screenshots/gx-core/en/create-torrent.png)

## 4. Series & movies: what is really missing

**Series & movies** turns the client into an acquisition tool. Every title says
in words what you have: *Downloaded*, *Downloading*, *Found, not downloaded*.
The progress bar is over the episodes **found so far**; enable the real episode
list and the denominator becomes the actual series.

![Series & movies](screenshots/gx-core/en/wanted.png)

- State filters: all, with missing, downloading, complete, not monitored.
- One primary action per row; everything else is in the **More** menu.
- **Why not?** opens the releases found and each verdict.
- **Episodes** opens the per-season grid with the state of every episode.
- **Search**, in the *Add title* dialog, lists under every name the episodes
  already in the archive with their resolution and language: picking one fills
  the title, the quality and the language from that release.

![Episodes](screenshots/gx-core/en/wanted-episodes.png)

### Quality profiles and upgrades

In **Settings → Quality profiles** you create a profile with the accepted
resolutions (360p…2160p) and a **cutoff**. A title can choose the profile
instead of the quality field. With a cutoff, an episode already downloaded at
720p is replaced by a 1080p release when it appears: the new torrent enters, the
old one leaves the session and **the files stay**.

![Quality profiles](screenshots/gx-core/en/quality-profiles.png)

### Real episode list (optional)

In **Settings → Series and movies** you can enable **TVmaze**: the series title
is sent to `api.tvmaze.com` and gx-core knows the episodes that already aired,
even if no release ever passed through the feeds. It is off by default; without
a match the page keeps using what the feeds and indexers saw and says so on the
row.

## 5. RSS, archive and search

**RSS & indexers** manages feeds, ordered PASS/FAIL rules, Torznab indexers and
the local archive. An indexer can be checked with **Test** already in the
window where you add or edit it, before saving. Rules have a live preview, feed
tests, import/export and actions (folder, category, tags, pause, sequential,
first/last, top).
**Archive** makes everything the feeds returned searchable, even when it was not
downloaded. **Search** queries the indexers in parallel and shows size, seeds,
peers, age and indexer; each result is added at once or with the options
(destination, category, tags).

## 6. Statistics and settings

**Statistics** shows the 1 h / 24 h / 7 d graph, session and all-time totals,
disk and cache, network, queue and the busiest torrents.

![Statistics](screenshots/gx-core/en/stats.png)

**Settings** collects folders, watch folders, network, protocols, queue,
bandwidth and the per-day scheduler, seeding limits, automatic speed
(autotune, which measures the line and moves download and upload), DLNA,
notifications, categories, quality profiles, completion actions, maintenance
and updates.

![Settings](screenshots/gx-core/en/settings.png)

Category management and its path are on the same page: that is what makes
automatic torrent management really work.

![Categories](screenshots/gx-core/en/categories.png)

## 7. From the phone

The page is responsive and installable as an app: the manifest and the service
worker cache only the shared assets, never an authenticated page or an API
answer. When a password is set, **Sign out** appears.

![Mobile](screenshots/gx-core/en/mobile.png)

## 8. Integrations

gx-core exposes the **qBittorrent-compatible API** (`/api/v2`) for Sonarr,
Radarr, Lidarr, Readarr, Prowlarr and mobile apps. In **Settings → Network** you
set the **API key** (`X-Api-Key` or Bearer) and the version reported by
`app/version` (default `4.6.7`). The native `/api/v1`, Prometheus and SSE remain
available for scripts and automations. A feed keeps its identifier when you
rename it, move it or give it a new address, rules report their last match and
the episodes already taken, and the tags set by a client stay: apps that read
these fields find them filled. Compatibility is broad but not a 100% clone: the
limits are in [`gx-core.md`](gx-core.md) §6.

## 9. Security

- The page listens on `0.0.0.0:8080` in the container; login is optional but
  recommended if you expose it beyond the LAN.
- CSRF, DNS rebinding and “what counts as LAN” are handled server-side; trusted
  proxies must be declared.
- The API key and token are secrets: keep them out of public URLs.
- Web seeds and magnet sources cannot reach the local network unless you allow
  it.

## 10. Reproducing this manual’s environment

The images come from a `gx-torrent` container with four public, legal torrents,
a couple of categories and one followed title. The steps are those of chapter
1; torrents are added from **Add** or the API:

```bash
curl -X POST --data-urlencode \
  'urls=https://webtorrent.io/torrents/big-buck-bunny.torrent' \
  http://127.0.0.1:8080/api/v2/torrents/add
```

No personal data, no private trackers, no copyrighted content.
