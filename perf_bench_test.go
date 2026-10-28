package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// Benchmarks for the paths the second performance pass touches. They exist to
// give a before/after figure for the changes recorded in docs/measurements.md;
// none of them asserts a threshold, so a slower machine does not fail a build.

// benchSeriesConfig builds a configuration with `count` monitored series, each
// with one alias, the shape a real library has.
func benchSeriesConfig(count int) *Config {
	cfg := DefaultConfig()
	cfg.Series = make([]SeriesConfig, count)
	for index := 0; index < count; index++ {
		cfg.Series[index] = SeriesConfig{
			Name:    fmt.Sprintf("Monitored Series %04d", index),
			Aliases: []string{fmt.Sprintf("Alias %04d", index)},
			Seasons: "1+",
			Enabled: true,
		}
	}
	return &cfg
}

// BenchmarkFindSeriesMatchUnmatched measures the case that dominates a cycle:
// a release nobody monitors, which runs the tolerant second pass over every
// configured series and alias.
func BenchmarkFindSeriesMatchUnmatched(b *testing.B) {
	cfg := benchSeriesConfig(500)
	season := int64(1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if series := cfg.FindSeriesMatch("Some Unknown Show 2026 1080p WEB-DL", &season); series != nil {
			b.Fatalf("unexpected match: %s", series.Name)
		}
	}
}

// BenchmarkFindSeriesMatchMatched measures the same scan when the release is
// monitored but its parsed name carries a year, so the exact pass misses and
// the tolerant pass has to reach the right series (worst case: the last one).
func BenchmarkFindSeriesMatchMatched(b *testing.B) {
	cfg := benchSeriesConfig(500)
	season := int64(3)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		series := cfg.FindSeriesMatch("Monitored Series 0499 2026", &season)
		if series == nil {
			b.Fatal("monitored release not matched")
		}
	}
}

