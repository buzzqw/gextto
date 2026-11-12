package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// crossnet.go makes a torrent and an eD2k download of the same file work
// together, the way Shareaza mixed its networks. The two are paired when they
// are the same episode (or movie) and the eD2k file has exactly the size of
// one file of the torrent. Then:
//
//   - the torrent takes from aMule's .part file every piece that passes its
//     own SHA-1 check (gx-torrent's external merge: a wrong byte adds nothing),
//     each time aMule has moved on and when it finishes;
//   - when the torrent finishes first, the eD2k copy is not needed and is
//     removed from aMule;
//   - when eD2k finishes first, its file completes the torrent, which imports
//     the episode once and keeps seeding it; the eD2k completion is not
//     imported a second time;
//   - a torrent stuck without progress is looked for on eD2k, and a result of
//     the exact same size is started beside it.
//
// The pairs Gextto starts itself are kept in crossnet_pairs.json, because
// their eD2k side is not a download of its own in the database.

// crossnetSetting turns the cooperation on (default on).
const crossnetSetting = settingTorrentEd2kTogether

const (
	// crossnetMergeStep is how much aMule must have added before the torrent
	// reads the .part file again; crossnetMergeEvery the least gap between
	// two reads (a merge pauses the torrent's fetches for its duration).
	crossnetMergeStep  = 32 << 20
	crossnetMergeEvery = 5 * time.Minute
	// crossnetStuckAfter is how long a torrent must make no progress before
	// eD2k is searched for the same file; crossnetSearchEvery the gap
	// between two searches for one torrent.
	crossnetStuckAfter  = 30 * time.Minute
	crossnetSearchEvery = 6 * time.Hour
	crossnetFile        = "crossnet_pairs.json"
)

// externalMerger is an engine that adopts verified pieces of one of its
// files from a file outside its session (gx-torrent).
type externalMerger interface {
	MergeExternal(hash string, fileIndex int, path string) (bool, error)
}

// crossnetPair is one torrent and one eD2k download of the same file.
type crossnetPair struct {
	Ed2kHash    string `json:"ed2k_hash"`
	TorrentHash string `json:"torrent_hash"`
	FileIndex   int    `json:"file_index"`
	Size        int64  `json:"size"`
	Label       string `json:"label"`
	// Started marks a pair whose eD2k side Gextto started for a stuck torrent.
	Started bool `json:"started,omitempty"`

	lastDone  int64
	lastMerge time.Time
	completed bool // the finished eD2k file was handed to the torrent
}

type crossnetState struct {
	mu         sync.Mutex
	loaded     bool
	pairs      map[string]*crossnetPair // by eD2k hash
	files      map[string][]models.FileView
	stuckSince map[string]time.Time
	lastDone   map[string]int64
	searchedAt map[string]time.Time
	searching  bool
	tempDir    string
	tempAt     time.Time
}

var crossnet = crossnetState{
	pairs:      map[string]*crossnetPair{},
	files:      map[string][]models.FileView{},
	stuckSince: map[string]time.Time{},
	lastDone:   map[string]int64{},
	searchedAt: map[string]time.Time{},
}

// crossnetEnabled reports whether torrents and eD2k work together.
func crossnetEnabled(cfg *Config, engine TorrentEngine) bool {
	if cfg == nil || !ed2kEnabled(cfg) || !settingsBool(cfg, crossnetSetting, true) {
		return false
	}
	_, ok := engine.(externalMerger)
	return ok
}

// crossnetKey identifies the content of a release across networks: an
// episode by series, season and episode; a movie by kind and year (the exact
// size does the rest). Packs are left out: their files do not map one to one.
func crossnetKey(release *models.Release) string {
	if release == nil || release.IsPack {
		return ""
	}
	switch release.Kind {
	case "series":
		if release.Series == nil || release.Season == nil || release.Episode == nil {
			return ""
		}
		return fmt.Sprintf("s|%s|%d|%d", strings.ToLower(strings.TrimSpace(*release.Series)), *release.Season, *release.Episode)
	case "movie":
		year := int64(0)
		if release.Year != nil {
			year = *release.Year
		}
		return fmt.Sprintf("m|%d", year)
	}
	return ""
}

func crossnetPath(cfg *Config) string { return filepath.Join(cfg.DataDir, crossnetFile) }

