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
	// 1080p + webdl + h264 + aac + HDR = 1000 + 200 + 50 + 40 + 100
	if quality.Score() != 1390 {
		t.Fatalf("score = %d, want 1390", quality.Score())
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
	if got := quality.ScoreWithSettings(settings); got != quality.Score()+300+60 {
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
	if got, want := h265.ScoreWithSettings(map[string]string{"score_codec_h265": "500"}), int64(590); got != want {
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
	if got, want := fiveOne.ScoreWithSettings(map[string]string{"score_audio_5.1": "77"}), int64(87); got != want {
		t.Fatalf("5.1 score = %d, want %d (codec plus channel bonus)", got, want)
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

func TestAudioDowngradeCannotWinThroughScoreAlone(t *testing.T) {
	old := Quality{Resolution: "1080p", Source: "webdl", Codec: "h265", Audio: "truehd", HDR: "HDR10"}
	worse := old
	worse.Audio = "ac3"
	if got := worse.UpgradeReason(&old, 1400, 1200, 0); got != "" {
		t.Fatalf("audio downgrade reason = %q, want none", got)
	}
	better := Quality{Resolution: "1080p", Source: "webdl", Codec: "h265", Audio: "dts-hd", HDR: "HDR10"}
	betterOld := better
	betterOld.Audio = "ac3"
	if got := better.UpgradeReason(&betterOld, 1250, 1200, 0); got == "" {
		t.Fatal("a higher-ranked audio codec should still be able to win")
	}
}

func TestAudioGroupsAndChannelFactor(t *testing.T) {
	cases := []struct {
		name     string
		quality  Quality
		wantTier int
		want     int64
	}{
		{"TrueHD 5.1", Quality{Audio: "truehd", AudioChannels: "5.1"}, 5, 160},
		{"DTS-HD MA 5.1", Quality{Audio: "dts-hd", AudioChannels: "5.1"}, 5, 160},
		{"DTS-HD HRA 5.1", Quality{Audio: "dts-hd-hra", AudioChannels: "5.1"}, 4, 100},
		{"DTS 5.1", Quality{Audio: "dts", AudioChannels: "5.1"}, 4, 100},
		{"DDP 5.1", Quality{Audio: "ddp", AudioChannels: "5.1"}, 4, 100},
		{"AC3 5.1", Quality{Audio: "ac3", AudioChannels: "5.1"}, 3, 60},
		{"bare 5.1", Quality{Audio: "5.1", AudioChannels: "5.1"}, 3, 60},
		{"AAC 5.1", Quality{Audio: "aac", AudioChannels: "5.1"}, 2, 50},
		{"AAC 2.0", Quality{Audio: "aac", AudioChannels: "2.0"}, 2, 40},
		{"TrueHD 7.1 Atmos", Quality{Audio: "truehd", AudioChannels: "7.1", AudioObjects: "atmos"}, 5, 190},
	}
	for _, tc := range cases {
		if got := tc.quality.AudioTier(); got != tc.wantTier {
			t.Errorf("%s tier = %d, want %d", tc.name, got, tc.wantTier)
		}
		if got := tc.quality.Score(); got != tc.want {
			t.Errorf("%s score = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAudioRankIncludesChannels(t *testing.T) {
	fiveOne := Quality{Audio: "ac3", AudioChannels: "5.1"}
	sevenOne := Quality{Audio: "ac3", AudioChannels: "7.1"}
	if sevenOne.AudioRank() <= fiveOne.AudioRank() {
		t.Fatalf("7.1 rank %d should exceed 5.1 rank %d", sevenOne.AudioRank(), fiveOne.AudioRank())
	}
	lossless := Quality{Audio: "truehd", AudioChannels: "5.1"}
	if lossless.AudioRank() <= sevenOne.AudioRank() {
		t.Fatal("lossless 5.1 should outrank lossy 7.1")
	}
}

func TestGroupedAudioScoreSettings(t *testing.T) {
	trueHD := Quality{Audio: "truehd"}
	if got := trueHD.ScoreWithSettings(map[string]string{"score_audio_lossless": "200"}); got != 200 {
		t.Fatalf("lossless group score = %d, want 200", got)
	}
	dtsHD := Quality{Audio: "dts-hd"}
	if got := dtsHD.ScoreWithSettings(map[string]string{"score_audio_lossless": "200", "score_audio_dts-hd": "120"}); got != 200 {
		t.Fatalf("group must win over legacy key, got %d", got)
	}
	if got := dtsHD.ScoreWithSettings(map[string]string{"score_audio_dts-hd": "120"}); got != 120 {
		t.Fatalf("legacy key fallback = %d, want 120", got)
	}
	ac3 := Quality{Audio: "ac3", AudioChannels: "5.1"}
	if got := ac3.ScoreWithSettings(map[string]string{"score_audio_standard": "70", "score_channels_5.1": "15"}); got != 85 {
		t.Fatalf("group + channel score = %d, want 85", got)
	}
	atmos := Quality{Audio: "truehd", AudioChannels: "7.1", AudioObjects: "atmos"}
	if got := atmos.ScoreWithSettings(map[string]string{"score_audio_objects": "30"}); got != 200 {
		t.Fatalf("group + channels + object score = %d, want 200", got)
	}
}
