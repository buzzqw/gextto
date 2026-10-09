package gextto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

const salvageTestHash = "5a1a5a1a5a1a5a1a5a1a5a1a5a1a5a1a5a1a5a1a"

// salvageSession is a stalled-torrent fake that also lists the torrent files,
// like every real engine does.
type salvageSession struct {
	stallSession
	files []models.FileView
}

func (s *salvageSession) Files(string) ([]models.FileView, bool, error) { return s.files, true, nil }

func salvageVideo(t *testing.T, path string) {
	t.Helper()
	header := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81}
	if err := os.WriteFile(path, append(header, make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func salvageEpisode(t *testing.T, db *Database, episode int64) (archive string, downloaded bool) {
	t.Helper()
	var path, at *string
	err := db.db.QueryRow("SELECT e.archive_path, e.downloaded_at FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name='Show' AND e.season=1 AND e.episode=?1", episode).Scan(&path, &at)
	if err != nil {
		return "", false
	}
	if path != nil {
		archive = *path
	}
	return archive, at != nil
}

// TestMonitorStalledSalvagesCompletePackEpisodes: un pack abbandonato con E01 e
// E02 completi e E03 a metà porta E01 ed E02 in libreria prima della rimozione;
// E03 resta mancante.
func TestMonitorStalledSalvagesCompletePackEpisodes(t *testing.T) {
	db := newTestDB(t)
	root := t.TempDir()
	downloads := filepath.Join(root, "downloads")
	library := filepath.Join(root, "library")
	folder := filepath.Join(downloads, "Show.S01.1080p.WEB-DL.ITA")
	for _, dir := range []string{folder, library} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{"libtorrent_dead_swarm_giveup_min": "4320"}
	cfg.ArchiveRoot = &library
	cfg.CleanupAction = "delete"

	release := ParseRelease("Show.S01.1080p.WEB-DL.ITA", "magnet:?xt=urn:btih:"+salvageTestHash, "test")
	if release == nil || !release.IsPack {
		t.Fatalf("expected a pack release: %+v", release)
	}
	if err := db.RegisterTorrentScored(release, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	// Placeholders as the approval of a pack leaves them: the first episode
	// carries the hash, every one the magnet link.
	for _, episode := range []int64{1, 2, 3} {
		var hash any
		if episode == 1 {
			hash = salvageTestHash
		}
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) SELECT id,1,?1,?2,1000,?3,?4 FROM series WHERE name='Show'",
			episode, release.Title, hash, release.Magnet); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"Show.S01E01.1080p.WEB-DL.ITA.mkv", "Show.S01E02.1080p.WEB-DL.ITA.mkv", "Show.S01E03.1080p.WEB-DL.ITA.mkv", "Show.S01E04.1080p.WEB-DL.ITA.mkv"}
	files := []models.FileView{}
	for index, name := range names {
		salvageVideo(t, filepath.Join(folder, name))
		downloaded := int64(80)
		switch index {
		case 2:
			downloaded = 40
		case 3:
			// Reported complete but never really written (preallocated zeros):
			// the integrity check must keep it out of the library.
			if err := os.WriteFile(filepath.Join(folder, name), make([]byte, 80), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		files = append(files, models.FileView{Path: "Show.S01.1080p.WEB-DL.ITA/" + name, Size: 80, Downloaded: downloaded})
	}
	session := &salvageSession{files: files, stallSession: stallSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{{
		Hash: salvageTestHash, Name: "Show.S01.1080p.WEB-DL.ITA", SavePath: downloads, State: "stalled", Stalled: true,
		HasMetadata: true, Progress: 74, TotalDone: 200, NumPeers: 1, NumComplete: 0,
	}}}}}
	since := time.Now().Add(-73 * time.Hour)
	watch := map[string]StallWatch{salvageTestHash: {lastProgressAt: since, lastDone: 200, stalledSince: &since, nextRetryAt: time.Now().Add(time.Hour)}}

	MonitorStalled(&cfg, session, db, NewNotifier(), watch)

	if len(session.removed) != 1 {
		t.Fatalf("abandoned pack not removed: %v", session.removed)
	}
	for _, episode := range []int64{1, 2} {
		archive, downloaded := salvageEpisode(t, db, episode)
		if !downloaded || !strings.HasPrefix(archive, library) {
			t.Fatalf("E%02d not salvaged: archive=%q downloaded=%v", episode, archive, downloaded)
		}
		if _, err := os.Stat(archive); err != nil {
			t.Fatalf("E%02d library file missing: %v", episode, err)
		}
	}
	for _, episode := range []int64{3, 4} {
		if _, downloaded := salvageEpisode(t, db, episode); downloaded {
			t.Fatalf("E%02d (incomplete or not a real video) must stay missing", episode)
		}
	}
	if entries, _ := os.ReadDir(library); len(entries) != 2 {
		t.Fatalf("library holds %d files, want only E01 and E02", len(entries))
	}
	if status, _ := db.TorrentStatus(salvageTestHash); status == nil || *status != "error" {
		t.Fatalf("abandoned pack status = %v, want error", status)
	}
}

// TestSalvagePackEpisodesKeepsOnlyTheirUpgrades: per gli episodi recuperati
// vale il file nuovo; per quelli non recuperati torna la versione precedente.
func TestSalvagePackEpisodesKeepsOnlyTheirUpgrades(t *testing.T) {
	db := newTestDB(t)
	release := ParseRelease("Show.S01.1080p.WEB-DL.ITA", "magnet:?xt=urn:btih:"+salvageTestHash, "test")
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	backups := []upgradeBackup{}
	for _, episode := range []int64{1, 2} {
		// The upgrade already pointed the row at the pack; the backup keeps the
		// old 720p copy.
		result, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_link,downloaded_at,archive_path) VALUES (?1,1,?2,'pack',1000,?3,NULL,'')",
			seriesID, episode, release.Magnet)
		if err != nil {
			t.Fatal(err)
		}
		rowID, _ := result.LastInsertId()
		season, ep := int64(1), episode
		oldPath, oldAt := "/library/old-720p.mkv", "2026-01-01 10:00:00"
		backups = append(backups, upgradeBackup{Kind: "series", RowID: rowID, SeriesID: &seriesID, Season: &season, Episode: &ep,
			Title: "old", QualityScore: 500, DownloadedAt: &oldAt, ArchivePath: &oldPath, SizeBytes: 10})
	}
	payload, _ := json.Marshal(backups)
	if _, err := db.db.Exec("INSERT INTO upgrade_backup(new_hash,payload_json,created_at) VALUES (?1,?2,datetime('now'))", salvageTestHash, string(payload)); err != nil {
		t.Fatal(err)
	}

	restored, err := db.SalvagePackEpisodes(salvageTestHash, release, []PackEpisode{{Episode: 1, Path: "/library/new-1080p.mkv", SizeBytes: 80, Score: 1000}})
	if err != nil || !restored {
		t.Fatalf("salvage: restored=%v err=%v (E02 had a previous copy)", restored, err)
	}
	if archive, downloaded := salvageEpisode(t, db, 1); !downloaded || archive != "/library/new-1080p.mkv" {
		t.Fatalf("salvaged E01 = %q, want the new file", archive)
	}
	if archive, downloaded := salvageEpisode(t, db, 2); !downloaded || archive != "/library/old-720p.mkv" {
		t.Fatalf("E02 = %q, want the previous copy restored", archive)
	}
	if restored, err := db.RestoreUpgrade(salvageTestHash); err != nil || restored {
		t.Fatalf("the backup must be consumed by the salvage: restored=%v err=%v", restored, err)
	}
}