// crossnetLoadLocked reads the pairs kept from the last run, once.
func crossnetLoadLocked(cfg *Config) {
	if crossnet.loaded {
		return
	}
	crossnet.loaded = true
	raw, err := os.ReadFile(crossnetPath(cfg))
	if err != nil {
		return
	}
	var pairs []*crossnetPair
	if json.Unmarshal(raw, &pairs) != nil {
		return
	}
	for _, p := range pairs {
		if p != nil && p.Ed2kHash != "" && p.TorrentHash != "" {
			crossnet.pairs[strings.ToLower(p.Ed2kHash)] = p
		}
	}
}

// crossnetSaveLocked keeps the pairs Gextto started (the others are found
// again from the database at every round).
func crossnetSaveLocked(cfg *Config) {
	var keep []*crossnetPair
	for _, p := range crossnet.pairs {
		if p.Started {
			keep = append(keep, p)
		}
	}
	path := crossnetPath(cfg)
	if len(keep) == 0 {
		_ = os.Remove(path)
		return
	}
	raw, err := json.MarshalIndent(keep, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// crossnetTempDir is aMule's temporary folder, read every ten minutes.
func crossnetTempDir(cfg *Config) string {
	crossnet.mu.Lock()
	dir, at := crossnet.tempDir, crossnet.tempAt
	crossnet.mu.Unlock()
	if dir != "" && time.Since(at) < 10*time.Minute {
		return dir
	}
	var prefs amule.Preferences
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		prefs, callErr = client.Preferences(ctx)
		return callErr
	}); err != nil {
		return dir
	}
	crossnet.mu.Lock()
	crossnet.tempDir, crossnet.tempAt = prefs.TempDir, time.Now()
	crossnet.mu.Unlock()
	return prefs.TempDir
}

