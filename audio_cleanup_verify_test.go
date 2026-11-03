package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// richAudioTrack is one audio track of a rich fixture.
type richAudioTrack struct {
	language  string
	isDefault bool
	delay     float64
}

// richFixture describes a file that looks like a real release: several audio
// tracks with flags, subtitles that must stay off, a font attachment.
type richFixture struct {
	videoCodec string // libx264 (default) or libx265
	audio      []richAudioTrack
	subtitles  int  // non-default SubRip tracks
	assTrack   bool // one more non-default ASS track
	font       bool
}

func makeRichAudioCleanupFixture(t *testing.T, path string, fixture richFixture) {
	t.Helper()
	directory := t.TempDir()
	args := []string{"-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25:duration=6"}
	inputs := 1
	for index, track := range fixture.audio {
		args = append(args, "-itsoffset", fmt.Sprintf("%.3f", track.delay),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=6", 300+index*100))
		inputs++
	}
	subtitleInputs := []int{}
	for index := 0; index < fixture.subtitles; index++ {
		srt := filepath.Join(directory, fmt.Sprintf("sub%d.srt", index))
		body := "1\n00:00:01,000 --> 00:00:02,000\nprima riga\n\n2\n00:00:03,500 --> 00:00:04,250\nseconda riga\n"
		if err := os.WriteFile(srt, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-i", srt)
		subtitleInputs = append(subtitleInputs, inputs)
		inputs++
	}
	assInput := -1
	if fixture.assTrack {
		ass := filepath.Join(directory, "sub.ass")
		body := "[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,2,10,10,10,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,riga uno\nDialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,riga due\n"
		if err := os.WriteFile(ass, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-i", ass)
		assInput = inputs
	}
	args = append(args, "-map", "0:v")
	for index := range fixture.audio {
		args = append(args, "-map", fmt.Sprintf("%d:a", index+1))
	}
	for _, input := range subtitleInputs {
		args = append(args, "-map", fmt.Sprintf("%d:s", input))
	}
	if assInput >= 0 {
		args = append(args, "-map", fmt.Sprintf("%d:s", assInput))
	}
	codec := fixture.videoCodec
	if codec == "" {
		codec = "libx264"
	}
	args = append(args, "-c:v", codec, "-preset", "ultrafast", "-c:a", "aac", "-c:s", "copy")
	if codec == "libx265" {
		args = append(args, "-x265-params", "log-level=error")
	}
	for index, track := range fixture.audio {
		if track.language != "" {
			args = append(args, fmt.Sprintf("-metadata:s:a:%d", index), "language="+track.language)
		}
		flag := "0"
		if track.isDefault {
			flag = "default"
		}
		args = append(args, fmt.Sprintf("-disposition:a:%d", index), flag)
	}
	subtitleCount := fixture.subtitles
	if fixture.assTrack {
		subtitleCount++
	}
	for index := 0; index < subtitleCount; index++ {
		args = append(args, fmt.Sprintf("-metadata:s:s:%d", index), "language=ita",
			fmt.Sprintf("-disposition:s:%d", index), "0")
	}
	args = append(args, "-disposition:v:0", "default")
	if fixture.font {
		font := filepath.Join(directory, "font.ttf")
		if err := os.WriteFile(font, []byte("not really a font, but an attachment all the same"), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "-attach", font, "-metadata:s:t", "mimetype=application/x-truetype-font")
	}
	args = append(args, path)
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("cannot build the fixture: %v\n%s", err, output)
	}
}

func requireAllAudioTools(t *testing.T) {
	t.Helper()
	requireAudioTools(t)
	if _, err := exec.LookPath("mkvmerge"); err != nil {
		t.Skip("mkvmerge not installed")
	}
}

func streamsOfType(info MediaInfo, kind string) []StreamInfo {
	var streams []StreamInfo
	for _, stream := range info.Streams {
		if stream.Type == kind {
			streams = append(streams, stream)
		}
	}
	return streams
}

// A release with the English track as default, an untagged track, two
// subtitles that are off and a font: each tool must give the same result —
// the Italian track becomes the default, the untagged one stays, the subtitles
// stay off, the font stays, every packet is the source's.
func TestAudioCleanupBothToolsKeepFlagsSubtitlesAndUntagged(t *testing.T) {
	requireAllAudioTools(t)
	for _, backend := range []string{"mkvmerge", "ffmpeg"} {
		t.Run(backend, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "release.mkv")
			makeRichAudioCleanupFixture(t, source, richFixture{
				audio: []richAudioTrack{
					{language: "eng", isDefault: true},
					{language: "ita", delay: 0.3},
					{language: ""},
				},
				subtitles: 2,
				assTrack:  true,
				font:      true,
			})
			info, err := ProbeResult(source)
			if err != nil {
				t.Fatal(err)
			}
			opts := AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll}
			plan := PlanAudioCleanup(info, opts)
			if plan.Skipped() {
				t.Fatalf("plan skipped: %s", plan.SkipReason)
			}
			if plan.DefaultPosition != 1 {
				t.Fatalf("default position = %d, want the Italian track", plan.DefaultPosition)
			}
			target := filepath.Join(directory, "out.mkv")
			if err := audioCleanupAttempt(context.Background(), backend, source, target, info, plan, opts); err != nil {
				t.Fatalf("%s copy refused: %v", backend, err)
			}
			output, err := ProbeResult(target)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.AudioTracks) != 2 || output.AudioTracks[0].Language != "ita" {
				t.Fatalf("audio tracks: %+v", output.AudioTracks)
			}
			if !output.AudioTracks[0].Default {
				t.Error("the Italian track did not become the default one")
			}
			if output.AudioTracks[1].Default {
				t.Error("the untagged track became a default one")
			}
			subtitles := streamsOfType(output, "subtitle")
			if len(subtitles) != 3 {
				t.Fatalf("subtitles: %+v", subtitles)
			}
			for _, subtitle := range subtitles {
				if slices.Contains(subtitle.Disposition, "default") {
					t.Errorf("subtitle %d switched itself on: %v", subtitle.Index, subtitle.Disposition)
				}
			}
			if attachments := streamsOfType(output, "attachment"); len(attachments) != 1 {
				t.Errorf("attachments: %+v", attachments)
			}
		})
	}
}

