# Gextto changelog

This is what changes in Gextto from the point of view of the people who use it,
in date order. Entries are grouped by day and by visible effect: commits that
only build, test, document or maintain the project internally do not get a
separate line, but are represented in the entry of the change they accompany.
Technical detail stays in the commits and in `docs/`.

The installed version is the one shown by Gextto (`<series>.<build>`, for
example `1.1.1961`): the third number is always the build and the second is
raised by hand when a new series is started. The updated
manual is online: [English](docs/MANUAL.en.md) ·
[italiano](docs/MANUAL.it.md). For reports:
[GitHub issues](https://github.com/buzzqw/gextto/issues).

## 27 October 2026

- **eD2k links now arrive from indexarr too.** If you add an eD2k source in indexarr (for example the DDUnlimited forum), its results now reach Gextto as `ed2k://` links instead of being discarded because they carry no magnet or `.torrent`. Gextto shows them as the eD2k network in search and in the archive, and queues them in aMule when you download, exactly like the ones it finds by itself; the torrent client is not involved. Nothing to configure: the source only has to be enabled in indexarr and the indexarr indexer already among Gextto's sources. With indexarr a search waits up to 45 seconds (instead of the 5 used for other sources) so the eD2k source can open the topic pages; Sonarr, Radarr and the other programs do not get this wait and stay as before.

- **The web search no longer cuts every source together after 15 seconds.** There used to be a single 15-second budget for all the web sources of a search, and it included the wait in the queue: with eight engines and four slots, the second half might never start. Now each engine gets its own 15 seconds from when it starts, so none is skipped just because it was waiting; the answers already received are kept. A single engine slower than 15 seconds is still left behind for that search, but it no longer stops the others and no longer floods the log: the detail goes to the debug log.

- **A series that has not aired yet no longer shows up as missing.** When the metadata says a series is still "In Production" (or planned), Gextto does not treat the announced episodes as missing: it used to infer them from the planned season count and the *Missing* tab listed series that had not aired yet. The same goes for an episode with a known future air date. Real gaps are unchanged.

- **«Search missing» shows what it found.** The missing-episode search for a series used to say only how many results it had found ("Found 1 result for 15 episodes") and then threw them away: to see one you had to guess the right episode and search it by hand. Now the *Missing* tab lists the results under the series, with the episode, the title, the source and a **Download** button. The search no longer stops at the first 15 episodes: it covers all of them, runs in the background with visible progress and a cancel button, and downloads nothing by itself. Torrent results also stay in the local archive, so searching the single episode finds them at once.

- **Foreign audio tracks remove themselves after the import.** Under *Library and renaming* there is a new option, off by default: once a movie or an episode has entered the library, Gextto keeps only the audio tracks in the chosen language (Italian by default) and removes the others. Nothing is re-encoded: video and audio stay the same as before, HDR and sync included. It touches a file only when it has more than one audio track and at least one is tagged in that language; untagged tracks are not considered. Before replacing the file it checks tracks, duration and sync, and leaves the original alone if anything does not add up. If there are several tracks in the chosen language you can keep them all, only the best one or only the first. Files still seeding are not touched: the library copy becomes independent and the torrent keeps sharing. It needs ffprobe, ffmpeg and mkvmerge; for MKV files Gextto uses mkvmerge, which preserves the container exactly. The full Docker image already ships them; on a manual install they must be present on the host. The option can be enabled only when all three are there: otherwise the row stays blocked with a warning and the log says so. The language is chosen from a list (or typed as the two or three letter code). Every cleaned file sends a notification on the configured channels (Telegram, webhook, email) with the file name, how many tracks were removed and the space saved. It cannot cause re-downloads: the file name stays the same and the only score components that can move (size and the codec of the remaining track) stay below the 200-point upgrade threshold.

- **With indexarr a full search takes less than half the time, and loses no results.** The 03:15 run on 27 October took 12 minutes. For each title Gextto sent indexarr 46 requests, one per indexarr source, and waited for the slowest with two slots per source: 5,244 requests for 104 titles. It also opened the `.torrent` link of each of about 400 candidates, one after another, including those already in the library. Now indexarr gets a single search per title, queries its own sources in parallel and returns the results already free of duplicates. Gextto reads every page of the answer, up to 1000 results per title: measured on 14 real titles, the distinct releases found are the same or more (Reacher 702 instead of 567, CIA 872 instead of 773). When a result already declares its infohash, Gextto decides on that and opens the link only for the release it actually downloads; when it does not (MirCrew topics, whose magnet indexarr fetches on demand), Gextto judges it on title and quality first and opens the link only if it would download it. The same goes for in-progress season packs: their file list is checked only for a pack that would be downloaded. That way indexarr counts only real downloads; before, it counted every candidate. With Jackett and Prowlarr the search stays per site as before, and there too links are opened only for what is downloaded. First test run: the indexer search went from 3 min 40 s to 1 min 23 s, and the whole run from 12 to 5 minutes before the deferred links.

- **Two log lines are easier to read.** When an indexer answers with a Torznab error, the log shows its code and explanation ("Torznab 100: site requires a login") instead of the raw XML. And when a better file replaces two copies with the same name in different folders, the log writes one line ("… (2 copies in different folders) are in the trash") instead of the same line twice.

- **With indexarr, download errors name the right site.** indexarr now says which site each result comes from, as Jackett already did, and Gextto shows it as "indexarr:MirCrew". When a site behind indexarr keeps failing (for example an expired MirCrew login), the log no longer says "indexarr keeps failing: sign in again or disable it", which was misleading: it names the failing site and tells you to check its login inside indexarr. The same applies to sites behind Jackett and Prowlarr. Gextto also recognises indexarr on its new port 8788 (8484 is still recognised) and the setup wizard suggests the new address. If you enable "Send magnet downloads through indexarr" in indexarr, Gextto keeps working as before: it downloads from indexarr's link, follows the redirect to the magnet and spots duplicates by infohash, so indexarr can count which sources actually gave you your downloads.

## 26 October 2026

- **Auto speed no longer stays stuck at the minimum on a free line.** Every six hours the reference of the "at rest" ping expires and Gextto runs a short trial, slowing the downloads down. If the ping was already good, the trial could not lower it, and Gextto read that as "the line is still congested": it cut the speed down to 128 KB/s and kept it there for the whole download, with a perfect ping. You had to switch to *No limit* by hand and then back to Auto. Now a trial that starts from a good ping and does not lower it gives the line back to Gextto, and the cap is also dropped when only the background traffic of the DHT network is left. Also, when the ping degrades, Auto remembers the speed at which it happened and settles just below it, trying again once a minute, instead of climbing straight back over it: on a saturated line the ping is high for a fraction of the time it was, with the same bandwidth used. There is nothing to configure.

- **gx-torrent wastes less CPU while downloading.** A profile under real load (five public ISOs together, about 8.5 MB/s and 120 peers) showed two useless jobs in the engine, now gone. First, it zeroed every buffer before filling it with the incoming data anyway, about 2 % of the CPU. Second, when a torrent also has HTTP sources (webseeds), it rebuilt and re-sorted the list of gaps for every piece asked of every peer; that pick now costs a third and allocates no memory. The rest of the CPU goes into the traffic itself (network, protocol encryption, piece checks). Torrents with padding files download as before, and a new test checks it. There is nothing to configure.

- **Gextto and gx-torrent use less CPU and memory.** A Gextto restart no longer re-reads every interface translation when they have not changed: about one second of CPU and 45 MB of allocations less at each start. The *Series* page opens with a third of the work (from about 110 to 37 ms of CPU on the test mini PC) and the automatic refresh of the logs, which runs every 5 seconds while the page is open, costs a fraction of what it did. The dashboard and the status read by the TUI reuse the count of seen releases for 30 seconds. gx-torrent loads the IP filter in little more than half the time and with a sixth of the memory, and Gextto stops querying the database for every seeding torrent at every check. Nothing visible changes and there is nothing to configure.

- **The log reads as sentences in the lines that were still technical too.** About sixty lines still wrote a label followed by fields ("torrent link resolution failed · target: … · source: … · error: …", "managed gx-torrent exited unexpectedly · error: … · restarts: 1"). They now say what happened in one sentence, for example "⚠️ Could not fetch the .torrent of Silo S02E05 from mircrew: …" or "❌ gx-torrent stopped unexpectedly (…): Gextto restarts it". This covers `.torrent` links that cannot be fetched, manual and eD2k searches, gx-torrent stops and restarts, the IP filter, the FTP test, provider logins, moves to the trash and the RAM disk, and the cleanup of completed downloads and of the release archive. The log stays in English and every line stays linked to the history of its download.

- **gx-torrent no longer restarts when the network disk is slow.** When the download folder is on a network share (NFS, SMB) and the NAS takes more than a minute to confirm a write, gx-torrent mistook the wait for an internal hang and shut down, stopping every download until Gextto restarted it. It now waits for the disk to answer, writes in the log that the storage is slow and, once it answers, that the torrent is running normally again. A real hang is still detected as before.

- **A 4K without seeders is back in play when its swarm revives.** When Gextto gave up on a download because nobody in the swarm had the complete file, the release stayed blocklisted forever: if the 4K came back to life a week later it was never tried again and the 1080p stayed. The block now lasts a configurable period (seven days by default, in *Stalled torrents → Let a release without seeders back in after*; 0 keeps it blocked forever) and when it expires the release is back among the candidates: if it has found seeders in the meantime it is downloaded as an upgrade, while until then the copy in the library stays where it is. On the Blocklist page the new *Expires* column shows the date, or "forever" for the blocks you decide, and the log says when the release can be tried again.

- **qBittorrent 5.3: the engine works again with 5.2 and 5.3 too.** With qBittorrent 5.2 and 5.3 Gextto could no longer reach the Web API (the login answer changed), so the engine looked unreachable; since 5.0 torrents added paused started at once and the per-torrent seed limits were refused. Gextto now understands both generations of the Web API and works with qBittorrent-nox 4.6 and later, including the 5.3 release installed by managed mode. No setting needs to change: update Gextto and restart the service.

- **The indexer test speaks naturally.** The log line is no longer a row of technical fields ("indexer test · kind: … · name: … · host: … · ok: … · status: …"): it is now a sentence, for example "✅ The indexer «indexarr» at 192.168.1.161 answered the test (HTTP 200)", or "⚠️ The indexer «indexarr» … did not answer the test: the service is unreachable" when the service does not answer.

- **indexarr is a manager type of its own.** In the *Integrations → Torznab indexers* list and in the initial setup you can now choose **indexarr** as the type, with the base URL (normally `http://host:8484`) and the API key; it no longer has to pass for Jackett. Gextto uses its native `/api` Torznab endpoint for the aggregate search and the Jackett-compatible paths to read the indexer list and query one tracker at a time, as it already does with Jackett and Prowlarr. The indexer test and the health check use `t=caps` and recognise indexarr, so a wrong API key or URL is reported like for the other managers. Entries already configured as Jackett keep working: to use the native endpoint, change the type to **indexarr**.

## 25 October 2026

- **The library check no longer renames the same episode back and forth.** The periodic name check compared two ways of writing the same audio (`EAC3` and `ddp`) and, depending on which answer arrived first, renamed the file every six hours: "Wolf Like Me" S01E05 and S01E06 changed name on every pass and the count said "4 renamed" for two files. The check now keeps the spelling already in the name and renames only when the codec is really different; a file that is already correct is not renamed a second time within the same check. The same applies to HDR (`DV` and `DV HDR10`) and to movies.

- **Fewer repeated lines in the log.** The gx-torrent log no longer repeats "memory released: no active downloads for …" every ten minutes: it writes the line when the idle period starts and then at most every six hours, while still releasing memory every ten minutes. The "Searches need attention" reminder for a source that stays broken arrives at most once every six hours (it was hourly) and the summary of a partly useful pack appears once, not at every re-check. The mircrew login reason, which the service returns in Italian, is now in English in the log and in the notification; the original text stays in the detail.

## 24 October 2026

- **Copies of the same release no longer multiply, and one is a spare for the other.** The same torrent often comes from several sources (feed and search of the same indexer, Jackett, Prowlarr, indexarr), and sites that only give a `.torrent` link made it look like a different release every time. Gextto now merges the copies when they have the same infohash (even when it is declared next to the `.torrent` link or written in base32) and, when the infohash is missing, when name and size match within rounding. When in doubt it does not merge: two different infohashes, or names that differ in group, resolution, language, PROPER or episode, stay separate releases. Of a merged release it uses the copy with the best swarm and keeps the others as spares: when the chosen link fails or is in quarantine, it tries the same release from another source right away instead of waiting for the next cycle. The log shows a line «🔁 … trying the same release from …». In the manual search every torrent appears once.

## 23 October 2026

- **Comics tell what they are doing too, in the log.** The comics check no longer lists technical lines ("comics: candidate posts · posts: 1", "title checked · queued: 0"): it says how many titles it is checking, what it downloads and why («📥 Downloading «Poison Ivy #41 (2026)» — new comic on GetComics»), when a title is already up to date or has nothing new, and when an issue has no download link yet. Errors are sentences with the title and the reason too.

- **Weekly Pack: downloaded once only, and the log says why.** Before queueing a Weekly Pack, Gextto checks its own history for an earlier download (and whether it is still in the download list): if it already got it — even when you deleted the folder after reading it or removed it from the list — it does not download it again and marks it as handled. The log line now explains the reason: "📥 Downloading Weekly Pack 2026-10-14 — its download link is available now" when the link arrives after the pack was found without one, "… — new pack of the week" for a freshly published pack, and "Weekly Pack … was already downloaded on 2026-10-14: not downloading it again" when the history says it has been there.

- **The log speaks the language of the people who use Gextto, always in English.** The start, search, speed and download lines are now readable sentences instead of lists of technical fields: "Gextto restarted (run #594) — 1.1.2013 · it was down for 6 seconds · web interface at …", "Speed: no speed limit (Auto)", "Searching: 25% done (26 of 104 titles)". Titles, episodes and file names keep their own language; the sentence around them and the technical errors are in English. Technical detail stays in the debug log, where it belongs.

- **A download from an already dead swarm no longer starts.** If the source reports zero seeders and zero peers, the release is discarded. If it is an upgrade with no seeders, Gextto does not start it and keeps the library copy. For sources that do not report seeders, a 15-minute probe (30 minutes for a magnet with no peer at all) checks whether anyone answers: when no peer connects, the download is discarded and the release is blocklisted, so the next search does not propose it again. The waiting times drop: dead swarm 30 minutes (was 72 hours), metadata 3 hours (was 24), alternative after 30 minutes (was 6 hours).

- **A restart is visible at once.** A clear divider line now comes before the restart line, so the point where Gextto started again is easy to find in a long log. The first start has no divider.

- **Fewer repetitions and fewer decorations.** A restart no longer takes six lines (start, restart, next search and speed are one line), the rows of dashes and equals signs are gone, search progress appears at each quarter instead of every minute, and a torrent waiting for its file list repeats every six hours instead of every hour.

- **A missing episode is no longer called a "better version".** When the library has no copy, the download says "fills a missing episode" even if the comparison had classed it as an upgrade. The download line now says "replacing" for the copy it replaces, so it is no longer confused with the source it comes from.

- **One story for a rejected upgrade.** When MediaInfo does not confirm an improvement, the log says so in one line that explains the comparison and that the library copy stays; the internal steps (removal from the session, move to the trash) go to the detail. The same line no longer appears twice when two checks look at the same vanished file.

- **Less housekeeping noise.** Browser sessions closed as idle no longer fill the normal log; failed download removals also say the reason reported by the engine. The gx-torrent log remains the engine's technical log, unchanged.

- **An automatic check keeps the rule.** A test reads the code and rejects an INFO, WARN or ERROR message written with internal names or with technical fields inside the sentence: readability no longer depends on remembering it.

## 22 October 2026

- **"Edit selected": a movie's requirements follow the value they qualify.** Changing the language or the subtitles of several movies, a profile included, now updates the matching hard requirement too: before, the old one stayed and the movies accepted no release at all until their page was opened again. The switch is now called **Allow upgrades**, with **Yes**/**No**, like the rest of the interface.

- **Quality profiles: a repeated name is reported, it no longer loses a row.** Saving two profiles with the same name (even with different capitals) is refused with a message in the section: before, the second row disappeared without explanation. The same notice now covers any rejected save of the structured lists.

- **Quality profiles: "Start from a profile" no longer erases the titles' exclusions.** In **Edit selected** the profile fills the fields but leaves the exclusions unticked, because they belong to each title: tick them only to replace them. On the title page they are still added, as before. A profile removed in the meantime no longer fills the fields from another profile: a warning appears.

- **Quality profiles: Jellyseerr no longer receives a number that does not exist.** The v3 list of titles reports the first saved profile instead of the fixed number 1; a request naming a removed profile is refused (400) instead of silently applying another one, as the title pages already did.

- **Quality profiles: recognised with different capitals and codes too.** The title page preselects the profile also when the quality is written `1080P` or the language is `it` instead of `ita` (or `ita,eng` in another order): before, **Current values** appeared for a difference of spelling only.

- **Quality profiles: the editor says when they are generated.** The section text now says that the library combinations are added by themselves **until you save the list**, and that removing every profile generates them again; the default language and the exclusions are described as they actually behave.

- **TUI: "Allow upgrades" for movies too.** The movie edit form has the same switch as the series, and the value is saved.

- **Series page: the TMDB ID is there too.** The series page showed only **TVDB ID**, even though TMDB is the default metadata source when the TMDB key is configured. Both fields are now there, with **TMDB** first: saving one keeps the other, and a wrong id can be fixed without touching the library.

- **Upgrades: no re-download when the copy's name lost its source or group.** When the archived copy does not declare the source or the group (for example a file imported or renamed with the title only), those fields are no longer credited to the release found: without proof that the copy is worse, it is not replaced. `ffprobe` also fills the audio channels when the name has lost them. A library file called just `[1080p][h264][EAC3]` is therefore not re-downloaded just because the release found is the same one with the full name `WEB-DL DDP5.1 ...`.

- **Upgrades: the score no longer re-downloads files already in the library.** The rule that requires a real technical gain, not just a higher score, now applies everywhere. Season packs already downloading no longer fetch episodes already present at the same quality, a file of the same quality no longer replaces the archived copy after the download, and a pack episode is scored with its own size instead of the whole pack's. **Allow upgrades: No** is honoured even when the archive scan cannot find the file (a moved folder, an unreachable disk), as long as the library says where it is. The *Library impact* panel now looks at the files actually present, like the cycle does.

- **Quality profiles: renaming no longer changes the number.** A profile's number is what Jellyseerr remembers: before, renaming a profile in the editor (or saving from a page opened before a change) gave it a new number. Now every editor row carries its own number.

- **Quality profiles: the title page no longer adds exclusions by itself.** A profile is preselected only when the exclusions match too: before, quality, audio, language, subtitles and upgrades were enough, and saving any field (seasons, for example) could add the profile's exclusions without choosing it. The **Current values** entry now shows the title's exclusions too. An empty language in a profile also means Gextto's default language, as in the add form, instead of accepting any language on the page and in the v3 API.

- **Generated quality profiles: minimum audio and upgrades too.** The profiles created automatically from the library (the ones Jellyseerr sees too) now carry the minimum audio and **Allow upgrades** of the titles they come from, not only quality, language and subtitles; their name also says which audio they require. A request added from Jellyseerr with that profile no longer loses these two constraints.

- **Series and movie pages: the quality profile instead of single fields.** On the title page **Quality**, **Language** and **Subtitles** make room for **Quality profile**, as in the add form. The profile matching the title's values is preselected, with its summary; when none matches, **Current values** appears, and saving with it changes nothing. Choosing a profile and saving copies its quality, minimum audio, language, subtitles and **Allow upgrades** into the title and adds the profile's exclusions to the title's. The profile stays a template: editing it in *Quality and upgrades* does not change titles already created.

- **Series page: the read-only Name field is gone.** It repeated the title already shown in the header and looked editable without being so. To fix a wrong name, delete the series and add it again.

- **Tidier series and movie pages.** The **Minimum audio** and **Allow upgrades** fields are no longer on the title page: they come from the quality profile when you add a series or a movie, and are changed for several titles at once with **Edit selected**. The value already set on each title stays and still applies. On the series page the fields are realigned (Exclude on the whole row, **Anime** as a Yes/No choice).

- **Movie page: one field per value.** **Required languages** and **Subtitle requirements** are gone: they duplicated **Language** and **Subtitles** (and, when filled, silently won). At the first start their values are moved into the remaining fields without changing any decision: a movie with language «any» but required language «chi» now has Language `chi`. Instead of the subtitle requirements there is **Subtitles required: Yes/No**. The **Exclude** field takes the whole row.

- **Quality profiles: language and subtitles to pick or type, fewer duplicates.** In the profile editor **Language** and **Subtitles** suggest the usual choices (ita, eng, ita,eng, multi, any) and also take a value typed by hand. The profiles generated from the library (the ones Jellyseerr sees too) no longer come from a single title's exclusions or from the `any` quality, which means no limit: no more duplicates such as *any · ita* next to *Default*.

- **Quality profiles: resolution from a list.** In the profile editor (*Quality and upgrades*) the **Quality** field is now a menu of the resolutions Gextto recognises, the same as on the title pages, instead of free text. A value typed by hand in the past stays selected until you change it.

- **"No upgrades" becomes "Allow upgrades".** In the quality profiles, in **Edit selected** and in the TUI the switch is now called **Allow upgrades**, with **Yes** (default: a better release replaces the archived file) or **No** (the archived file stays). It is the same setting as before, worded positively: no title changes behaviour.

- **Upgrades checked with MediaInfo before entering the library.** When a completed download is about to replace an archived episode or movie, Gextto reads with `ffprobe` the new file (still in the download folder) and the copy in the library before moving or copying anything. If the name promised more than the real content (a «2160p» upscaled from 1080p, a fake «HDR», audio other than declared) and the real quality is not an upgrade, the download is discarded and blocklisted and the library copy stays. Without `ffprobe` the decision taken before the download stands; season packs are not checked this way yet.

- **WEB-DL and WEBRip: you decide whether they are the same quality.** The rule that always replaced a WEBRip or HDTV with a WEB-DL now follows the **Source** weights of *Quality and upgrades*: when WEB-DL and WEBRip weigh the same, a WEB-DL no longer replaces a WEBRip already in the library. HDR, a higher resolution, REPACK/PROPER and a score better by at least the minimum difference still count.

- **Quality and upgrades: check the impact before re-downloading.** The new **Impact on the library** panel replays the releases already seen in the feeds with the current rules and weights and says how many archived episodes the next cycles would replace, with a few examples, without downloading anything. Use it after changing weights or thresholds: if the number is high, review the change before a mass re-download starts. The result turns into a warning above 5% of the archived episodes.

- **Adding series and movies: profiles and "Allow upgrades".** The add form now asks only for the **Quality profile** (with a summary of what it sets): the new title's quality, minimum audio, language, subtitles, exclusions and **Allow upgrades** come from the profile, with no other fields that could contradict it. An **Extra exclusions** field remains for words that apply to that title only, added to the profile's. After adding, the single values are changed on the title's page, where **Allow upgrades** is now available for movies too. Quality profiles gain two fields, **Minimum audio** and **Allow upgrades**, which are also applied to titles added from Jellyseerr and with **Edit selected**, where the minimum audio of several titles can now be changed at once. Also fixed an error that could occasionally reject a form save on the detail pages (for example a yes/no switch).

- **Quality and upgrades: page reorganised.** The section now starts with the **upgrade rules** (minimum difference, stop threshold, replacing archived versions), followed by the score examples, the new **Test a release** panel and the **Quality profiles**, moved here from *Sources*. The **Video**, **Audio** and **Release fixes** (PROPER, REPACK, REAL) weights are still there in closed cards, to open only when you want to change the preferences. **Test a release** takes a release name and shows the score item by item, the copy already in the library with its score and whether it would be replaced, with the reason, without downloading anything. Also fixed the quality profile editor, which did not appear in the current interface.

- **Upgrades: no more re-downloads of the same quality.** An upgrade "by score" now needs a real quality gain (resolution, source, codec, audio, HDR/DV, fixes, group) of at least the **minimum difference**: a bigger file, subtitles or a healthier swarm help choose between equivalent releases, but on their own they no longer re-download an episode or a movie already in the library. The missing-episode search records the release score without the swarm-health bonus. Library scores are also recomputed automatically when a Gextto update changes the scoring rules, not only when you change the weights (this happens once, at the first start of this version). Also fixed: **custom groups** saved with capital letters (for example `Me7alh`, `TheBlackKing`) never gave their bonus and could not be deleted; they are now converted to lower case and work.

- **Audio and video: grouped weights and per-title presets.** In **Quality and upgrades** the audio scores are now one card per group: **Lossless** (TrueHD, DTS-HD, FLAC, PCM), **DTS / DD+** (including DTS-HD HRA), **AC3 / 5.1**, **AAC / Opus**, **MP3**, plus a separate **channels and Atmos/DTS:X** card. Video is grouped into one **Video** card with 4K, Full HD, HD, SD, Video codec, Source and HDR/DV subgroups; **Audio** and **Upgrade and replacement** are the other cards, with their own subgroups. The parser tells DTS-HD MA from HRA, recognises FLAC, PCM and Opus, and no longer mistakes `AAC 5.1` for a bare 5.1 layout. The series and movie add/edit forms now have a single **Minimum audio** preset that already includes the channels (`AAC / Opus 5.1+`, `AC3 / 5.1`, `AC3 7.1`, `DTS / DD+ 5.1+`, `DTS / DD+ 7.1`, `Lossless 5.1+`, `Lossless 7.1`, from lowest to highest): with **Any** (the default, including for the whole existing library) nothing is enforced; choosing a preset rejects lower-ranked releases. Old audio weights still at their default values are aligned once, without touching custom values.

- **TV series: no false upgrades from audio or score drift.** The pre-download decision compared the new release score with the quality-only score of the archived file, while completion compared size and other bonuses too: an identical file could look like an upgrade, be downloaded again and then discarded. The archived file now uses the same `FileScore` as completion, the approval score no longer includes the swarm-health bonus, and a lower-ranked audio codec can neither win through the score alone nor replace a better track already in the library. The parser also recognises `TrueHD`, `7.1`, `DD+` and the common variants, so an unrecognised audio track no longer skews the comparison.

- **Movies already on the NAS: no wasted download.** Before downloading a movie, Gextto now also checks the file in the archive folder, not only the database row. If the file exists and is equal or better, the download does not start; if the database was rebuilt or the row was missing, the file on disk still counts. The same information appears in the decision explanation.

- **eD2k: safer import and more complete searches.** Gextto now waits until aMule has actually moved a completed file into the incoming folder, with the expected size and stable for a moment, before importing it; a file still being copied from another disk or Docker mount is no longer treated as ready. eD2k/Kad queries are normalized (accents, typographic apostrophes, parentheses) and, when `SxxExx` finds nothing, Gextto also tries `1x05`, `1x5` and `01x05`. With qBittorrent, a completion with no importable video now names the files it skipped, and the idle-seed limit is sent as `inactiveSeedingTimeLimit`.

- **Client identity is now a drop-down.** In **Configuration → Torrent engine** the identity announced by gx-torrent shows the presets (`gx-core`, qBittorrent, Transmission, Rain, anacrolix/torrent) in a select menu instead of a free-text field; the version stays a separate field.

- **eD2k respects move/copy.** Downloads completed by aMule also follow the **Move episodes/packs into the archive** option: when it is on the file is moved into the library and removed from the incoming folder (aMule stops sharing it); when it is off the file is copied or linked and stays in the incoming folder for sharing. For eD2k the move happens at completion, because seeding is managed by aMule and Gextto has no end-of-seed moment to wait for.

- **Docker: the whole stack in one command.** The new `docker-full/docker-compose.yml` starts Gextto with `gx-torrent`, Jackett, Prowlarr, FlareSolverr and aMule, with folders, ports and permissions already laid out. The image builds Gextto and `gx-torrent` from the checkout; the Italian and English guides cover first configuration, indexers, eD2k, updating, backup and troubleshooting. The client identity was verified with real qBittorrent 5.3.0 and Transmission 4.1.3 in Docker: they see `gx-core 1.1.252` by default and `qBittorrent 5.2.3` with the preset.

## 21 October 2026

- **Manual import of a file that already exists.** The new **Import file** tab
  of **Maintenance** reads a video or a folder already on disk (typed, or picked
  with **Browse**, which shows folders and videos), proposes series,
  season and episode (or movie and year) and the destination; you can correct
  them and confirm. Gextto renames, moves, records the episode or movie and
  refreshes Jellyfin/Plex. If a file with the same name already exists the
  import stops, unless **Replace the existing file** is ticked (the old copy
  goes to the trash). Multi-episode files are recorded for every episode.
- **Named quality profiles and a v3 API for Jellyseerr.** In Configuration →
  Sources you can name the combinations of quality, language, subtitles and
  exclusions; Gextto generates a profile for every combination already present
  in the library (the first is **Default**). Jellyseerr sees the profiles and
  can add series and movies through `/api/v3` (`system/status`,
  `qualityprofile`, `languageprofile`, `rootfolder`, `tag`, `series/lookup`,
  `POST series`, `movie/lookup`, `POST movie`, `queue`). Gextto follows only the
  requested seasons; a request for a series already in the library adds the new
  seasons, and a movie already there is not added twice. Each profile keeps its
  number when you remove or reorder others, so Jellyseerr always applies the
  one it picked. This is not full Sonarr/Radarr compatibility. Profiles are
  **templates**: their values are copied into the title, which stays free to
  change; changing a profile does not modify titles already created.
- **Edit several titles at once.** In the **Series** and **Movies** tables
  every row has a checkbox: tick the rows (even every filtered one), open
  **Edit selected** and change quality, language, subtitles, exclusions,
  upgrades or state on all of them. You can start from a profile's values.
  Everything else in each title is left alone.
- **Seed forever: the ratio or the time is enough.** 0 means infinite for both
  the seed ratio and the seed time, and now **either one** is enough for the
  seed never to end: the torrent keeps seeding and, with *Move* on, a copy goes
  into the library instead of a move. Before, a torrent with an infinite ratio
  still stopped at the time limit and was moved. Note: setting only the ratio
  and leaving the time at 0 now means an unlimited seed; to end the seed both
  the ratio **and** the time must be greater than 0. It applies to eD2k
  downloads too, which follow the seed rules and the global limits: with an
  unlimited seed the file is copied and aMule keeps sharing it.
- **Missing becomes a tab of Series, grouped by series.** Instead of a list of
  episodes one under another, the **Missing** tab of Series shows a block per
  series with a line per season and the episodes as buttons. A click on an
  episode opens the results to download; **Search missing** shows it is working
  and writes the outcome next to the series (before, a click on Search gave no
  feedback). There is a series filter; the Missing menu entry is gone.
- **Sources that do not answer: the log now says which and why.** The
  end-of-search warning only said "1 indexer". It now names every source that
  failed, how many of its searches failed and the last error, for example
  "indexer Jackett: 3 of 106 searches failed — timeout".
- **eD2k episodes: move or copy like movies.** With **Move episodes/packs to
  the archive** on, episodes downloaded with aMule are moved into the library
  at completion too, as movies already were; before, they could be copied and
  left in aMule, because an eD2k download looked set to "seed forever" even
  when the configured seed is finite. With an unlimited seed they are copied
  and stay shared instead (see above).
- **eD2k downloads in the log and in the history.** eD2k downloads started by
  hand (from "Download" in a search or from a pasted link) now appear in the
  log as soon as they are added, or with the reason if aMule refuses them.
  Every line of an eD2k download carries its `acq:` code and goes into the
  download's **History**, as for torrents: the history of eD2k downloads used
  to stay empty. It applies to new downloads.
- **Aligned buttons.** In Configuration the "Default" and "Save" buttons of the
  options with a note under the field (e.g. "0 = unlimited") now sit level with
  the field instead of lower. "Add" in Add torrent, the Custom groups rows and
  the Log toolbar are aligned too; there "Stop scrolling", "Filter" and "Copy"
  no longer break onto two lines.
- **gx-torrent announces its real version, and can use a compatible identity.** It used to say `gx-core 0.0.0` to peers and trackers. It now announces the real version (`gx-core 1.1.xxx`) and, if you want, can present itself as qBittorrent, Transmission, Rain or anacrolix/torrent with a chosen version. Only the announced identity changes: some private trackers only accept whitelisted clients, so check their rules before using it.

- **Downloads with thousands of torrents: the table stays smooth.** Above 500
  torrents the table keeps only the rows near what you are looking at and loads
  the others as you scroll: filters, sorting and selection still work on the
  whole session, without the old 300-row limit and without a huge DOM.
- **DLNA: search by title and Xbox/Windows Media Player.** Players can search
  for a title (video or audio) and get results from the library folders too;
  Xbox 360 and Windows Media Player accept the server through the service they
  require. On the gx-torrent page the **Download folder** button adds the
  configured download folder to the DLNA folders.
- **Watch folders: the text says what happens.** The section now explains that
  the `.torrent` files it finds are **downloaded** and their content added to
  the session, not only "added automatically".
- **DHT feeds: you can publish them now.** From the **Tools** menu (or the API)
  you sign a BEP 46 feed with the selected torrents, or with every torrent of
  the session, and get the `dht://…` address to give to whoever should follow
  it; Gextto keeps the key and bumps the sequence on every publish.
- **RSS feeds: when IPv6 stalls, Gextto retries over IPv4.** A host reachable
  over IPv6 that never sends data (extto.org behind Cloudflare is one) made the
  feeds time out one after another. After the first failed attempt the next ones
  dial IPv4, so ext.to feeds no longer disappear because of a timeout.
- **Downloads: eD2k rows show more data and the badge has a colour.** Each eD2k
  row now shows, besides state, progress, speed and sources, the **uploaded
  bytes**, the **estimated time** and the **ratio** between uploaded and
  downloaded; the **eD2k** badge has a colour of its own, so it stands out from
  the torrents.
- **Bandwidth: the ping next to Auto and an autotune that watches upload.** With
  the speed on Auto, the measured **ping** appears next to the **↓ Download**
  speed in the top bar; on an asymmetric line
  upload counts as load from a tenth of the capacity (between 32 and 256 KiB/s),
  so Auto caps it before it saturates the network. If upload starts loading
  during the control phase it is capped at once, without waiting for a new
  trial.
- **eD2k: «Recommended settings» removed.** The button that changed aMule's
  preallocation is gone; preallocation stays an explicit switch in
  Configuration → eD2k and Kad.
- **Hardlink: the log explains the same NAS on separate mounts.** When downloads
  and library are two NFS mounts of the same server a hardlink cannot cross
  them: Gextto now says so and suggests mounting a common parent or keeping the
  library in the same export, instead of the generic "different filesystems".
- **qBittorrent: «Propose» fills the path mapping.** The button reads
  qBittorrent's download and temporary folders and proposes the
  `gextto_path=qbittorrent_path` lines; check them and save.
- **Tooltips on «active» and «ok».** The **active** dot at the bottom of the
  menu and the **ok** badge at the top now explain what they mean: the first
  that Gextto is really working, the second the service health.
- **eD2k: exchanged totals are no longer zero and shared files show the last
  upload.** The eD2k page always showed **Session** as `0 B` because it asked
  aMule for the statistics at a detail level that does not carry them; now there
  are two rows, the bytes exchanged **in this session** and **since the start**
  (every session), both on the page and in the **Health** card. In **Shared
  files** requests and accepted requests show session and total, next to the
  file's speed and its **last upload** (`—` when aMule never uploaded it): the
  counters are aMule's, so the session column starts from zero whenever
  `amuled` restarts while the total one does not.
- **The Health page is fully translated.** The rows of the *Torrent engine*
  panel (queue, port, router, uTP, cache, disk, session totals…), the path
  labels, the process memory and the sortable table headers stayed in Italian
  even with the interface in another language: they now follow the chosen
  language (English, German, French, Spanish, Polish). The same goes for the
  *Connection* column in the Peers tab.
- **«Search missing» now really uses eD2k.** Before, eD2k only came into play in
  the automatic gap-filling pass: *Search missing* and single-episode search
  ignored aMule even with the integration on. Now, when **Use eD2k when torrents
  find nothing** is on and the torrents find nothing, the search asks eD2k/Kad
  too. A series search runs **one query per season** (not one per episode, which
  would be much slower): the results show the **eD2k/Kad** source and the
  **Download** button hands them to aMule. The automatic fallback now also logs
  how many results it finds, so its use can be verified; when it finds nothing
  it logs nothing.
  Manual eD2k searches show only files of the series searched for: a series
  with a similar name (for example «Lost Girl» when searching «Lost») no
  longer shows up among the results.
- **Lighter web interface, especially on slow networks.** Pages now travel
  compressed: the Archive drops from about 473 KB to 39 KB and the dashboard
  from 33 KB to 8 KB. Stylesheets and scripts are compressed and stay in the
  browser cache tagged with the build number: after the first load they are no
  longer downloaded on every page, and on the next check the server answers
  «not modified» with no body. Gextto stays responsive even on slow links or
  from a phone.
- **Archive: the Download button of eD2k results no longer ends up off
  screen.** With long file names the eD2k results table grew wider than the
  window and the **Actions** column with **Download** stayed out of sight: you
  had to scroll sideways. Now the name is shortened with an ellipsis (the full
  text is in the tooltip) and **Download** stays visible on phones too. Added a
  test that checks the button is present in the archive results.
- **Maintenance → Backup: no more confusion between saving and backing up.**
  The button in the settings panel is now **Save settings** instead of *Save
  backup*: it saves retention, schedule, FTP, cloud folder, extra paths and
  Telegram without creating any archive. To create a backup right away use
  **Backup now**, now also below the backups table (as well as in Quick
  actions). **Backup now** and **Verify** say they are working as soon as they
  start, stay disabled until the operation finishes, and then report whether
  the backup was created or the archive is intact. Fixed as well the internal
  `maintenance-t0` name that could show up in the page title after an action
  started from the backups table.
- **Configuration → Your sources: indexer errors open Health.** When the last
  cycle had errors, the entry becomes **Indexer errors** and opens **Health**
  with the details, instead of indexer management (which stays under
  **Integrations**). Fixed as well the wording for a single error, now «1
  error» instead of «1 errors».
- **eD2k and Kad: aMule folders in plain sight.** In **Configuration → eD2k
  and Kad** you now see aMule's incoming (download) and temporary folders,
  read-only. The tooltip explains that they are changed in aMule/amuled and
  that after the change Gextto must be restarted (or the page reopened) so it
  uses the new path.
- **Housekeeping: more useful tooltips.** The housekeeping options now say what
  they clean and what they leave alone: files, the library, the release Archive
  and active torrents are outside their scope.
- **Release archive: automatic cleanup now works.** The «Automatic archive
  cleanup», «Archive — maximum age» and «Archive — keep at least N entries»
  settings were saved but never run. They now run with the periodic
  housekeeping and remove only release-catalogue rows older than the limit:
  downloaded files, the library, download history and active torrents are
  untouched.
- **Torrent details: tooltips that actually explain.** Hovering a value in the
  **General** tab, an action button or a table header in the torrent details now
  shows what the value means or what the button does, translated into every
  interface language.
- **gx-torrent gives memory back to the system.** After a period of heavy
  downloading gx-torrent could stay at hundreds of MB of RAM even with no
  traffic. After 5 minutes with the transfer rate below the slow-torrent
  threshold it now returns the unused memory to the system (at most once every
  10 minutes); the caches rebuild by themselves on the next transfer.
- **Archive inflated by repeated releases: fixed.** The archive could hold the
  same release dozens of times when the indexer (for example Jackett)
  regenerated the `.torrent` link on every search: the link changed, the
  content did not. A release is now recognised by its stable identifier and
  stays one row. **Maintenance → Archive SQL maintenance** has two more
  operations: **Recent releases**, to see what just came in, and **Duplicates
  by title and source**, to clean up the rows already accumulated while keeping
  the newest. Magnets are never touched. A release with a magnet is still
  recognised by its torrent: when a feed publishes a REPACK under the same
  identifier, both enter the archive, each with its own title. When the
  indexer offers the `.torrent` link, Gextto downloads that even if it knows
  the hash (Jackett and Prowlarr): on private indexers a magnet without trackers would find nobody.
- **Download notifications say where the torrent comes from.** The «New episode
  download started» / «Movie download started» message (Telegram, email and the
  other channels) now has a **📡 Source** line with the feed, the indexer or the
  local archive the release was taken from, for example «ExtTo -
  TheBlackKing», «Knaben (Jackett)» or «ExtTo (local release archive)».
- **Backup and Simkl settings: «Save» now really saves.** In
  **Maintenance → Backup** the *Save backup* button stored none of its fields
  (neither *Files and folders to include* nor the others), and *Save Simkl* did
  the same: the page reloaded as if everything had gone well. The values are
  now saved, and a rejected value (for example a path that is not absolute)
  shows a notice with the reason. Anyone who tried to change these settings
  has to enter them once more.
- **Many active torrents, CPU under control.** Besides the limit of a single
  torrent, gx-torrent now keeps a total of connections for the whole machine
  (75 per core: 300 on a 4-core mini PC), shared among the running torrents:
  downloads come first, a seeding torrent asks for half and for the minimum
  (15) when nobody downloads from it. A torrent on its own always gets all of
  its connections, so the total only matters with many torrents together. A
  torrent just added gets its limit at once instead of running without one for
  the first 30 seconds, and a torrent moving a few bytes per second no longer
  counts as «making progress». In the log, when the total trims a torrent the
  line says so («shared with 9 running torrents»).
- **Seed with seed: connection closed at once.** While a torrent seeds,
  gx-torrent closes the connections to peers that already hold everything we
  hold: there is nothing to exchange, and keeping them cost CPU and memory for
  nothing.
- **A less repetitive Gextto log.** The automatic speed tuning (Autotune) writes
  a line when its mode changes and then a digest every 15 minutes (how many
  adjustments, the range, the current values) instead of a line every minute;
  the single steps stay in the debug log. «Search interrupted» appears once when
  Gextto stops, the title search progress is not repeated when nothing changed
  in the last minute, and gx-torrent's summary of peer network errors comes
  every 30 minutes instead of every 10.

- **The machine decides the connections, not a fixed number.** Gextto now lets
  gx-torrent choose how many connections to keep for each torrent from the
  machine's cores and RAM, and trims the ones on torrents that have made no
  progress for a couple of minutes despite many peers: the useless connections
  make room for the queued ones, CPU stays low and the speed does not drop.
  Every change goes to the log («automatic peer cap: 55 connections per
  torrent …», «peer cap for …»), so it is clear why the number moved. Under
  **Configuration → Queues and performance → Connections** there is **Automatic connections
  (sized from the machine)**, on by default; **Max connections per torrent**
  stays the ceiling (-1 = no ceiling) and a limit chosen in a single torrent's
  *Limits* tab always wins. The same option is on the gx-torrent daemon page
  (*Automatic peer cap*). Anyone who prefers a fixed number turns the automatic
  one off.
- **One installation never touches another's engine.** If the gx-torrent of
  another Gextto installation answers on the configured port, the new instance
  now stops with a clear message instead of adopting it: before, it could
  reload its IP filter and restart its session, interrupting the transfers in
  progress. The automatic interface tests now use their own engine.

- **IP filter: the default list is now eMule Security.** Gextto and gx-torrent
  start with the eMule Security list
  (`https://upd.emule-security.org/ipfilter.gz`) instead of the Aetherinox
  one. The new list is in P2P format, updated every day and blocks fewer
  legitimate addresses: it no longer blocks whole provider or hosting ranges
  such as `158.173.0.0/16`, which also stopped available peers. Anyone who has
  already set a different URL keeps it; to switch to the new list, paste it in
  **Configuration → Speed and network → Encryption and IP filter**, field **IP filter (file/URL)**, and press **Load / update now**.
- **Maintenance: an SQL tab for the release Archive, with a language
  selector.** Under **Maintenance** there is now the **Archive SQL
  maintenance** tab: a selector at the top chooses the language for the
  operations, and the list offers releases older than 6 months or 1 year,
  releases in the selected language or without it, empty titles, plus read-only
  diagnostics (age distribution, releases per source, archive health, duplicate
  titles). Every deletion operation first shows the preview with the count and
  the first 200 rows, then asks for confirmation. While the query runs a fixed
  indicator shows the work in progress; when it finishes the panel scrolls to
  the result. It is not a free SQL console: it runs only the bundled scripts and
  does not touch the library, active torrents or settings. The language is inferred from the title, so releases
  with no marker fall into the “not in the selected language” list.
- **Version: the third number restarts every 1000 builds, the build stays
  visible.** When the counter reaches 2000 the version goes from `1.1.1999` to
  `1.2.0`, then `1.2.999` and `1.3.0`, and so on: the third number no longer
  grows forever, and gx-torrent follows the same rule from its build 1000. So
  the exact reference is never lost, after the rollover the version shows the
  **build** in parentheses (`1.2.925 (build 2925)`, also in `--version`, in the
  API and in the header) and the bottom of the **Maintenance** page shows the
  version, the full build and the **commit** the binary was compiled from (the
  short hash; the full one on hover). The current version, `1.1.1925`, stays as
  it is.
- **Download history: the torrent name stays readable.** On medium-width
  screens (a phone held sideways or a narrow window) the **Name** column shrank
  to a few pixels and the title wrapped one letter per line, because the table
  kept tag, score and source visible anyway. Those columns now fold one at a
  time as the space shrinks, so the name always gets a useful line (the full
  name stays in the hover tooltip). Nothing changes on a wide screen.

## 20 October 2026

- **Dashboard: summary in the top bar and readable names.** **Free space** and
  **Archived magnets** moved from the page body to the far right of the top bar
  controls row, under the clock, and appear only on the Dashboard; on a wide
  screen **Italiano** opens that row under the first badge, the Gextto one.
  Series/Movies and Engine are no longer shown at the top. In **To check** the
  download name stops at its season/episode (the
  full name appears on hover), so the reason has room and the table no longer
  needs side scrolling.
- **Top bar: Aggiorna and the state next to Add torrent.** The **Aggiorna**
  (refresh) button and the service state dot (the green **ok**) now sit in the
  first row, right after **Add torrent**, instead of the controls row below.
  Language, text size, font and theme stay below.
- **Notice when the MirCrew login breaks.** If the forum login of the
  `mircrew-indexer` service stays broken for more than 15 minutes a notification
  arrives (Telegram, email or webhook, as configured), with the reason reported
  by the service and what to do; a second notice arrives when the login works
  again. Before, the only possible notice was two failed `.torrent` links in the
  same cycle, which does not fire while searches still answer from the local
  index.
- **Fixes to eD2k and multi-episode files.** A file with a dash before the
  resolution or the year (for example `S01E01-1080p`) is no longer taken for a
  file holding hundreds of episodes, so renaming and import always pick the
  right file. The button that reloads aMule's shared files now shows the list
  read after the request, not the previous one. If an eD2k search runs out of
  time while waiting to retry, Gextto says so instead of blaming another
  search, and it spots sooner the results of a search started by another
  program connected to aMule. The outcomes of aMule actions show only fixed,
  translated messages: on an error the details are in the log. aMule's and
  gx-torrent's CPU and RAM in the top bar cost the machine less and stay steady
  with several tabs open.
- **Backup: files and folders outside the data folder too.** In the backup
  panel of Maintenance, **Files and folders to include** takes one absolute
  path per line for files or folders to put in the backup ZIP, for example the
  Docker scripts you use to install services. In the archive they appear under
  `extra/` with their original path; a path that no longer exists is skipped
  with a log warning, without failing the backup. Leave it empty to include
  only the Gextto configuration.
- **eD2k: filter the results and Scarica always there.** Above the eD2k search
  results (eD2k page and archive) there is a box to filter by words in the name,
  minimum sources, minimum size and already-known files, with sorting by
  sources, size or name: it works on the results already received, without
  running the search again. The **Scarica** button stays on every result, also
  in the release search, which no longer loses its results box after the first
  search.
- **Clearer top bar.** The status badges (Gextto, gx-torrent, aMule, speeds,
  torrents, peers and next cycle) have a row of their own and are wider;
  **Add torrent** stays beside them, while language, text size, font, theme and
  **Refresh** move to the row below.
- **eD2k: consistent results even with another search running.** aMule has a
  single result list, shared with its own interface and with every other
  connected tool: if another search replaces it while Gextto is searching,
  Gextto runs the search again and, if the interference continues, says so
  instead of showing unrelated files. It applies to the search on the eD2k page
  and to the archive search with **eD2k** ticked.
- **eD2k: aMule preallocation in the configuration tab.** **Configuration →
  eD2k and Kad** has the aMule panel with the **Preallocate the whole file in
  aMule** switch and **Recommended settings**: the value is applied to aMule at
  once (it is aMule's setting, not one stored in Gextto). With aMule in Docker
  the folders are checked inside the container: if they are missing there Gextto
  reports them but does not create them from outside.
- **eD2k: missing aMule folders reported and recreated.** When aMule's incoming
  or temporary folder is missing (a network disk unmounted, a folder removed),
  the eD2k page says so and **Create the folders** makes them again: the flow of
  *Failed to get file system statistics* in the log stops and completed files
  are imported again.
- **eD2k and Kad get their own Configuration tab.** The nine aMule/eD2k rows
  (connection, search network, fallback, stalled downloads) are no longer inside
  **Sources**: there is a dedicated **eD2k and Kad** tab, and the “Open the
  configuration” links land there.
- **eD2k: session totals, preallocation and warnings.** The eD2k page shows the
  bytes aMule exchanged in this session and whether aMule preallocates files;
  **Recommended settings** switches full preallocation off (the one that blocks
  `amuled` on network disks). **Health** gains an eD2k card with state, speeds,
  totals and the latest warnings from aMule's log.
- **Pieces: the map shows on a stopped torrent too.** Opening the details of a
  paused or parked torrent, the **Pieces** tab said “metadata not yet present”
  even at 99% downloaded. The verified pieces are there: the map now shows them,
  with the missing ones in grey.
- **Search: eD2k results can be queued too.** In the manual search, eD2k results
  showed “Search only” with no **Add**, as if they could not be downloaded. They
  now queue like torrents: the `ed2k://` link goes to aMule. With the
  integration off the eD2k results do not appear, instead of sitting there
  without an action.
- **Search bar: the button is one line tall again.** **Open eD2k links** is no
  longer as tall as the link field: it keeps a normal button height.
- **eD2k: the box says eD2k, and the link button stays on one line.** In the
  Downloads search (and the other search boxes) the checkbox is no longer
  called “Kad” but **eD2k**: the network is the one chosen in Configuration →
  Sources → eD2k and Kad (Kad, servers or both), as its tooltip now says. The
  **Open eD2k links** button no longer wraps: the link field takes the available
  width and the button stays on one line.
- **eD2k page with tabs, and aMule in the TUI.** The eD2k page now has three
  tabs — **aMule and search**, **Shared files** and **aMule log** — instead of
  the panels at the bottom of the page. In the TUI, with eD2k on, the **Status**
  tab shows the **aMule** line (version, server, High/Low ID, Kad, upload and
  queue) and aMule's downloads appear in the **Torrents** tab: `Enter` opens the
  detail with sources, priority, last data received and the `ed2k://` link, `y`
  copies the link, `p` pauses or resumes, `d` takes it off aMule.
- **Top bar: CPU and RAM per process.** The two CPU and RAM boxes are now one
  **Gextto** badge with `CPU% · RAM`, and next to it **gx-torrent** (the torrent
  engine, when the daemon is local) and **aMule** (when the eD2k integration is
  on and `amuled` runs on the same machine). Each badge refreshes every 5
  seconds like the other figures; a process on another host gets no badge,
  because those figures cannot be read from there.
- **eD2k: completed downloads reach the history at once.** An eD2k file that
  completed and was imported into the library appeared in the history only at
  the next reconciliation, up to six hours later. It is now recorded as finished
  as soon as the processing ends, like a torrent removed after completion.
- **eD2k: see what aMule is uploading.** The eD2k page has a **Shared files**
  panel: for every file the bytes uploaded in this session and over all time,
  requests and accepted ones, queued clients, priority and the `ed2k://` link to
  copy; below it, the clients you are uploading to with software, speed and
  queue position. **Reload shared files** asks aMule to rescan its folders, so
  files added or removed by hand show up at once; the shared folders are listed
  read-only.
- **Several episodes in one file: the name carries the range.** When a download
  covers contiguous episodes (for example `S01E01E02`), the library file is
  renamed `S01E01-E02`: Jellyfin and Plex list both episodes and the archive
  scan registers the file under each of them. A non-contiguous range keeps the
  first episode in the name.
- **eD2k: movies completed by aMule reach the library.** A movie downloaded
  through aMule with the library folder configured was not imported: it stayed
  in a loop of failed moves. It is now copied or linked like an episode and goes
  through the same quality check before touching the NAS: if an equal or better
  version is already there, the download is discarded (to the trash) and not
  copied. With no library folder the movie stays in aMule's incoming folder and
  is marked completed.
- **eD2k and Kad move to aMule.** The `gx-ed2k` daemon is retired: for the
  eD2k/Kad network Gextto now drives **aMule** (`amuled`), installed and started
  on its own with external connections (EC) enabled. With aMule on you can
  **search** Kad or the eD2k servers, **paste `ed2k://` links** (more than one,
  even mixed with text) and **actually download them**: aMule's downloads appear
  in **Downloads** next to the torrents and the HTTP downloads, with progress,
  speed, sources and the pause, resume and remove buttons. When aMule completes
  a file, Gextto renames it and imports it into the library like a torrent (by
  hardlink or copy, so aMule keeps sharing it). The new **eD2k** page (Discovery
  menu) shows aMule's state (server and high/low ID, Kad, speeds, network), the
  server list with a button to connect, the commands to connect and disconnect
  eD2k and Kad, the download of `server.met` and `nodes.dat`, the search and the
  box for links. **What to do:** in Configuration → Sources → eD2k and Kad switch
  on **eD2k and Kad through aMule** and enter the address, port and password of
  aMule's external connections (the old gx-ed2k URL and token options and the
  UPnP one are gone). The automatic gap-filling fallback stays a second switch,
  off by default. An update removes the old `gx-ed2k` binary.
- **eD2k: completions, stalled downloads and non-video links.** Files completed
  by aMule 3.x (which drops them from its queue at once) are recognised and
  imported, also after a Gextto restart. An eD2k download chosen by Gextto that
  receives no data for 7 days (option **Give up on a stalled eD2k download
  after**, 0 = never) is cancelled in aMule, blocklisted and the episode is
  searched again; downloads added by hand and paused ones are left alone. A link
  to a file that is not a video stays aMule's download only and no longer ends up
  in quarantine. The old extto and gx-ed2k settings are deleted (aMule's
  address, port and password are kept). The manual explains how to run aMule in
  Docker and open the ports with UPnP.
- **eD2k: a link aMule already has is still followed.** When aMule answers that
  the file is already listed, or is slow to answer while it creates a large part
  file, Gextto checks the queue and, if the file is there, registers it and
  imports it into the library when it completes. With aMule on a network disk
  set `AllocateFullFile=0` in `amule.conf`: preallocation blocks aMule for tens
  of seconds.
- **eD2k: bandwidth, details, log, counters and TUI.** aMule follows the same
  bandwidth limits as the torrents (rule, exception, schedule). In Downloads ⓘ
  opens an eD2k download's details, with sources, last data received, link to
  copy and the priority in aMule. The eD2k page shows aMule's log; the dashboard
  and the menu count eD2k downloads too, and the TUI lists them with pause,
  resume and remove. The automatic fallback searches Kad and the eD2k servers
  together. Fixed an error that marked an eD2k episode as failed right after its
  library import and kept the automatic fallback from starting a download.
- **eD2k: Kad + servers, Archive and dashboard.** The eD2k page and **eD2k search
  network** offer **Kad + eD2k servers**, which runs both searches and merges the
  results. In **Archive** the **Also search eD2k** box shows aMule's results
  below the archive, with **Download**. On the dashboard the traffic chart has
  dashed lines for aMule, apart from the torrents', and the top bar also adds
  aMule's upload.

## 19 October 2026

- **eD2k/Kad search, when torrents are not enough.** An optional search
  provider, off by default, searches Kad and eD2k servers through the `gx-ed2k`
  daemon (pure Go, our own). In manual search the eD2k results appear next to
  the torrent ones with a network badge and the source count; a pasted
  `ed2k://` link is checked and shown with name, size and hash. Configuration →
  Sources has an **eD2k and Kad** group: the switch, the daemon URL and token, a
  second switch to use it in gap filling only when torrents find nothing, the
  source floor and the timeout. The automatic fallback is off and accepts only
  an exact episode match with a recognised quality. On the **Sources** page the
  **eD2k and Kad** entry shows a connection sensor (**open**, **limited** by
  firewall/NAT, **unknown**) and the daemon tries to open the Kad port with
  **UPnP** (UDP 4672 and TCP 4662): when it works eD2k servers assign a HighID.
  Learned Kad nodes are saved and reused on restart. **There is no eD2k
  transfer**: the daemon says it can only search and the add button stays
  disabled, so the library cannot fill with wrong files. The **Downloads**
  **Search a release** panel has a **Kad** checkbox: tick it to include the
  network in that single search, next to DB, web and indexers. If the provider
  is off, that search starts it without changing the global setting. The automatic
  fallback also queries the series' **alternate titles** (for example
  "Murdoch Mysteries" for the Italian title), so a localized name does not hide
  releases published under the original one.
- **Standalone gx-ed2k, with updates.** The search daemon's Linux archive now
  ships with the other releases (same download folder) and its page has
  **Check for updates**: it compares the build with the latest published one
  and, when newer, links to the archive for the right architecture. A
  **Windows** archive (`gx-ed2k-windows-<arch>.zip`) is published too, with
  `install-service.ps1`, which registers the service and opens UDP 4672/TCP
  4662 in the Windows firewall. On Linux the check can also **install** the
  update (checksum and version verified, old binary kept, automatic restart);
  on Windows the zip download remains. A build compiled from local source
  offers nothing.
- **Safer updates.** An update, from the **Update** button or by running the
  installer again, succeeds only when the new version's web interface answers.
  If the new version goes down a few seconds after starting, the previous one
  comes straight back: before, it counted as a success and the service stayed
  down. Running the installer again keeps the user of the installed service,
  instead of switching to the `gextto` account when run as root without `sudo`.
- **Traffic chart.** On the Dashboard the part of the last 30 minutes with no
  data yet stays empty, instead of a ramp rising from zero to the first value.
  Speeds under 1 KB/s count as zero: that is just protocol chatter, and the
  chart stays flat instead of twitching.
- **The Dashboard bandwidth tells the truth.** With **Auto** speed the
  **In session** card shows the **Auto** badge instead of "No limit": the
  speed is not free, Gextto is measuring the line and adapting to it.
- **Clearer startup logs.** The lines that restore saved state (stalled
  downloads, metadata waits, pending moves) now list the torrents involved, not
  just how many there are, so it is immediately clear what they refer to.
- **Translations.** The download counter reads "Torrents: 5 · HTTP: 0", right in
  every language; the engine tile on the Dashboard is called "Engine" in English
  and "Motor" in German.
- **Speed: a rule and an exception.** In Configuration → Speed and network a
  single choice, **How the speed is decided**, sets the rule: Auto, fixed limits or
  hourly schedule. With the schedule you also pick whether Auto or the fixed limits
  apply outside the windows. Your settings stay as they were: nothing to redo. In
  Downloads the **Bandwidth** menu says what applies now and where it comes from,
  and makes an **exception**: Auto or a limit, for 1, 3, 6 or 12 hours or until you
  remove it. The exception always beats the rule, schedule included: a limit you
  set is no longer overridden by a schedule window. The mechanism that measures
  the line is called **Auto** everywhere.
- **A tidier Downloads page.** Filter, tag and sorting share one row; **Clean
  completed**, **Orphaned files** and the automatic refresh are in the **Tools**
  menu. Pause, Resume, Check again, tags and Remove from session appear only when
  you select torrents, and the selection is no longer lost when the list refreshes
  on its own. "Remove completed items after seeding" left the toolbar: it is set in
  Configuration → Seeding, and the Tools menu takes you there. "Unlock pin" is in the
  pinned torrent's details; the list marks it with a 📌 badge.
- **Bandwidth menu.** The temporary-limit fields at the top of the panel give way
  to a **Bandwidth** button that shows the limits in force. From its menu you set
  Auto or a download or upload limit on the fly (64 KB/s to 10 MB/s) and its
  duration, in one click. Exact values stay under **Custom values…**.

- **Discarded packs leave no wrong traces.** When the inspection decides that a
  downloading pack is not needed, Gextto restores the previous version of the
  episode: the library no longer keeps the quality and title of the discarded
  release over the old file.
- **Much clearer decision logs.** An upgrade download says what quality is
  already in the library (`from: S01E01 480p h264 ...`); a discarded pack shows
  the episode-by-episode comparison; a partly useful pack lists the episodes
  kept and those skipped.
- **Readable metadata wait.** The “nobody provided the file list” message now
  reports peers, bytes received and the start time; when the list arrives, a
  line says so; if the counter restarts, the log explains why.
- **The metadata wait survives restarts.** If a magnet is waiting for its file
  list and Gextto restarts, the clock resumes where it was: after 24 hours
  without a list it is dropped as a dead link, instead of getting a fresh
  window at every restart.
- **Real quality versus recorded release.** If the file in the library and the
  recorded title disagree (for example a 480p file with a 1080p title), the
  quality check reports it and says that the file wins.
- **Manual and changelog online.** The **Manual** and **Changelog** entries open
  the updated documentation on GitHub in a new tab, in the interface language:
  Italian for the Italian UI, English for the others. Updating the manual and
  changelog no longer requires rebuilding Gextto; the old `/?view=manual`
  address leads to the online manual.

## 18 October 2026

- **Pack inspection while downloading.** Gextto reads the file list as soon as
  it arrives, compares the episodes with the library and discards a pack that
  adds nothing; with the dedicated option it downloads only the useful files,
  not the whole pack.
- **Dead `.torrent` links set aside.** A link that fails in two different cycles
  is quarantined and no longer blocks the search; a magnet the indexer cannot
  serve yet is not treated as a dead link.
- **More consistent Auto speed.** The Auto switch follows the configuration and
  search timeouts are diagnosed without filling the log with warnings.
- **Lighter engine.** gx-core no longer lowers the descriptor limit and does not
  keep an unnecessary file pool.

## 17 October 2026

- **Tidier search.** Jackett and Prowlarr are queried one indexer at a time;
  dead links are quarantined; the archive is indexed by torrent (infohash) and
  not by the magnet text.
- **Finer seed rules.** Seeds can be stopped by source or category and an idle
  seed limit can be set; the state survives a restart.
- **More observable API.** Prometheus metrics, a session-bound event stream and
  qBittorrent 5 compatible calls for gx-torrent.
- **Less wasted memory.** The engine releases the memory of the IP filter and
  uTP buffers after use; the peer reader no longer spins on dead uTP connections.
- **Diagnostics on demand.** Opt-in pprof profiles on loopback only, shared by
  Gextto and gx-torrent.
- **Backup and restore verified** end to end together with the release packages.

## 16 October 2026

- **Livelier dashboard.** A new 30-minute bandwidth chart, a single “In session”
  frame and “To check” warnings that stay until the problem is resolved, with a
  Clear button.
- **Auto speed and time window.** Day mode has three states, the “Auto” window
  is handled by the auto-tune and the proposed speed takes the observed peak
  into account.
- **More guided configuration.** Easy/Expert levels, an “Automatic management”
  card that adapts, provenance badges for values and a tuning system that
  proposes changes instead of applying them silently.
- **Season packs only when useful.** A pack is downloaded only when it contains
  at least one missing episode or a real upgrade.
- **More honest scoring.** Swarm health enters the candidate choice; archived
  episodes are scored from the real file and the original-language title avoids
  placeholder titles.
- **Safer downloads.** Approval and torrent add go through a single crash-safe
  path.

## 15 October 2026

- **Poster library.** TV series and movie lists can be shown as cards with
  poster, status and progress, on phones too.
- **More comfortable torrent detail.** The detail opens from the name and becomes
  a bottom panel with engine statistics and a working “re-download”.
- **More complete gx-torrent standalone.** More menu, Wanted with season packs,
  feed folders, interactive episode search, archive maintenance and
  accessibility checks.
- **More precise notifications.** A season pack distinguishes new episodes from
  upgraded copies.
- **Support scripts.** Smart `/tmp` cleanup with a systemd timer and
  `gx-core-test.sh` using a local build beside production.
- **User log in English** and less noise in the status heartbeat.

## 14 October 2026

- **Access keys from the UI.** Gextto API keys and the gx-torrent token can be
  generated and copied from the interface.
- **Engine fallback.** If the configured engine is unavailable, Gextto can use
  qBittorrent-nox on demand; the build with embedded libtorrent is no longer
  shipped.
- **gx-torrent keeps growing.** Statistics page, alternative VueTorrent
  interface, automatic torrent management (autoTMM), watch folders, torrent
  creator, cookies, alternative limits, RSS and search APIs.
- **Opt-in DLNA/UPnP media server** for completed content, with library folders
  and subtitles.
- **In-app update** for gx-torrent standalone too.
- **Per-day bandwidth scheduler** in the Gextto UI and gx-core version visible in
  the header.

## 13 October 2026

- **Swarm Merging.** The engine reuses data already present in other releases or
  layouts of the same content, even when a sibling appears later.
- **WebSocket tracker** (`ws`/`wss`) and **DHT feeds** (BEP 46).
- **More responsive uTP.** Adaptive RTO with a minimum-latency floor and
  loss-driven backoff; retransmission and SYN-drop counters in both UIs.
- **Multi-piece pipelining**: a peer can download two pieces at once.
- **Disk guard**: downloads pause when the folder is running out of space.
- **New gx-torrent UI** on the shared design system, standalone log and process
  CPU/RAM.
- **Clearer Wanted**: Have/Missing status and episode management in the dialog.

## 12 October 2026

- **gx-torrent standalone usable from the LAN**, with a dedicated Wanted tab,
  instant feed check and manual search.
- **FlareSolverr** for feeds and indexers; the archive becomes the main index and
  indexers are used for gaps.
- **Custom themes** with user-chosen colours and a third “cream” theme; feedback
  when the trash is emptied.
- **Anti-SSRF**: web seeds and magnet sources cannot reach the LAN.
- **Engine**: opt-in mmap storage with `fdatasync`, DHT retried every 15 seconds
  on a cold start, graduated file priorities 0–7, Swarm Merging phase 1, BEP
  44/46.
- **Downloads**: “Clean completed” no longer blocks on a huge trash copy; global
  seed limits are honoured.
- **UI**: two-column torrent detail, complete provider status, Torznab address
  consistent between Gextto and gx-torrent.

## 11 October 2026

- **More resilient DHT**: optional per-subnet node cap (anti-Sybil), tokens bound
  to the infohash, routing-table concentration visible.
- **NAT**: mapping type inferred from DHT observers and PCP (RFC 6887) support.
- **Latency-based auto-speed** (anti-bufferbloat).
- **More powerful RSS**: scoped feeds, folders, episode filters, smart episode,
  REPACK/PROPER, disableable rules and “follow a series”.
- **BEPs**: 54 (`lt_donthave`), 53 (`ws=`), 21 (`upload_only`), 38, 31.
- **Standalone**: wizard, indexers, RSS, limits and scheduler visible in the UI.
- **Security**: hardening of create-torrent, login redirect and session cookies.

## 10 October 2026

- **Windows**: native service, zip package, junction instead of symlinks,
  cross-volume move and wizard; gx-torrent also downloads and compiles on
  Windows.
- **BitTorrent v2** (BEP 52): metainfo, file tree, SHA-256 piece layers, magnet
  and v2-only torrents.
- **Dual-stack IPv6**: UDP tracker, `peers6`, PEX, DHT.
- **qBittorrent API** ever more complete, RSS, webhook notifications, bandwidth
  scheduler and standalone mode with login/CSRF/LAN.
- **Six languages** for UI, TUI and notifications; accessibility and tooltips.
- **Observability**: cycle duration and sources, threshold alerts, “To check”
  panel.
- **Abandoned packs**: already completed episodes are brought into the library
  before removing the torrent.

## 9 October 2026

- **Safer installer**: service running as the local user, update with rollback,
  uninstall, dry-run and self-test in CI.
- **Guided setup**: language, folders with Browse, TVDB as an alternative to
  TMDB, port and bandwidth tests.
- **In-app update** and portable package.
- **Pure-Go build by default**, with libtorrent opt-in.
- **Settings**: “Default” button on every field, gx-torrent as the default
  engine, options that do not match the engine are disabled.
- **Holepunching BEP 55** for gx-torrent.
- **TVDB** fully supported as an alternative to TMDB.
- **Notifications** on Gextto restart/crash and gx-torrent problems.
- **First/last piece** as a global setting; fixes to rename, seed and season
  packs.

## 8 October 2026

- **Notifications to Discord, Slack, ntfy, Gotify and Pushover**, in addition to
  Telegram and webhooks.
- **Tidier v2 UI**: Configuration by areas, sticky headers, jobs panel only when
  needed, grouped Integrations.
- **gx-torrent**: BEP 16 super-seeding, HTTP streaming with Range, per-torrent
  limits, per-piece diagnostics, web seeds and trackers editable at runtime.
- **gx-torrent survives Gextto restarts** and has its own build number, rotating
  log and a panel in the Gextto UI.
- **Database**: no rewrite on every restart or stall tick.
- **Security**: integer conversion and path hardening.

## 7 October 2026

- **gx-torrent becomes the default engine**, with parity with libtorrent: save
  paths, bandwidth, queues, categories, uTP, LSD, IP filter, cache, single peer
  port, file selection and stall handling.
- **gx-torrent UI**: working web page, torrent detail, statistics, LAN, IP
  filter, token moved to the Access tab.
- **Optional login** for clients outside the LAN.
- **iCal calendar** of releases, original broadcasts and movies.
- **Anime**: absolute episode numbering.
- **Comics**: `ComicInfo.xml` also in CBZ files downloaded directly.
- **Archive**: hardlink seeding files into the library instead of copies.
- **Upgrades**: score threshold above which a copy is no longer replaced.
- **Jellyfin/Plex**: rescan only the changed folder.
- **TUI**: new library, downloads, detail and maintenance handling.

## 6 October 2026

- **More complete TUI**: library, downloads, torrent detail and maintenance,
  with a layout suited to narrow terminals and low traffic.
- **Telegram backup** split when it exceeds the API limit.
- **Notifications** without duplicates and without alerts for damaged pieces.
- **Trash and cleanup**: orphan files in the temporary folder go to the trash;
  sparse files stay sparse.
- **Movies**: single-video torrent folders archived as flat files; leftover
  folders renamed.
- **Stalls**: the wait state survives restarts.
- **Plain-language log** and names of the files read by the MediaInfo backfill.
- **Scores** recalculated when weights change.

## 5 October 2026

- **Archive**: episode matching in subfolders, trash as default, write probe and
  upgrade notice; post-seed finalisation.
- **Cycle**: placeholder rollback if the download is skipped, RAM-disk budget
  respected, dry-run and trash more consistent.
- **UI**: table search that stays after an action, no form flicker, visible
  Prowlarr status.
- **Stalls**: persistent retry state and a warning after three hours.
- **REPACK** preferred when replacing the archive.

## 4 October 2026

- **Library**: specials preserved, sources deduplicated, episode search in a
  modal, FTS index rebuilt when out of sync.
- **Series**: alphabetical order by default and search actions on the page.
- **Dashboard**: upcoming titles linked to TMDB/TVDB.
- **Health**: mircrew-indexer service row and provider links.
- **Maintenance**: SQLite integrity and indexes.
- **Mobile**: more readable bottom navigation and improved download layout.
- **Torrent detail**: clearer tabs and stall logs.

## 3 October 2026

- **Maintenance**: VACUUM and optimisation with start/completion toasts, correct
  database sizes and verified integrity.
- **Archive**: “archive and remove” option, completion notifications and content
  cleanup.
- **Content filters** applied before archiving.
- **Scores**: custom groups at the bottom and corrected modifiers.
- **Installer** and PayPal support.
- **Log**: individual download starts and seeding summary.

## 2 October 2026

- **Trakt and the Anacrolix backend removed**: gx-torrent and qBittorrent-nox
  remain.
- **Mobile**: bottom tab bar, simplified top bar, torrent cards and Log always
  reachable.
- **Torrent safety**: safeguards on removal, trash routing, temporary file
  cleanup and concurrency locks.
- **Post-processing**: lower-quality duplicates, season packs, MediaInfo fallback
  and trash.
- **CodeQL** and v2 log selector fixed.
- **Search**: gap search and torrent results improved.

## 1 October 2026

- **The v2 UI becomes the official interface**: dashboard, downloads, archive,
  series/movie detail, search, logs and configuration in SSR + HTMX.
- **Dashboard**: clock, live metrics, downloaded movie history, progressive
  search and upcoming releases.
- **Cycle**: priority to gaps already in the archive, search progress and manual
  monitoring summary.
- **Providers and backup** clearer in checks.
- **TUI**: accessible library tab and expanded metrics.
- **Dozens of UI fixes** for alignment, tables, modals and mobile.

## 30 September 2026

- **Episode sources** in a modal in the series detail.
- **Engine-aware settings**: options that do not match the active engine are
  disabled.
- **Seed and archive**: seed before archiving, `auto_remove_completed` honoured,
  seed ratio and archive cleanup effective.
- **Housekeeping**: configurable retention.
- **UI**: job manager for long operations, table accessibility, selectable fonts
  and TMDB calendar at the top of Explore.
- **FlareSolverr**: expired and orphan sessions destroyed.
- **Mobile** more essential.

## 29 September 2026

- **New languages**: German, French, Spanish and Polish.
- **UI**: performance tiles on mobile, history pagination, comics history,
  archive actions, tooltips and diagnostics.
- **Installer**: self-test, safe updates, rollback, uninstall and dry-run.
- **Indexers, FlareSolverr and RAM disk** improved.
- **MediaInfo backfill** more reliable.
- **Log**: connection test results at INFO, fewer duplicates and technical
  details.
- **Concurrency, deterministic shutdown and data integrity**.
- **Sonarr/Radarr replacement plan** documented.

## 28 September 2026

- **Complete migration to the server-rendered UI**: all 16 pages, forms, tables
  and actions.
- **Mobile-first**: phone layout, touch targets, fonts and pastel theme.
- **Library**: series/movie forms with quality, languages, subtitles, aliases
  and NAS path; detail with cast, next episode and seasons.
- **Archive and Explore**: TMDB search, calendar, episode sources, “in library”
  badge.
- **Managed qBittorrent** with watchdog and fallback.
- **TUI**: archive/missing/blocklist tabs, key editing and improved rendering.
- **Shutdown** of workers before destroying the session.
- **Cloudflare/FlareSolverr** more robust.

## 27 September 2026

- **First public Gextto release.**
- **New Go server-rendered UI**: migration of the 16 pages, i18n editor, library,
  archive, blocklist, maintenance and integrations.
- **Torrent engines**: selectable qBittorrent, Anacrolix backend, capability
  matrix and connectivity checks.
- **Security**: CodeQL fixes, contained paths, integer conversion and secret
  handling.
- **Bilingual TUI** Italian/English.
- **Logs and tooltips** clearer.
- **Dependencies** updated and UI bundle rebuilt.
