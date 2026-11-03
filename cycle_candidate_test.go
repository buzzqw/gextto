package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// The archive index of a series is cached per series name, while the series
// match depends on the season. A first candidate from a season the series
// does not follow used to leave an empty index in the cache, and the
// candidates of the followed seasons were then judged as if nothing were on
// disk.
func TestArchiveIndexIsNotCachedForAnUnfollowedSeason(t *testing.T) {
	cfg := &Config{
		DataDir: t.TempDir(),
		Series:  []SeriesConfig{{Name: "Series A", Enabled: true, IgnoredSeasons: []int64{1}, ArchivePath: t.TempDir()}},
	}
	pass := &candidatePass{
		cfg:               cfg,
		emptyArchiveIndex: &models.ArchiveQualityIndex{},
		archiveIndexCache: map[string]*models.ArchiveQualityIndex{},
		movieArchiveCache: map[string]*models.ArchiveQuality{},
	}
	name := "Series A"
	season1, season2 := int64(1), int64(2)

	pass.archiveFor(&models.Release{Kind: "series", Series: &name, Season: &season1})
	if _, cached := pass.archiveIndexCache[name]; cached {
		t.Fatal("a candidate from an ignored season cached the index of the whole series")
	}
	pass.archiveFor(&models.Release{Kind: "series", Series: &name, Season: &season2})
	if _, cached := pass.archiveIndexCache[name]; !cached {
		t.Fatal("the index of a followed season was not cached")
	}
}