// crossnetLocalPath finds a path of aMule's filesystem on this one: the same
// path (a bind mount at the same place, or aMule on this host) or, for a
// containerised aMule run by the same user, through its /proc root.
func crossnetLocalPath(path string) string {
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return path
	}
	for _, pid := range amuleProcessPIDs() {
		candidate := filepath.Join("/proc", strconv.Itoa(pid), "root", path)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// crossnetFiles returns a torrent's files, cached while it downloads.
func crossnetFiles(engine TorrentEngine, hash string) []models.FileView {
	crossnet.mu.Lock()
	files, ok := crossnet.files[hash]
	crossnet.mu.Unlock()
	if ok {
		return files
	}
	files, _, err := engine.Files(hash)
	if err != nil || len(files) == 0 {
		return nil
	}
	crossnet.mu.Lock()
	crossnet.files[hash] = files
	crossnet.mu.Unlock()
	return files
}

// crossnetFileIndex is the index of the torrent file of exactly size bytes
// that is a video, or -1.
func crossnetFileIndex(files []models.FileView, size int64) int {
	for i, f := range files {
		if f.Size == size && isVideoExtension(f.Path) {
			return i
		}
	}
	return -1
}

// crossnetTorrentDone reports a torrent that has all its data.
func crossnetTorrentDone(view models.TorrentView) bool {
	return view.Progress >= 100 || view.IsSeeding
}

// crossnetLabel names the content for the log.
func crossnetLabel(release *models.Release, fallback string) string {
	if release != nil {
		if label := logTarget(release); label != "" {
			return label
		}
	}
	return "«" + fallback + "»"
}

// crossnetTorrentsByKey groups the torrents by the content they hold.
func crossnetTorrentsByKey(db *Database, torrents map[string]models.TorrentView) map[string][]models.TorrentView {
	byKey := map[string][]models.TorrentView{}
	for hash, view := range torrents {
		meta, err := db.TorrentMeta(hash)
		if err != nil || meta == nil {
			continue
		}
		if key := crossnetKey(&meta.Release); key != "" {
			byKey[key] = append(byKey[key], view)
		}
	}
	return byKey
}

// crossnetPairOne pairs one eD2k file (hash, size) with a torrent of the
// same content holding a video file of exactly that size, or returns nil.
func crossnetPairOne(db *Database, engine TorrentEngine, byKey map[string][]models.TorrentView, hash string, size int64, name string) *crossnetPair {
	if size <= 0 {
		return nil
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return nil
	}
	key := crossnetKey(&meta.Release)
	if key == "" {
		return nil
	}
	for _, view := range byKey[key] {
		index := crossnetFileIndex(crossnetFiles(engine, view.Hash), size)
		if index < 0 {
			continue
		}
		pair := &crossnetPair{Ed2kHash: hash, TorrentHash: strings.ToLower(view.Hash), FileIndex: index, Size: size,
			Label: crossnetLabel(&meta.Release, name)}
		crossnet.mu.Lock()
		crossnet.pairs[hash] = pair
		crossnet.mu.Unlock()
		logging.Info(fmt.Sprintf("🤝 %s is downloading on both networks: the torrent and eD2k now work on the same file", pair.Label))
		return pair
	}
	return nil
}

// crossnetPairs pairs the eD2k downloads with the torrents of the same file:
// the kept pairs first, then those the database reveals.
func crossnetPairs(cfg *Config, db *Database, engine TorrentEngine, downloads []amule.Download, torrents map[string]models.TorrentView) {
	byKey := crossnetTorrentsByKey(db, torrents)
	crossnet.mu.Lock()
	known := map[string]bool{}
	for hash := range crossnet.pairs {
		known[hash] = true
	}
	crossnet.mu.Unlock()
	for _, download := range downloads {
		hash := strings.ToLower(download.Hash)
		if known[hash] || download.Complete() {
			continue
		}
		crossnetPairOne(db, engine, byKey, hash, download.Size, download.Name)
	}
}

// crossnetTick is one round of the cooperation, every minute from the event
// worker.
func crossnetTick(cfg *Config, db *Database, engine TorrentEngine, searcher *Engine, now time.Time) {
	if !crossnetEnabled(cfg, engine) {
		return
	}
	merger := engine.(externalMerger)
	crossnet.mu.Lock()
	crossnetLoadLocked(cfg)
	crossnet.mu.Unlock()
	downloads := Ed2kDownloads(cfg)
	torrents := map[string]models.TorrentView{}
	for _, view := range SessionSnapshot(engine) {
		torrents[strings.ToLower(view.Hash)] = view
	}
	crossnetPairs(cfg, db, engine, downloads, torrents)
	byHash := map[string]amule.Download{}
	for _, download := range downloads {
		byHash[strings.ToLower(download.Hash)] = download
	}
	crossnet.mu.Lock()
	pairs := make([]*crossnetPair, 0, len(crossnet.pairs))
	for _, p := range crossnet.pairs {
		pairs = append(pairs, p)
	}
	crossnet.mu.Unlock()
	changed := false
	for _, pair := range pairs {
		view, torrentLive := torrents[pair.TorrentHash]
		download, ed2kLive := byHash[pair.Ed2kHash]
		switch {
		case !torrentLive:
			// The torrent is gone (removed or failed): eD2k goes on alone and
			// its completion is imported as usual — unless Gextto started it
			// only to help the torrent, then it is not wanted any more.
			if pair.Started && ed2kLive && !download.Complete() {
				crossnetRemoveEd2k(cfg, db, pair, "the torrent it was helping is gone")
			}
			crossnetForget(pair)
			changed = true
		case crossnetTorrentDone(view):
			if ed2kLive && !download.Complete() {
				crossnetRemoveEd2k(cfg, db, pair, "the torrent finished first")
				logging.Info(fmt.Sprintf("✅ %s: the torrent finished first, so its eD2k copy was removed from aMule", pair.Label))
			} else if ed2kLive || pair.completed {
				_ = db.MarkEd2kAbsorbed(pair.Ed2kHash)
			}
			crossnetForget(pair)
			changed = true
		case !ed2kLive:
			// aMule 3.x moves a finished file out of the queue into its shared
			// files; a file in neither was cancelled by hand.
			if path := crossnetSharedPath(cfg, pair.Ed2kHash); path != "" {
				if !pair.completed {
					crossnetMergeComplete(cfg, db, merger, pair, path)
				}
				continue
			}
			crossnetForget(pair)
			changed = true
		case download.Complete():
			if !pair.completed {
				crossnetMergeComplete(cfg, db, merger, pair, crossnetCompletedPath(cfg, download))
			}
		default:
			crossnetMergePartial(cfg, merger, pair, download, now)
		}
	}
	if changed {
		crossnet.mu.Lock()
		crossnetSaveLocked(cfg)
		crossnet.mu.Unlock()
	}
	crossnetFindStuck(cfg, db, engine, searcher, torrents, now)
}

func crossnetForget(pair *crossnetPair) {
	crossnet.mu.Lock()
	delete(crossnet.pairs, pair.Ed2kHash)
	delete(crossnet.files, pair.TorrentHash)
	crossnet.mu.Unlock()
}

// crossnetRemoveEd2k cancels the eD2k side of a pair in aMule.
func crossnetRemoveEd2k(cfg *Config, db *Database, pair *crossnetPair, why string) {
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.Delete(ctx, pair.Ed2kHash)
	}); err != nil {
		logging.Warn(fmt.Sprintf("%s: the eD2k copy could not be removed from aMule (%s): %v", pair.Label, why, err))
		return
	}
	ed2kForgetQueue()
	_ = db.MarkEd2kAbsorbed(pair.Ed2kHash)
}

