package gextto

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestUpgradeImpactCountsOnlyRealReplacements(t *testing.T) {
	db := newTestDB(t)
	archive := openTestArchive(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	for episode, title := range map[int64]string{
		1: "Show.S01E01.720p.WEB-DL.H264-OLD",
		2: "Show.S01E02.1080p.WEB-DL.H264-OLD",
	} {
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (?1,1,?2,?3,0,'2024-01-01T00:00:00+00:00',?4)", seriesID, episode, title, "/nas/"+title+".mkv"); err != nil {
			t.Fatal(err)
		}
	}
	seen := []string{
		"Show.S01E01.1080p.WEB-DL.H264-NEW", // real upgrade of a 720p copy
		"Show.S01E02.1080p.WEB-DL.H264-NEW", // same quality: no replacement
		"Show.S01E03.1080p.WEB-DL.H264-NEW", // not archived: a gap, not an upgrade
	}
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Show", Seasons: "*", Enabled: true}}
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	for index, title := range seen {
		release := ParseRelease(title, "magnet:?xt=urn:btih:"+string(rune('a'+index))+"000000000000000000000000000000000000000", "test")
		release.DiscoveredAt = time.Now().UTC()
		if err := archive.SaveBatch([]models.Release{*release}, &cfg); err != nil {
			t.Fatal(err)
		}
	}
	report, err := UpgradeImpact(&cfg, db, archive)
	if err != nil {
		t.Fatal(err)
	}
	if report.Series != 1 || report.Releases != 3 || report.Episodes != 1 || len(report.Items) != 1 || report.Items[0].Target != "Show S01E01" {
		t.Fatalf("report = %+v", report)
	}
	// Nothing was written: the 720p row is still the archived copy.
	var title string
	if err := db.db.QueryRow("SELECT title FROM episodes WHERE series_id=?1 AND episode=1", seriesID).Scan(&title); err != nil || title != "Show.S01E01.720p.WEB-DL.H264-OLD" {
		t.Fatalf("dry-run wrote the episode row: %q %v", title, err)
	}
	// «Niente upgrade» series are not counted.
	cfg.Series[0].DisableUpgrades = true
	if report, _ := UpgradeImpact(&cfg, db, archive); report.Episodes != 0 {
		t.Fatalf("a no-upgrade series must not count: %+v", report)
	}
}
