package gextto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// miFakeFfprobeSuccess is a shell script that ignores the ffprobe flags and
// prints a fixed JSON document, echoing the probed path back as the container
// name so tests can tell which file was selected.
const miFakeFfprobeSuccess = `#!/bin/sh
last=""
for arg in "$@"; do last="$arg"; done
printf '{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"pix_fmt":"yuv420p10le","color_transfer":"smpte2084","color_primaries":"bt2020"},{"codec_type":"audio","codec_name":"eac3","channels":6,"tags":{"language":"ita"}}],"format":{"format_name":"%s","duration":"120.5"}}' "$last"
`

// miFakeFfprobeFailure writes a diagnostic to stderr and exits non-zero.
const miFakeFfprobeFailure = `#!/bin/sh
echo "boom: invalid data" >&2
exit 1
`

// miInstallFakeFfprobe drops an executable named `ffprobe` in a temp dir and
// prepends it to PATH, restoring the old PATH with t.Cleanup.
func miInstallFakeFfprobe(t *testing.T, script string) string {
	t.Helper()
	binDir := t.TempDir()
	path := filepath.Join(binDir, "ffprobe")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	return path
}

// miUsePath sets PATH to exactly the given directories for the test, restoring
// the previous value with t.Cleanup.
func miUsePath(t *testing.T, dirs ...string) {
	t.Helper()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator))); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
}

// miParseRoot unmarshals synthetic ffprobe JSON and parses it.
func miParseRoot(t *testing.T, raw string) MediaInfo {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return ParseFfprobe(root)
}

// TestMediaInfoParseHDR10 ports the
// `parses_hdr10_10bit_with_audio_and_subtitles` test.
func TestMediaInfoParseHDR10(t *testing.T) {
	info := miParseRoot(t, `{
        "streams": [
            {"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,
             "pix_fmt":"yuv420p10le","color_primaries":"bt2020","color_transfer":"smpte2084"},
            {"codec_type":"audio","codec_name":"eac3","channels":6,"tags":{"language":"ita"}},
            {"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"eng"}},
            {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"ita"}}
        ],
        "format": {"format_name":"matroska,webm","duration":"5400.42"}
    }`)

	if got := info.Resolution(); got != "1080p" {
		t.Fatalf("Resolution = %q, want 1080p", got)
	}
	if info.VideoCodec != "hevc" {
		t.Fatalf("VideoCodec = %q, want hevc", info.VideoCodec)
	}
	if info.BitDepth != 10 {
		t.Fatalf("BitDepth = %d, want 10", info.BitDepth)
	}
	if info.HDR != "HDR10" {
		t.Fatalf("HDR = %q, want HDR10", info.HDR)
	}
	if info.AudioCodec != "eac3" {
		t.Fatalf("AudioCodec = %q, want eac3", info.AudioCodec)
	}
	if info.AudioChannels != 6 {
		t.Fatalf("AudioChannels = %d, want 6", info.AudioChannels)
	}
	if got, want := info.AudioLanguages, []string{"ita", "eng"}; !equalStrings(got, want) {
		t.Fatalf("AudioLanguages = %#v, want %#v", got, want)
	}
	if got, want := info.SubtitleLanguages, []string{"ita"}; !equalStrings(got, want) {
		t.Fatalf("SubtitleLanguages = %#v, want %#v", got, want)
	}
	if info.RuntimeSeconds != 5400 {
		t.Fatalf("RuntimeSeconds = %d, want 5400", info.RuntimeSeconds)
	}
	if !info.HasHDR() {
		t.Fatal("HasHDR should be true")
	}
	if info.Container != "matroska,webm" {
		t.Fatalf("Container = %q", info.Container)
	}
}

// TestMediaInfoParseDolbyVision ports the
// `parses_dolby_vision_from_side_data` test.
func TestMediaInfoParseDolbyVision(t *testing.T) {
	info := miParseRoot(t, `{
        "streams": [{
            "codec_type":"video","codec_name":"hevc","width":3840,"height":2160,
            "pix_fmt":"yuv420p10le","color_primaries":"bt2020","color_transfer":"smpte2084",
            "side_data_list":[{"side_data_type":"DOVI configuration record"}]
        }],
        "format": {"format_name":"matroska","duration":"60"}
    }`)
	if !strings.HasPrefix(info.HDR, "DV") {
		t.Fatalf("HDR = %q, want a DV flavour", info.HDR)
	}
	if got := info.Resolution(); got != "2160p" {
		t.Fatalf("Resolution = %q, want 2160p", got)
	}
}

