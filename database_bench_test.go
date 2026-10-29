package gextto

import (
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// The cost of registering a started download. It runs once per download, not
// per cycle or per request, so the unit benchmarks below only exist to keep the
// figures in docs/measurements.md repeatable: the registration of a release
// that has already been downloaded also clears the completion of the previous
// attempt (beginNewDownloadAttempt).

const benchRegisterHash = "00112233445566778899aabbccddeeff00112233"

func benchRegisterRelease() *models.Release {
	return &models.Release{
		Title:  "Bench.Release.2026.1080p.WEB.H264",
		Magnet: "magnet:?xt=urn:btih:" + benchRegisterHash,
		Kind:   "movie",
	}
}

func benchRegisterDB(b *testing.B) *Database {
	b.Helper()
	db, err := OpenDatabase(filepath.Join(b.TempDir(), "gextto.db"))
	if err != nil {
		b.Fatalf("OpenDatabase: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func BenchmarkRegisterTorrentScoredFirstAdd(b *testing.B) {
	db := benchRegisterDB(b)
	release := benchRegisterRelease()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.RegisterTorrentScored(release, 1000); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegisterTorrentScoredRepeatQueued(b *testing.B) {
	db := benchRegisterDB(b)
	release := benchRegisterRelease()
	if err := db.RegisterTorrentScored(release, 1000); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.RegisterTorrentScored(release, 1000); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRegisterTorrentScoredAfterCompletion is the re-download path: the
// row is completed and its archived copy is gone, so the completion is
// cleared. MarkTorrentCompleted is outside the timed region.
func BenchmarkRegisterTorrentScoredAfterCompletion(b *testing.B) {
	db := benchRegisterDB(b)
	release := benchRegisterRelease()
	if err := db.RegisterTorrentScored(release, 1000); err != nil {
		b.Fatal(err)
	}
	gone := filepath.Join(b.TempDir(), "film", "gone.mkv")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := db.MarkTorrentCompleted(benchRegisterHash, gone, 4096); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := db.RegisterTorrentScored(release, 1000); err != nil {
			b.Fatal(err)
		}
	}
}