// The full run on an HEVC release: whichever tool passes, the result is
// published and the packets are those of the source.
func TestRunAudioCleanupPublishesHEVCRelease(t *testing.T) {
	requireAllAudioTools(t)
	if output, _ := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output(); !strings.Contains(string(output), "libx265") {
		t.Skip("libx265 not available")
	}
	path := filepath.Join(t.TempDir(), "Serie.1x01.Titolo.1080p.HEVC.mkv")
	makeRichAudioCleanupFixture(t, path, richFixture{
		videoCodec: "libx265",
		audio: []richAudioTrack{
			{language: "ita", isDefault: true},
			{language: "eng"},
		},
		subtitles: 1,
	})
	report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita"})
	if err != nil || !report.Applied {
		t.Fatalf("cleanup failed: applied=%v error=%v reason=%s", report.Applied, err, report.SkipReason)
	}
	after, err := ProbeResult(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.AudioTracks) != 1 || after.AudioTracks[0].Language != "ita" || !after.AudioTracks[0].Default {
		t.Fatalf("audio tracks: %+v", after.AudioTracks)
	}
}

// Untagged tracks are kept by default in a real run, removed when asked.
func TestRunAudioCleanupUntaggedModes(t *testing.T) {
	requireAllAudioTools(t)
	for mode, want := range map[string]int{
		audioCleanupUntaggedKeep:   2,
		audioCleanupUntaggedRemove: 1,
	} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "film.mkv")
			makeRichAudioCleanupFixture(t, path, richFixture{audio: []richAudioTrack{
				{language: "ita", isDefault: true}, {language: "eng"}, {language: ""},
			}})
			report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Untagged: mode})
			if err != nil || !report.Applied {
				t.Fatalf("cleanup failed: applied=%v error=%v reason=%s", report.Applied, err, report.SkipReason)
			}
			after, err := ProbeResult(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.AudioTracks) != want {
				t.Fatalf("audio tracks = %d, want %d: %+v", len(after.AudioTracks), want, after.AudioTracks)
			}
		})
	}
	t.Run(audioCleanupUntaggedSkip, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "film.mkv")
		makeRichAudioCleanupFixture(t, path, richFixture{audio: []richAudioTrack{
			{language: "ita"}, {language: "eng"}, {language: ""},
		}})
		before, _ := os.Stat(path)
		report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Untagged: audioCleanupUntaggedSkip})
		if err != nil || report.Applied {
			t.Fatalf("the file should be left alone: applied=%v error=%v", report.Applied, err)
		}
		after, _ := os.Stat(path)
		if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("the file was modified")
		}
	})
}

