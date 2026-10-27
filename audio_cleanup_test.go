package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
)

// --- probing ---------------------------------------------------------------

func TestParseFfprobeKeepsAudioTracks(t *testing.T) {
	raw := `{
		"streams": [
			{"index":0,"codec_type":"video","codec_name":"hevc","start_time":"0.000000","width":3840,"height":2160},
			{"index":1,"codec_type":"audio","codec_name":"eac3","channels":6,"bit_rate":"640000","start_time":"0.000000","tags":{"language":"ita","title":"Italiano 5.1"},"disposition":{"default":1}},
			{"index":2,"codec_type":"audio","codec_name":"ac3","channels":6,"bit_rate":"448000","start_time":"0.500000","tags":{"language":"eng","title":"English"},"disposition":{"default":0}},
			{"index":3,"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"ita"}}
		]
	}`
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatal(err)
	}
	info := ParseFfprobe(root)
	if len(info.AudioTracks) != 2 {
		t.Fatalf("audio tracks = %d, want 2", len(info.AudioTracks))
	}
	first := info.AudioTracks[0]
	if first.Language != "ita" || first.Channels != 6 || first.Bitrate != 640000 || !first.Default {
		t.Errorf("first track parsed wrong: %+v", first)
	}
	if !info.AudioTracks[1].HasStart || info.AudioTracks[1].StartTime != 0.5 {
		t.Errorf("start time not parsed: %+v", info.AudioTracks[1])
	}
	if len(info.Streams) != 4 || info.Streams[2].Type != "audio" {
		t.Errorf("stream list not parsed: %+v", info.Streams)
	}
}

// --- decision --------------------------------------------------------------

func audioTrack(index int, language, codec string, channels, bitrate int64, commentary bool) AudioTrackInfo {
	return AudioTrackInfo{Index: index, Language: language, Codec: codec, Channels: channels, Bitrate: bitrate, Commentary: commentary}
}

func TestPlanAudioCleanup(t *testing.T) {
	video := StreamInfo{Index: 0, Type: "video", Codec: "hevc"}
	base := MediaInfo{
		Container: "matroska",
		Streams:   []StreamInfo{video},
	}
	cases := []struct {
		name    string
		tracks  []AudioTrackInfo
		policy  string
		keep    []int
		drop    []int
		skipped bool
	}{
		{name: "single track", tracks: []AudioTrackInfo{audioTrack(1, "ita", "aac", 2, 0, false)}, skipped: true},
		{name: "no italian", tracks: []AudioTrackInfo{audioTrack(1, "eng", "aac", 2, 0, false), audioTrack(2, "fra", "aac", 2, 0, false)}, skipped: true},
		{name: "only italian", tracks: []AudioTrackInfo{audioTrack(1, "ita", "aac", 2, 0, false), audioTrack(2, "ita", "ac3", 6, 0, false)}, policy: audioCleanupPolicyAll, skipped: true},
		{
			name: "ita and eng",
			tracks: []AudioTrackInfo{
				audioTrack(1, "ita", "eac3", 6, 640000, false),
				audioTrack(2, "eng", "eac3", 6, 768000, false),
			},
			policy: audioCleanupPolicyAll, keep: []int{0}, drop: []int{1},
		},
		{
			name: "untagged removed when italian exists",
			tracks: []AudioTrackInfo{
				audioTrack(1, "ita", "aac", 2, 0, false),
				audioTrack(2, "eng", "aac", 2, 0, false),
				audioTrack(3, "", "aac", 2, 0, false),
			},
			policy: audioCleanupPolicyAll, keep: []int{0}, drop: []int{1, 2},
		},
		{
			name: "best prefers channels over commentary",
			tracks: []AudioTrackInfo{
				audioTrack(1, "ita", "aac", 2, 128000, true),
				audioTrack(2, "ita", "eac3", 6, 640000, false),
				audioTrack(3, "eng", "eac3", 8, 768000, false),
			},
			policy: audioCleanupPolicyBest, keep: []int{1}, drop: []int{0, 2},
		},
		{
			name: "first keeps the first italian",
			tracks: []AudioTrackInfo{
				audioTrack(1, "eng", "eac3", 6, 640000, false),
				audioTrack(2, "ita", "ac3", 6, 448000, false),
				audioTrack(3, "ita", "eac3", 6, 640000, false),
			},
			policy: audioCleanupPolicyFirst, keep: []int{1}, drop: []int{0, 2},
		},
		{
			name: "language codes are equivalent",
			tracks: []AudioTrackInfo{
				audioTrack(1, "it-IT", "aac", 2, 0, false),
				audioTrack(2, "eng", "aac", 2, 0, false),
			},
			policy: audioCleanupPolicyAll, keep: []int{0}, drop: []int{1},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			info := base
			info.AudioTracks = testCase.tracks
			for _, track := range testCase.tracks {
				info.Streams = append(info.Streams, StreamInfo{Index: track.Index, Type: "audio", Codec: track.Codec})
			}
			plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita", Policy: testCase.policy})
			if plan.Skipped() != testCase.skipped {
				t.Fatalf("skipped = %v (%s), want %v", plan.Skipped(), plan.SkipReason, testCase.skipped)
			}
			if testCase.skipped {
				return
			}
			if fmt.Sprint(plan.KeepPositions) != fmt.Sprint(testCase.keep) {
				t.Errorf("keep = %v, want %v", plan.KeepPositions, testCase.keep)
			}
			if testCase.drop != nil && fmt.Sprint(plan.DropPositions) != fmt.Sprint(testCase.drop) {
				t.Errorf("drop = %v, want %v", plan.DropPositions, testCase.drop)
			}
		})
	}
}

