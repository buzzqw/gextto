// manual_identity.go works out what a torrent that arrived without a release
// is. A magnet pasted in «Aggiungi torrent», an uploaded .torrent or an add
// through the qBittorrent-compatible API carries a payload and no identity:
// without a release Gextto can only leave the finished files where they were
// downloaded, and the series never sees them. The torrent's own name says what
// it is — a scene name is a scene name — and when the name says nothing (a pack
// called «S01») the names of the files it carries do.

package gextto

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// manualSource is the source of a release Gextto worked out by itself from a
// torrent it did not choose.
const manualSource = "manual"

// tevNeedsManualIdentity reports whether a torrent still has no release: a row
// can exist without one (the "do not rename" flag creates it), and a torrent
// added by hand has no row at all.
func tevNeedsManualIdentity(metadata *models.TorrentMeta) bool {
	if metadata == nil {
		return true
	}
	return strings.TrimSpace(metadata.Release.Title) == ""
}

// tevRegisterManualRelease gives an identity to a torrent added by hand and
// returns the release it found, or nil when nothing can be told about it: the
// torrent then stays what it was, a download that ends where it is.
func tevRegisterManualRelease(cfg *Config, torrents TorrentSession, db *Database, event models.TorrentEvent) *models.TorrentMeta {
	if cfg == nil || db == nil || strings.TrimSpace(event.Hash) == "" {
		return nil
	}
	release := tevManualReleaseIdentity(cfg, torrents, event)
	if release == nil {
		logging.Debug("a torrent added by hand says nothing about what it is",
			"hash", event.Hash, "name", event.Name)
		return nil
	}
	if err := db.RegisterTorrentScored(release, cfg.ReleaseScore(release)); err != nil {
		logging.Debug("could not register a torrent added by hand",
			"hash", event.Hash, "name", event.Name, "error", err.Error())
		return nil
	}
	// Only when the torrent has no reason of its own: a free search result
	// recorded as a transfer must keep staying in the download folder.
	if reason, err := db.TorrentReason(event.Hash); err == nil && (reason == nil || strings.TrimSpace(*reason) == "") {
		_ = db.SetTorrentReason(event.Hash, manualReason)
	}
	logging.Info(fmt.Sprintf("🧩 «%s» added by hand, recognised as %s: it goes to the library",
		event.Name, logTarget(release)), "hash", event.Hash)
	return &models.TorrentMeta{Release: *release}
}

// tevManualReleaseIdentity is the identity of a torrent added by hand: from its
// name first, then from the names of the files it carries.
func tevManualReleaseIdentity(cfg *Config, torrents TorrentSession, event models.TorrentEvent) *models.Release {
	if cfg == nil {
		return nil
	}
	magnet := "magnet:?xt=urn:btih:" + strings.ToLower(strings.TrimSpace(event.Hash))
	if parsed := ParseRelease(strings.TrimSpace(event.Name), magnet, manualSource); parsed != nil && manualReleaseUsable(cfg, parsed) {
		return parsed
	}
	return tevManualReleaseFromFiles(cfg, event, magnet)
}

// manualReleaseUsable reports whether a release Gextto worked out by itself is
// one it can file into the library: an episode or a season of a series it
// follows, or a film with the year that names it. Anything else would land in
// the archive root under a name nobody chose.
func manualReleaseUsable(cfg *Config, release *models.Release) bool {
	switch release.Kind {
	case "series":
		if release.Series == nil || release.Season == nil {
			return false
		}
		if release.Episode == nil && !release.IsPack {
			return false
		}
		return cfg.FindSeriesMatch(*release.Series, release.Season) != nil
	case "movie":
		return release.Year != nil
	}
	return false
}

// tevManualReleaseFromFiles reads the identity from the files of a torrent
// whose name says nothing: a pack called «S01» is a season of whatever series
// the episodes inside it name, and a series Gextto does not follow is left
// alone like any other unknown name.
func tevManualReleaseFromFiles(cfg *Config, event models.TorrentEvent, magnet string) *models.Release {
	files, err := VideoFiles(CompletionPath(&event))
	if err != nil || len(files) == 0 {
		return nil
	}
	type packGroup struct {
		series   string
		season   int64
		episodes []int64
		quality  models.Quality
	}
	bySeason := map[string]*packGroup{}
	for _, file := range files {
		parsed := ParseRelease(filepath.Base(file), magnet, manualSource)
		if parsed == nil || parsed.Kind != "series" || parsed.Series == nil || parsed.Season == nil || parsed.Episode == nil {
			continue
		}
		if *parsed.Episode <= 0 {
			continue
		}
		series := cfg.FindSeriesMatch(*parsed.Series, parsed.Season)
		if series == nil {
			continue
		}
		key := fmt.Sprintf("%s|%d", series.Name, *parsed.Season)
		group := bySeason[key]
		if group == nil {
			group = &packGroup{series: series.Name, season: *parsed.Season, quality: parsed.Quality}
			bySeason[key] = group
		}
		group.episodes = append(group.episodes, *parsed.Episode)
	}
	best := (*packGroup)(nil)
	for _, group := range bySeason {
		if best == nil || len(group.episodes) > len(best.episodes) {
			best = group
		}
	}
	if best == nil {
		return nil
	}
	episodes := uniqueSortedInt64(best.episodes)
	if len(episodes) == 0 {
		return nil
	}
	season, first := best.season, episodes[0]
	release := &models.Release{
		Title:        strings.TrimSpace(event.Name),
		Magnet:       magnet,
		Source:       manualSource,
		Kind:         "series",
		Series:       &best.series,
		Season:       &season,
		Episode:      &first,
		EpisodeRange: episodes,
		IsPack:       len(episodes) > 1,
		Quality:      best.quality,
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	if strings.TrimSpace(release.Title) == "" {
		release.Title = fmt.Sprintf("%s S%02d", best.series, best.season)
	}
	return release
}
