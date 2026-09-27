package models

import "testing"

func TestQualityScoreBreakdownSumsToTotal(t *testing.T) {
	quality := Quality{
		Resolution: "1080p",
		Source:     "webdl",
		Codec:      "h264",
		Audio:      "aac",
		HDR:        "HDR10",
	}
	var total int64
	for _, item := range quality.ScoreBreakdown() {
		total += item.Value
	}
	if total != quality.Score() {
		t.Fatalf("breakdown %d != score %d", total, quality.Score())
	}
	// 1080p + webdl + h264 + aac + HDR = 1000 + 200 + 50 + 30 + 100
	if quality.Score() != 1380 {
		t.Fatalf("score = %d, want 1380", quality.Score())
	}
}

func TestUpgradeReasonPortsLegacyRules(t *testing.T) {
	base := Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac"}
	cases := []struct {
		name string
		q    Quality
		want string
	}{
		{"resolution jump", Quality{Resolution: "2160p", Source: "webdl", Codec: "h264", Audio: "aac"}, "resolution"},
		{"hdtv to webdl", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac"}, "source"},
		{"hdr", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", HDR: "HDR10"}, "hdr"},
		{"repack", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", IsRepack: true}, "repack"},
	}
	for _, tc := range cases {
		old := base
		if tc.name == "hdtv to webdl" {
			old.Source = "hdtv"
		}
		got := tc.q.UpgradeReason(&old, tc.q.Score(), old.Score(), 200)
		if got != tc.want {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.want)
		}
	}
	// Same quality, small delta is not an upgrade.
	if got := base.UpgradeReason(&base, base.Score()+50, base.Score(), 200); got != "" {
		t.Errorf("small delta reason = %q, want none", got)
	}
	// Large delta is a score upgrade.
	if got := base.UpgradeReason(&base, base.Score()+500, base.Score(), 200); got != "score" {
		t.Errorf("large delta reason = %q, want score", got)
	}
}

func TestMissingArchivedSourceDoesNotCreateUpgrade(t *testing.T) {
	archived := Quality{Resolution: "1080p", Source: "unknown", Codec: "h264", Audio: "ddp"}
	candidate := archived
	candidate.Source = "webrip"
	if got := candidate.UpgradeReason(&archived, candidate.Score(), archived.Score(), 200); got != "" {
		t.Fatalf("reason = %q, want none", got)
	}
}

func TestScoreWithSettingsOverrides(t *testing.T) {
	quality := Quality{Codec: "h265", Audio: "aac"}
	settings := map[string]string{"score_codec_h265": "500", "score_audio_aac": "100"}
	if got := quality.ScoreWithSettings(settings); got != quality.Score()+300+70 {
		t.Fatalf("score with settings = %d", got)
	}
}

func TestRemuxWinsAtEqualScore(t *testing.T) {
	webdl := Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac"}
	remux := webdl
	remux.Source = "remux"
	if got := remux.UpgradeReason(&webdl, remux.Score(), webdl.Score(), 200); got != "remux" {
		t.Fatalf("reason = %q, want remux", got)
	}
}

func TestLanguageDoesNotChangeQualityScore(t *testing.T) {
	italian := Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", Language: "ita", IsIta: true}
	english := italian
	english.Language = "eng"
	english.IsIta = false
	if italian.Score() != english.Score() {
		t.Fatalf("language changed the score: %d != %d", italian.Score(), english.Score())
	}
}

func TestStandardBonusOverridesPreserveBaseScore(t *testing.T) {
	quality := Quality{IsDV: true, IsProper: true, IsRepack: true, IsReal: true, HDR: "HDR10"}
	settings := map[string]string{
		"score_bonus_dv":     "300",
		"score_bonus_hdr":    "100",
		"score_bonus_proper": "75",
		"score_bonus_repack": "50",
		"score_bonus_real":   "100",
	}
	if got := quality.ScoreWithSettings(settings); got != quality.Score() {
		t.Fatalf("default overrides changed the score: %d != %d", got, quality.Score())
	}
}
