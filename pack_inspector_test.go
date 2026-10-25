package gextto

import (
	"database/sql"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

type packInspectEngine struct {
	TorrentEngine
	torrents   []models.TorrentView
	files      map[string][]models.FileView
	removed    map[string]bool
	priorities map[string][]int32
}

func (m *packInspectEngine) List() []models.TorrentView { return m.torrents }

func (m *packInspectEngine) Files(hash string) ([]models.FileView, bool, error) {
	return m.files[hash], true, nil
}

func (m *packInspectEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	if m.removed == nil {
		m.removed = map[string]bool{}
	}
	m.removed[hash] = deleteFiles
	return true, nil
}

func (m *packInspectEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	if m.priorities == nil {
		m.priorities = map[string][]int32{}
	}
	m.priorities[hash] = append([]int32(nil), priorities...)
	return true, nil
}

const packInspectMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

func packInspectFixture(t *testing.T, files []models.FileView) (*Config, *Database, *packInspectEngine, string) {
	t.Helper()
	db := newTestDB(t)
	seriesName := "Show"
	if _, err := db.db.Exec("INSERT INTO series(name, enabled) VALUES(?1, 1)", seriesName); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		t.Fatalf("series id: %v", err)
	}
	// E01 is on disk with a score no 1080p H264 pack can beat.
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,quality_score,archive_path,downloaded_at) VALUES(?1,1,1,4000,'/library/Show - S01E01 - [1080p][h265][AC3 5.1].mkv',datetime('now'))", seriesID); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	season := int64(1)
	episode := int64(1)
	release := models.Release{
		Title:              "Show - Stagione 1 [IN CORSO][02/08]",
		Magnet:             packInspectMagnet,
		Kind:               "series",
		Series:             &seriesName,
		Season:             &season,
		Episode:            &episode,
		IsPack:             true,
		EpisodeRange:       []int64{1, 2, 3},
		InProgressEpisodes: 3,
		Quality:            models.Quality{Resolution: "1080p", Codec: "h264"},
	}
	if err := db.RegisterTorrentScored(&release, release.Quality.Score()); err != nil {
		t.Fatalf("register torrent: %v", err)
	}
	hash := "0123456789abcdef0123456789abcdef01234567"
	engine := &packInspectEngine{
		torrents: []models.TorrentView{{Hash: hash, Name: "Show.S01.1080p", Progress: 10, TotalDone: 1024}},
		files:    map[string][]models.FileView{hash: files},
	}
	cfg := DefaultConfig()
	cfg.DryRun = false
	cfg.Settings = map[string]string{packInspectionSetting: "true"}
	cfg.Series = []SeriesConfig{{Name: seriesName, Enabled: true}}
	return &cfg, db, engine, hash
}

func TestInspectActivePacksRemovesUselessPack(t *testing.T) {
	cfg, db, engine, hash := packInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.mkv"},
	})
	checked := map[string]packInspectionCheck{}
	InspectActivePacks(cfg, engine, db, checked)
	if got, ok := engine.removed[hash]; !ok || !got {
		t.Fatalf("useless pack not removed with its files: %v", engine.removed)
	}
	blocked, err := db.IsBlocklisted(hash)
	if err != nil {
		t.Fatalf("is blocklisted: %v", err)
	}
	if !blocked {
		t.Fatal("useless pack was not blocklisted")
	}
}

func TestInspectActivePacksKeepsPackWithAWantedEpisode(t *testing.T) {
	cfg, db, engine, hash := packInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.mkv"},
		{Path: "Show.S01E03.mkv"}, // not in the library: useful
	})
	checked := map[string]packInspectionCheck{}
	InspectActivePacks(cfg, engine, db, checked)
	if _, removed := engine.removed[hash]; removed {
		t.Fatal("a pack with a wanted episode was removed")
	}
}

func TestInspectActivePacksDownloadsOnlyUsefulFiles(t *testing.T) {
	cfg, db, engine, hash := packInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.mkv", Size: 1000},
		{Path: "Show.S01E03.mkv", Size: 2000}, // not in the library: useful
		{Path: "Show.S01E03.srt", Size: 10},   // sidecar of the useful episode
	})
	cfg.Settings[packInspectionPrioritizeSetting] = "true"
	checked := map[string]packInspectionCheck{}
	InspectActivePacks(cfg, engine, db, checked)
	if _, removed := engine.removed[hash]; removed {
		t.Fatal("a partially useful pack was removed")
	}
	got := engine.priorities[hash]
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 1 {
		t.Fatalf("priorities = %v, want [0 1 1]", got)
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		t.Fatalf("torrent meta: %v", err)
	}
	if len(meta.Release.SkippedEpisodes) != 1 || meta.Release.SkippedEpisodes[0] != 1 {
		t.Fatalf("skipped episodes = %v, want [1]", meta.Release.SkippedEpisodes)
	}
}

