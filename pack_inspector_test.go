package gextto

import (
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
