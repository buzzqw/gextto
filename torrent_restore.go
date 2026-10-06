package gextto

// Recovery of downloads lost by an unclean stop. The embedded libtorrent
// session is persisted through its resume files; a download added after the
// last save and before a crash or a forced kill exists only in torrent_meta.
// At start-up those downloads are added again from their magnet, in the folder
// that already holds their data, so libtorrent checks and reuses it.

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// maxRestoredTorrents bounds one start-up recovery, so a damaged database can
// never flood the session.
const maxRestoredTorrents = 50

// missingTorrent is an active download recorded in the database but absent
// from the torrent session.
type missingTorrent struct {
	Hash    string
	Release models.Release
}

// missingActiveTorrents lists the downloads the database still considers active
// (not completed, failed or removed) that the session does not know.
func (d *Database) missingActiveTorrents(live map[string]struct{}) ([]missingTorrent, error) {
	rows, err := d.db.Query("SELECT hash FROM torrent_meta WHERE status NOT IN ('completed','error','removed') AND removed_at IS NULL")
	if err != nil {
		return nil, err
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := live[strings.ToLower(hash)]; !ok {
			hashes = append(hashes, strings.ToLower(hash))
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	missing := make([]missingTorrent, 0, len(hashes))
	for _, hash := range hashes {
		meta, err := d.TorrentMeta(hash)
		if err != nil || meta == nil || !strings.HasPrefix(strings.TrimSpace(meta.Release.Magnet), "magnet:") {
			continue
		}
		missing = append(missing, missingTorrent{Hash: hash, Release: meta.Release})
	}
	return missing, nil
}

// magnetDisplayName returns the dn= parameter of a magnet link.
func magnetDisplayName(magnet string) string {
	parsed, err := url.Parse(strings.TrimSpace(magnet))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("dn"))
}

// existingDataFolder finds the folder that already holds a download's data: the
// first candidate containing an entry named like the torrent. An empty result
// means no data was found, and the engine picks its usual folder.
func existingDataFolder(name string, candidates []string) string {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return ""
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return dir
		}
	}
	return ""
}

// restoreMissingTorrents adds back the active downloads missing from the
// embedded session. It runs once at start-up, before the workers: the first
// cycle would otherwise mark them as missing and stop tracking them.
func restoreMissingTorrents(cfg *Config, db *Database, engine TorrentEngine) int {
	if cfg == nil || db == nil || engine == nil || cfg.DryRun {
		return 0
	}
	if _, embedded := engine.(embeddedEngine); !embedded {
		// External clients (qBittorrent) keep their own torrents across restarts.
		return 0
	}
	snapshot := engine.List()
	live := make(map[string]struct{}, len(snapshot))
	folders := []string{}
	seenFolder := map[string]struct{}{}
	addFolder := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if _, ok := seenFolder[dir]; ok {
			return
		}
		seenFolder[dir] = struct{}{}
		folders = append(folders, dir)
	}
	for _, torrent := range snapshot {
		live[strings.ToLower(torrent.Hash)] = struct{}{}
	}
	if dir := cfg.RamdiskDir(); dir != nil {
		addFolder(*dir)
	}
	if cfg.LibtorrentTempDir != nil {
		addFolder(*cfg.LibtorrentTempDir)
	}
	addFolder(cfg.LibtorrentDir)
	for _, torrent := range snapshot {
		addFolder(torrent.SavePath)
	}
	missing, err := db.missingActiveTorrents(live)
	if err != nil {
		logging.Warn("could not look for downloads missing from the torrent engine", "error", err)
		return 0
	}
	restored := 0
	for _, item := range missing {
		if restored >= maxRestoredTorrents {
			break
		}
		release := item.Release
		candidates := append([]string{}, folders...)
		if dir, ok := DownloadDirFor(&release, cfg); ok {
			candidates = append([]string{dir}, candidates...)
		}
		folder := existingDataFolder(magnetDisplayName(release.Magnet), candidates)
		var preferred *string
		if folder != "" {
			preferred = &folder
		}
		added, err := engine.AddWithPath(release.Magnet, cfg, preferred)
		if err != nil || !added {
			logging.Warn(fmt.Sprintf("⚠️ %s was lost from the torrent engine and could not be added back", logTarget(&release)),
				"error", err)
			continue
		}
		restored++
		if folder != "" {
			logging.Info(fmt.Sprintf("♻️ %s had been lost by an unclean stop; added back, and the data already in %s will be checked and reused",
				logTarget(&release), folder))
		} else {
			logging.Info(fmt.Sprintf("♻️ %s had been lost by an unclean stop; added back (no earlier data found, it starts again)",
				logTarget(&release)))
		}
	}
	return restored
}
