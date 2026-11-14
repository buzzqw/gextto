package gextto

// content_inspector.go checks the payload of a torrent the cycle started as a
// magnet, once the engine reports its file list. The pre-download guard
// (content_guard.go) reads the `.torrent` the cycle already downloaded; a
// magnet has no file list until the engine has talked to its peers, and that
// moment is early enough: the list arrives before the data, so an unsafe
// payload is removed with almost nothing on disk.
//
// It runs for every series and movie torrent the cycle added, not only for
// packs, and it is not tied to the pack inspection setting: a program in the
// payload is not a matter of taste. A torrent started by hand (or from a
// watched folder) is the operator's choice and is never discarded here.

import (
	"fmt"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	// contentInspectionMetadataWait is how long a torrent whose file list has
	// not arrived yet is skipped before asking the engine again.
	contentInspectionMetadataWait = 30 * time.Second
	// contentInspectionSettled is how long a checked torrent is skipped. The
	// file list of a torrent never changes, so one verdict is enough.
	contentInspectionSettled = 6 * time.Hour
)

// contentInspectionCheck remembers until when a torrent needs no further look.
type contentInspectionCheck struct {
	until time.Time
}

// InspectActiveContent removes the torrents whose payload the guard refuses
// (content_guard.go). checked caches the verdicts so the 750 ms tick does not
// ask the engine for the file list of every settled torrent.
func InspectActiveContent(cfg *Config, torrents TorrentEngine, db *Database, notifier *Notifier, checked map[string]contentInspectionCheck) {
	if cfg == nil || db == nil || torrents == nil || checked == nil || cfg.DryRun {
		return
	}
	now := time.Now()
	list := torrents.List()
	live := make(map[string]struct{}, len(list))
	for _, torrent := range list {
		live[strings.ToLower(torrent.Hash)] = struct{}{}
	}
	for hash := range checked {
		if _, ok := live[strings.ToLower(hash)]; !ok {
			delete(checked, hash)
		}
	}

	for _, torrent := range list {
		if seen, ok := checked[torrent.Hash]; ok && now.Before(seen.until) {
			continue
		}
		settled := contentInspectionCheck{until: now.Add(contentInspectionSettled)}
		if torrent.Progress >= 99.99 {
			// The completion path decides; the import checks the real file.
			checked[torrent.Hash] = settled
			continue
		}
		meta, err := db.TorrentMeta(torrent.Hash)
		if err != nil || meta == nil {
			checked[torrent.Hash] = settled
			continue
		}
		release := meta.Release
		if release.Kind != "series" && release.Kind != "movie" {
			checked[torrent.Hash] = settled
			continue
		}
		if reason, err := db.TorrentReason(torrent.Hash); err == nil && reasonIsManual(reason) {
			checked[torrent.Hash] = settled
			continue
		}
		files, ok, err := torrents.Files(torrent.Hash)
		if err != nil || !ok || len(files) == 0 {
			// Metadata has not arrived yet: ask again later.
			checked[torrent.Hash] = contentInspectionCheck{until: now.Add(contentInspectionMetadataWait)}
			continue
		}
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Path)
		}
		why := unsafePayloadReason(names, release.Kind)
		if why == "" {
			checked[torrent.Hash] = settled
			continue
		}
		discardUnsafePayload(db, torrents, notifier, torrent, release, why)
		delete(checked, torrent.Hash)
	}
}

// discardUnsafePayload removes a torrent whose payload the guard refuses,
// puts back the library row the release replaced when it was approved and
// records the refusal where the next searches read it. The torrent may have
// been added stopped at its metadata (a pack waiting for its file list): the
// row that keeps its announcement is dropped with it, so the pack is never
// started.
func discardUnsafePayload(db *Database, torrents TorrentEngine, notifier *Notifier, torrent models.TorrentView, release models.Release, why string) {
	previous, _ := db.UpgradeReplacedInfo(torrent.Hash)
	restored, restoreErr := db.RestoreUpgrade(torrent.Hash)
	if restoreErr != nil {
		logging.Warn("could not restore the version replaced by a refused payload",
			"hash", torrent.Hash, "name", torrent.Name, "error", restoreErr.Error())
	}
	if !restored {
		if rollbackErr := db.RollbackRelease(&release); rollbackErr != nil {
			logging.Warn("could not roll back a refused payload",
				"hash", torrent.Hash, "name", torrent.Name, "error", rollbackErr.Error())
		}
	}
	_ = db.MarkTorrentError(torrent.Hash, why)
	refuseUnsafePayload(db, &release, time.Now())
	_ = db.ForgetRemovedTorrent(torrent.Hash)
	_ = db.DeletePackFileCheck(torrent.Hash)
	removed, removeErr := torrents.Remove(torrent.Hash, true)
	if removeErr != nil {
		logging.Warn("could not remove a torrent whose payload is refused",
			"hash", torrent.Hash, "error", removeErr.Error())
	}
	fate := fmt.Sprintf("removed with what it had downloaded (%s)", formatBytes(torrent.TotalDone))
	if removeErr != nil || !removed {
		fate = "to be removed"
	}
	logging.Info(fmt.Sprintf("🗑️ «%s» not downloaded: %s. It is %s; the next search will look for another version",
		release.Title, why, fate), "hash", torrent.Hash)
	logging.Debug("refused payload details", "hash", torrent.Hash, "name", torrent.Name,
		"downloaded_bytes", torrent.TotalDone, "previous release", previous)
	if notifier != nil {
		data := map[string]any{
			"hash":             torrent.Hash,
			"name":             torrent.Name,
			"title":            release.Title,
			"kind":             release.Kind,
			"downloaded_bytes": torrent.TotalDone,
			"reason":           why,
		}
		if release.Series != nil {
			data["series"] = *release.Series
		}
		if release.Season != nil {
			data["season"] = *release.Season
		}
		if err := notifier.NotifyEvent("download_discarded", data); err != nil {
			logging.Warn("discard notification failed", "hash", torrent.Hash, "error", err)
		}
	}
}