func TestInspectActivePacksRestoresUpgradeBackup(t *testing.T) {
	db := newTestDB(t)
	seriesName := "Show"
	if _, err := db.db.Exec("INSERT INTO series(name, enabled) VALUES(?1, 1)", seriesName); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		t.Fatalf("series id: %v", err)
	}
	oldTitle := "Show - S01E01 - [1080p][h265]"
	oldPath := "/library/Show - S01E01 - [1080p][h265].mkv"
	result, err := db.db.Exec(`INSERT INTO episodes(series_id,season,episode,title,quality_score,archive_path,downloaded_at)
		VALUES(?1,1,1,?2,4000,?3,datetime('now'))`, seriesID, oldTitle, oldPath)
	if err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	episodeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("episode id: %v", err)
	}
	previous, err := db.loadSeriesUpgradeBackup(db.db, episodeID, seriesName)
	if err != nil {
		t.Fatalf("load previous row: %v", err)
	}
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := db.saveUpgradeBackup(db.db, hash, previous); err != nil {
		t.Fatalf("save upgrade backup: %v", err)
	}

	season := int64(1)
	episode := int64(0)
	release := models.Release{
		Title:              "Show - Stagione 1",
		Magnet:             packInspectMagnet,
		Kind:               "series",
		Series:             &seriesName,
		Season:             &season,
		Episode:            &episode,
		IsPack:             true,
		EpisodeRange:       []int64{1},
		InProgressEpisodes: 1,
		Quality:            models.Quality{Resolution: "1080p", Codec: "h264"},
	}
	if err := db.RegisterTorrentScored(&release, release.Quality.Score()); err != nil {
		t.Fatalf("register torrent: %v", err)
	}
	// Simulate the row left behind by the discarded release: the new title,
	// score and magnet sit over the old file still on disk.
	if _, err := db.db.Exec("UPDATE episodes SET title=?1, quality_score=1880, magnet_hash=?2, magnet_link=?3 WHERE id=?4",
		release.Title, hash, release.Magnet, episodeID); err != nil {
		t.Fatalf("simulate stale row: %v", err)
	}

	engine := &packInspectEngine{
		torrents: []models.TorrentView{{Hash: hash, Name: "Show.S01.1080p", Progress: 10, TotalDone: 1024}},
		files:    map[string][]models.FileView{hash: {{Path: "Show.S01E01.mkv"}}},
	}
	cfg := DefaultConfig()
	cfg.DryRun = false
	cfg.Settings = map[string]string{packInspectionSetting: "true"}
	cfg.Series = []SeriesConfig{{Name: seriesName, Enabled: true}}
	InspectActivePacks(&cfg, engine, db, map[string]packInspectionCheck{})
	if _, removed := engine.removed[hash]; !removed {
		t.Fatal("the useless pack was not removed")
	}

	var score int64
	var title, archivePath string
	var magnetHash sql.NullString
	if err := db.db.QueryRow("SELECT quality_score, COALESCE(title,''), COALESCE(archive_path,''), magnet_hash FROM episodes WHERE id=?1", episodeID).
		Scan(&score, &title, &archivePath, &magnetHash); err != nil {
		t.Fatalf("read restored episode: %v", err)
	}
	if score != 4000 || title != oldTitle || archivePath != oldPath || magnetHash.Valid {
		t.Fatalf("episode not restored: score=%d title=%q path=%q magnet=%v", score, title, archivePath, magnetHash)
	}
	var backups int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM upgrade_backup WHERE new_hash=?1", hash).Scan(&backups); err != nil {
		t.Fatalf("count backups: %v", err)
	}
	if backups != 0 {
		t.Fatalf("upgrade backup row left behind: %d", backups)
	}
}

func TestInspectActivePacksRespectsTheSetting(t *testing.T) {
	cfg, db, engine, hash := packInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.mkv"},
	})
	cfg.Settings[packInspectionSetting] = "false"
	checked := map[string]packInspectionCheck{}
	InspectActivePacks(cfg, engine, db, checked)
	if _, removed := engine.removed[hash]; removed {
		t.Fatal("inspection ran while the setting was off")
	}
}

func TestSameEpisodeSetIgnoresOrderAndDuplicates(t *testing.T) {
	if !sameEpisodeSet([]int64{3, 1, 2}, []int64{1, 2, 3, 3}) {
		t.Fatal("the same episode set must match")
	}
	if sameEpisodeSet([]int64{1, 2}, []int64{1, 2, 3}) {
		t.Fatal("a different episode set must not match")
	}
	if !sameEpisodeSet(nil, []int64{}) {
		t.Fatal("two empty sets must match")
	}
}