// crossnetMergePartial lets the torrent read aMule's .part file when aMule
// has added enough since the last read.
func crossnetMergePartial(cfg *Config, merger externalMerger, pair *crossnetPair, download amule.Download, now time.Time) {
	if download.Done-pair.lastDone < crossnetMergeStep || now.Sub(pair.lastMerge) < crossnetMergeEvery {
		return
	}
	part := download.PartFile()
	dir := crossnetTempDir(cfg)
	if part == "" || dir == "" {
		return
	}
	path := crossnetLocalPath(filepath.Join(dir, part))
	if path == "" {
		return
	}
	pair.lastDone, pair.lastMerge = download.Done, now
	if _, err := merger.MergeExternal(pair.TorrentHash, pair.FileIndex, path); err != nil {
		logging.Debug("external merge from aMule refused", "torrent", pair.TorrentHash, "error", err)
		return
	}
	logging.Debug("torrent reads the eD2k part file", "label", pair.Label, "ed2k_done", download.Done)
}

// crossnetMergeComplete hands the finished eD2k file to the torrent.
func crossnetMergeComplete(cfg *Config, db *Database, merger externalMerger, pair *crossnetPair, path string) {
	path = crossnetLocalPath(path)
	if path == "" {
		return
	}
	if _, err := merger.MergeExternal(pair.TorrentHash, pair.FileIndex, path); err != nil {
		logging.Debug("external merge of the finished eD2k file refused", "torrent", pair.TorrentHash, "error", err)
		return
	}
	pair.completed = true
	_ = db.MarkEd2kAbsorbed(pair.Ed2kHash)
	logging.Info(fmt.Sprintf("🤝 %s: eD2k finished first and its file completes the torrent, which imports the episode and keeps sharing it", pair.Label))
}

// crossnetSharedPath is where aMule shares a finished file of hash, or "".
func crossnetSharedPath(cfg *Config, hash string) string {
	var shared []amule.SharedFile
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		shared, callErr = client.SharedFiles(ctx)
		return callErr
	}); err != nil {
		return ""
	}
	for _, file := range shared {
		if strings.EqualFold(file.Hash, hash) && file.Dir != "" && file.Name != "" && !strings.HasSuffix(file.Dir, ".met") {
			return crossnetLocalPath(filepath.Join(file.Dir, filepath.Base(file.Name)))
		}
	}
	return ""
}

// crossnetCompletedPath is where aMule put a finished file, on this host.
func crossnetCompletedPath(cfg *Config, download amule.Download) string {
	incoming := ed2kIncomingDir(cfg)
	name := filepath.Base(strings.TrimSpace(download.Name))
	if incoming == "" || name == "" || name == "." {
		return ""
	}
	return crossnetLocalPath(filepath.Join(incoming, name))
}

// crossnetAbsorbs reports whether an eD2k completion belongs to a pair whose
// torrent takes the file: then the completion is not imported on its own
// (the torrent imports it once). It hands the file to the torrent at once.
func crossnetAbsorbs(cfg *Config, db *Database, engine TorrentEngine, event models.TorrentEvent) bool {
	if event.Kind != "torrent_finished" || !crossnetEnabled(cfg, engine) {
		return false
	}
	hash := strings.ToLower(event.Hash)
	crossnet.mu.Lock()
	crossnetLoadLocked(cfg)
	pair := crossnet.pairs[hash]
	crossnet.mu.Unlock()
	path := filepath.Join(event.SavePath, filepath.Base(event.Name))
	snapshot := SessionSnapshot(engine)
	if pair == nil {
		// Finished before a round paired it: pair it now, from the file.
		info, err := os.Stat(crossnetLocalPath(path))
		if err != nil {
			return false
		}
		torrents := map[string]models.TorrentView{}
		for _, v := range snapshot {
			if !crossnetTorrentDone(v) {
				torrents[strings.ToLower(v.Hash)] = v
			}
		}
		if pair = crossnetPairOne(db, engine, crossnetTorrentsByKey(db, torrents), hash, info.Size(), event.Name); pair == nil {
			return false
		}
	}
	var view *models.TorrentView
	for _, v := range snapshot {
		if strings.EqualFold(v.Hash, pair.TorrentHash) {
			v := v
			view = &v
			break
		}
	}
	if view == nil {
		// The torrent is gone: the eD2k file is imported as usual.
		crossnetForget(pair)
		return false
	}
	if !crossnetTorrentDone(*view) && !pair.completed {
		crossnetMergeComplete(cfg, db, engine.(externalMerger), pair, path)
		if !pair.completed {
			// The torrent could not take it: import the eD2k file as usual.
			return false
		}
	}
	_ = db.MarkEd2kAbsorbed(hash)
	return true
}