// TestSalvageSkipsWithoutLibraryFolder: senza cartella della libreria un pack
// finito resterebbe nei download, che l'abbandono cancella: niente recupero.
func TestSalvageSkipsWithoutLibraryFolder(t *testing.T) {
	db := newTestDB(t)
	downloads := t.TempDir()
	folder := filepath.Join(downloads, "Show.S01.1080p.WEB-DL.ITA")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	salvageVideo(t, filepath.Join(folder, "Show.S01E01.1080p.WEB-DL.ITA.mkv"))
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}
	release := ParseRelease("Show.S01.1080p.WEB-DL.ITA", "magnet:?xt=urn:btih:"+salvageTestHash, "test")
	session := &salvageSession{files: []models.FileView{{Path: "Show.S01.1080p.WEB-DL.ITA/Show.S01E01.1080p.WEB-DL.ITA.mkv", Size: 80, Downloaded: 80}}}
	torrent := models.TorrentView{Hash: salvageTestHash, Name: "Show.S01.1080p.WEB-DL.ITA", SavePath: downloads}
	if count, restored := tev_salvagePackEpisodes(&cfg, session, db, torrent, &models.TorrentMeta{Release: *release}); count != 0 || restored {
		t.Fatalf("salvaged %d without a library folder", count)
	}
}
