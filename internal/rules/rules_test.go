package rules

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func release(resolution string, sizeMB int64) *models.Release {
	return &models.Release{
		Title:     "Show.S01E01." + resolution + ".WEB-DL",
		Quality:   models.Quality{Resolution: resolution},
		Kind:      "series",
		SizeBytes: sizeMB * 1_048_576,
		Seeders:   -1,
		Peers:     -1,
	}
}

func TestHardFloorRejectsOnlyObviousFakes(t *testing.T) {
	if DeniedReason(release("1080p", 1500)) != "" {
		t.Fatal("healthy 1080p rejected")
	}
	if DeniedReason(release("1080p", 40)) == "" {
		t.Fatal("absurd 1080p accepted")
	}
	if DeniedReason(release("2160p", 200)) == "" {
		t.Fatal("absurd 2160p accepted")
	}
	unknown := release("1080p", 1500)
	unknown.SizeBytes = 0
	if DeniedReason(unknown) != "" {
		t.Fatal("unknown size rejected")
	}
}

func TestAdaptiveFloorNeverExcludesHealthyEpisodes(t *testing.T) {
	if EffectiveFloorMB("2160p", nil) != HardFloorMB("2160p") {
		t.Fatal("no history should use the hard floor")
	}
	large := int64(6440)
	if EffectiveFloorMB("2160p", &large) != SaneFloorMB("2160p") {
		t.Fatal("floor should not be raised")
	}
	small := int64(1000)
	if EffectiveFloorMB("2160p", &small) != 500 {
		t.Fatal("floor should be lowered to half")
	}
}

func TestHardcodedSubtitlesRejected(t *testing.T) {
	r := release("1080p", 1500)
	r.Quality.HardcodedSubs = true
	if DeniedReason(r) != "hardcoded subtitles" {
		t.Fatalf("reason = %q", DeniedReason(r))
	}
}

func TestSizeBonusCapped(t *testing.T) {
	if SizeScoreBonus(release("1080p", 100)) != 0 {
		t.Fatal("below sane floor should not bonus")
	}
	if SizeScoreBonus(release("1080p", 220)) != 1 {
		t.Fatal("expected +1")
	}
	if SizeScoreBonus(release("1080p", 100_000)) != 100 {
		t.Fatal("bonus should cap at 100")
	}
}