// crossnetFindStuck looks for the same file on eD2k for a torrent that has
// not moved for crossnetStuckAfter, one search at a time, and starts a result
// of exactly the size of the torrent's video file beside it.
func crossnetFindStuck(cfg *Config, db *Database, engine TorrentEngine, searcher *Engine, torrents map[string]models.TorrentView, now time.Time) {
	if searcher == nil || !ed2kFallbackEligible(cfg) {
		return
	}
	paired := map[string]bool{}
	crossnet.mu.Lock()
	for _, p := range crossnet.pairs {
		paired[p.TorrentHash] = true
	}
	var candidate string
	for hash, view := range torrents {
		done := int64(view.TotalDone)
		if crossnetTorrentDone(view) || paired[hash] {
			delete(crossnet.stuckSince, hash)
			continue
		}
		if done != crossnet.lastDone[hash] || view.DownloadRate > 0 {
			crossnet.lastDone[hash] = done
			crossnet.stuckSince[hash] = now
			continue
		}
		since, ok := crossnet.stuckSince[hash]
		if !ok {
			crossnet.stuckSince[hash] = now
			continue
		}
		if now.Sub(since) >= crossnetStuckAfter && now.Sub(crossnet.searchedAt[hash]) >= crossnetSearchEvery && candidate == "" {
			candidate = hash
		}
	}
	for hash := range crossnet.stuckSince {
		if _, live := torrents[hash]; !live {
			delete(crossnet.stuckSince, hash)
			delete(crossnet.lastDone, hash)
		}
	}
	if candidate == "" || crossnet.searching {
		crossnet.mu.Unlock()
		return
	}
	crossnet.searching = true
	crossnet.searchedAt[candidate] = now
	crossnet.mu.Unlock()
	go func() {
		defer func() {
			crossnet.mu.Lock()
			crossnet.searching = false
			crossnet.mu.Unlock()
		}()
		crossnetSearchFor(cfg, db, engine, searcher, candidate)
	}()
}

// crossnetSearchFor searches eD2k for the episode of a stuck torrent and
// starts the result whose size is exactly that of the torrent's video file.
func crossnetSearchFor(cfg *Config, db *Database, engine TorrentEngine, searcher *Engine, hash string) {
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		return
	}
	release := meta.Release
	if crossnetKey(&release) == "" || release.Kind != "series" {
		return
	}
	series := cfg.FindSeriesMatch(*release.Series, release.Season)
	if series == nil {
		return
	}
	files := crossnetFiles(engine, hash)
	if len(files) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	results := searcher.SearchSeriesED2K(ctx, cfg, series, *release.Season, *release.Episode, false)
	for i := range results {
		link, err := ed2k.ParseFileLink(results[i].Ed2kLink)
		if err != nil {
			continue
		}
		index := crossnetFileIndex(files, link.Size)
		if index < 0 {
			continue
		}
		added, err := addEd2kRelease(cfg, &results[i])
		if err != nil {
			logging.Warn(fmt.Sprintf("%s: the same file was found on eD2k but could not be started: %v", logTarget(&release), err))
			return
		}
		pair := &crossnetPair{Ed2kHash: strings.ToLower(link.Hash), TorrentHash: strings.ToLower(hash), FileIndex: index,
			Size: link.Size, Label: logTarget(&release), Started: true}
		crossnet.mu.Lock()
		crossnet.pairs[pair.Ed2kHash] = pair
		crossnetSaveLocked(cfg)
		crossnet.mu.Unlock()
		if added {
			logging.Info(fmt.Sprintf("🤝 %s has not moved for %s: the same file was found on eD2k and now downloads on both networks", pair.Label, logDuration(crossnetStuckAfter)))
		}
		return
	}
	logging.Debug("no eD2k copy of the stuck torrent's file", "torrent", hash, "results", len(results))
}