func TestPlanAudioCleanupNeedsTwoTracks(t *testing.T) {
	info := MediaInfo{
		AudioTracks: []AudioTrackInfo{audioTrack(1, "ita", "aac", 2, 0, false)},
		Streams:     []StreamInfo{{Index: 0, Type: "video", Codec: "h264"}},
	}
	plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita"})
	if !plan.Skipped() || !strings.Contains(plan.SkipReason, "single audio track") {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

// --- command construction --------------------------------------------------

func TestAudioCleanupFFmpegCommandCopiesAndDropsByPosition(t *testing.T) {
	plan := AudioCleanupPlan{
		Info: MediaInfo{AudioTracks: []AudioTrackInfo{
			audioTrack(1, "ita", "eac3", 6, 0, false),
			audioTrack(2, "eng", "eac3", 6, 0, false),
			audioTrack(3, "eng", "ac3", 6, 0, false),
		}},
		KeepPositions: []int{0},
		DropPositions: []int{1, 2},
	}
	argv, err := audioCleanupCommand("ffmpeg", "in.mkv", "out.mkv", plan)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	for _, required := range []string{"-c copy", "-map -0:a:1", "-map -0:a:2", "-map 0:v?", "-map 0:t?"} {
		if !strings.Contains(joined, required) {
			t.Errorf("command misses %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, "-map 0:a:2") {
		t.Errorf("command maps a dropped track: %s", joined)
	}
}

// --- scores ----------------------------------------------------------------

func TestAudioCleanupLanguagesDoNotChangeTheScore(t *testing.T) {
	settings := map[string]string{}
	withBoth := models.Quality{
		Resolution: "2160p", Source: "webdl", Codec: "h265", Audio: "eac3",
		AudioChannels: "5.1", HDR: "HDR10", Languages: []string{"ita", "eng"},
	}
	onlyItalian := withBoth
	onlyItalian.Languages = []string{"ita"}
	if ScoreQuality(&withBoth, settings) != ScoreQuality(&onlyItalian, settings) {
		t.Fatal("the quality score must not depend on the language list")
	}
}

func TestAudioCleanupSizeCannotTriggerReDownload(t *testing.T) {
	settings := map[string]string{}
	archived := models.Quality{
		Resolution: "2160p", Source: "webdl", Codec: "h265", Audio: "eac3", AudioChannels: "5.1",
	}
	candidate := archived

	// The archived copy was larger before the cleanup: the size bonus is the
	// only score component that can move, and it is capped at 100.
	small := &models.Release{Quality: archived, SizeBytes: 15 << 30}
	large := &models.Release{Quality: candidate, SizeBytes: 40 << 30}
	bonusDelta := rules.SizeScoreBonus(large) - rules.SizeScoreBonus(small)
	if bonusDelta > 100 {
		t.Fatalf("size bonus delta = %d, want <= 100", bonusDelta)
	}

	// Even if the candidate's total score were 100 points higher, the same
	// technical quality gives no upgrade reason.
	oldScore := int64(3000)
	newScore := oldScore + bonusDelta
	if qualityUpgradeAllowed(&candidate, &archived, &candidate, newScore, oldScore, 200, settings) {
		t.Fatal("a same-quality, larger file must not replace a cleaned copy")
	}
}

// --- integration -----------------------------------------------------------

type testAudioTrack struct {
	language string
	title    string
	delay    float64
}

func requireAudioTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// makeAudioCleanupFixture writes a short video with the requested audio tracks.
func makeAudioCleanupFixture(t *testing.T, path string, tracks []testAudioTrack) {
	t.Helper()
	args := []string{"-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25:duration=6"}
	for index, track := range tracks {
		args = append(args, "-itsoffset", fmt.Sprintf("%.3f", track.delay),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=6", 300+index*100))
	}
	args = append(args, "-map", "0:v")
	for index := range tracks {
		args = append(args, "-map", fmt.Sprintf("%d:a", index+1))
	}
	args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	for index, track := range tracks {
		if track.language != "" {
			args = append(args, fmt.Sprintf("-metadata:s:a:%d", index), "language="+track.language)
		}
		if track.title != "" {
			args = append(args, fmt.Sprintf("-metadata:s:a:%d", index), "title="+track.title)
		}
	}
	args = append(args, path)
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("cannot build the fixture: %v\n%s", err, output)
	}
}

func TestAudioCleanupMissingTools(t *testing.T) {
	original := audioCleanupLookPath
	defer func() { audioCleanupLookPath = original }()

	present := map[string]bool{"ffprobe": true, "ffmpeg": true, "mkvmerge": true}
	audioCleanupLookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	if !AudioCleanupAvailable() {
		t.Fatal("all tools present, want available")
	}
	delete(present, "mkvmerge")
	if AudioCleanupAvailable() {
		t.Fatal("mkvmerge missing, want unavailable")
	}
	missing := AudioCleanupMissingTools()
	if len(missing) != 1 || missing[0] != "mkvmerge" {
		t.Fatalf("missing = %v, want [mkvmerge]", missing)
	}
	delete(present, "ffprobe")
	missing = AudioCleanupMissingTools()
	if len(missing) != 2 || missing[0] != "ffprobe" || missing[1] != "mkvmerge" {
		t.Fatalf("missing = %v, want [ffprobe mkvmerge]", missing)
	}
}

func TestAudioCleanupSettingRequiresTools(t *testing.T) {
	original := audioCleanupLookPath
	defer func() { audioCleanupLookPath = original }()
	audioCleanupLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	if err := validateSettingValue(audioCleanupSetting, "true", nil); err == nil {
		t.Fatal("enabling the cleanup without the tools must be refused")
	}
	if err := validateSettingValue(audioCleanupSetting, "false", nil); err != nil {
		t.Fatalf("disabling the cleanup must always work: %v", err)
	}
}

func TestAudioCleanupBlockedInSettingsWhenToolsMissing(t *testing.T) {
	original := audioCleanupLookPath
	defer func() { audioCleanupLookPath = original }()
	audioCleanupLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=rename", nil)
	if code != http.StatusOK {
		t.Fatalf("settings page -> %d", code)
	}
	index := strings.Index(body, `id="input-audio_cleanup"`)
	if index < 0 {
		t.Fatal("the audio cleanup option is missing from the settings page")
	}
	start := strings.LastIndex(body[:index], "<input")
	end := index + strings.Index(body[index:], ">")
	if start < 0 || end <= index {
		t.Fatal("cannot read the audio cleanup input tag")
	}
	tag := body[start : end+1]
	if !strings.Contains(tag, "disabled") {
		t.Fatalf("the switch must be disabled when a tool is missing: %s", tag)
	}
	if !strings.Contains(body, audioCleanupUnavailableNote) {
		t.Fatal("the missing-tools warning is not rendered")
	}
	if strings.Contains(body, `id="save-audio_cleanup"`) {
		t.Fatal("a blocked option must not offer the save button")
	}
}

func TestAudioCleanupLanguageIsADropdownWithCustomOption(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=rename", nil)
	if code != http.StatusOK {
		t.Fatalf("settings page -> %d", code)
	}
	if !strings.Contains(body, "data-v2-lang-combo") {
		t.Fatal("the language field is not a dropdown")
	}
	if !strings.Contains(body, `<option value="ita" selected>Italiano (ita)</option>`) {
		t.Fatal("the Italian option is missing or not selected")
	}
	if !strings.Contains(body, `value="__custom__"`) || !strings.Contains(body, "data-v2-lang-custom") {
		t.Fatal("the free-text «other language» option is missing")
	}
	index := strings.Index(body, `id="input-audio_cleanup_language"`)
	if index < 0 {
		t.Fatal("the language field is missing")
	}
	start := strings.LastIndex(body[:index], "<select")
	end := index + strings.Index(body[index:], ">")
	if start < 0 || end <= index {
		t.Fatal("cannot read the language select tag")
	}
	tag := body[start : end+1]
	if !strings.Contains(tag, `name="value"`) || strings.Contains(tag, "disabled") {
		t.Fatalf("the language dropdown must be editable: %s", tag)
	}
}

func TestAudioCleanupNotificationContent(t *testing.T) {
	report := AudioCleanupReport{
		NewSize:    35 << 30,
		SavedBytes: 2 << 30,
		Backend:    "mkvmerge",
		Dropped:    []AudioTrackInfo{{Index: 2}, {Index: 3}},
	}
	data := audioCleanupNotificationData("/library/Film (2026).mkv", report)
	body := formatEvent("audio_cleanup", data)
	for _, want := range []string{"Film (2026).mkv", "Tracce rimosse: 2", "2.00 GB", "mkvmerge"} {
		if !strings.Contains(body, want) {
			t.Errorf("notification body misses %q:\n%s", want, body)
		}
	}
	if subject := formatEmailSubject("audio_cleanup", data); !strings.Contains(subject, "Film (2026).mkv") {
		t.Errorf("email subject misses the file name: %s", subject)
	}
}

func TestAudioCleanupNotificationIsDelivered(t *testing.T) {
	var event string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			event, _ = body["event"].(string)
			payload, _ = body["data"].(map[string]any)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	notifier := NewNotifier()
	notifier.webhookURL = &server.URL
	notifier.webhookFormat = "gextto"
	notifyAudioCleanup(notifier, "/library/Film.mkv", AudioCleanupReport{
		NewSize: 1000, SavedBytes: 250, Backend: "ffmpeg",
		Dropped: []AudioTrackInfo{{Index: 2}},
	})

	if event != "audio_cleanup" {
		t.Fatalf("event = %q, want audio_cleanup", event)
	}
	if payload["file"] != "Film.mkv" {
		t.Fatalf("file = %v, want Film.mkv", payload["file"])
	}
	if removed, _ := payload["removed"].(float64); removed != 1 {
		t.Fatalf("removed = %v, want 1", payload["removed"])
	}
}

func TestRunAudioCleanupKeepsItalianAndTiming(t *testing.T) {
	requireAudioTools(t)
	path := filepath.Join(t.TempDir(), "film.mkv")
	makeAudioCleanupFixture(t, path, []testAudioTrack{
		{language: "eng"},
		{language: "ita", title: "Italiano", delay: 0.6},
	})

	before, err := ProbeResult(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll})
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if !report.Applied {
		t.Fatalf("cleanup not applied: %s", report.SkipReason)
	}
	if report.SavedBytes <= 0 {
		t.Errorf("saved bytes = %d, want > 0", report.SavedBytes)
	}

	after, err := ProbeResult(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.AudioTracks) != 1 || after.AudioTracks[0].Language != "ita" {
		t.Fatalf("audio tracks after cleanup: %+v", after.AudioTracks)
	}
	if after.RuntimeSeconds != 0 && before.RuntimeSeconds != 0 {
		if delta := after.RuntimeSeconds - before.RuntimeSeconds; delta < -2 || delta > 2 {
			t.Errorf("duration changed: %ds -> %ds", before.RuntimeSeconds, after.RuntimeSeconds)
		}
	}
	// The kept track was 0.6s late in the source: the delay must survive.
	italianBefore := before.AudioTracks[1]
	italianAfter := after.AudioTracks[0]
	videoBefore, _ := audioCleanupFirstVideo(before.Streams)
	videoAfter, _ := audioCleanupFirstVideo(after.Streams)
	if italianBefore.HasStart && italianAfter.HasStart {
		deltaBefore := italianBefore.StartTime - videoBefore.StartTime
		deltaAfter := italianAfter.StartTime - videoAfter.StartTime
		if delta := deltaAfter - deltaBefore; delta > 0.1 || delta < -0.1 {
			t.Errorf("A/V offset moved: %.3fs -> %.3fs", deltaBefore, deltaAfter)
		}
	}
}

func TestRunAudioCleanupBreaksHardlinkWithoutTouchingSeedingFile(t *testing.T) {
	requireAudioTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "download.mkv")
	library := filepath.Join(directory, "library.mkv")
	makeAudioCleanupFixture(t, source, []testAudioTrack{
		{language: "ita"},
		{language: "eng"},
	})
	if err := os.Link(source, library); err != nil {
		t.Skipf("hardlinks not available: %v", err)
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}

	report, err := RunAudioCleanup(context.Background(), library, AudioCleanupOptions{Language: "ita"})
	if err != nil || !report.Applied {
		t.Fatalf("cleanup failed: applied=%v error=%v reason=%s", report.Applied, err, report.SkipReason)
	}

	// The seeding copy keeps the old inode and both tracks.
	seeding, err := ProbeResult(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeding.AudioTracks) != 2 {
		t.Fatalf("the seeding file was modified: %+v", seeding.AudioTracks)
	}
	// The library entry is a new inode with only the Italian track.
	libraryInfo, err := os.Stat(library)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(sourceInfo, libraryInfo) {
		t.Fatal("the library file still shares the seeding inode")
	}
	cleaned, err := ProbeResult(library)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned.AudioTracks) != 1 || cleaned.AudioTracks[0].Language != "ita" {
		t.Fatalf("library tracks: %+v", cleaned.AudioTracks)
	}
}

func TestAudioCleanupFFmpegBackendVerifiesOutput(t *testing.T) {
	requireAudioTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "source.mkv")
	target := filepath.Join(directory, "out.mkv")
	makeAudioCleanupFixture(t, source, []testAudioTrack{
		{language: "ita", delay: 0.4},
		{language: "eng"},
		{language: "fra"},
	})
	info, err := ProbeResult(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll})
	if plan.Skipped() {
		t.Fatalf("plan skipped: %s", plan.SkipReason)
	}
	argv, err := audioCleanupCommand("ffmpeg", source, target, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runAudioCleanupCommand(context.Background(), argv, info.RuntimeSeconds, "test"); err != nil {
		t.Fatalf("ffmpeg remux failed: %v", err)
	}
	output, err := ProbeResult(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAudioCleanupOutput(info, output, plan, "ita", true); err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if len(output.AudioTracks) != 1 || output.AudioTracks[0].Language != "ita" {
		t.Fatalf("audio tracks: %+v", output.AudioTracks)
	}
}

func TestAudioCleanupMkvmergeBackendUsesContainerIDs(t *testing.T) {
	requireAudioTools(t)
	if _, err := exec.LookPath("mkvmerge"); err != nil {
		t.Skip("mkvmerge not installed")
	}
	directory := t.TempDir()
	source := filepath.Join(directory, "source.mkv")
	target := filepath.Join(directory, "out.mkv")
	makeAudioCleanupFixture(t, source, []testAudioTrack{
		{language: "eng"},
		{language: "eng", title: "Commentary"},
		{language: "ita"},
	})
	info, err := ProbeResult(source)
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll})
	argv, err := audioCleanupCommand("mkvmerge", source, target, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runAudioCleanupCommand(context.Background(), argv, info.RuntimeSeconds, "test"); err != nil {
		t.Fatalf("mkvmerge remux failed: %v", err)
	}
	output, err := ProbeResult(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyAudioCleanupOutput(info, output, plan, "ita", true); err != nil {
		t.Fatalf("verification failed: %v", err)
	}
	if len(output.AudioTracks) != 1 || output.AudioTracks[0].Language != "ita" {
		t.Fatalf("audio tracks: %+v", output.AudioTracks)
	}
}

func TestAudioCleanupUpdatesArchivedSize(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.db.Close()

	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease("Show.S01E01.1080p.WEB-DL.ITA", magnet, "test")
	if release == nil {
		t.Fatal("release not parsed")
	}
	path := "/nas/Show/Show - S01E01.mkv"
	if err := db.MarkReleaseCompleted(release, path, 5_000); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	if err := db.UpdateArchivedSizeAfterCleanup("", path, 3_000); err != nil {
		t.Fatalf("update size: %v", err)
	}
	var size int64
	if err := db.db.QueryRow("SELECT size_bytes FROM episodes WHERE archive_path=?1", path).Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != 3_000 {
		t.Fatalf("archived size = %d, want 3000", size)
	}
}

func TestRunAudioCleanupLeavesUntouchedFilesAlone(t *testing.T) {
	requireAudioTools(t)
	cases := []struct {
		name   string
		tracks []testAudioTrack
	}{
		{name: "single track", tracks: []testAudioTrack{{language: "ita"}}},
		{name: "no italian", tracks: []testAudioTrack{{language: "eng"}, {language: "fra"}}},
		{name: "untagged", tracks: []testAudioTrack{{}, {}}},
		{name: "only italian", tracks: []testAudioTrack{{language: "ita"}, {language: "ita"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.mkv")
			makeAudioCleanupFixture(t, path, testCase.tracks)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if report.Applied {
				t.Fatal("the file should not have been touched")
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
				t.Fatal("the file was modified")
			}
		})
	}
}