// A copy whose audio was re-encoded has the same headers but other bytes: the
// packet check must refuse it. A copy with the audio half a second late must
// be refused as a desync.
func TestAudioCleanupPacketCheckRefusesDamagedCopies(t *testing.T) {
	requireAudioTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "source.mkv")
	makeRichAudioCleanupFixture(t, source, richFixture{audio: []richAudioTrack{
		{language: "ita", isDefault: true}, {language: "eng"},
	}})
	info, err := ProbeResult(source)
	if err != nil {
		t.Fatal(err)
	}
	opts := AudioCleanupOptions{Language: "ita"}
	plan := PlanAudioCleanup(info, opts)
	if plan.Skipped() {
		t.Fatalf("plan skipped: %s", plan.SkipReason)
	}

	good := filepath.Join(directory, "good.mkv")
	if err := audioCleanupAttempt(context.Background(), "ffmpeg", source, good, info, plan, opts); err != nil {
		t.Fatalf("the faithful copy was refused: %v", err)
	}

	reencoded := filepath.Join(directory, "reencoded.mkv")
	run := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v\n%s", err, output)
		}
	}
	run("-i", source, "-map", "0:v", "-map", "0:a:0", "-c:v", "copy", "-c:a", "aac", "-b:a", "96k", reencoded)
	checkRefused(t, source, reencoded, info, plan, "differs from the source")

	shifted := filepath.Join(directory, "shifted.mkv")
	run("-i", source, "-itsoffset", "0.5", "-i", source, "-map", "0:v", "-map", "1:a:0", "-c", "copy", shifted)
	checkRefused(t, source, shifted, info, plan, "distance from the video")
}

func checkRefused(t *testing.T, source, copied string, info MediaInfo, plan AudioCleanupPlan, reason string) {
	t.Helper()
	output, err := ProbeResult(copied)
	if err != nil {
		t.Fatal(err)
	}
	err = verifyAudioCleanupPackets(context.Background(), source, copied, info, output, plan, true)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("the damaged copy %s was accepted or refused for another reason: %v", filepath.Base(copied), err)
	}
}

func TestParseAudioCleanupPacketLine(t *testing.T) {
	line := "stream_index=2|pts_time=1.234000|dts_time=1.200000|data_hash=MD5:00112233445566778899aabbccddeeff"
	parsed, ok := parseAudioCleanupPacketLine(line)
	if !ok || parsed.stream != 2 || !parsed.packet.hasTime || parsed.packet.time != 1.234 || parsed.packet.hash[15] != 0xff {
		t.Fatalf("parsed = %+v ok=%v", parsed, ok)
	}
	parsed, ok = parseAudioCleanupPacketLine("stream_index=0|pts_time=N/A|dts_time=0.5|data_hash=MD5:00112233445566778899aabbccddeeff")
	if !ok || parsed.packet.time != 0.5 {
		t.Fatalf("dts fallback: %+v ok=%v", parsed, ok)
	}
	for _, bad := range []string{"", "side_data_type=foo", "stream_index=1|pts_time=0", "stream_index=x|data_hash=MD5:00"} {
		if _, ok := parseAudioCleanupPacketLine(bad); ok {
			t.Errorf("%q parsed as a packet", bad)
		}
	}
}