// TestMediaInfoSkipsMotionImageStreams ports the
// `skips_motion_image_video_streams` test.
func TestMediaInfoSkipsMotionImageStreams(t *testing.T) {
	info := miParseRoot(t, `{
        "streams": [
            {"codec_type":"video","codec_name":"mjpeg","width":600,"height":600},
            {"codec_type":"video","codec_name":"h264","width":1280,"height":720,"pix_fmt":"yuv420p"}
        ],
        "format": {"format_name":"mp4","duration":"120"}
    }`)
	if info.VideoCodec != "h264" {
		t.Fatalf("VideoCodec = %q, want h264", info.VideoCodec)
	}
	if got := info.Resolution(); got != "720p" {
		t.Fatalf("Resolution = %q, want 720p", got)
	}
	if info.BitDepth != 8 {
		t.Fatalf("BitDepth = %d, want 8", info.BitDepth)
	}
	if info.HDR != "" {
		t.Fatalf("HDR = %q, want empty", info.HDR)
	}

	// A file with only motion-image streams exposes no video metadata.
	onlyImage := miParseRoot(t, `{
        "streams": [{"codec_type":"video","codec_name":"png","width":100,"height":100}],
        "format": {"format_name":"png","duration":"0"}
    }`)
	if onlyImage.VideoCodec != "" || onlyImage.Resolution() != "" {
		t.Fatalf("motion image should be skipped, got %#v", onlyImage)
	}
}

// TestMediaInfoBitDepthHandlesEndianness ports the
// `bit_depth_handles_endianness_suffixes` test.
func TestMediaInfoBitDepthHandlesEndianness(t *testing.T) {
	cases := map[string]int64{
		"yuv420p10le": 10,
		"yuv420p":     8,
		"yuv444p12be": 12,
		"gbrp16le":    16,
		"yuv420p9le":  9,
		"yuv420p14le": 14,
		"unknown":     8,
		"":            8,
	}
	for pixFmt, want := range cases {
		if got := BitDepthFromPixFmt(pixFmt); got != want {
			t.Fatalf("BitDepthFromPixFmt(%q) = %d, want %d", pixFmt, got, want)
		}
	}
}

// TestMediaInfoCodecAndAudioLabels covers the ffprobe -> Gextto label mapping.
func TestMediaInfoCodecAndAudioLabels(t *testing.T) {
	codecs := map[string]string{
		"hevc":       "h265",
		"H265":       "h265",
		"h264":       "h264",
		"avc":        "h264",
		"av1":        "av1",
		"vp9":        "vp9",
		"mpeg2video": "mpeg2",
		"vc1":        "vc1",
	}
	for input, want := range codecs {
		got, ok := CodecLabel(input)
		if !ok || got != want {
			t.Fatalf("CodecLabel(%q) = (%q,%v), want (%q,true)", input, got, ok, want)
		}
	}
	for _, unknown := range []string{"", "prores", "theora"} {
		if got, ok := CodecLabel(unknown); ok {
			t.Fatalf("CodecLabel(%q) = (%q,true), want not found", unknown, got)
		}
	}

	audio := map[string]string{
		"eac3":   "ddp",
		"EAC3":   "ddp",
		"ac3":    "ac3",
		"aac":    "aac",
		"dts":    "dts",
		"truehd": "truehd",
		"flac":   "flac",
		"opus":   "opus",
		"mp3":    "mp3",
	}
	for input, want := range audio {
		got, ok := AudioLabel(input)
		if !ok || got != want {
			t.Fatalf("AudioLabel(%q) = (%q,%v), want (%q,true)", input, got, ok, want)
		}
	}
	if got, ok := AudioLabel("vorbis"); ok {
		t.Fatalf("AudioLabel(vorbis) = (%q,true), want not found", got)
	}
}

