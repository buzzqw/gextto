package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// An indexer that does not report seeders must not be punished: many public and
// RSS sources never populate the swarm counters and rely on the -1 sentinel.
func TestHealthAdjustmentLeavesUnknownSwarmNeutral(t *testing.T) {
	release := models.Release{Seeders: -1, Peers: -1}
	if got := healthAdjustment(&release); got != 0 {
		t.Fatalf("unknown swarm adjustment = %d, want 0", got)
	}
}

func TestHealthAdjustmentPenalizesEmptySwarm(t *testing.T) {
	dead := models.Release{Seeders: 0, Peers: 0}
	// The penalty must clear the upgrade margin, so the release cannot win a
	// slot over a viable alternative.
	if got := healthAdjustment(&dead); got > -DefaultUpgradeMinScoreDiff {
		t.Fatalf("empty swarm adjustment = %d, want at least -%d", got, DefaultUpgradeMinScoreDiff)
	}
	// 0 seeders with peers is not dead: leechers still complete each other, so
	// the discount stays mild (below the upgrade margin).
	if got := healthAdjustment(&models.Release{Seeders: 0, Peers: 12}); got >= 0 || got <= -DefaultUpgradeMinScoreDiff {
		t.Fatalf("seedless-with-peers adjustment = %d, want a mild penalty", got)
	}
}

func TestHealthAdjustmentBonusIsCapped(t *testing.T) {
	if got := healthAdjustment(&models.Release{Seeders: 3, Peers: 1}); got <= 0 || got > 150 {
		t.Fatalf("healthy swarm adjustment = %d, want a small positive bonus", got)
	}
	if got := healthAdjustment(&models.Release{Seeders: 1_000_000, Peers: 5}); got != 150 {
		t.Fatalf("huge swarm adjustment = %d, want it capped at 150", got)
	}
}

// The doc's scenario: an empty swarm with a slightly higher raw score must lose
// to a live alternative at the same quality.
func TestDeadSwarmLosesToHealthyAlternative(t *testing.T) {
	cfg := DefaultConfig()
	dead := models.Release{
		Quality:   models.Quality{Resolution: "2160p"},
		SizeBytes: 40 << 30, // the larger size gives it a slightly higher raw score
		Seeders:   0,
		Peers:     0,
	}
	healthy := models.Release{
		Quality: models.Quality{Resolution: "2160p"},
		Seeders: 40,
		Peers:   5,
	}
	if cfg.ReleaseScore(&dead) < cfg.ReleaseScore(&healthy) {
		t.Fatalf("test setup wrong: dead raw score %d is not above healthy %d",
			cfg.ReleaseScore(&dead), cfg.ReleaseScore(&healthy))
	}
	if cfg.AcquisitionScore(&dead) >= cfg.AcquisitionScore(&healthy) {
		t.Fatalf("dead swarm scored %d, healthy scored %d; the live swarm must win",
			cfg.AcquisitionScore(&dead), cfg.AcquisitionScore(&healthy))
	}
}

func benchmarkSwarmRelease() models.Release {
	return models.Release{
		Quality:   models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h265", Audio: "ddp"},
		SizeBytes: 4 << 30,
		Seeders:   25,
		Peers:     3,
	}
}

func BenchmarkReleaseScore(b *testing.B) {
	cfg := DefaultConfig()
	release := benchmarkSwarmRelease()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cfg.ReleaseScore(&release)
	}
}

func BenchmarkAcquisitionScore(b *testing.B) {
	cfg := DefaultConfig()
	release := benchmarkSwarmRelease()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cfg.AcquisitionScore(&release)
	}
}

// Quality stays sovereign: the health bonus is capped below the upgrade margin,
// so a dead 4K still beats a healthy 1080p.
func TestQualityStillBeatsSeeders(t *testing.T) {
	cfg := DefaultConfig()
	uhd := models.Release{Quality: models.Quality{Resolution: "2160p"}, Seeders: 0, Peers: 0}
	fhd := models.Release{Quality: models.Quality{Resolution: "1080p"}, Seeders: 5000, Peers: 500}
	if cfg.AcquisitionScore(&uhd) <= cfg.AcquisitionScore(&fhd) {
		t.Fatalf("dead 4K scored %d, healthy 1080p scored %d; quality must win",
			cfg.AcquisitionScore(&uhd), cfg.AcquisitionScore(&fhd))
	}
}