// ffmpeg maps every kept stream by index, in file order, and gives each one
// explicit flags so nothing is switched on by inference.
func TestAudioCleanupFFmpegCommandKeepsOrderAndFlags(t *testing.T) {
	info := MediaInfo{
		Streams: []StreamInfo{
			{Index: 0, Type: "video", Codec: "hevc", Disposition: []string{"default"}},
			{Index: 1, Type: "audio", Codec: "eac3", Disposition: []string{"default"}},
			{Index: 2, Type: "audio", Codec: "eac3"},
			{Index: 3, Type: "subtitle", Codec: "subrip", Disposition: []string{"forced"}},
			{Index: 4, Type: "data", Codec: "bin_data"},
			{Index: 5, Type: "attachment", Codec: "ttf"},
		},
		AudioTracks: []AudioTrackInfo{
			{Index: 1, Language: "eng", Codec: "eac3", Default: true},
			{Index: 2, Language: "ita", Codec: "eac3"},
		},
	}
	plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita"})
	if plan.Skipped() || plan.DefaultPosition != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	argv, err := audioCleanupCommand("ffmpeg", "in.mkv", "out.mkv", plan)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, " ")
	for _, required := range []string{
		"-map 0:0 -map 0:2 -map 0:3 -map 0:5 ", "-c copy", "-default_mode passthrough",
		"-disposition:0 default", "-disposition:1 default", "-disposition:2 forced",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("command misses %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"-map 0:1", "-map 0:4", "-disposition:3"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("command contains %q: %s", forbidden, joined)
		}
	}

	mp4, err := audioCleanupCommand("ffmpeg", "in.mp4", "out.mp4", plan)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(mp4, " "); strings.Contains(joined, "0:5") || strings.Contains(joined, "-disposition") {
		t.Errorf("an MP4 cannot hold attachments and keeps its own flags: %s", joined)
	}
}

func TestPlanAudioCleanupKeepsFlagsWhenTheDefaultStays(t *testing.T) {
	info := MediaInfo{
		Streams: []StreamInfo{{Index: 0, Type: "video"}},
		AudioTracks: []AudioTrackInfo{
			{Index: 1, Language: "ita", Default: true},
			{Index: 2, Language: "eng"},
		},
	}
	if plan := PlanAudioCleanup(info, AudioCleanupOptions{Language: "ita"}); plan.DefaultPosition != -1 {
		t.Fatalf("default position = %d, want -1", plan.DefaultPosition)
	}
}

