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
		{"webrip to webdl", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac"}, "source"},
		{"hdr", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", HDR: "HDR10"}, "hdr"},
		{"repack", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", IsRepack: true}, "repack"},
		{"proper", Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "aac", IsProper: true}, "proper"},
	}
	for _, tc := range cases {
		old := base
		if tc.name == "hdtv to webdl" {
			old.Source = "hdtv"
		}
		if tc.name == "webrip to webdl" {
			old.Source = "webrip"
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
	if got := italian.ScoreWithSettings(map[string]string{"score_bonus_ita": "500"}); got != italian.Score() {
		t.Fatalf("legacy Italian-language bonus changed the score: %d != %d", got, italian.Score())
	}
}

func TestCustomReleaseGroupScoreUsesConfiguredGroupName(t *testing.T) {
	quality := Quality{Group: "tbk"}
	base := quality.Score()
	got := quality.ScoreWithSettings(map[string]string{"score_group_tbk": "125"})
	if got != base+125 {
		t.Fatalf("custom group score = %d, want base %d + 125", got, base)
	}
}

func TestStandardBonusOverridesPreserveBaseScore(t *testing.T) {
	quality := Quality{IsDV: true, IsProper: true, IsRepack: true, IsReal: true, HDR: "HDR10"}
	settings := map[string]string{
		"score_bonus_dv":     "300",
		"score_bonus_hdr":    "100",
		"score_bonus_proper": "75",
		"score_bonus_repack": "100",
		"score_bonus_real":   "100",
	}
	if got := quality.ScoreWithSettings(settings); got != quality.Score() {
		t.Fatalf("default overrides changed the score: %d != %d", got, quality.Score())
	}
}

func TestScoreSettingsDoNotApplyDTSModifierToDTSHD(t *testing.T) {
	quality := Quality{Audio: "dts-hd"}
	if got, want := quality.ScoreWithSettings(map[string]string{"score_audio_dts": "0"}), quality.Score(); got != want {
		t.Fatalf("DTS modifier affected DTS-HD: got %d, want %d", got, want)
	}
	if got, want := quality.ScoreWithSettings(map[string]string{"score_audio_dts-hd": "200"}), int64(200); got != want {
		t.Fatalf("DTS-HD modifier = %d, want %d", got, want)
	}
}

func TestScoreSettingsUseOneKeyPerToken(t *testing.T) {
	// Legacy alias keys (x265, eac3, ...) are no longer read: each parsed token
	// has exactly one configurable key.
	h265 := Quality{Codec: "h265", Audio: "ddp"}
	if got, want := h265.ScoreWithSettings(map[string]string{"score_codec_h265": "500"}), int64(580); got != want {
		t.Fatalf("canonical codec score = %d, want %d", got, want)
	}
	if got, want := h265.ScoreWithSettings(map[string]string{"score_codec_x265": "999"}), h265.Score(); got != want {
		t.Fatalf("legacy codec alias affected the score: %d != %d", got, want)
	}
	if got, want := h265.ScoreWithSettings(map[string]string{"score_audio_eac3": "999"}), h265.Score(); got != want {
		t.Fatalf("legacy audio alias affected the score: %d != %d", got, want)
	}
	// AC3 and 5.1 are distinct tokens with their own key.
	ac3 := Quality{Audio: "ac3"}
	if got, want := ac3.ScoreWithSettings(map[string]string{"score_audio_ac3": "111"}), int64(111); got != want {
		t.Fatalf("ac3 score = %d, want %d", got, want)
	}
	fiveOne := Quality{Audio: "5.1"}
	if got, want := fiveOne.ScoreWithSettings(map[string]string{"score_audio_5.1": "77"}), int64(77); got != want {
		t.Fatalf("5.1 score = %d, want %d", got, want)
	}
}

func TestUnknownGroupDoesNotBehaveAsGlobalGroup(t *testing.T) {
	quality := Quality{Group: "unknown"}
	if got, want := quality.ScoreWithSettings(map[string]string{"score_group_unknown": "999"}), quality.Score(); got != want {
		t.Fatalf("unknown group modifier = %d, want %d", got, want)
	}
}

func TestDolbyVisionAndHdrAreAdditive(t *testing.T) {
	// A Dolby Vision release carries HDR="DV": both bonuses apply.
	quality := Quality{HDR: "DV", IsDV: true}
	if got, want := quality.Score(), int64(400); got != want {
		t.Fatalf("Dolby Vision + HDR base score = %d, want %d", got, want)
	}
	if got, want := quality.ScoreWithSettings(map[string]string{"score_bonus_hdr": "150"}), int64(450); got != want {
		t.Fatalf("HDR override score = %d, want %d", got, want)
	}
}

func TestConfiguredScoreBreakdownSumsToConfiguredScore(t *testing.T) {
	quality := Quality{Resolution: "1080p", Source: "webdl", Codec: "h265", Audio: "dts-hd", Group: "tbk"}
	settings := map[string]string{"score_res_1080p": "900", "score_audio_dts-hd": "200", "score_group_tbk": "75"}
	var sum int64
	for _, item := range quality.ScoreBreakdownWithSettings(settings) {
		sum += item.Value
	}
	if got := quality.ScoreWithSettings(settings); sum != got {
		t.Fatalf("configured breakdown %d != score %d", sum, got)
	}
}