// benchDatabase opens a state database in the benchmark's temporary directory.
func benchDatabase(b *testing.B) *Database {
	b.Helper()
	db, err := OpenDatabase(filepath.Join(b.TempDir(), "gextto.db"))
	if err != nil {
		b.Fatalf("OpenDatabase: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

// BenchmarkRescore measures one full re-score of the tracked torrents. The cost
// is three autocommit UPDATEs per torrent plus the final passes over the
// episodes and movies tables.
func BenchmarkRescore(b *testing.B) {
	db := benchDatabase(b)
	cfg := DefaultConfig()
	const torrents = 2000
	year := int64(2026)
	for index := 0; index < torrents; index++ {
		release := models.Release{
			Title:        fmt.Sprintf("Show %04d S01E01 1080p WEB-DL", index),
			Magnet:       fmt.Sprintf("magnet:?xt=urn:btih:%040x", index+1),
			Source:       "bench",
			Kind:         "series",
			Year:         &year,
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		}
		if err := db.RegisterTorrentScored(&release, release.Quality.Score()); err != nil {
			b.Fatalf("RegisterTorrentScored: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := db.Rescore(&cfg); err != nil {
			b.Fatalf("Rescore: %v", err)
		}
	}
}

// BenchmarkArchiveGaps measures one gap calculation: it reads the episode table
// several times, plus the series metadata, status, ignored and forced rows.
func BenchmarkArchiveGaps(b *testing.B) {
	db := benchDatabase(b)
	const seriesCount, episodesPerSeason = 50, 40
	for series := 0; series < seriesCount; series++ {
		name := fmt.Sprintf("Series %02d", series)
		for season := int64(1); season <= 2; season++ {
			for episode := int64(1); episode <= episodesPerSeason; episode++ {
				if err := db.SyncArchiveFileScored(name, season, episode,
					fmt.Sprintf("%s S%02dE%02d", name, season, episode),
					fmt.Sprintf("/library/%s/S%02dE%02d.mkv", name, season, episode), 1024, 1000); err != nil {
					b.Fatalf("SyncArchiveFileScored: %v", err)
				}
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		gaps, err := db.ArchiveGaps()
		if err != nil {
			b.Fatalf("ArchiveGaps: %v", err)
		}
		if len(gaps) != 0 {
			b.Fatalf("gaps = %d, want none", len(gaps))
		}
	}
}

// BenchmarkSyncArchiveFileScored measures recording one archived library file:
// four statements, each one its own transaction, plus the stat of the present
// copy when a worse score arrives for an existing episode.
func BenchmarkSyncArchiveFileScored(b *testing.B) {
	dir := b.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "gextto.db"))
	if err != nil {
		b.Fatalf("OpenDatabase: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	// The file exists on disk, so the "keep the better copy" branch stats it.
	path := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		if err := db.SyncArchiveFileScored("Bench Series", 1, int64(index%500+1),
			"Bench Series S01E01 1080p WEB-DL", path, 1024, 1000); err != nil {
			b.Fatalf("SyncArchiveFileScored: %v", err)
		}
	}
}

// BenchmarkEngineListSnapshot measures one read of the torrent snapshot the UI
// and the periodic worker take: a copy of every row plus a sort by hash.
func BenchmarkEngineListSnapshot(b *testing.B) {
	const torrents = 2000
	e := &gxTorrentEngine{cache: make(map[string]models.TorrentView, torrents)}
	for index := 0; index < torrents; index++ {
		hash := fmt.Sprintf("%040x", index+1)
		e.cache[hash] = models.TorrentView{
			Hash: hash, Name: fmt.Sprintf("Torrent %04d", index), State: "downloading",
			TotalDone: 1024, TotalSize: 2048, NumPeers: 12, NumSeeds: 30,
			Collections: []string{"collection"},
		}
	}
	e.settings.PollInterval = time.Hour
	e.lastAttempt = time.Now() // no daemon round-trip
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if views := e.List(); len(views) != torrents {
			b.Fatalf("List() = %d views, want %d", len(views), torrents)
		}
	}
}

// BenchmarkArchiveReadDuringFullScan measures a short read (the row counter the
// pages and the API ask for) while a full scan of the archive is running. With
// one connection the counter waited for the scan to finish; with the second
// read handle it answers immediately.
func BenchmarkArchiveReadDuringFullScan(b *testing.B) {
	dir := b.TempDir()
	archive, err := OpenArchive(filepath.Join(dir, "gextto_archive.db"))
	if err != nil {
		b.Fatalf("OpenArchive: %v", err)
	}
	b.Cleanup(func() { _ = archive.Close() })
	const rows = 50000
	releases := make([]models.Release, 0, 1000)
	year := int64(2026)
	for index := 0; index < rows; index++ {
		releases = append(releases, models.Release{
			Title:        fmt.Sprintf("Release %06d 1080p WEB-DL ita eng", index),
			Magnet:       fmt.Sprintf("magnet:?xt=urn:btih:%040x", index+1),
			Source:       "bench",
			Kind:         "movie",
			Year:         &year,
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		})
		if len(releases) == cap(releases) {
			if err := archive.SaveBatch(releases, &Config{}); err != nil {
				b.Fatalf("SaveBatch: %v", err)
			}
			releases = releases[:0]
		}
	}
	if len(releases) > 0 {
		if err := archive.SaveBatch(releases, &Config{}); err != nil {
			b.Fatalf("SaveBatch: %v", err)
		}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	var stopOnce sync.Once
	shutdown := func() {
		stopOnce.Do(func() { close(stop) })
		<-done
	}
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// A full scan with a filter that matches nothing: it reads and
			// filters every title, which is what the content-filter page does.
			if _, err := archive.BrowseContentFilteredPage("", []string{"[non-latino]"}, 1, 100); err != nil {
				return
			}
		}
	}()
	b.Cleanup(shutdown)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := archive.Count(); err != nil {
			b.Fatalf("Count: %v", err)
		}
	}
	b.StopTimer()
	shutdown()
}