func TestRunAudioCleanupRemovesLeftoversAndKeepsPermissions(t *testing.T) {
	requireAllAudioTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "Serie [1080p] 1x01.mkv")
	makeRichAudioCleanupFixture(t, path, richFixture{audio: []richAudioTrack{
		{language: "ita", isDefault: true}, {language: "eng"},
	}})
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(directory, ".Serie [1080p] 1x01.gxt-audio-deadbeef.mkv")
	other := filepath.Join(directory, ".Altro file.gxt-audio-deadbeef.mkv")
	for _, name := range []string{leftover, other} {
		if err := os.WriteFile(name, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita"})
	if err != nil || !report.Applied {
		t.Fatalf("cleanup failed: applied=%v error=%v reason=%s", report.Applied, err, report.SkipReason)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Error("the leftover of this file was not removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("the leftover of another file was removed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("permissions = %v, want 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if strings.Contains(entry.Name(), audioCleanupTemporaryMarker) && entry.Name() != filepath.Base(other) {
			t.Errorf("temporary left behind: %s", entry.Name())
		}
	}
}

func TestRunAudioCleanupSkipsAFileThatIsGone(t *testing.T) {
	report, err := RunAudioCleanup(context.Background(), filepath.Join(t.TempDir(), "gone.mkv"), AudioCleanupOptions{Language: "ita"})
	if err != nil || report.Applied || report.SkipReason == "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestAudioCleanupRefusesWithoutSpace(t *testing.T) {
	plan := AudioCleanupPlan{Info: MediaInfo{RuntimeSeconds: 100}}
	if err := audioCleanupCheckSpace(filepath.Join(t.TempDir(), "x.mkv"), 1<<61, plan); err == nil || !strings.Contains(err.Error(), "not enough free space") {
		t.Fatalf("err = %v", err)
	}
	if err := audioCleanupCheckSpace(filepath.Join(t.TempDir(), "x.mkv"), 1<<10, plan); err != nil {
		t.Fatalf("a small file was refused: %v", err)
	}
	plan.Drop = []AudioTrackInfo{{Bitrate: 640_000}}
	if got := audioCleanupEstimatedSize(100_000_000, plan); got != 100_000_000-8_000_000 {
		t.Fatalf("estimated size = %d", got)
	}
}

// The queue takes every file of a long pack, saves itself, and a new run
// picks up what the previous one left.
func TestAudioCleanupQueueIsUnboundedSavedAndResumed(t *testing.T) {
	original := audioCleanupLookPath
	defer func() { audioCleanupLookPath = original }()
	audioCleanupLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	dataDir := t.TempDir()
	cfg := &Config{DataDir: dataDir, Settings: map[string]string{audioCleanupSetting: "true"}}

	release := make(chan struct{})
	var mu sync.Mutex
	var processed []string
	queue := newAudioCleanupQueue()
	queue.process = func(job audioCleanupJob, _ *Config, _ *Database) {
		<-release
		mu.Lock()
		processed = append(processed, job.Path)
		mu.Unlock()
	}
	queue.configure(cfg, nil)

	library := t.TempDir()
	jobs := make([]audioCleanupJob, 0, 150)
	for index := 0; index < 150; index++ {
		path := filepath.Join(library, fmt.Sprintf("Serie.1x%03d.mkv", index))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, audioCleanupJob{Path: path, Options: AudioCleanupOptions{Language: "ita"}})
	}
	if added := queue.enqueue(jobs); added != 150 {
		t.Fatalf("added %d of 150", added)
	}
	if added := queue.enqueue(jobs[:10]); added != 0 {
		t.Fatalf("duplicates queued: %d", added)
	}
	statePath := filepath.Join(dataDir, audioCleanupQueueFile)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var saved []audioCleanupJob
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) != 150 {
		t.Fatalf("saved %d jobs (%v)", len(saved), err)
	}

	// A new run (the old worker is still blocked: a stopped process) reads
	// the file; a file deleted meanwhile is dropped.
	if err := os.Remove(jobs[149].Path); err != nil {
		t.Fatal(err)
	}
	resumed := newAudioCleanupQueue()
	resumedDone := make(chan string, 200)
	resumed.process = func(job audioCleanupJob, _ *Config, _ *Database) { resumedDone <- job.Path }
	resumed.resume(cfg, nil)
	for index := 0; index < 149; index++ {
		select {
		case path := <-resumedDone:
			if path != jobs[index].Path {
				t.Fatalf("job %d = %s, want %s", index, path, jobs[index].Path)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d jobs resumed", index)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(resumed.pending()) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("the queue file was not removed once empty: %v", err)
	}
	// Let the first worker finish without touching the data folder again.
	queue.mu.Lock()
	queue.statePath = ""
	queue.mu.Unlock()
	close(release)
}

func TestAudioCleanupQueueIsDroppedWhenSwitchedOff(t *testing.T) {
	dataDir := t.TempDir()
	statePath := filepath.Join(dataDir, audioCleanupQueueFile)
	file := filepath.Join(t.TempDir(), "film.mkv")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal([]audioCleanupJob{{Path: file}})
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	queue := newAudioCleanupQueue()
	queue.process = func(audioCleanupJob, *Config, *Database) { t.Error("a job ran with the cleanup off") }
	queue.resume(&Config{DataDir: dataDir, Settings: map[string]string{audioCleanupSetting: "false"}}, nil)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("queue file still there: %v", err)
	}
	if len(queue.pending()) != 0 {
		t.Fatal("jobs queued with the cleanup off")
	}
}

func TestAudioCleanupUntaggedSettingIsValidated(t *testing.T) {
	for _, value := range []string{"", "keep", "remove", "skip", "KEEP"} {
		if err := validateSettingValue(audioCleanupUntaggedSetting, value, &Config{}); err != nil {
			t.Errorf("%q refused: %v", value, err)
		}
	}
	if err := validateSettingValue(audioCleanupUntaggedSetting, "drop", &Config{}); err == nil {
		t.Error("an unknown value was accepted")
	}
	if got := normalizeAudioCleanupUntagged(""); got != audioCleanupUntaggedKeep {
		t.Errorf("empty value = %q, want keep", got)
	}
	opts, enabled := audioCleanupSettings(&Config{Settings: map[string]string{audioCleanupSetting: "true"}})
	if !enabled || opts.Untagged != audioCleanupUntaggedKeep {
		t.Errorf("default options = %+v enabled=%v", opts, enabled)
	}
}