// TestMediaInfoHDRFromStream covers the colour-metadata mapping directly.
func TestMediaInfoHDRFromStream(t *testing.T) {
	cases := []struct {
		name   string
		stream map[string]any
		want   string
	}{
		{"hlg", map[string]any{"color_transfer": "arib-std-b67"}, "HLG"},
		{"hdr10", map[string]any{"color_transfer": "smpte2084"}, "HDR10"},
		{"hdr10plus", map[string]any{
			"color_transfer": "smpte2084",
			"tags":           map[string]any{"NUMBER_OF_DYNAMIC_HDR_LAYERS": "2"},
		}, "HDR10+"},
		{"dv_hlg", map[string]any{
			"color_transfer": "arib-std-b67",
			"side_data_list": []any{map[string]any{"side_data_type": "DOVI configuration record"}},
		}, "DV HLG"},
		{"dv", map[string]any{
			"color_transfer": "smpte2084",
			"side_data_list": []any{map[string]any{"side_data_type": "DOVI configuration record"}},
		}, "DV"},
		{"dv_hdr10plus", map[string]any{
			"color_transfer": "smpte2084",
			"tags":           map[string]any{"dynamic_hdr_plus": "1"},
			"side_data_list": []any{map[string]any{"side_data_type": "DOVI configuration record"}},
		}, "DV HDR10+"},
		{"bt2020_only", map[string]any{"color_primaries": "bt2020"}, "HDR"},
		{"sdr", map[string]any{"color_transfer": "bt709"}, ""},
		{"empty", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HDRFromStream(tc.stream); got != tc.want {
				t.Fatalf("HDRFromStream = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMediaInfoApplyToQualityIsAdditiveOnly ports the
// `apply_to_quality_is_additive_only` test.
func TestMediaInfoApplyToQualityIsAdditiveOnly(t *testing.T) {
	info := MediaInfo{
		HDR:            "HDR10",
		VideoCodec:     "hevc",
		AudioCodec:     "eac3",
		Width:          1920,
		Height:         1080,
		AudioLanguages: []string{"ita"},
	}

	unknown := models.Quality{Resolution: "1080p", Codec: "unknown", Audio: "unknown"}
	info.ApplyToQuality(&unknown)
	if unknown.HDR != "HDR10" {
		t.Fatalf("HDR = %q, want HDR10", unknown.HDR)
	}
	if unknown.Codec != "h265" {
		t.Fatalf("Codec = %q, want h265", unknown.Codec)
	}
	if unknown.Audio != "ddp" {
		t.Fatalf("Audio = %q, want ddp", unknown.Audio)
	}
	if got, want := unknown.Languages, []string{"ita"}; !equalStrings(got, want) {
		t.Fatalf("Languages = %#v, want %#v", got, want)
	}
	// A known resolution is never overwritten.
	if unknown.Resolution != "1080p" {
		t.Fatalf("Resolution = %q, want 1080p", unknown.Resolution)
	}

	// Known values are never overwritten.
	known := models.Quality{Resolution: "1080p", HDR: "HLG", Codec: "h264", Audio: "dts"}
	info.ApplyToQuality(&known)
	if known.HDR != "HLG" || known.Codec != "h264" || known.Audio != "dts" {
		t.Fatalf("known quality was modified: %#v", known)
	}

	// Empty values are filled from the probe, and DV sets the flag.
	dv := MediaInfo{HDR: "DV HDR10", VideoCodec: "hevc", Width: 3840, Height: 2160}
	empty := models.Quality{}
	dv.ApplyToQuality(&empty)
	if empty.HDR != "DV HDR10" || empty.Codec != "h265" {
		t.Fatalf("empty quality = %#v", empty)
	}
	if !empty.IsDV {
		t.Fatal("IsDV should be set for a Dolby Vision probe")
	}
	if empty.Resolution != "2160p" {
		t.Fatalf("Resolution = %q, want 2160p", empty.Resolution)
	}

	// No probe data leaves an unknown-only quality untouched.
	MediaInfo{}.ApplyToQuality(&unknown)
	if unknown.Codec != "h265" {
		t.Fatalf("empty probe overwrote codec: %q", unknown.Codec)
	}
}

// TestMediaInfoResolutionBuckets covers the frame-size buckets.
func TestMediaInfoResolutionBuckets(t *testing.T) {
	cases := []struct {
		width, height int64
		want          string
	}{
		{3840, 2160, "2160p"},
		{1920, 1080, "1080p"},
		{1280, 720, "720p"},
		{720, 576, "576p"},
		{720, 480, "480p"},
		{0, 0, ""},
	}
	for _, tc := range cases {
		info := MediaInfo{Width: tc.width, Height: tc.height}
		if got := info.Resolution(); got != tc.want {
			t.Fatalf("Resolution(%dx%d) = %q, want %q", tc.width, tc.height, got, tc.want)
		}
	}
}

// TestMediaInfoProbeBestResultNamesMissingFile ports the
// `probe_best_result_names_a_missing_file` test.
func TestMediaInfoProbeBestResultNamesMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gextto-mediainfo-missing")
	if _, err := ProbeBestResult(missing); err == nil || !strings.Contains(err.Error(), "non trovata") {
		t.Fatalf("ProbeBestResult(missing) error = %v, want 'non trovata'", err)
	}
}

// TestMediaInfoProbeBestReturnsNoneForMissingFile ports the
// `probe_best_still_returns_none_for_a_missing_file` test.
func TestMediaInfoProbeBestReturnsNoneForMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gextto-mediainfo-none")
	if got := ProbeBest(missing); got != nil {
		t.Fatalf("ProbeBest(missing) = %#v, want nil", got)
	}
}

// TestMediaInfoProbeUsesFakeFfprobe exercises Probe/ProbeResult with a fake
// binary on PATH.
func TestMediaInfoProbeUsesFakeFfprobe(t *testing.T) {
	miInstallFakeFfprobe(t, miFakeFfprobeSuccess)
	if !Available() {
		t.Fatal("Available should report true with a runnable fake ffprobe")
	}

	file := filepath.Join(t.TempDir(), "movie.mkv")
	mustWriteFile(t, file, "not really a movie")

	info := Probe(file)
	if info == nil {
		t.Fatal("Probe returned nil")
	}
	if info.VideoCodec != "hevc" || info.BitDepth != 10 || info.HDR != "HDR10" {
		t.Fatalf("probe info = %#v", info)
	}
	if info.Container != file {
		t.Fatalf("Container = %q, want the probed path %q", info.Container, file)
	}
	if info.RuntimeSeconds != 121 {
		t.Fatalf("RuntimeSeconds = %d, want 121 (rounded 120.5)", info.RuntimeSeconds)
	}

	result, err := ProbeResult(file)
	if err != nil {
		t.Fatalf("ProbeResult: %v", err)
	}
	if result.Container != info.Container || result.VideoCodec != info.VideoCodec ||
		result.BitDepth != info.BitDepth || result.HDR != info.HDR ||
		result.RuntimeSeconds != info.RuntimeSeconds {
		t.Fatalf("ProbeResult and Probe disagree: %#v vs %#v", result, *info)
	}
}

// TestMediaInfoProbeBestPicksLargestVideoInFolder checks the directory branch
// chooses the largest recognised video file.
func TestMediaInfoProbeBestPicksLargestVideoInFolder(t *testing.T) {
	miInstallFakeFfprobe(t, miFakeFfprobeSuccess)

	folder := t.TempDir()
	small := filepath.Join(folder, "small.mkv")
	big := filepath.Join(folder, "big.mkv")
	mustWriteFile(t, small, "x")
	mustWriteFile(t, big, strings.Repeat("x", 64))
	mustWriteFile(t, filepath.Join(folder, "huge.txt"), strings.Repeat("x", 4096))

	info := ProbeBest(folder)
	if info == nil {
		t.Fatal("ProbeBest returned nil for a folder with a video")
	}
	if info.Container != big {
		t.Fatalf("ProbeBest chose %q, want the largest video %q", info.Container, big)
	}

	result, err := ProbeBestResult(folder)
	if err != nil {
		t.Fatalf("ProbeBestResult: %v", err)
	}
	if result.Container != big {
		t.Fatalf("ProbeBestResult chose %q, want %q", result.Container, big)
	}
}

// TestMediaInfoProbeBestResultFolderErrors covers the empty/no-video errors.
func TestMediaInfoProbeBestResultFolderErrors(t *testing.T) {
	miInstallFakeFfprobe(t, miFakeFfprobeSuccess)

	empty := t.TempDir()
	if _, err := ProbeBestResult(empty); err == nil || !strings.Contains(err.Error(), "cartella vuota o senza file video") {
		t.Fatalf("empty folder error = %v", err)
	}

	noVideo := t.TempDir()
	mustWriteFile(t, filepath.Join(noVideo, "readme.txt"), "hello")
	if _, err := ProbeBestResult(noVideo); err == nil || !strings.Contains(err.Error(), "cartella vuota o senza file video") {
		t.Fatalf("no-video folder error = %v", err)
	}
}

// TestMediaInfoProbeReportsFfprobeFailure checks a non-zero ffprobe exit is
// surfaced as an error and Probe returns nil.
func TestMediaInfoProbeReportsFfprobeFailure(t *testing.T) {
	miInstallFakeFfprobe(t, miFakeFfprobeFailure)

	file := filepath.Join(t.TempDir(), "broken.mkv")
	mustWriteFile(t, file, "x")

	_, err := ProbeResult(file)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ProbeResult error = %v, want ffprobe stderr", err)
	}
	if got := Probe(file); got != nil {
		t.Fatalf("Probe = %#v, want nil", got)
	}
}

// TestMediaInfoUnavailableWhenFfprobeMissing checks Available reflects PATH.
func TestMediaInfoUnavailableWhenFfprobeMissing(t *testing.T) {
	miUsePath(t, t.TempDir())
	if Available() {
		t.Fatal("Available should be false without ffprobe on PATH")
	}
	if got := Probe("whatever.mkv"); got != nil {
		t.Fatalf("Probe without ffprobe = %#v, want nil", got)
	}
}

// equalStrings reports whether two string slices are element-wise equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
