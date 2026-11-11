# Gextto changelog

This is what changes in Gextto from the point of view of the people who use it,
in date order. Entries are grouped by day and by visible effect: commits that
only build, test, document or maintain the project internally do not get a
separate line, but are represented in the entry of the change they accompany.
Technical detail stays in the commits and in `docs/`.

The installed version is the one shown by Gextto (`<series>.<build>`, for
example `1.2.2066`): the third number is always the build and the second is
raised by hand when a new series is started. The updated
manual is online: [English](docs/MANUAL.en.md) ·
[italiano](docs/MANUAL.it.md). For reports:
[GitHub issues](https://github.com/buzzqw/gextto/issues).

**Gextto and gx-torrent are two programs, with the same torrent engine.**
Gextto is the daemon that downloads, renames and archives, with its own
interface. gx-torrent is the client that also runs on its own, the **standalone
version**, with a page, settings and a qBittorrent-compatible API of its own:
the entries that describe the latter — feeds and rules, Series & movies,
categories, quality profiles, after-download actions, indexer search, add and
create, the app on the phone — apply **only to the standalone version** and do
not change Gextto's interface, where those pages do not exist. Engine fixes, on
the other hand, apply to both.

## 11 November 2026
- **«Rename folder content» gives movies their own template.** For a movie it always proposed a bare «Title (Year)», whatever format was chosen, dropping the source, resolution, codec, HDR and languages the file name carried: it now uses the movie format, as the import already does and as it did for episodes. If you gave the episode template the quality tags, check *Custom template for movies* too: the default keeps only the resolution.
- **The automatic search is lighter and recognises titles with an accented capital.** Comparing every release with the films you follow (name, original title, aliases and translations) took a third of a search's CPU and almost four fifths of the memory it used: the same comparison now costs twelve times less. In the same check, a title, alias or translation starting with an accented capital («È…», «À…», «É…») was never recognised in a release that spells it without the accent: now it is, for films and series alike. Nothing to configure.

## 10 November 2026
- **A download added by hand from a free search stays in the transfer folder.** What you pick from the search of the Dashboard, Download or Explore, or from a pasted eD2k link, is no longer moved and renamed into the series or films folder on the NAS: it stays in the transfer folder under its own name, is shared within the seed limits and does not count as an episode or film you already have. **Download** on the results of a title you follow, from its page (a missing episode, for instance), still brings it into the library next to the others. Manual downloads already running end as before. Nothing to configure.
- **The library check no longer replaces a good episode with a worse copy.** When a series folder held a file Gextto did not know — copied by hand or left by an old download — of the same episode at a lower quality, the rename could give it the name of the episode already in the library and send the good one to the trash. It happened with the default file-name format, which does not write the quality in the name. Gextto now compares the unknown copy with the recorded quality of the episode and, when it is not better, discards it. If it happened to you, the good episode is in Gextto's trash. Nothing to configure.
- **After a restart the log says it all in one line.** The restart line also says when the next automatic search runs (`next search at 21:40 (in 34 minutes)`), instead of adding it in a second line right below. Nothing to configure.
- **A torrent's details in Gextto have a Speed tab and the piece availability.** The **Speed** tab shows that torrent's download and upload chart over the last 30 minutes, with average and peak: it is already filled when you open it, because Gextto keeps the history of the torrents that are transferring. With gx-torrent the **Pieces** tab has a bar under the map saying how many connected peers have the pieces you are missing, red where nobody has them: you see at once whether a stuck download is waiting for a source that is not there. With qBittorrent the piece map now shows too. In the head of *Session downloads*, ↓ and ↑ say how much the engine downloaded and uploaded since it started. Nothing to configure.
- **Gextto's Downloads view filters by state.** Above the torrent list there are buttons with the count of each state — all, downloading, seeding, paused, completed, waiting for seeds, error — and a click shows only those; the choice stays while the list refreshes. States without torrents are not shown. A finished torrent counts as completed even when the engine reports it paused after the seed limit. Nothing to configure.
- **The gx-torrent page tells more about each torrent.** In the General tab two thin bars show where the downloaded pieces are and, while downloading, how many connected peers have the pieces still missing: red marks pieces nobody can give right now. Two new tabs: **Speed**, with the torrent's download and upload graph over the last minutes, and **Log**, with what happened to it (added, completed, stopped, errors, a tracker that stops or starts answering again, rejected pieces and who sent them, moves, rechecks). In the list an icon before the name shows the state at a glance, and the status bar shows how much you downloaded and uploaded since start. When adding a `.torrent` you can set each file's priority, and with **Add dropped files straight away** the next files dropped on the page go in without the dialog. It applies to the gx-torrent page (also the one of Gextto's engine); nothing to configure.
- **A piece check no longer takes the whole disk and memory.** When a torrent is checked again (by hand, after a move or an abrupt restart), the engine reads at low priority, pauses briefly when it notices someone else using the disk or the NAS, and does not fill the system's memory with the files it has just checked: before, checking a large collection pushed everything else out of the cache and the machine stayed slow afterwards. On the NAS the check is as fast as before when nothing else uses it. It applies to Gextto and to gx-torrent; nothing to configure.
- **With a speed limit the ping stays lower.** When a limit is set, by you or by automatic speed, the engine no longer sends a whole second's quota at once but small slices of a few hundredths of a second: on a slow line that burst filled the modem's queue by itself and raised the ping of everything else, games and video calls included. Each adjustment made by automatic speed no longer hands out a fresh burst either. The average speed is still the one set. It applies to Gextto and to gx-torrent; nothing to configure.
- **The engine is more correct with peers and with malformed torrents.** Three fixes that apply to Gextto and to gx-torrent. When the engine stops sending to a peer (choke), it now tells it which requests it will not serve, as the protocol asks, and keeps sending the pieces it had granted anyway: before, it dropped them in silence and the other side waited for them until its own timeout. A peer that goes quiet with pieces still in flight is noticed even when another piece from it had just arrived, and those pieces move to other peers instead of staying stuck. A torrent with an empty file name no longer puts that file in place of the torrent's folder, and one where a file has the same name as a folder is refused as soon as it is added, with a clear message, instead of failing half way through the download. Nothing to configure.
- **LimeTorrents returns results.** The site moved its search page and its download links to a new address, and the old one bounced back and forth between http and https: for days, at every cycle, the web search reported it as unreachable. Gextto now tries the addresses of the site one after the other — `limetorrent.store`, `www.limetorrents.fun` and `limetorrent.net` — and stops at the first that gives results, so an address that changes or stops answering no longer ends the search; the results come ordered by seeders as before. Nothing to configure.
- **The log no longer fills with the same line.** When the engine failed the same way hundreds of times in a second (a file the disk refused, for instance), the log received hundreds of identical lines and what mattered disappeared. Now the first line is shown, the identical copies are counted, and every half hour a summary says how many there were; after a minute the line can appear again, so a problem that goes on is never hidden. It applies to Gextto and to the gx-torrent log.
- **If gx-torrent dies while Gextto is replacing it, that is now recorded.** During an update or a restart the engine can exit with an error, and Gextto's log said nothing because the exit was expected: the detail ended up only in the engine's crash file. A line now says where that detail is. Nothing to configure.
- **BT4G returns results again.** The site changed its results page (rows drawn by the browser and links that no longer carry the infohash), so the search found no release at all. Gextto now reads the same search in the feed format the site offers next to the page: every result comes with its magnet, without opening a page per result, and the results are ordered as the site gives them. Nothing to configure.
- **BitSearch leaves the list of web engines.** The service has answered with an error to every query (HTTP 500) for days, so the engine could no longer find anything and only cost time in every search. It is no longer offered in *Configuration → Sources*; anyone who had it ticked needs to do nothing: the saved entry is dropped on its own and no longer appears among the active engines either. Nothing to configure.
- **Torrentz2 leaves the list of web engines.** The domain it relied on no longer exists and every address of the service ends on a parking page: the engine could not return a result any more, but it stayed in *Configuration → Sources* as an option promising something that is not there. Anyone who had it ticked needs to do nothing: the saved entry is dropped on its own and no longer appears among the active engines either. Nothing to configure.
- **A TV series can be renamed.** Until now the name was chosen only when the series was added: correcting it meant removing the series and adding it again, losing the link with the archived episodes and their history. The series page now has a **Name** field in **Edit series**: changing it keeps episodes, the missing-episode search, exclusions, blocks and download history linked to the series. The archive folder is not moved: the files stay where they are and new episodes keep arriving in the same folder. Nothing to configure.

## 9 November 2026
- **The `.torrent` files of removed downloads no longer stay on disk.** Gextto kept an internal copy of every download's `.torrent` file and never deleted it when the torrent left the session: the state folder grew with every download, even after archiving. The internal copy is now removed when a torrent is removed or archived; the folder chosen in **Copy .torrent files to** is left untouched. Nothing to configure.
- **The `.torrent` files downloaded from indexers no longer pile up either.** For listings that expose only a `.torrent` link, Gextto downloaded the file into a staging folder and never deleted it. It now removes it as soon as the candidate has been judged or added; the routine cleanup also removes any leftovers older than a week. The folder chosen in **Copy .torrent files to** is left untouched. Nothing to configure.

## 8 November 2026
- **«Search missing»: several results are downloaded one after the other without losing the list.** Before, pressing «Download» on the first result reloaded the page and the results list vanished: anyone wanting several episodes had to run the search again every time. Now the download adds the release and shows a notice at the top, while the results stay where they are; this also applies to the manual search in Explore and to the search of a single episode. Nothing to configure.

## 7 November 2026
- **Translation corrections reach people who already have Gextto installed.** Until today a wrong translation, corrected in the interface, stayed wrong for anyone who had already started Gextto at least once: the program never touched the entries it had already stored. Now the stored entries follow the updated texts: after a restart you see the corrections, the new translations appear and those of texts that no longer exist are gone, in every language. The chosen language and the settings do not change; nothing to configure.
- **gx-torrent uses less RAM while downloading.** So that the data never stops flowing, each peer keeps two pieces open, but the second one receives nothing until the first is complete: a piece now takes its memory when its first block arrives, not when it opens. Receiving over uTP no longer creates throw-away memory for every packet either, so there are fewer memory clean-ups during a download. In the client comparison, with four peers on a 100 Mbit/s line the peak falls from 41 to 32 MiB over TCP and from 48 to 33 MiB over uTP, and at full speed from 27 to 24 MiB, at the same speed and CPU. It applies to Gextto and to standalone gx-torrent. Nothing to configure.

## 6 November 2026
- **Gextto gives memory back when it is done.** After a search, a library scan or a completed download the program hands the system the memory it no longer needs, instead of keeping it until a restart: the RAM figure in the top bar goes down accordingly. The search also uses less memory while it runs, so its peak is lower. gx-torrent does the same while it is only seeding: it no longer reserves a 32 MiB read cache before there is a seed to serve, but starts at 8 MiB and grows with the seeds. Right after a start and during an upload it uses less RAM: in the client comparison the upload peak falls from 58 to 22 MiB at the same speed. Nothing to configure.
- **The search does less work on the archive.** Releases already stored are no longer rewritten on every cycle, and every missing episode is still checked on every search, with far less work: on a large library the cycle uses much less CPU with the same result. The check on releases already stored looks only at the ones just read, not at the whole archive: with tens of thousands of releases archived, a search and a cycle cost what they cost on a small archive. Nothing to configure.
- **Reading the quality, language and episode of a name costs far less.** The parser no longer runs dozens of regular expressions for every name: searches and library renames use less CPU, and on a small PC the difference shows in cycles and scans. Nothing to configure.
- **In Salute → Stato provider the «Dettaglio tecnico» heading wraps onto two lines.** The heading of the column with the raw error breaks between its two words instead of staying on one line: the column is narrower and the table fits the panel without scrolling sideways, while the error text still reads on two lines and stays whole in the tooltip. Nothing to configure.
- **The automatic speed control writes at most one line every 15 minutes.** On a busy line it wrote «starting with…» every few minutes; now the line says what it is doing («limiting the speed to keep the line responsive») and the steps in between go into the 15-minute summary, where «no limit» always reads the same way. Nothing to configure.
- **Restart and shutdown read in words.** The stop line no longer writes «(SIGTERM/SIGINT)» and there is no separate line with the *old* gx-torrent version before the divider any more: the start line says that gx-torrent was restarted with its new program. The work still running is called by its name («media integrity check»), not the internal one, and the wait for a long copy repeats every minute instead of every 15 seconds. The «file(s)» are gone too. Nothing to configure.
- **Files gone from the library are reported once per series.** When several episodes of a series are no longer on disk there is one line per series, saying how many are missing, instead of one per episode in the same second. Nothing to configure.
- **The line for a damaged file says what is wrong, and the technical details stay at the end.** The sentence says the file cannot be played through without errors; the decoder's technical messages stay as a detail at the end of the line. Nothing to configure.
- **The integrity check reports every 50 files.** The periodic report comes every 50 checked files, not at every half-hour round with the same numbers; a stop before any file was checked no longer writes «stopped». Nothing to configure.
- **The search summary no longer lists filled missing episodes as a kind of download.** The missing episodes found are counted among the started downloads («2 started (1 new, 1 upgrade; 1 of them fills a missing episode)»), and an unknown quality no longer turns into «unknown unknown ITA». Nothing to configure.
- **Rename and archive say what happened.** The lines «rename skipped: archive import in progress» and those with «0 discarded, 0 errors» became sentences that name only what happened and when it will be retried; two links of the same release resolved in the same search no longer raise a warning with the database error, because the extra copy is removed quietly. Nothing to configure.

## 5 November 2026
- **Gextto has a new icon.** The browser tab, the shortcut on your phone and the documentation site show the new GX logo instead of the old icon. Nothing to configure; if the browser still shows the old icon, reload the page or reinstall the app from the browser menu.
- **The three native gx-torrent options are also in Gextto's Configuration.** Under **Configuration → Torrent engine → gx-torrent** you now find **recheck data when a download completes**, **anonymous mode** and **announce to all trackers in a tier**, the same options as the standalone page. They apply to the managed daemon at the next start of Gextto: gx-torrent is restarted then (never during a move) and no downloaded data is touched. With the recheck on, the download counts as completed, and Gextto starts archiving or renaming it, only after the check.
- **gx-torrent: the files to download are chosen before adding them.** When you pick a `.torrent` with the add dialog, it lists the files it contains with a checkbox each: what you leave unchecked is not downloaded, and a selection with nothing checked is refused. A magnet has no file list until its metadata arrives; add it paused at metadata and choose from the *Files* tab. Nothing to configure.
- **gx-torrent: the creator writes v2 and hybrid torrents.** **Tools → Create .torrent** now offers **v1** (read by every client), **v2** (BEP 52) and **hybrid** (the same files in both formats, for the two swarms), plus a **source** string. **Start seeding after creating** adds the new torrent and seeds the folder or file at once instead of downloading the `.torrent`. The formats and the source string are also in the native API and in the qBittorrent-compatible creator.
- **gx-torrent: anonymous mode, the check at completion and all trackers of a tier.** Three qBittorrent options now exist: **Anonymous mode** (Network) removes the client version from the peer id and the handshake and sends no User-Agent to public trackers, without hiding your IP address; **Recheck data when a download completes** (Queue) verifies the files once before seeding them; **Announce to all trackers in a tier** (Network, on by default) asks every tracker of a multi-tracker tier together and merges the peers.
- **gx-torrent: a new language is a file, not a rebuild.** Besides the six built-in languages, the page loads JSON language packs from `<data>/lang`: a pack gives its code, its name and the translated strings, and any string it does not carry stays in English, so a partial pack is usable. A malformed pack is reported in the log and skipped. Nothing else to do.
- **The releases of a feed removed from Sources are no longer retried.** Every archive row now remembers which feed it came from, even after that feed is deleted (or commented out with `#`) from the list: its `.torrent` links are no longer opened, so they no longer end up in quarantine and no longer report the error of a feed that is gone. The releases stay in the catalogue and can still be opened by hand; putting the feed back makes them usable again on their own. Rows archived before this version behave as before. Nothing to configure.
- **A movie is recognised by its whole title, not by a word it shares with another film.** The alternative titles TMDB knows include one-word character names: that single word, wherever it appeared in a release name, was enough for a release of a different film to look like the right copy and to be downloaded in its place. A release name must now **open** with the film title (or one of its alternative titles), compared word by word the way the series match already does and the way Sonarr and Radarr do: the tag an indexer puts before the title (a language, a resolution, a group in square brackets) is skipped, the year keeps its usual tolerance, and at the same length the film's own name wins over an alternative title. The film page's **archive matches** now lists only the releases it would really take. Nothing to configure.
- **The gx-torrent page now translates sentences that carry a value, and no longer translates data.** The page used to be translated after it was assembled: a sentence with a number or a name inside stayed in English ("3 torrent: added"), and a name that happened to match a label was translated by mistake (a category called "All" became "Alle", a title called "Dateien" changed under your eyes). Now every phrase is translated while the page is assembled, with the values where the language wants them, and torrent names, categories, tags, paths and titles stay exactly as you wrote them. The outcome messages of the actions (add, remove, IP filter and trackers, and so on) are translated too, as are the labels of feeds, rules, indexers, Series & movies, quality profiles and settings that used to stay in English. Nothing to configure.
- **A pack that has just been chosen no longer sits still copying empty files.** When the pack inspection decides to download only some episodes, Gextto moves the files it does not need into a folder of its own. If the download folder is a network share (NAS) and that folder is on a local disk, the files that were never downloaded are now dropped and recreated where they belong: before, they were copied in full, tens of GB of zeros read from the network and written to the disk, and the download waited for the copy to end. A 100 GB pack with two useful episodes started after about twenty minutes and some sixty GB of useless traffic; now it starts at once. Nothing to configure.
- **The archive no longer retries releases from a switched-off indexer.** When you disable an indexer (Jackett, say) the releases already collected stay in the archive catalogue, but Gextto no longer opens their `.torrent` links: before, it tried one now and then and reported the error of a service you had turned off. Switching the indexer back on makes them usable again. Nothing to configure.
- **A release archived twice no longer blocks the saving.** When the same release was in the catalogue once with its `.torrent` link and the infohash the indexer declared, and once as a bare link from another source, Gextto could not store the resolved torrent and tried again at every search, downloading the same file from the indexer each time. Now the extra copy is removed and the release settles once.
- **The audio cleanup leaves alone the files it cannot clean.** For some files the cleaned result is not identical to the original: the check notices and the file stays as it is. Before, it was tried again every week, forever, and appeared among the files left alone every night; now it is remembered and tried again only if the file changes (size or date). A file that fails for a passing reason — a tool interrupted, a disk that does not answer — is still tried again after a week.
- **Movies have a name template of their own.** With the **Personalized** format the template applied to episodes only: movies always became `Title (Year) [resolution]`. Now **Configuration → Renaming → Name composition** has a second field, *Custom template for movies*, with its own buttons (Title, Year, Source, Group, Resolution, video and audio codec, channels, HDR, languages) and its own preview. The default is the name movies already had, so the library does not change until you edit the template. It applies to the movies renamed from then on.
- **Gextto on a phone: more room for the content.** The top bar keeps only the title, **＋** and **Refresh**: theme, text size, **Font**, language and **Donate** are in the **More** menu (before, they took a whole row on every page). The **Donate** button no longer floats over the rows of the page. In the **TV series** and **Movies** lists every title is a compact card: name and status on one line, **Pause** and **Delete** side by side below, instead of three lines per title. On the **Dashboard**, when nothing was transferred in the last 30 minutes, one line says so instead of an empty chart. Nothing to configure.
- **gx-torrent: a tidier page on a phone and on 1366-pixel screens.** On a phone the sections (Torrents, Series & movies, RSS…) have a row of their own under the brand instead of being cut mid-word at the edges, and the ❤ sits in the top bar instead of covering the fields. On 1366-pixel laptops the table no longer runs past the edge: the *Availability*, *Time active*, *Last activity* and *Wasted* columns are hidden by default and can be turned on from **Columns** (whoever already chose their columns sees no change). At the first start of the standalone version the `/ui` address also leads to the initial setup, like every other page.
- **Two interface fixes.** On the dashboard, the **Warnings** table wraps long release names instead of growing wider: the **Actions** column stays in view without scrolling sideways. On the gx-torrent page, the **Add** button in the middle of the empty list showed a huge grey "+" over its label: now it has its normal icon.
- **The download list is no longer read halfway through an update.** With gx-torrent or qBittorrent, Gextto's background work and the interface could read the torrent list at the same moment it was being updated: in rare cases a check could see an incomplete or damaged list. Now the read waits for the update to finish.
- **"Restore the source in the names" no longer overwrites a library file.** If the folder already held a file with the name to restore, the rename replaced it without a warning and one of the two files was lost. Now that episode is skipped, counts as an error in the summary, and the log says which file already exists.
- **The night audio clean-up can no longer hang on a NAS file.** Reading the tracks with mkvmerge had no time limit: if the share stopped answering, the maintenance stayed stuck there. Now after two minutes that file is left as it is and the next one follows.
- **Long names shortened without breaking accented letters.** In the log and the interface, download names, indexer messages and overly long addresses were cut at a fixed number of bytes, and an accented letter could be left split in half (an unreadable character). Now the cut always falls between two whole characters; the same goes for a very long text pasted in the TUI.
- **gx-torrent: adding torrents through the qBittorrent-compatible API has a size limit.** An add request of any size was written in full to temporary files on disk before being read. Now, like the other uploads, a request over 129 MiB (four `.torrent` files of the largest accepted size) is refused with error 413. Programs that add ordinary torrents see no difference.
- **A comics page with non-UTF-8 characters no longer stops the reading.** If a page of the comics source held, inside a script or a style, bytes in an encoding other than UTF-8 (for example a Latin-1 page with accented letters), Gextto could miscompute where the script ended and, when the script was at the bottom of the page, stop with an internal error instead of reading the articles. Now the page is read correctly, and faster on pages with many scripts.
- **API keys no longer stay in the clear in error messages.** When an error carried an address with `api_key=` followed, later in the same message, by another with `apikey=`, only the second was hidden and the first key reached the log as it was. Now both are hidden, also when the message holds non-UTF-8 characters.
- **Less background work on large archives.** The periodic clean-up of temporary files compared every series folder with every other one, reading its real path on disk again each time (on a NAS, one request per folder of the path): with dozens of series that was thousands of accesses per pass. Now each folder is read once, and a series folder inside the archive is no longer walked twice. The seed-limit check also asks the database fewer questions per torrent, and checks no disks at all when the automatic removal of completed torrents is off. Nothing to configure.
- **The movie list can no longer be emptied by an interrupted read.** If, at start-up or when the configuration was read again, the settings database was busy or damaged halfway through reading the movies, Gextto kept the part read so far; the next library save then rewrote the list from that part, and the missing movies disappeared from the monitored ones. Now an incomplete read stops with a clear error and leaves the library alone: restart once the database is readable again.
- **Lighter feed rules, and faster clean-up of the "seen in feeds" entries.** Rules with a regular expression no longer compile it again for every release of every feed on every check: the same rule takes about half the time and a quarter of the memory. In **Maintenance**, deleting a selection of seen-in-feed entries writes everything at once instead of one entry at a time, so even a thousand entries go in a moment. Nothing to configure.
- **A film is searched in the chosen language, in English and in the original
  language.** After **Aggiorna TMDB** a film without an Italian title can take
  its original name — Chinese, for instance — and before this fix the indexer
  searches used only that name. The automatic cycle and **Search** now try the
  monitored name, the original title (the film's original language), the title
  in the language chosen for the film and the English one. The other titles
  TMDB lists are not queried one by one, because they would multiply the
  requests, but they still count for recognising releases and for the archive
  search. If a language has no title on TMDB the English one is used anyway.
  Nothing to configure: the titles are read once and stay saved with the film;
  if you do not like a title you can change **Name** on the page.
- **Larger season packs are no longer refused for their piece count.** A 4K
  pack of a few hundred gigabytes can carry hundreds of thousands of pieces:
  Gextto accepted at most 65,536 and discarded the torrent with «too many
  pieces», even when the metadata was intact (it had been checked against the
  info hash). The ceiling is now much higher, and the limits that remain are
  the real ones: the size of the `.torrent` file and of the metadata. A torrent
  discarded this way is not in the session: the one chosen by the automatic
  cycle comes back on the next round, one added by hand has to be added again.
- **A movie's alternative titles are visible on its page.** Yesterday's entry
  said those titles stay saved with the film, but the page showed them
  nowhere. Under the plot, the **Alternative titles (TMDB)** line opens the
  list of names the film was released under abroad: they are the ones Gextto
  uses to recognise releases and to search the archive. A film added by hand,
  whose TMDB entry was never read, shows **not read yet**: **Aggiorna TMDB**
  now reads the id, the metadata and the titles in one go — before, on those
  films it answered «invalid id» and did nothing. Nothing to configure.
- **Autotune and speed lines in the log only while something downloads.**
  The Autotune and speed-limit lines appeared even when nothing was
  downloading — with the session idle, with only seeding torrents, or after a
  restart — and they talked about a speed nobody was using. They are now
  written only while at least one torrent is receiving data; when a download
  starts again, the first line says at what speed. Nothing to configure.
- **After an update the interface shows the new version at once.** The browser
  was told to keep the stylesheets and scripts for a year, under an address
  that changed only with the build number: an update that touched only the look
  of a page could leave the old file on the phone or on the computer, and the
  torrent titles went on appearing cut. The address of those files now changes
  with their content too, so a reload is enough to see the new version. Nothing
  to configure: from the next update it takes care of itself.

## 4 November 2026
- **On a phone a torrent title is readable in full.** In the Downloads page a
  long name was cut on the first line, with an ellipsis and no way to see the
  rest — on a phone the mouse tooltip that shows it does not exist. The name now
  wraps and reads in full; the rest of the card (progress, speeds, actions)
  stays where it was. Nothing to configure.
- **A film is also recognised by the title it was released under abroad.** The alternative titles TMDB knows for a movie — the international one and those used in other countries — now help recognise releases: a film you follow under its Italian title arrives even when the release carries another name, both in the automatic cycle and with **Search now**, and the page's archive matches are searched with all those titles. Working titles are excluded, because they sometimes belong to another film. Alternative titles are read once and kept with the film. Nothing to configure.
- **Archive entries holding only the `.torrent` link are no longer lost.** Some sources show an entry with no magnet, only the link to the `.torrent` file, and a title that often says neither resolution nor language: the entry was dropped. Gextto now opens the link once, reads the torrent's real name and stores it in place of the incomplete title, so the entry can be judged and is no longer opened in later cycles (at most twenty links per cycle). Nothing to configure.
- **A film is no longer mistaken for another because of a number.** The alternative titles Gextto receives from TMDB can be written in other scripts — Korean, Hebrew, Cyrillic — and only the trailing number of those titles survived: that alone was enough to recognise a release, so a 2026 film with a «3» in its name was taken for another film of the same year and its release was downloaded in place of the right one. Every word of an alternative title now counts in its own script, and a title made of digits alone cannot identify a film. A download already started by mistake has to be removed from Downloads: the film goes back in the queue by itself at the next cycle. Nothing to configure.
- **The RSS page interval now says what it really does.** The “Refresh interval” entry talked only about feeds, but it is the period of the whole cycle: it also decides how often the titles in **Wanted** are searched on the indexers, and it applies even with no feed configured — with only indexarr, for instance. It is now called **Check interval (minutes)** and the description says so. Nothing to configure: the saved value stays the same.
- **The torrent detail always has its magnet.** On a torrent added from a `.torrent` file, or restored after a migration, the **Magnet** row stayed empty and **Copy magnet** was disabled: the link was shown only when the torrent had arrived from a magnet or a feed. It is now rebuilt from the torrent's info hash, name and trackers, so the button is enabled for every torrent in the session. The **Magnet** button next to it opens the link as plain text, no longer as a JSON response. Nothing to configure.
- **Adding a tracker to a torrent that has none works again.** On a torrent born without any tracker — a `.torrent` with no announce URL, or one whose trackers had all been removed — the command that adds a tracker answered with a read error and did nothing: the list stayed empty. The missing list is now read as empty and the first tracker is added normally, even right after clearing the list. Nothing to configure.
- **Saving a torrent detail tab now confirms it.** Pressing **Save limits**, or saving trackers, tags, web seeds, file priorities and the storage move, rebuilt the tab with the new values without saying anything: it looked as if the click had done nothing. A notice now appears at the top saying what was saved (“Torrent limits saved.”, “Trackers updated.” and so on); if something fails the notice is red, gives the reason and the tab stays as it was, with the values you just typed. **Announce**, **Restart** and **Mark as failed** now leave the same notice too, where before they acted in silence. Nothing to configure.
- **The movie list says which versions are already in the library.** In the **Monitored** tab of the Movies page, next to the *active* or *paused* status, a **✓** appears for every movie whose version has already been downloaded: hovering over the mark shows the path of the library file, or the archived title when the move was not recorded, so there is no need to open the detail to know whether the movie has arrived. The mark also appears in the **Posters** view. Nothing to configure.
- **A film is no longer taken by the wrong entry when its title contains another.** When one entry in the monitored-movies list has its name contained in another, a release of the longer-titled film could land on the shorter entry just because it came first in the list: the longer entry never received anything, stayed waiting forever, and the release was judged with the other entry's quality settings, so it could be refused even when it would have been accepted. Now, at the same year, the most specific title wins whatever the order; the shorter entry still takes the releases that contain only its name. A colon changes nothing: a release is recognised the same way with or without it. Nothing to configure.

## 3 November 2026
- **A pack added by hand is no longer discarded.** With *Pack inspection* on, Gextto also took out of the session a season pack you had started yourself, as soon as it saw that all its episodes were already in the library: you added it again and a few minutes later it was gone. The check now applies only to packs chosen by the automatic cycle; what you download by hand — from *Add torrent*, a search, the Archive — keeps downloading. Nothing to configure.
- **The manual has been rewritten as a guide to using Gextto.** Instead of describing one page after another, it now follows what you want to do: install, make the first download in test mode and then for real, follow a series or a movie, see where the files end up, set up sources and quality, keep the library in order, make a backup and solve problems starting from the symptom. The full list of settings stays in the appendices. It opens from the **Manual** entry, as before.
- **«Automatic cycle» is an Active / Not active choice again.** On some installations, in *Configuration → General*, the «Automatic cycle (search and download)» setting showed up as a «one value per line» text box instead of the *Active* / *Not active* choice: an old version had saved the value as a one-item list. The cycle worked anyway; the page now shows the right choice again, and the same goes for any other yes/no setting saved that way. Nothing to configure: the next save puts the value back in its normal form.
- **A movie's notification no longer talks about episodes, and the reason is written in plain words.** When a movie download started, the reason line said “🆕 New episode approved”: it now says “🆕 New movie approved”, and the same for “Missing movie found”, “Last resort: movie outside the profile” and “Movie approved”. In *Downloads* the “Because:” line showed the internal code (“approved”, “gap_filled”, “manual”): it now shows the reason in plain words, translated, for episodes and for movies. Nothing to configure.
- **Auto leaves room for a call or a game.** With the speed on Auto, the latency guard reacts to a queue, and a call or a game builds almost none: it uses little bandwidth, so the ping stays good while Gextto is already taking the line. When the router's WAN counters answer (the **both** and **router** modes), Gextto now stops taking more room while another device is active — the caps stay where they are, they do not drop — and resumes growing when it is gone. The **ping** mode still reads no router, by design. Nothing to configure.
- **uTP sends a whole write in one call.** In the engine Gextto and gx-torrent share, every packet of a uTP write went out with its own system call: on a fast local network that alone is a large part of the CPU (half of it on our loopback measurement), and it is what kept a gigabit or a 2.5 Gbit link from being saturated by one connection. A write now travels as one segmented datagram burst — the kernel splits it, Linux 4.18 and later — with the same bytes and the same order; elsewhere, or for an address the kernel will not segment, the packets go out one by one as before. System CPU for the same 730 MB dropped from 5.9 to 3.7 seconds. Nothing to configure.
- **File priorities follow their order.** The priority scale (Skip, Normal, High, Maximum) was read in two bands: 5, 6 and 7 were all "high" and 1 to 4 were all "normal", so inside a band the order was whichever piece the swarm made rarest. Now each level is fetched before the lower ones — Maximum before High, and every level before the normal files — and at equal rarity the higher priority decides. A torrent whose files carry mixed levels downloads them in that order; nothing to configure.
- **A single web seed now downloads several files at once.** An HTTP source was fetched one range at a time, and within a range the files one after the other: a host that takes a couple of seconds to answer each file — an archive.org item, for instance — set the speed of the whole download. The engine now opens up to four ranges on the same source, so four files travel together and an idle range takes half of a busy one. On our local measurement, with a 200 ms wait per request and eight files, the same download went from 3.2 to 0.8 seconds. The interface still shows one entry per web seed, with the speed of all its ranges summed. Nothing to configure.
- **A release nobody is sharing comes back less and less often.** When a release is left with an empty swarm — abandoned during the download or refused before starting — Gextto blocks it for the configured period and then tries it again, because a swarm can revive. A release that comes back after that period and is still empty now stays away four times as long, and twelve times as long the third time, up to three months; the count is forgotten after six months and a download that succeeds starts it over. A swarm that revives is still seen again, and the log says for how long the release is out. *Settings* keeps the first period, seven days by default. Nothing else to configure.
- **The engine finds more peers in one reply.** A `get_peers` answer carried a single peer instead of the eight it is allowed, and a scrape request answered with the swarm's size filters *instead of* the peers, while the protocol asks for both in the same reply. Peer discovery through the DHT is now faster on both counts, and the engine also drops a dead contact wherever it sits in the list instead of only the first one. Nothing to configure.
- **Reading a block for an upload reuses memory instead of allocating it.** The read cache took a fresh buffer from the system for every block read from disk and cleared it before use; the engine now recycles the buffer of a block the cache has dropped, and never while an upload is still copying out of it. On our measurement machine a block read that misses the cache went from 3.9 to 0.75 microseconds and from 16.5 KB allocated to 192 bytes: less CPU and less work for the collector while seeding, with the same memory footprint. Nothing to configure.
- **Every pattern of a release name is tried, not only the first one that matches.** A title can look like a season and an episode in more than one way, and some patterns match names they cannot read: a season zero, a date that does not exist, a "pack in progress" with no usable count. Gextto stopped at the first one and filed the release as a film; it now tries the next pattern, so a series title is never read as a film only because the first pattern it matched was the wrong one. Any title that stopped that way was a film already — on the 342,587 titles this installation has seen, no title changed kind — so nothing in the library moves. Nothing to configure.
- **The translation editor is gone, and the section is now called “Diagnostics and updates”.** *Configuration → Diagnostics and translations* had a panel to edit, import, export and delete the interface translations one by one. Hardly anyone used it, and a change made there stayed on your installation only, never reaching the people who fix the texts. The panel has been removed, together with its API commands; the section now holds only the debug switch and the update check, and is renamed to match. The language is chosen as always from the selector at the top, and the texts ship translated into six languages with every Gextto version. If a text looks badly translated, report it in a GitHub issue. Nothing to configure.
- **A torrent seeded from a NAS no longer stops on a passing read error.** In the engine Gextto and gx-torrent share, a read that failed with an I/O error stopped the torrent as if the file were damaged. On a NAS, an SMB share or a FUSE mount that error is often a hiccup of the network, and the same file reads fine when it is opened again. The engine now reopens the file and retries for about a second; only when the error persists does the torrent stop, as before. Nothing to configure.
- **A peer that reconnects after a dropped connection is accepted at once.** When a peer lost its connection without closing it (a router that resets connections, a laptop going to sleep), the engine kept its slot busy for two more minutes and refused the new connection from the same address. Now, when the old connection has received nothing for at least a minute, the new one replaces it. Nothing to configure.
- **A torrent with huge pieces is refused instead of taking gigabytes of memory.** A crafted .torrent file or magnet could declare pieces of several gigabytes, and the engine reserved memory for each. A torrent with pieces above 256 MiB is now refused when it is added, with a clear message; real torrents, even multi-terabyte ones with 128 MiB pieces, are added as before. In the same pass the DHT refuses signed values that lack their key or sequence number, instead of keeping them as plain values.
- **A REPACK already in the library is no longer downloaded again.** When a REPACK replaced an episode, the name Gextto gives the library file does not include the word “Repack”: if the episode record had lost track of the downloaded release (after an archive scan, for instance), the next search took that same REPACK for an upgrade of the copy just imported, downloaded it again, and the second copy stayed shared in aMule's folder without being archived. The import now writes on the episode record which release reached the library, and the search treats a release already imported, whose file is still in the library, as a duplicate; if you deleted the file yourself, the release can be downloaded again. Nothing to configure.
- **A release from a season you do not follow no longer hides the series archive.** During a search Gextto reads once per cycle which episodes of a series are already in the archive, so it does not download again what is already there in the same or a better quality. When the first candidate it looked at belonged to a season the series does not follow (ignored, or outside the season list), Gextto kept an empty archive for the rest of the cycle, and the candidates of the followed seasons were judged as if nothing were on disk. Now that candidate leaves no trace and the archive is read for the right seasons. Nothing to configure.
- **Pages are translated while they are built, and more text reaches your language.** Until now the interface was written in Italian and then translated again as a whole on every page and every automatic refresh; sentences with a value inside — "Piece map: 120 of 400 downloaded", "Open … on TMDB", "Delete all 37 entries…?", "Turn season 2 on or off" — stayed Italian, and so did the **Open** button of the section picker without JavaScript, the "No limit" of the bandwidth menu and the tooltip of the automatic refresh button. Now every text is translated as the page is built, values included, and the 25 new sentences are in every language. Nothing to configure.
- **Notices are translated even when they carry a value.** The messages the interface builds with a number or a detail — “Cleanup completed: 12 files moved to the trash”, “The engine did not answer: …”, “History unavailable: …”, “VACUUM completed: 2 GB freed” — stayed Italian in every language: they were assembled before the translation, so the finished text no longer matched any dictionary entry. The sentence is now translated first and the values are filled in after it, and the 29 missing sentences were added to all six languages. An automatic check stops a translation from losing a placeholder (`%d`, `%s`), which would show as “%!d(MISSING)” on the page. Nothing to configure.
- **Test mode switches off without restarting Gextto.** Turning dry-run off in the settings — or finishing the first-run wizard — kept every command that changes something answering “dry-run does not…” until a restart: the commands read the start-up configuration instead of the saved one. Every command now reads the current configuration, so switching test mode off takes effect at once; a daemon started with `--dry-run` stays read-only for the whole session, as it must. Nothing to configure.
- **A torrent's HTTP mirrors respect the configured limit.** In the engine Gextto and gx-torrent share, with **Maximum webseed sources** (`webseed-max-sources`) set, a torrent with several mirror addresses was truncated to ten or — when the limit was below ten — could stop the client. The configured limit now applies, and zero still means “no limit”, both for added torrents and for addresses added while downloading.
- **An unreadable list no longer erases series and indexers.** When the series list (or the indexer list) stored in the settings could not be read, Gextto restarted from an empty list: the next save wrote that emptiness over the library. It now keeps the list it already loaded and says in the log what it could not read, so nothing disappears silently. Nothing to configure.
- **On/off options work in every spelling.** Some options — the hourly speed schedule, temporary limits, the temporary folder — written as “on”, a spelling Gextto itself can save, were read as off by the panel and as on by the engine: the panel said “disabled” while the rule was active. Every read now accepts the same spellings. Nothing to configure.
- **Copying the databases to a new folder reports its errors.** When moving the data folder, the safety copy of the databases ignored a failed journal copy or checkpoint and carried on: the copy could be missing the latest writes without saying so. The operation now stops and reports the error instead of leaving an incomplete copy. Nothing to configure.
- **Night maintenance: the heavy jobs run at night, one at a time, while Gextto is idle.** In *Settings → Maintenance* there is the new **Night maintenance** group, with a window (01:00–07:00 by default) in which Gextto optimises the databases every night, compacts them when they have a lot of free space and checks their integrity once a week, with an immediate notification if it finds a problem. It also prepares the library report: the files the archive knows but the disk no longer has, each with **Mark as missing** to have it downloaded again; a folder that cannot be read, like a NAS that is not mounted, is listed apart instead of looking like hundreds of vanished files. With the new option **Clean the audio of files already in the library** the audio track cleanup also goes through the existing archive, one file at a time and with the same checks: every file is examined once, and files still seeding through a hardlink wait for the end of the seed, so the space is not used twice. Every job starts only when no download is transferring, no import is in progress and the load is low. **What changes for you:** by default the automatic integrity check and MediaInfo analysis wait for the window too; if you want them at any hour, switch off **Integrity check and MediaInfo analysis only in the window**. The panel under the options shows what is running or why it waits, and **Run now** starts a pass at once; in the morning the log, and a notification when there is something to say, sums up the night.
- **Audio track cleanup: untagged tracks stay, and no file is replaced before it has been compared packet by packet.** If you use **Remove audio tracks in other languages**, there is a new option, **Audio tracks without a language**: by default the tracks that declare no language (often the original audio) now stay in the file, while before they were removed; they can still be removed, or every file containing one can be left untouched. Before replacing a file, Gextto now compares every packet of the video, of the kept audio and of the subtitles with the original, and checks that languages and flags did not change, so a subtitle that was off does not switch itself on in the player. When the copy made with mkvmerge fails the check, Gextto makes it again with ffmpeg instead of giving up. When the default track was among the removed ones, the track in the chosen language becomes the default. On network shares the file permissions are no longer touched, so the ACLs of a NAS stay as they were. The cleanup does not start when the folder has no room for the temporary copy. A pack with many episodes no longer loses files on the way, and files waiting when Gextto restarts are cleaned after the restart, together with any leftover of an interrupted copy. The spellings of language codes (pl and pol, pt and por, ja and jpn…) are now equivalent for every language in the list. Nothing to do: the option stays off until you switch it on.
- **The router port is no longer reopened every 20 minutes, and aMule's log stops filling with UPnP events.** Some routers answer “no such entry” to the single lookup of a forwarding rule they do have in their table: gx-torrent then believed the port was not open and reopened it at every 20-minute check. Each reopening changes the router's update counter, which the router announces to every UPnP client on the network, and aMule wrote five lines for each announcement: its log ended up holding almost nothing else, and so did the **aMule log** tab. The check now reads the rule table when the single lookup gives no answer, so nothing already open is reopened and **Test ports** tells the truth; on top of that the **aMule log** tab hides the router's notification blocks, which say nothing about aMule, and keeps everything else. Nothing to configure.

## 2 November 2026
- **Uploads and uTP downloads use less CPU.** Sending to a fast peer now takes about a quarter less CPU and reaches about 14% more speed on the test machine (262 MB/s against 230). Downloading over uTP takes about a quarter less CPU at the same speed, and about a fifth less when the peer uses encryption. Nothing to set: both apply to Gextto and to standalone gx-torrent; an upload rate limit keeps working as before.
- **indexarr now knows which downloads really arrived.** indexarr used to record only that it had handed the link over, so its Download page counted every request the same way and the number did not say how many files had actually finished. When a torrent or an eD2k file completes, Gextto now tells indexarr, which marks that row as downloaded; everything else stays “delivered only”. It uses the indexarr entries already configured for searches, so there is nothing to set up. If indexarr is unreachable the download is not delayed and nothing is retried.
- **The router port no longer closes once an hour.** gx-torrent opened the port with UPnP asking for a one-hour lease, but at every 20-minute check it only verified that the mapping existed: the lease was never renewed, the router deleted the mapping when it expired and the client reopened it at the next check. That meant incoming traffic lost for up to 20 minutes every hour, and the router announced a removal and an addition every hour to every UPnP client on the network (aMule writes it in its log as a burst of events). The check now reads the remaining lease and renews the mapping before it expires, so the port stays open and the router has nothing to announce every hour. Nothing to configure.
- **A series folder name works on every disk and system.** With “Create folder” the proposed folder was built from the title as it is: a colon (or another character Windows, exFAT sticks and many SMB shares refuse) was fine on Linux, but reading or copying the library from Windows, from an exFAT disk or through an SMB share it could no longer be created or renamed. Those characters — and a trailing dot or space — are now dropped from the proposed folder name, with the same rule the episode file names already follow, so the library stays usable everywhere. A path you type yourself is left untouched. Nothing to configure.
- **Releases without seeders no longer come back at every search.** When the indexer said zero seeders and zero peers the release was refused, but it stayed among the candidates: at every cycle Gextto evaluated it again and wrote the same line ("Skipped … nobody is sharing it"), always for the same titles. Now such a release goes on the blocklist like one abandoned mid-download: it stays out for the period of **Let a release without seeders back in after** (7 days; 0 = forever), so the log names it once and then stays quiet, and if the swarm revives it is a candidate again. It applies to upgrades with leechers only too, and the copy in the library stays where it is. Nothing to configure.
- **The integrity check now says in the log when it picks up again.** Stopping it from the panel left only a "check stopped" line, and the run that resumed from the saved position — including the one the next interval started on its own — wrote nothing: it looked as if the check had never come back. Now, when the check starts again after you stopped it, the log says how many files were already checked. Nothing to configure.
- **The routine cleanup says what it removed.** The line "removed 13 old records" did not say what had gone. It now names only the categories that actually lost rows, with the age that kept them — for example "removed 14 old entries: 3 old search cycles, 8 failed torrents older than 7 days, 2 feed entries already seen older than 30 days" — and it also counts the upgrade backups and the removed download history, which used to stay out of the total. Nothing to configure.
- **In Salute → Stato provider the explanations wrap onto two lines.** **Situazione**, **Cosa fare** and **Dettaglio tecnico** are no longer cut to one line with dots: the reason, the fix and the raw error can be read without opening the tooltip, and the full text stays in the tooltip. Their headings wrap onto two lines too, so the table stays narrower, and the cells always take the room of two lines, even when the text is short or empty (for example the detail of a healthy provider): the rows do not jump. Nothing to configure.
- **In Salute, Coda titoli and Import falliti say what they show.** Hovering over the panel title shows a sentence about its content: the titles held by a delay profile with the time still missing, and the library imports that failed over the last seven days with the reason (completion handling failed or the file refused by the integrity check). Nothing to configure.
- **The integrity check now says in the log when it picks up again.** Stopping it from the panel left only a "check stopped" line, and the run that resumed from the saved position — including the one the next interval started on its own — wrote nothing: it looked as if the check had never come back. Now, when the check starts again after you stopped it, the log says how many files were already checked. Nothing to configure.
- **Torrents connect to all their peers from the start.** Two peers that looked alike to the engine overwrote each other in its address list, and one of them stayed out until the next tracker answer, about a minute or more later. In a small swarm this cost a lot: on a 100 Mbit/s line with four peers a download went from 6.9 to 11.2 MB/s. Nothing to do.
- **Upload and file checks are faster.** The engine told the system not to read ahead on torrent files, which slowed every read from disk: sending to a fast peer goes from about 120 MB/s to 180–240 MB/s, and rechecking a 4 GB file from 8.8 to about 4 seconds on a mini PC with NVMe. A torrent also goes back to seeding as soon as its check ends, and a queued torrent starts as soon as another one stops, with no wait of up to 3 seconds. On µTP the engine uses 10% less CPU.
- **A recheck on a torrent added with "skip check" really reads the files.** It ended at once without reading anything, so damaged data stayed hidden. Now it checks everything, and the torrent leaves that mode.
- **A peer that speaks only µTP no longer disappears when it refuses the TCP attempt.** With a peer configured for µTP only, the TCP attempt is still accepted and immediately reset. The engine took it as the winner, cancelled the µTP attempt and lost the peer until the next announce. Now, if the TCP connection is reset before the initial handshake with the peer, the engine retries once over µTP alone: the peer stays reachable and the download does not stall. It applies to Gextto and gx-torrent. Nothing to configure.

## 1 November 2026
- **eD2k copies with no declared resolution now enter as a "last resort" too.** When nothing within the quality profile existed for a missing episode, the files found on eD2k were thrown away if the name did not state the resolution — the common case, for instance `Series.2x05.Title.Of.The.Episode.ITA.DLMux.x264-GROUP.mkv`. They stayed out even when they were the only Italian copy, and taking them meant going through **Search missing**. The rules are now the same as for torrents: a copy outside the profile (resolution outside it or unknown) is taken as a **🆘 last resort**, with the badge, and the upgrade replaces it as soon as a copy within the profile appears. Archives, non-video files and the source floor are still excluded.
- **The missing episodes of a season now arrive together, not one a day.** When the torrents had nothing for a missing episode, the automatic cycle asked eD2k one episode at a time: with a season full of holes (37 missing episodes of the same series, for instance) and a limit of five searches per cycle, it took days, and pressing **Search missing** by hand was the practical answer. Now, when a season is left without results, Gextto asks eD2k for it once and hands what it finds to every missing episode of that season: the same two searches **Search missing** makes, but inside the cycle. The rules are unchanged: the copy must be in the right language with a known resolution, and outside the profile it stays a **last resort** with its badge. Nothing to configure.
- **Dry-run no longer starts real downloads.** In test mode the log said "Test mode: would download …" but the release was still handed to the torrent client, which began downloading: anyone following the manual and starting in dry-run with an active title found the download running. The dry run now stops at the decision — the "would download" line stays, but no torrent is added and no data is fetched — and a release that could not be started (without a hash, for example) is reported as in a real cycle. Torrents already started by earlier versions stay in the client until you remove them.
- **A healthy file is no longer moved to the recycle bin because of an `ffmpeg` warning.** The integrity check called a file damaged whenever `ffmpeg` wrote anything, including a message about the timestamps of the check's own output rather than about the file being read. On a release with out-of-order frames — it happens with some encoders, and it covers a whole season, not one episode — the episode left the library, the release was blocklisted and downloaded again, and the same thing happened to the next episode. Those messages are now ignored, including on long files where they repeat thousands of times and the check keeps only the beginning: a line cut in the middle is no longer read as an error. The check now also asks `ffmpeg` to keep the original timestamps while decoding, so it no longer produces that warning itself, and it ignores by principle the messages `ffmpeg` writes about its own output (`[null @ …]`), because they describe the check, not the file. A file is damaged only when `ffmpeg` reports a problem reading or decoding it, or cannot finish the check. If the check already moved a healthy file to the recycle bin, you can put it back from there.
- **Adding a search result no longer gets lost when the indexer answers with a magnet.** For a release it has no `.torrent` file for, indexarr and Jackett answer the download with a redirect to `magnet:`. gx-torrent followed it as if it were a web address and failed with `unsupported protocol scheme "magnet"`, so the release never got added: the redirect is now read and the torrent is added as that magnet, from the page, from `/api/v1/add` and from the qBittorrent-compatible API. It applies to Gextto too, which adds the releases found by the indexers.
- **Installing from the archive works even when it is unpacked into `/opt/gextto` or `/opt/gx-torrent`.** Someone who unpacks the package straight into the install folder and runs `sudo ./install-service.sh` (or `./run.sh 2`) got an error (`install: … are the same file` for gx-torrent, `cp` copying a folder into itself for Gextto) and the service was never created: the files already in place are now left where they are and the installation goes on. Nothing to configure.
- **The launcher inside the downloaded archive takes the choice as an argument, and says why it does not start.** In `gextto-linux-<arch>.tar.gz` and `gx-torrent-linux-<arch>.tar.gz` the `run.sh` script accepts `./run.sh 1` (direct start) and `./run.sh 2` (systemd service): before, the number was not read and reached the program, which ignored it, so the choice looked like it had no effect. `./run.sh --help` lists the two entries, an unknown choice is rejected instead of starting the program, and in the menu an answer other than 1 or 2 is asked again. When Gextto (or gx-torrent) does not start because another copy is already listening, the message says which address is taken and what to do: stop that copy (`systemctl --user stop gextto` for the user service, `systemctl stop gextto` for the system one), or start this one on other ports with `GEXTTO_LISTEN` and `GEXTTO_ENGINE_LISTEN`.
- **The Health tables say at once how to read them, and the provider name stays whole.** In *Health → Provider status* the “Disabled until” heading goes on two lines, so “gx-torrent” no longer breaks in two and the **Situation** column, the one that matters, gains room instead of being cut off. In *Health → Feed activity* the line above the table no longer lists initials (T, S, F, P) that then did not appear in the table: it now says in words that each value of the 1, 7 and 30 day columns is in the order attempts/successes/failures/taken. Nothing to configure.
- **The dashboard shows the whole of Gextto's download and upload traffic.** In the **In session** card the two lines of the last-30-minutes chart are in different colours and carry the overall speed — torrents and aMule — instead of the torrents alone: when only aMule is transferring, the chart no longer stays flat. aMule keeps its dashed lines and the legend breaks the total down into **Torrent** and **eD2k**. The two peaks of the period are labelled **Picco download** and **Picco upload**, so they no longer read as a second speed contradicting the one in the top bar, which stays the speed of the moment. Nothing to configure.
- **Feeds of the same site no longer step on each other's toes, and a site that asks to slow down is heard.** When several feeds point at the same site (the uploader listings of ext.to or TorrentGalaxy, for instance), Gextto reads them one at a time, spaced apart, instead of asking for all of them together. When the site answers “too many requests”, it leaves that site alone: the pause starts at ten minutes and doubles every time the site answers that way again, up to six hours, while the other feeds of that site wait for a later cycle instead of failing one after the other. Good answers bring the pause back to normal and no feed is marked as broken or disabled because of this. It applies to the feed check of the standalone gx-torrent too. Nothing to configure.
- **An indexer that just failed behind Prowlarr is not retried at once.** When the manager reports a recent failure on one of its indexers (less than half an hour), Gextto skips that lane in the next search instead of spending another wait: before, it did that only for indexers the manager had disabled altogether. An old failure is still tried, because the site may be back and dropping it would cost results. Nothing to configure.
- **Gextto shows that the eD2k client is aMule, and where it runs.** The **aMule** panel of *Settings → eD2k and Kad* and the **eD2k and Kad (aMule)** card of *Health* now show **in use**, the aMule version, the EC address and **Where it runs**: *on this machine*, *in a container, on this machine* or *on another machine*. In the sources list the entry is called **eD2k and Kad (aMule)**. It keeps an aMule in Docker on the same machine apart from one installed on the system or one on another host. The aMule panel also says it is a separate program, started outside Gextto: Gextto does not start, stop or update it. Nothing to configure.
- **Health no longer repeats the same aMule warnings, and ignores the ones aMule fixes by itself.** In the **eD2k and Kad (aMule)** card of the Health page the same log message appeared once for every line aMule wrote — the error about the `/incoming` folder, for example, twice — and the latest warnings could shrink to a few identical lines. Gextto now shows each message once, with its most recent time, and does not list the attempts aMule retries and completes by itself: at the start of a month the new month's IP-to-country database is not published yet, aMule gets a 404, retries with the previous month's file and keeps the one it already has. The `/incoming` error, when present, stays: it is usually the incoming folder of an aMule category left at `/incoming` by a Docker image, and it is fixed in aMule; the eD2k manual explains how. Nothing to configure.
- **indexarr's own indexers now show up in Health.** In *Health → Source status* Gextto listed the indexers behind a Prowlarr instance but not the ones behind indexarr: a paused or failing source was visible only as missing results. They are listed now, with the `indexarr` type, the reason of the failure (indexarr exposes it next to the Prowlarr fields, where Prowlarr keeps it only in its log) and, when the manager knows it, the moment the next attempt is allowed. indexarr exposes the same read-only documents as Prowlarr (`/api/v1/indexer` and `/api/v1/indexerstatus`), so there is nothing to configure.
- **Feeds that have not worked for a week switch themselves off, and now you can see how they work.** In **Health** there is **Feed activity**: for every feed the state, the last success, the last error, the last results and the counts of attempts/successes/failures/releases taken over the last 1, 7 and 30 days, with a summary of the last 24 hours and of the feed that produced the most in the last 30 days. Feeds with the same address but different filters (for example ext.to uploaders) stay separate rows, with the filter visible and credentials masked; until a first attempt the row says “waiting for the first cycle”. If a feed has not read successfully for 7 consecutive days (editable; `0` turns the rule off) and at least one other feed worked on the same day, Gextto puts a `#` in front of it, disables it and says so in the log; in *Settings → Sources* a **Disabled feeds** frame lists the disabled ones with the date and reason, and removing the `#` tries again. When every feed fails it disables nothing: it is almost always the network, not the sites.
- **The file integrity check now lives in one place, under Settings.** The options and the panel are no longer on two different pages: in *Settings → Maintenance → File integrity check* you find the switch of the automatic check, the interval, the files per pass, the quick 4K check and the automatic re-download, and right below them the **Check status** panel with the progress, the damaged files with **Redownload** and the **Check now**, **Stop** and **Start over** buttons. The «File integrity check» panel is gone from *Maintenance*: the manual actions stay available with the automatic pass off too. Nothing to configure.
- **The integrity check can replace a damaged file by itself.** *Settings → Maintenance → File integrity check* has a new option, **File integrity check — re-download damaged files**, off by default. When it is on, as soon as the check finds a really damaged file it blocklists the release, moves the file out of the library (to the trash, if you configured one) and starts at once the search for a different release of the same episode or movie: it no longer waits for the end of the pass and you no longer have to press **Redownload**. If several episodes of the same series are damaged in the same pass, the search starts only once. When it is off everything stays as before and the replacement is your choice.
- **A check interrupted by a restart no longer calls files damaged.** If you restart Gextto while the integrity check is decoding a file, the running `ffmpeg` is stopped together with the service: that file is now no longer marked as damaged, because the decode did not finish and nothing is known about it. It no longer appears in the problem list and is checked again on the next pass, instead of staying there and, with the new option, ending up in a useless replacement search. Nothing to configure.
- **Background operations show the real percentage and read well on a phone.** On the dashboard and in *Maintenance* the bar of a finished operation said **1%** instead of 100%: the percentage is now the real one, and screen readers get it too. On a phone each operation becomes a card — name and state on the first line, progress bar, message and **Cancel** button below — instead of a wide table that scrolled sideways and left the message and the button off screen. The generic message (“done”, “running”) is no longer repeated next to the state: it stays only when it says something more, such as the file being checked.
- **gx-torrent standalone: the pages read well on a phone.** The *Archive* is now a card per item — title with episode and quality, **Size** and **Seeds** with their own label, and the **Add**, **Copy magnet** and delete buttons on a line below — instead of a table whose title was squeezed to one letter per line. In *Series & movies* the episode legend keeps each icon next to its label when it wraps, the line explaining what a feed downloads no longer ends cut off, and in the torrent detail the file names stay readable. The section bar brings the page you are on into view, so it is never left off screen, and the toolbar keeps a single ▶ and a single ⏸ (the *all* ones): without their labels the two selection buttons looked identical, and the per-torrent actions stay in the row menu.

- **Every page checked on a 320 and a 390 pixel phone.** *Series* and *Movies* now open with the list instead of the “Add” form, which stays right below it; the table shows the name, the state and the **Pause** and **Delete** buttons, which used to end up off screen. *Archive* and aMule's *Shared files* no longer collapse to one letter per line (they scroll sideways like the other tables), in *Health* the paths and the latest errors are fully readable instead of ending in an ellipsis or cut off, and the *Show technical keys* and *Modified only* switches are left-aligned. On the dashboard “Last downloads” shows the name, the completed tick and the date, the session counter wraps instead of being cut, and in *Browse* the two searches come before the calendar. In *Import file* the path field has a line of its own, and the panel has its inner margins back. The bottom navigation bar stays readable even on a 320 pixel screen.

## 31 October 2026
- **An indexer that says “wait” is heard.** When an indexer answers `429 Too Many Requests` with a `Retry-After`, Gextto disables the source at least for the requested time instead of retrying on its own schedule; the safety cap is one day. Nothing to configure: the state and the deadline appear in *Sources in backoff*.
- **A hash declared twice no longer confuses the identity.** Some indexers hex-encode the 40-character infohash a second time. Gextto now recognises that form and uses the inner hash for the magnet and for deduplication, instead of discarding the value and treating the same release as new.
- **Freeleech: free, not free, or not stated.** When the indexer says a download does not count toward the ratio (Torznab `downloadvolumefactor=0` or `freeleech`, Prowlarr's `freeleech`/`neutralleech` flags, or indexarr's `freeleechknown`/`freeleech`), the release shows the **Freeleech** badge and, with the same swarm, is preferred for a missing episode. If the indexer explicitly says the download counts, the release is **Not freeleech**; if it says nothing, it is **Not stated** and gets no bonus: an unknown release is never treated as paid. The weight is in *Settings → Scores → Freeleech bonus* (150 by default, 0 turns it off) and does not enter the stored score, so it never re-downloads an episode already in the library. The standalone **Freeleech only** rule accepts only known free releases, not unmarked ones. Nothing to configure if you do not want the bonus.
- **An episode already taken from a feed is no longer taken again after thirty days.** The feed's persistent archive is now checked next to the recent-items memory: an item that stays in a feed beyond the thirty-day window is not added a second time.
- **gx-torrent standalone: a rule can take freeleech releases only.** The RSS rule window has a **Freeleech only** checkbox: the rule accepts only the releases the indexer marks as free and discards the others. Together with the other conditions (episode, size, seeders, age) it keeps a feed or a title from consuming ratio. Nothing to configure if you do not want it.
- **gx-torrent standalone: an episode already taken does not come back after thirty days.** The list of items a feed has already seen expired after a month: if the release stayed in the feed and the torrent was no longer in the session, a later cycle could take it again. The feed's persistent archive is now the truth and the list's expiry is no longer enough to let it back in: the same release is not downloaded twice.
- **One instance per data folder and per port.** If you start a second Gextto on the same data folder, or the interface port is already taken, the second one stops at once and says so in the log, instead of opening the databases and loading the IP filter only to fail at the end. It is there for a leftover service: stop it and start again. Nothing to configure.
- **The damaged-file check is a periodic option.** In *Settings → Maintenance → File integrity check* you switch the check on and choose how many minutes apart and how many files per pass: Gextto analyses the library with `ffprobe` and `ffmpeg` a little at a time, in the background, and reports the damaged or unreadable files with the reason read from the tool. It saves its position and the files already checked, so it resumes after a restart and skips the unchanged ones; **Start over** rechecks the whole library. Next to a damaged episode there is **Redownload**: the broken file leaves the library, the old release goes to the blocklist and Gextto searches the same episode choosing a different release, so it does not take the same corrupt file again. When the new episode enters the library, the log says exactly which release replaced the damaged file. `ffprobe` and `ffmpeg` are required.
- **The file check no longer calls a merely slow file “damaged”.** A long file decoded on a modest computer can take more than the fixed thirty-minute limit: the time allowed for the decode now grows with the file duration (up to six hours) and a file that still cannot be read within the limit is reported as **Unreadable**, not as damaged, and is tried again by itself in a later pass. In *Maintenance → File integrity check* damaged and unreadable files have separate counts, and the log says which files it could not read instead of declaring them broken. Replacement stays your choice: the check never starts a search on its own, the **Redownload** button next to the episode moves the file to the trash, blocklists the old release and searches a different release. Nothing to configure.
- **4K files get a quick check.** *Settings → Maintenance → File integrity check* has a new option, **File integrity check — quick check for 4K files**, on by default: for 4K files Gextto reads the whole file but decodes only the keyframes, so a 4K episode goes from about an hour to a few minutes. It still finds truncated or unreadable files; it may miss a small defect between one keyframe and the next. Turn it off on the same page to decode every frame again.
- **The notification anti-spam accepts no more than the interface allows.** The window within which the same event is not sent again is at most seven days, as the *Notifications* tab says: a higher value coming from an imported configuration or hand-edited by mistake is brought back to the limit instead of being used as it is. Nothing to configure.
- **gx-torrent standalone: after an action in the settings the page stays where you are.** Saving a profile or a category, adding a category, testing the port, sending a test notification: the page no longer jumps back to the first tab, it stays on the one you clicked and the confirmation appears in the bottom-right corner, so you see at once that the button did something.
- **gx-torrent standalone: quality is chosen only through the profile.** In the *Add title* dialog the *Quality* and *Quality profile* fields did the same thing and it was not clear which won: there is now a single **Quality profile** field with a **Create or edit profiles** link under it that opens the settings on the right tab. Without a profile the title accepts any resolution. The “what would be taken” preview applies the profile. Titles already saved with a quality keep using it until you choose a profile.
- **gx-torrent standalone: the Donate button.** The bottom-right corner of every interface page now has a **Donate** button that opens PayPal in a new tab; on a phone only the heart remains. Its label and accessible name are translated into every interface language. Nothing to configure.
- **The gx-torrent manual and README now tell the parts that were missing.** The illustrated manual has the client's **CPU and RAM figures** (at rest, downloading, loopback, and how memory comes back after a burst), the **automatic speed** that measures the line, the **notification channels** with the events, the test button and the script, and the Docker start says plainly that no image is published and that the sources must be downloaded first. The standalone archive README was rewritten: seven settings tabs, the language picker, automatic speed, notifications, the real detail tabs and updates from the *System* tab. The settings screenshots were taken again and show the tabs.
- **A blocklisted release is no longer requested from the indexer.** When a feed or an indexer only sent the `.torrent` link, without the torrent identifier, Gextto opened the link to find out which release it was and only then discarded it: the indexer counted a download and the request came back on every search. The release title is now enough to recognise it and stop it at once, and the archive keeps the identifier the indexer declares next to the link, so the same release is no longer requested or duplicated. Nothing to configure.
- **The gx-core manual now shows screenshots that are entirely in English.** The images of the English version had some Italian text next to the rest (for example the sidebar said *Stato* and *In download* while the table said *Downloading*). They were taken again from a real instance whose interface was English from the first start, and the English version of the manual also has the phone screenshot that was missing. The images of the Italian version stay in Italian.
- **Small fixes to the gx-torrent interface.** In *Series & movies* the filter counters showed a placeholder (`{{WANTEDMISSING}}`) instead of the number, and now they say how many titles there are for each state. In a torrent's panel the file, peer and tracker counters read 0 on every tab except the open one; the *Status* column cut “Downloading” with the English interface; and on the statistics page the torrent count stuck to the word (“4Torrents”). Nothing to configure.
- **gx-torrent standalone: a title's menu no longer stops at the card's edge.** The **More** button next to every title in *Series & movies* opens *Why not?*, *Episodes*, *Search now*, *Edit*, pausing the monitoring and the bin that removes the title from the list: before, on the last title of the list, the menu ran past the card's edge and the last entries — the bin included — stayed invisible. It now opens in full and, when there is no room below, it opens upwards instead of running off the screen.
- **gx-torrent standalone: the language is changed from the first page.** At the bottom of the sidebar, under state, categories and trackers, there is now the interface language selector, each language written in its own language (English, Italiano, Deutsch, Français, Español, Polski). Choosing one reloads the page already translated and the choice stays saved, exactly like changing it from the settings: no need to go looking for it there.
- **gx-torrent standalone: automatic speed, an autotune that measures the line.** In *Settings → Automatic speed* gx-torrent can now find the right speed by itself: it probes the latency towards Google/Cloudflare (or a chosen host) and moves **download and upload** together, lowering them when the line queues and raising them when it is free again. Manual limits stay the ceiling, and the real line maxima can be entered so it reaches the right point sooner; with the maxima at 0 it discovers them by itself. **Optimize bandwidth** now clears both limits and turns automatic speed on. Nothing to configure for those who do not want it: it stays off by default.
- **gx-torrent standalone: the settings are now split into sections.** The Settings page was one long list; it now opens on seven tabs — **Folders & files**, **Network**, **Bandwidth**, **Queue**, **Library**, **Interface** and **System** — each showing only the groups that belong to it. The search box still looks in every tab at once, and **Save** at the bottom still saves the whole page, including the tabs that are closed. With the keyboard, the left and right arrows, Home and End move between the tabs; the selected tab stays after saving.
- **Notifications: the same channels as indexarr, with the events you choose.** Gextto's *Notifications* tab now has the dedicated channels — **Discord**, **Slack**, **Gotify**, **ntfy**, **Pushover** and **Apprise** — besides Telegram, webhook and e-mail, plus a **custom program** run on every event with the details in the `GEXTTO_*` variables. Several channels can be used together and a slow one does not hold back the others. Four switches choose what to receive — downloads, errors, health and engine, maintenance —, the **anti-spam** window does not repeat the same event within the given minutes (`0` turns it off) and the **message language** can differ from the interface one. At the top of the tab, **Send test notification** tries every configured channel at once.
- **gx-torrent standalone: notifications also reach Apprise, Discord, Slack, Gotify, ntfy, Pushover and a script.** *Settings → Notifications* now has the channels indexarr knows: besides Telegram, the webhook and e-mail you can fill in the **Apprise** server (with the key of one configuration), the dedicated **Discord** and **Slack** webhooks, **Gotify**, **ntfy** and **Pushover**, and the path of a **custom script** run on every event. Several channels can be used together: each one receives the same event and a slow one does not hold back the others. The script gets the details in the `GX_TORRENT_*` variables (app, event, title, message), never on the command line; a field left empty switches its channel off. The **Send test** button uses every saved channel.
- **gx-torrent standalone: searching a title also shows the episodes already in the archive.** When you add a series and press **Search**, under each suggested name you see the episodes the archive already holds, each with its resolution and language. Choosing one fills the title, the quality and the language from that release — the one already taken comes first, then the best resolution — and everything stays editable before saving. When there are more than twelve episodes, the number of the others sits next to the list.
- **gx-torrent standalone: test an indexer before saving it.** The window where you add or edit an indexer now has a **Test** button: it contacts the address and API key you have just typed with a Torznab request and shows at once whether the indexer answers and how long it took, without saving it to find out. When you are editing an indexer and leave the API key blank, the stored one is used, as on save. The test button on the rows already configured stays as it was.
- **gx-torrent standalone: what it costs, in numbers.** The package README now states the CPU and RAM measured on an Intel N97 mini-PC: **0.6–1.0 % of one core and 29–39 MB RSS at rest** with DHT on and a full IP filter, **18–37 % of one core at 8.2–8.8 MB/s with 116–132 peers** (2.0–4.3 s of CPU per 100 MB) and **234–274 MB/s over loopback** with about 100 MB peak. After a burst the memory falls from 550 MB to 35–40 MB in five minutes. The same figures are in the main README and in the technical document, with the machine, the date and the command to repeat them.
- **gx-torrent standalone: an RSS feed keeps its identity, and rules say what they took.** Renaming, moving or re-addressing a feed no longer changes its identifier: an app or a WebUI that remembers feeds by identifier no longer loses them. Download rules also report when they last took something and which episodes they already took, and the **tags** set by a client are kept instead of disappearing. Nothing to configure: this holds for the gx-torrent page and for the qBittorrent-compatible API.
- **gx-torrent standalone: an illustrated manual for gx-core.** The standalone client now has a user manual with real screenshots, taken from a Docker environment with legal torrents downloading: three-minute start, Torrents page and details, add and create, Series & movies with quality profiles and TVmaze, RSS, statistics, settings, phone and qBittorrent integrations. It is available in Italian and English as *Manuale gx-core* / *gx-core manual*.
- **gx-torrent standalone: the real episode list, optional.** *Settings → Series and movies* can ask TVmaze for the complete episode list of a series: episodes that have already aired become the denominator of the progress bar and count as missing even if no release ever appeared in the feeds. The series title is sent to api.tvmaze.com; the option is off by default and, when a title is not matched, the page keeps using what the feeds and indexers saw. The list is refreshed once a day.

- **gx-torrent standalone: quality profiles and upgrades.** *Settings → Quality profiles* creates a profile with the accepted resolutions and a cutoff; a title can choose it instead of the quality field. With a cutoff, an episode already downloaded at 720p is replaced by a 1080p release when it appears, without deleting the old files: the previous torrent leaves the session, the data stays. Without a profile the behaviour is unchanged.

- **gx-torrent standalone: more compatible with qBittorrent clients.** The compatible API now accepts the qBittorrent 5 **API key** (`X-Api-Key` or Bearer), exposes `app/processInfo` and `torrents/setComment`, fills the fields the WebUIs read (download path, last activity, ratio and seeding limits, super-seeding, tracker count, magnet, private flag and totals) and accepts limits and rename when adding. The version reported by `app/version` is configurable (default `4.6.7`), so a client that checks the version does not refuse the connection.
- **gx-torrent standalone: the page for following series and movies is now called “Series & movies” and is clearer.** “Wanted” was a system word and each row mixed too much: every title now says in words what you have (*Downloaded*, *Downloading*, *Found, not downloaded*) with a progress bar over the episodes found so far, has **one primary action** chosen from its state (search missing, open, episodes, releases) and a **More** menu for the rest. Above the list are the state filters (with missing, downloading, complete, not monitored) and the order (name, missing, last search, added); the next search sits beside the last one. The legend says plainly that “found” means seen by the feeds or the indexers: gx-torrent does not know the complete episode list of a series and does not pretend a partly seen season is complete. The add dialog is shorter too: two steps, seasons from/to, category and folder in sight, advanced options folded away. The *Releases* button becomes **Why not?** and shows what was found and why it was not taken.

- **gx-torrent standalone: categories are managed from the page, with their folder.** A category created from the torrent dialog started with no path: automatic management moved it to the download folder, so the option looked like it did nothing. In *Settings → Categories* you can now add, change the path of and remove a category; changing the path relocates the managed torrents of that category.

- **gx-torrent standalone: after a download it can move the data and run a program.** In *Settings → After download* you choose a completion folder (with one subfolder per category, if you want) and the torrent keeps seeding from the new place. You can also name a program to run on completion: the torrent details arrive in `GX_HASH`, `GX_NAME`, `GX_PATH`, `GX_CATEGORY` and `GX_SIZE`, never on the command line. Both actions are off by default.

- **gx-torrent standalone: the indexer search answers sooner and shows more.** The indexers are queried in parallel, so with several indexers you no longer wait for the sum of their times. Each result shows size, seeders, peers, age and indexer; the list sorts by seeders, size, date or name; *Options* opens the add dialog already filled in, to choose destination, category and tags.

- **gx-torrent standalone: the page installs as an app and the session can be closed.** From a phone or a browser the gx-torrent page can be added to the home screen (a manifest and a service worker for the shared assets only, never for authenticated pages or API answers) and, when a password is set, *Sign out* appears.

- **gx-torrent standalone: easier to add and create.** The add dialog also takes tags and seed limits (ratio and days); the toolbar has *Pause all* and *Resume all*; creating a `.torrent` exposes the piece size, the private flag, a comment and web seeds; in the *Trackers* tab a tracker URL can be edited instead of removed and added again. The qBittorrent-compatible API also serves the free space and the content of a folder, so VueTorrent and the mobile apps choose a path without errors.

## 30 October 2026
- **The eD2k speed in the top bar no longer stays at zero while aMule is uploading.** aMule reports its own speed over a one-second window: a client downloading from you in blocks made its speed appear for an instant and zero the rest of the time, and the sum with the torrents dropped to `0 B/s` even with hundreds of KB/s going out (on the eD2k page the same clients looked stopped between one block and the next). The top bar now uses the bytes aMule really sent and received in the last half minute: the speed stays the real one and no longer flickers. The per-file and per-client speeds on the eD2k page remain aMule's instant figures. Nothing to configure.

- **A torrent crafted on purpose no longer writes outside the download folder.** A torrent's name is the first part of every file's path, and it was cleaned less than the single file names: a torrent named `..` could create files above the chosen folder, and on Windows a path with backslashes was enough to climb further out. The name and every path are now refused when they leave the download folder, in both the engine and the daemon, and a torrent that tries is not added, with a clear message. Normal torrents are unchanged, including the ones with unusual but harmless characters in their names.

- **A peer can no longer exhaust gx-torrent's memory while the metadata is on its way.** When a magnet is added, the peer messages that depend on the metadata are queued to be replayed once it arrives; a hostile peer could keep sending very large ones — up to 30 MiB each — and grow the memory without limit. Each peer's queue is now bounded (1024 messages, 4 MiB), an oversized bitfield or extension message closes the connection instead of allocating tens of MiB, and a piece message shorter than its own header is refused. Normal peers notice nothing: before the metadata they send a few KB.

- **Hash requests for v2 torrents can no longer keep the disk busy.** To answer a peer that asks for the block hashes of a file, gx-torrent reads that file from disk on the torrent's loop: a burst of requests could keep it busy for a long time and starve the other torrents. The read now has a budget (a 32 MiB burst, then 16 MiB per second), the block tree is built one file at a time per torrent and the bytes being built count against the cache budget; the answers served from memory are not limited, so downloading does not slow down.

- **gx-torrent no longer accumulates abandoned HTTP connections.** RSS feeds, indexers, `.torrent` files taken from a URL and the IP filter list created a new HTTP client on every request: each one opened a fresh TCP and TLS connection and left its idle ones hanging forever. They all share one pool now, rebuilt only when the proxy or the outgoing interface changes: fewer handshakes on every refresh and no forgotten connection. Web seeds also keep the parallel connections of the same source open instead of reopening them for every piece.

- **The periodic flush no longer touches files that did not change.** Every 30 seconds the engine makes the data durable before saving the progress, and it did so for every open file of every torrent: on a pack with thousands of files that was thousands of pointless `fdatasync` calls every half minute, and on a network folder it could take longer than the interval. Files that received no data since the previous time are now skipped; if a flush fails the file stays marked and the next attempt retries it.

- **A web seed that ignores range requests is dropped instead of producing corrupt pieces.** When the server answers with the file from the beginning instead of the requested block, those bytes ended up in the wrong piece, the hash check failed and the blame fell on the peer or the web seed. The answer is now recognised for what it is, the source is dropped with a message, and the piece is fetched from the others.

- **A torrent's data opens even when the files are not ours.** gx-torrent opened its files without updating the access time (`O_NOATIME`), a permission the kernel grants only to the file's owner: a torrent whose data had been restored from a backup or written by another account never started. When the system refuses that permission, the file is now opened normally.

- **Less processor for every peer message.** Every message received from a peer was decoded with the standard library's generic reader, which allocated a buffer and went through reflection for a 13-byte request: in a swarm where we serve many blocks that is tens of thousands of messages per second. The fixed-size messages are now read into small buffers on the stack: about a fifth less time and half the allocations per message (531-572 to 407-451 nanoseconds). Nothing to configure.

- **With many torrents, the DHT announces stay on time.** The DHT peer searches were served one per second, so with hundreds of torrents the last ones waited minutes. They now leave in batches of ten per round, and the queue is read without holding up the rest. Together with that, the internal timers that were no longer needed are gone: gx-torrent no longer keeps a one-second timer per torrent just to count seeding time, and the DHT timers stop with the daemon instead of staying behind after a restart.

- **Pages no longer break when the configuration has not been loaded yet.** When the configuration was unavailable — at startup, or when reading it fails — the traffic panel and the speed-limit badge read an empty configuration and the page came back empty. Neither the limit line nor the badge is drawn in that case now, and the page opens normally. Nothing to configure.

- **The engine's health check no longer declares a torrent dead while it is answering.** The watchdog periodically asks each torrent's loop whether it is alive; when the wait expires while the torrent is writing to disk, it waits instead of reacting. One edge case was left: when the torrent answered in the very instant the wait expired, the random choice could take the "expired" branch, read "no write in progress" and end gx-torrent with an error. Before declaring the torrent dead the watchdog now makes one immediate last attempt: if the loop is listening, the answer arrives and the torrent carries on. Nothing to configure.

- **A comic downloaded as a CBR arrives with the right extension.** The file name came from the post title, which does not say whether the content is a CBZ (a ZIP) or a CBR (a RAR): every direct download therefore ended in `.cbz`, and Komga and Kavita refused to open the issues that were really RAR archives. Once the download completes the extension now follows the content: a RAR becomes `.cbr`, a ZIP stays `.cbz`. Files already downloaded are left alone. Nothing to configure.

- **A download link that ends on a magnet is no longer lost.** Some indexers do not hand out the `.torrent` file: they answer the download address with a redirect to a `magnet:` link, or they put the magnet in the response body. Gextto and gx-torrent treated that as an error, so the release was lost — from the search, from the feeds and from the add dialog. The address is now followed, recognised and added as a magnet, with the trackers it carries. An address that holds neither a `.torrent` nor a magnet says so with a clear message instead of failing while reading the file, and a redirect to something that is neither HTTP nor a magnet (a `file:` target, for example) is refused with the scheme named. Nothing to configure.

- **Abandoned searches do not stay in memory.** The qBittorrent-compatible search keeps its results until the client deletes them: a client that closes the tab or goes away mid-search left the results in memory for the life of the daemon. A finished search is now forgotten after 15 minutes if nobody deletes it. Anyone who reads the results and then deletes them, like Sonarr, Radarr and Prowlarr, sees no change in their flow.

- **BitTorrent v2 torrents are verified with a single check.** Every downloaded piece was checked twice: first with the check used for classic torrents (SHA-1), then with the v2 one. The first result was thrown away but still cost time on every piece — on a 4 MiB piece, almost a millisecond and a half of processor — and the same waste was in checking files already on disk and in the piece check of «Skip check» mode. Each piece now pays only for the check it needs: about 45 % less checking time per piece on v2 torrents, that is almost 4 % of a core less at 100 MB/s of download. Classic torrents are unchanged. Nothing to configure.

- **A single-domain cycle searches only that domain's titles.** Starting a movies-only search, Gextto still queried the indexers for every monitored series — name and aliases — and then threw those results away: the cycle spoke of 104 titles when the movies were 55, and the indexer phase took minutes longer than needed. The title search now follows the chosen domain: «movies only» searches only the movies, «series only» only the series and their aliases, the full cycle both. The feeds are still read whole, because they fill the shared local archive. Nothing to configure.
- **The installer detects the distribution and installs the dependencies.** It used to use the package names valid on Debian and Ubuntu even on openSUSE, where `zlib`, `useradd` and the media tools have different names, and it did not install the programs Gextto uses to read file quality and clean audio tracks. It now reads `/etc/os-release`, says which distribution it found and installs the base packages with the right names for Debian, Ubuntu, Fedora, openSUSE and Arch; when the repositories provide them it also installs `ffmpeg`, `mediainfo` and `mkvtoolnix`. The three media tools are optional: `--no-media-tools` (or `GEXTTO_SKIP_MEDIA_TOOLS=1`) skips them and Gextto stays fully working, with quality checks and audio cleanup limited as before. Nothing to configure for anyone using the standard installer.

- **eD2k shared files no longer widen the page.** On the *Shared files* tab a long name forced you to scroll right and left the last fields off screen. The name now wraps inside its cell and the bytes uploaded in this session sit under the total, in the same column: the table fits the page and shows more data at once. On wide screens the fields on the right have room for their labels and values, the last-upload header sits on two lines, and the file name takes the remaining space. Nothing to configure.

- **Feeds, indexers and .torrent links go through the proxy.** With a proxy configured for gx-torrent, only peers, trackers, web seeds and the IP filter list used it: RSS feeds, Torznab indexers and .torrent files taken from a URL went out directly. They now follow the proxy (or the VPN interface), while services on localhost and on the local network stay direct, because sending them through the proxy would break them. With a proxy configured, FlareSolverr is not used as a fallback: it would fetch on its own outside the tunnel, so it must be placed behind the same proxy. The update check stays as before. Nothing to configure.

- **The IP filter list is downloaded through the proxy.** With a proxy configured for gx-torrent, updating the IP filter list went straight out to the Internet: your address was visible and the traffic bypassed the tunnel. It now goes through the same proxy (or VPN interface) as peers, trackers and web seeds, even with an HTTP proxy that refuses CONNECT tunnels to port 80. Nothing to configure.

- **gx-torrent no longer stops accepting peers because of a passing error.** If the system refused an incoming connection for a moment — for example because too many files were open — the engine treated the listener as closed and accepted no peer until a restart. It now retries by itself, with growing waits, and resumes as soon as the system allows it; one line stays in the log. Nothing to configure.

- **An episode picked by hand from eD2k reaches the library.** Picking an eD2k result from an episode search (the missing episodes of a series included) registered the download with only its file name, without series, season and episode. Once it finished, Gextto did not recognise it as an episode, tried to move it like a torrent (which aMule does not allow) and repeated «Could not move the completed …» in the log every ten minutes. The download now keeps its series and episode and is imported at completion like any eD2k episode. Downloads already stuck this way are recognised at the next check and reach the library. An eD2k file Gextto cannot place stays in aMule's incoming folder, with a single log line. Nothing to configure.

- **Streaming a torrent added with «Skip check» reads only checked data.** With gx-torrent a torrent added without the check verifies each piece the first time a peer asks for it, but the streaming reader read the files as they were. Streaming now waits for the check of the piece it is about to read too, and starts it by itself if no peer asked for that piece yet; if the piece does not match, gx-torrent checks every file again, as it does for peers. The wait is a few milliseconds per piece. Nothing to configure.

## 29 October 2026
- **gx-torrent can add a torrent without checking it first («Skip check»).** When the files are already on disk and you know they are complete — for example a copy of what you were already seeding — the **Skip check** option in the add dialog (or `skip_checking` for programs that speak the qBittorrent API) puts the torrent into seeding at once, without reading every file back. Before, gx-torrent ignored the option. Safety stays: each piece is checked the first time a peer asks for it, and a piece that does not match is never sent; in that case the torrent leaves this mode, checks all its files again and downloads what is wrong. Once every piece has been checked the mode ends by itself, and it stays on across a restart. With qBittorrent the option is now actually passed to the client.

- **Peers on the local network run at full speed.** With gx-torrent the speed limits also slowed down transfers to the computers at home or in the office, which do not go through the Internet line. Now, as in qBittorrent, peers on the local network are outside them: the limit applies to the Internet only. To limit them too, turn on **Apply the limits to peers on the local network too** (Configuration → Speed and network → Speed limits in Gextto, *Bandwidth* on the gx-torrent page); from the gx-torrent page the choice applies at once to the peers already connected. qBittorrent-compatible interfaces read and change it as «Apply rate limit to peers on LAN». With a proxy configured every peer stays under the limits, since behind the proxy there is no telling who is on the local network.
- **A seeding torrent now knocks on peers, so the ratio can move.** Gextto used to keep the peers a tracker gave it only while downloading: once complete it waited for peers to connect to it, so a leecher behind a NAT was never served, the row showed **0 / 0** peers, the ratio stayed at 0,00 and it looked as if uploading did not work. While seeding the engine now keeps the tracker's peer list and dials up to ten of them, asks for peers when it finishes (instead of asking for none) and remembers the ones that turn out to be seeds, so it does not knock on them twice. The Scarico row also shows the **swarm** the trackers report (🌐 seeders / leechers): with 0 leechers nobody is downloading the file and there is nothing to upload, whatever the client does. Nothing to configure.

- **gx-torrent asks peers for data with far fewer packets.** Block requests to a peer went out one by one, each with its own socket write and its own network packet: 250 requests were 250 packets. Requests and the other short queued messages now leave together in one write, and the system calls for the same number of requests drop about fiftyfold. Less CPU and fewer packets on the network while downloading, especially with many fast peers; sending data to others is unchanged. Nothing to configure.

- **gx-torrent uses less CPU on large torrents.** When a peer disconnects, the engine no longer walks every piece of the torrent, only the ones that peer had: on a 27,000-piece torrent the cost drops about fourfold, which matters in swarms where peers come and go all the time. In sequential download (streaming) the choice of the next piece no longer starts from the beginning of the file, so near the end of a large download it costs nothing. Nothing to configure.

- **Several clients connected to gx-torrent stay incrementally updated.** qBittorrent-compatible web interfaces (VueTorrent and the like) ask gx-torrent only for what changed since their last reply. With two clients open at once — two browser tabs, or a web interface and another app — each made the other lose track, and both received the whole torrent list on every poll. Each client now receives the changes since its own last reply. Nothing to configure.

- **In Downloads the order covers the whole list, eD2k included.** The list showed the torrents alphabetically and, below them, the HTTP downloads and aMule's files in aMule's own order, and **Sort by** only reordered the torrents. Torrents, HTTP downloads and eD2k files are now one list: it is alphabetical by name when you open it, and **Sort by** with ↑/↓ orders them all together by status, progress, download and upload speed, estimated time, peers and ratio (what a row does not have stays at zero). Nothing to configure.

- **The log names FlareSolverr when it closes browser sessions.** The line that at start-up reports the browser sessions left open by the previous run only said «Closed 5 browser sessions…»: it now names FlareSolverr, so it is clear what it is about. The DEBUG line about idle sessions says it too.

- **An eD2k import is now a single log line.** When a file taken from eD2k reached the library the log wrote two nearly identical lines — the import with the episode name and the move with the download's raw name — which looked like two events. Now there is one line: it says what entered the library and where, whether it replaced a previous version, and that the file left aMule's incoming folder (or that it stays there in copy mode, or that an interrupted import was resumed). Nothing to configure.

- **The real quality check also says whether it found anything.** The line that now and then summarises the MediaInfo backfill of the library («Read the real quality of …») only said how many files it had read. It now also says for how many of them the reading added something the release name did not — and therefore for how many the next upgrade will be decided on the file instead of the name — or that the names already said everything and no decision changes. Files that could not be read or are no longer on disk stay at the end of the line.

- **The full Docker stack includes indexarr.** `docker-full` now also starts indexarr, the Torznab/Newznab indexer with native sources, next to Jackett, Prowlarr, FlareSolverr and aMule. The image is built from the indexarr source placed next to Gextto's (or where `INDEXARR_SRC` in `docker-full/.env` points) and `scripts/docker-full-build.sh` stamps its version. indexarr uses the host network on port 8788 (`INDEXARR_PORT` to change it): in Gextto add it as an **indexarr** indexer with URL `http://127.0.0.1:8788`. Before starting, also create `docker-full/data/indexarr`, and set a username and password in its UI right away. The Docker guide covers setup, updates and backups, including the vault key to keep apart.

- **Backups no longer save downloads in progress.** A Gextto ZIP no longer contains `gx-ed2k/incoming` (eD2k files still arriving) or `gx-torrent/parts` (the torrent engine's temporary pieces): they are partial data that is downloaded again, and on a large download they could weigh as much as the whole file. Configuration and the neighbouring folders stay in the backup. It applies to automatic, manual and command-line backups; backups already created are untouched.

- **A blocklisted torrent no longer fills the log.** When Gextto discards a release because it is on the blocklist, the line «Skipped … it is on the blocklist» moves from INFO to DEBUG: it stays visible when you raise the verbosity, but no longer appears on every cycle. The other skip reasons stay at INFO.


- **An upgrade is decided on the file you have, not on the release name.** Before downloading a "better" version Gextto compared the release name with the name of the library file: when that file had been renamed without its attributes — the standard name keeps only resolution and codec — any release with «2160p DV» in its name looked better. On 29 October this happened to three files in one day: downloaded, copied into the library and then discarded because the copy already there was equal or better. Now, when the real quality of the library copy has not been read yet, Gextto reads it from the file before deciding: when it is not a real improvement the download never starts, and the reading is kept for the next searches. When the file cannot be read or is not there, the decision stays the one of before.

- **A torrent that reached its seed limit no longer starts again by itself.** With gx-torrent a torrent that had reached its limit — ratio or time — was paused and resumed every few seconds, for minutes: two sides applied the limit with different rules and cancelled each other out (on 29 October, 24 pauses and resumes in five minutes). In managed mode the limit is now applied by Gextto alone, and resuming a "without limits" seed reads the same limits as the pause: a pause stays a pause, and the log tells it with a single line.

- **The gx-torrent log reads like Gextto's.** Every line of the torrent engine now carries its date, level (INFO, WARN, ERROR, DEBUG) and a sentence: before they were lines without a level, with the date in another format and values such as `handshake=240 io_timeout=14`. At start-up a single line says version, folders and interface address, and says whether the transfers stayed up; trackers that stop answering are counted in one line instead of one per tracker; the technical details — peer cap, cache, swarm noise — show only when the daemon runs with `-debug`. Gextto's shutdown also says how long it has been waiting for a copy in progress and, if other work remains, which.

- **An interrupted import no longer throws the download away.** If Gextto stopped — restart, shutdown, update — while copying a big eD2k file into the library, the next attempt compared the download with the copy it had already put in the library, judged it "not better" and moved aMule's file to the trash, leaving a raw release-named file in the library and the episode still marked as not archived. That copy is now recognised as its own (same name, same size): the import resumes from it, the file is renamed and recorded, and the download stays in aMule to keep sharing. It applies to episodes and movies.

- **Trackers behind Cloudflare work again.** With the IP filter applied to trackers too (the default) and the bundled list, which carries the Cloudflare ranges, the trackers hosted there — many private ones — were blocked before being contacted: the *Trackers* tab showed them as not working with "unknown error in announce" and after an hour the tracker was dropped from the session for six hours. Now the IP filter blocks peers and, only if you turn it back on, trackers: *Apply IP filter* in *Settings → Network* is off by default. When a tracker does not answer, the tab states the real reason — "tracker IP is blocked", rejected TLS certificate, proxy refusing the connection — instead of "unknown error in announce".

- **The "seed ∞" badge appears as soon as you set an unlimited seed.** Before it showed only while the torrent was seeding: a torrent still downloading, set to ratio 0 and days 0, did not show it. It now follows the setting (ratio or time 0 = seed without limits) and is visible during the download too.

- **Trackers are added and removed one at a time.** In the *Trackers* tab of the details the box no longer holds the list to rewrite: you type only the new trackers, which are added to the ones already there, and every row has its own **Remove** button that drops just that tracker. Before, saving the box with a single row wiped every other tracker of the torrent. It works with gx-torrent and with qBittorrent, and the last tracker of a qBittorrent torrent can now be removed too.

- **A torrent's limits: each field changes on its own.** In the *Limits* tab of the details — and from the TUI or the API too — changing one thing no longer wipes the others: setting the **seed days to 0** (seed forever) no longer moves the **ratio** to the global value, and changing the ratio no longer puts the days back to -1. The same goes for the speed limits: a field the request does not carry keeps its value instead of becoming "unlimited". After saving, the tab shows the new values at once. Anyone with a torrent whose ratio or days were scrambled puts them back once from the *Limits* tab.

- **A re-downloaded release is no longer taken for already finished.** When Gextto downloaded again a release it had downloaded before — the file is no longer in the library, or the first attempt failed — the new download was declared finished as soon as the engine moved its files, that is right after the metadata arrived and before a single byte had been downloaded. The integrity check then found a file still full of zeros, moved it to the trash and marked the download as an error: on 29 October this happened to a film, which is picked up again on the next cycle. The new attempt now starts clean (status, completion time and archived path of the previous attempt are cleared) and the file is downloaded and archived normally. The empty file this error left in the trash can be deleted: it holds nothing. When the library copy is still in place nothing changes: adding the same torrent again stays a duplicate that is already archived and does not touch the library.

- **Torrents can be sent from the browser with the Firefox extension too.** Gextto answers a subset of the qBittorrent API (`/api/v2`) on its own address, so an extension written for qBittorrent — *Torrent Control*, for example — can send magnets and `.torrent` files straight to Gextto: the torrent enters the queue, gx-core (or the active engine) downloads it, and it is renamed and archived like the others. In the extension, pick the *qBittorrent* client and put Gextto's address with the interface port as the server address (`http://192.168.1.161:5000/`); username and password are the ones of the protected access, when it is on. Leave labels/categories empty: Gextto answers with an explicit error instead of ignoring them, because they would change the queue tags and the folders on disk. The `.desktop` handlers are no longer needed for this case, though they stay available.

- **The search of an episode opens inside the page and says what it is doing.** The 🔍 of an episode no longer opens a window that covers the series: the results appear in a frame under the episode itself (on the *Missing* tab, under the season line), first the releases already in the archive and then the online ones. While the online search runs the frame says how far it is — how many sources it is asking, how many have answered (for example 12/106) and when it moves on to eD2k/Kad — and when it ends it stays there with the number of sources it asked. **Close** folds the frame away and puts the focus back on the button, and the episode and its surrounding rows stay visible: you never lose your place.

- **Episodes already in the library no longer disappear when a download is discarded.** When an eD2k file was refused — for example because the library copy was better — Gextto also rolled back the rows of *every* episode that had come from eD2k: the *Missing* tab showed episodes that were on the NAS, and only *Scan archive* put them back. On 29 October this happened to 1,096 episodes. The rollback now touches only the refused download's row, never the copies already archived. Anyone who has already lost the rows gets them back with **Maintenance → Scan archives** (or *Scan archive* on a single series). The completed-torrent cleanup also no longer fails on the movies table: the torrent hash is really cleared from the archived rows, so they can no longer be mistaken for a download to undo.

- **The archive scan lines have an icon too.** "Archive scan completed", "Archive scan finished with errors" and the files skipped during a scan now start with an icon like the other log lines, so they no longer look attached to the INFO level.

- **A feed is paused with a `#`.** In *Configuration → Sources → RSS feeds* a line starting with `#` — with or without a space (`# https://…` or `#https://…`) — is no longer read on every cycle, but stays in the list: removing the hash brings the feed back without retyping it.

## 28 October 2026

- **The interface no longer waits for the torrent list.** Pages, the traffic graph and the latency guard asked the engine for the torrent list, which could start a poll to gx-torrent before answering (up to 15 seconds with a slow daemon). They now read the last available list, which refreshes by itself; the list is also sorted once instead of on every read: with 2,000 torrents a read goes from 1.42 ms to 0.6 ms.

- **The engine wastes less memory for every packet it sends.** uTP created a timer, a structure and a copy of the data for every packet: at 100 MB/s that is tens of thousands of objects per second. Each connection now has a single timer that serves every waiting packet, with the same retransmission as before: in the loopback transfer test the allocations per 16 KiB block drop from 99 to 75 and the speed stays the same (151–159 MB/s).

- **Looking up films already in the archive reads the folder once.** For every candidate film Gextto re-read the whole archive (with 200 films and 800 files: 17 seconds and 434 MB of memory per cycle). It now reads the archive once per cycle and compares the candidates with that list: 200 films cost 140 ms.

- **Scanning the library no longer writes once per file.** When Gextto rechecks a series folder, every episode it found cost a separate write to disk: on 2,000 files that was about 3.3 seconds spent only saving, and on a network disk far more. The files of one series are now saved in a single write: the same scan drops to about 60 ms of saving on this disk, and stays instant on a RAM disk.

- **Looking for missing episodes costs less.** Gextto compares every release with all the configured series, and it used to re-process every name and alias of the library for every release: with 500 series that was milliseconds per release. That list is now prepared once and refreshed when you change the series, so the comparison drops to a tenth of a millisecond. The missing-episode calculation also reads the episode table once instead of twice.

- **The archive no longer holds up the rest of the interface.** While Gextto was reading or counting in the archive — a search, the content-filter page, up to a few seconds on a large archive — every other request queued behind it: the page opened only when that read finished. The archive's reads now use a separate connection: I measured a short read going from 116–122 ms to 0.26–0.35 ms while a full scan is running. In the same round the *Recalculate scores* action in maintenance goes from about 215 ms to about 55 ms for 2,000 torrents, and the engine no longer re-sorts the piece table on every peer request. Nothing to configure.

- **The interface costs less and answers sooner.** The archive content-filter page copied the magnet text of all 364,348 releases on every visit — 131 MB read to show at most 500 rows: it now reads only the titles and loads the visible page with a separate query. The Downloads table no longer lists the torrents twice on every refresh (one every 5 seconds while the page is open), the *Title queue* panel of the Health page no longer runs the whole health check every minute, the torrent list uses an index instead of sorting the whole table, and gx-torrent's debug lines are no longer built when the log is at WARNING. Nothing to configure.

- **The log says why a download ends and that the library copy stays.** When an already archived season pack finished seeding and its download copy went to the trash, the log only said that the folder had moved to the trash: it looked as if the whole pack had been thrown away. The line now says which limit ended the seed ("seed time reached", "ratio reached"…) and that the library copy stays, with its path. If the move to the trash fails, the message still says where the good copy is. Nothing to configure.

- **Weekly Packs arrive as soon as they are downloadable.** GetComics publishes a pack's page before its download links, and the comics check runs once a week: the pack waited for the next check, about a week. Now, on every regular cycle, Gextto rechecks only the packs already recorded but not downloaded yet and queues one as soon as its link appears, without redoing the comics check. The pack of the week of 21 October, waiting since the 23rd, is taken at the next cycle.

- **Monitored comics follow the series, not just the chosen issue.** A monitor with an issue number in its title was compared with that number only: the automatic check never found the new issues, which appeared only when searching by hand. The periodic check now also downloads the later issues of the same series; earlier issues, issues of other series and collections stay out. The post chosen in *Add comic* is downloaded as it is, and from there the series carries on by itself.

- **The Health page shows only the sources you actually configured.** If you removed an indexer from the integrations, or switched it off, its row stayed in *Health → Provider status* with the last error, as if it were still in use (for example "mircrew indexerr"). The table now lists only the indexers that are present and enabled and the configured feeds, and the row of a removed source is forgotten: adding it again starts without the old backoff. Nothing to configure.

- **«Search missing» no longer holds the series page.** The button on a series page started the search and left the page waiting for minutes, longer the more episodes were missing. It now starts the same background search as the *Missing* tab: the page shows how far it is, you can cancel it and, when it ends, it lists the results with **Download** without leaving the series. A search started by an external program or from the terminal answers within 90 seconds with the episodes it has checked so far (`complete` says whether it finished) instead of staying open, and a cancelled search marks as searched only the episodes it actually looked at: the deep pass does not skip the others for a day.

- **The outside-profile copy now appears in the manual search too.** When a missing episode has no copy within the quality profile, the list of *Search missing* and of the single episode now shows the copy in the requested language with the «🆘 last resort» badge: it is the one Gextto would take by itself, instead of reading «no results» for an episode it then downloaded. The same holds for a copy only the local archive has: the cycle can take it as a last resort, where it used to stay invisible.

- **A series «in production» that has already broadcast no longer loses its missing episodes.** Gextto treated every series with the status «In Production» or «Planned» as having no gaps, even when an episode had already aired: its missing episodes disappeared from the *Missing* tab. The status now counts only when the series has never broadcast; a known last air date overrides it.

- **Gextto moves to the 1.2 series.** From this build the version is `1.2.<build>` (the first is 1.2.2065); the build number carries on where it was. gx-torrent keeps its own series. The update check compares commit and build, so a 1.1 install sees 1.2 as a normal update.

- **eD2k downloads no longer end up as errors every cycle.** At the start of each search Gextto compares its downloads with the torrent engine and marks the missing ones as removed. It did that with eD2k files too, which live in aMule, not in the torrent engine: on 28 October 38 episodes still downloading in aMule were marked «missing from session». Gextto stopped watching for their completion (the finished file would have stayed in aMule's incoming folder) and proposed them again as missing every cycle. eD2k files are now left to aMule, and the rows already marked go back to the queue by themselves.
- **A file the engine or aMule already has no longer counts as a problem.** The cycle summary said «33 problems (see the warnings above)» with no warning above: they were files already queued. A duplicate is no longer a problem.
- **Fewer repeated pack warnings.** While gx-torrent moves a torrent's data (onto the RAM disk or off it), the pack file priorities cannot change: Gextto wrote a warning every 30 seconds (19 times for a 10-minute move). It now retries quietly.

- **The deep missing-episode search goes round every series.** Each deep pass searches only a few missing episodes online (5 by default). Gextto always took them from the top of the list, so with many unfindable gaps in the first series of the alphabet the series further down waited weeks for their turn. Now the episodes never searched come first, one series at a time and starting after the series served last, then the ones searched longest ago. Each pass reaches new series instead of starting again from the letter A. There is nothing to configure.

- **Last resort for missing episodes.** When the only copy of a missing episode in the right language had a resolution outside the profile, or did not declare one (common with eD2k files), Gextto used to drop it and the gap stayed open. Now it takes that copy as a last resort, only when no copy within the profile exists. Language, subtitles, audio and exclusions always apply. In *Downloads* the download carries the «🆘 last resort» badge, and the log, the download reason and the notification say so too. As soon as a copy within the profile appears, the upgrade replaces it as usual. It is on by default; turn it off in *Settings → Missing episodes → Last resort*. The copy is taken even when the same file also comes from the regular title search (it happens with DDUnlimited).

- **The log says "→ aMule" for eD2k downloads.** When Gextto started an eD2k file (for example from DDUnlimited through indexarr), the "📥 Downloading" log line named the torrent folder, even "RAM disk", although the file goes to aMule, which uses its own folders. The line now says "→ aMule". The download already went to the right place: only the message changes. Nothing to configure.

- **indexarr eD2k releases now download automatically too.** For eD2k links indexarr sends a seed and peer count of zero, because on the eD2k network the indexer cannot count them. Gextto read that as "nobody shares this file" and, in the automatic search, refused every indexarr eD2k release as dead: it could only be downloaded by hand. Now those numbers are ignored for an eD2k link and aMule finds the sources, as with the links Gextto finds by itself. Nothing to configure.

- **eD2k downloads taken from indexarr now show up in its Log → Download.** Gextto used to read the direct `ed2k` link and queue it in aMule without telling indexarr: for indexarr that download never happened, and the eD2k counter stayed at zero. Now, when the result comes from an indexer that also exposes an HTTP download link (indexarr, or Jackett and Prowlarr with the same shape), Gextto opens it once when the file is queued, without making the download wait: indexarr records the download and the source that provided it. The transfer remains the direct `ed2k://` link; the redirect is not followed and a failed note never blocks the download. A direct `ed2k://` link behaves as before.

## 27 October 2026

- **eD2k links now arrive from indexarr too.** If you add an eD2k source in indexarr (for example the DDUnlimited forum), its results now reach Gextto as `ed2k://` links instead of being discarded because they carry no magnet or `.torrent`. Gextto shows them as the eD2k network in search and in the archive, and queues them in aMule when you download, exactly like the ones it finds by itself; the torrent client is not involved. Nothing to configure: the source only has to be enabled in indexarr and the indexarr indexer already among Gextto's sources. With indexarr a search waits up to 45 seconds (instead of the 5 used for other sources) so the eD2k source can open the topic pages; Sonarr, Radarr and the other programs do not get this wait and stay as before.

- **The web search no longer cuts every source together after 15 seconds.** There used to be a single 15-second budget for all the web sources of a search, and it included the wait in the queue: with eight engines and four slots, the second half might never start. Now each engine gets its own 15 seconds from when it starts, so none is skipped just because it was waiting; the answers already received are kept. A single engine slower than 15 seconds is still left behind for that search, but it no longer stops the others and no longer floods the log: the detail goes to the debug log. With more engines than slots the search as a whole can take longer: the second half used to be cut anyway.

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

- **The log reads as sentences in the lines that were still technical too.** About sixty lines still wrote a label followed by fields ("torrent link resolution failed · target: … · source: … · error: …", "managed gx-torrent exited unexpectedly · error: … · restarts: 1"). They now say what happened in one sentence, for example "⚠️ Could not fetch the .torrent of an episode from mircrew: …" or "❌ gx-torrent stopped unexpectedly (…): Gextto restarts it". This covers `.torrent` links that cannot be fetched, manual and eD2k searches, gx-torrent stops and restarts, the IP filter, the FTP test, provider logins, moves to the trash and the RAM disk, and the cleanup of completed downloads and of the release archive. The log stays in English and every line stays linked to the history of its download.

- **gx-torrent no longer restarts when the network disk is slow.** When the download folder is on a network share (NFS, SMB) and the NAS takes more than a minute to confirm a write, gx-torrent mistook the wait for an internal hang and shut down, stopping every download until Gextto restarted it. It now waits for the disk to answer, writes in the log that the storage is slow and, once it answers, that the torrent is running normally again. A real hang is still detected as before.

- **A 4K without seeders is back in play when its swarm revives.** When Gextto gave up on a download because nobody in the swarm had the complete file, the release stayed blocklisted forever: if the 4K came back to life a week later it was never tried again and the 1080p stayed. The block now lasts a configurable period (seven days by default, in *Stalled torrents → Let a release without seeders back in after*; 0 keeps it blocked forever) and when it expires the release is back among the candidates: if it has found seeders in the meantime it is downloaded as an upgrade, while until then the copy in the library stays where it is. On the Blocklist page the new *Expires* column shows the date, or "forever" for the blocks you decide, and the log says when the release can be tried again.

- **qBittorrent 5.3: the engine works again with 5.2 and 5.3 too.** With qBittorrent 5.2 and 5.3 Gextto could no longer reach the Web API (the login answer changed), so the engine looked unreachable; since 5.0 torrents added paused started at once and the per-torrent seed limits were refused. Gextto now understands both generations of the Web API and works with qBittorrent-nox 4.6 and later, including the 5.3 release installed by managed mode. No setting needs to change: update Gextto and restart the service.

- **The indexer test speaks naturally.** The log line is no longer a row of technical fields ("indexer test · kind: … · name: … · host: … · ok: … · status: …"): it is now a sentence, for example "✅ The indexer «indexarr» at 192.168.1.161 answered the test (HTTP 200)", or "⚠️ The indexer «indexarr» … did not answer the test: the service is unreachable" when the service does not answer.

- **indexarr is a manager type of its own.** In the *Integrations → Torznab indexers* list and in the initial setup you can now choose **indexarr** as the type, with the base URL (normally `http://host:8484`) and the API key; it no longer has to pass for Jackett. Gextto uses its native `/api` Torznab endpoint for the aggregate search and the Jackett-compatible paths to read the indexer list and query one tracker at a time, as it already does with Jackett and Prowlarr. The indexer test and the health check use `t=caps` and recognise indexarr, so a wrong API key or URL is reported like for the other managers. Entries already configured as Jackett keep working: to use the native endpoint, change the type to **indexarr**.

## 25 October 2026

- **The library check no longer renames the same episode back and forth.** The periodic name check compared two ways of writing the same audio (`EAC3` and `ddp`) and, depending on which answer arrived first, renamed the file every six hours: two episodes of the same series changed name on every pass and the count said "4 renamed" for two files. The check now keeps the spelling already in the name and renames only when the codec is really different; a file that is already correct is not renamed a second time within the same check. The same applies to HDR (`DV` and `DV HDR10`) and to movies.

- **Fewer repeated lines in the log.** The gx-torrent log no longer repeats "memory released: no active downloads for …" every ten minutes: it writes the line when the idle period starts and then at most every six hours, while still releasing memory every ten minutes. The "Searches need attention" reminder for a source that stays broken arrives at most once every six hours (it was hourly) and the summary of a partly useful pack appears once, not at every re-check. The mircrew login reason, which the service returns in Italian, is now in English in the log and in the notification; the original text stays in the detail.

## 24 October 2026

- **Copies of the same release no longer multiply, and one is a spare for the other.** The same torrent often comes from several sources (feed and search of the same indexer, Jackett, Prowlarr, indexarr), and sites that only give a `.torrent` link made it look like a different release every time. Gextto now merges the copies when they have the same infohash (even when it is declared next to the `.torrent` link or written in base32) and, when the infohash is missing, when name and size match within rounding. When in doubt it does not merge: two different infohashes, or names that differ in group, resolution, language, PROPER or episode, stay separate releases. Of a merged release it uses the copy with the best swarm and keeps the others as spares: when the chosen link fails or is in quarantine, it tries the same release from another source right away instead of waiting for the next cycle. The log shows a line «🔁 … trying the same release from …». In the manual search every torrent appears once.

## 23 October 2026

- **Comics tell what they are doing too, in the log.** The comics check no longer lists technical lines ("comics: candidate posts · posts: 1", "title checked · queued: 0"): it says how many titles it is checking, what it downloads and why («📥 Downloading «… #41 (2026)» — new comic on GetComics»), when a title is already up to date or has nothing new, and when an issue has no download link yet. Errors are sentences with the title and the reason too.

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
- **Numbering: back to `1.1.<build>`, the second number is raised by hand.**
  Gextto no longer moves the second number by itself every 1000 builds: the
  version is `<series>.<build>`, the third number is always the build
  (`1.1.1961`) and the series stays `1.1` until it is deliberately changed in
  `version_series` (for example `1.2.2000`). It also applies to gx-torrent, with
  its own `gx_torrent.version_series`. The build and the commit stay visible at
  the bottom of **Maintenance**.
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
  with a similar name (for example a longer title that starts with the same word) no
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
  the original title for the localized one), so a localized name does not hide
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
