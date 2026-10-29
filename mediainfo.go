// Ground-truth media inspection via `ffprobe` (Sonarr/Radarr use the same
// idea). The release name is only a hint: reading the actual file gives the
// real bit depth, HDR flavour, audio tracks and subtitles, which makes
// upgrades and rescoring trustworthy.
//
// Parsing is a pure function over the `ffprobe` JSON so it can be tested
// without the binary; Probe is the thin process wrapper. When `ffprobe` is
// not installed the probe returns nil and callers keep the filename-based
// quality, so nothing breaks.

package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// AudioTrackInfo is one audio stream exactly as the container declares it.
// The release name is only a hint; these fields are what the file really
// carries, so the audio cleanup can decide without trusting any filename.
type AudioTrackInfo struct {
	Index      int     `json:"index"`
	Codec      string  `json:"codec"`
	Language   string  `json:"language,omitempty"`
	Title      string  `json:"title,omitempty"`
	Channels   int64   `json:"channels,omitempty"`
	Bitrate    int64   `json:"bitrate,omitempty"`
	Default    bool    `json:"default,omitempty"`
	Forced     bool    `json:"forced,omitempty"`
	Commentary bool    `json:"commentary,omitempty"`
	StartTime  float64 `json:"start_time,omitempty"`
	HasStart   bool    `json:"-"`
}

// StreamInfo is one stream of the container, in file order. The cleanup needs
// the full list to prove that nothing but the chosen audio tracks changed.
type StreamInfo struct {
	Index     int     `json:"index"`
	Type      string  `json:"type"`
	Codec     string  `json:"codec"`
	StartTime float64 `json:"start_time,omitempty"`
	HasStart  bool    `json:"-"`
}

// MediaInfo is the parsed result of probing a media file.
type MediaInfo struct {
	Container string `json:"container"`
	Width     int64  `json:"width"`
	Height    int64  `json:"height"`
	// VideoCodec is the raw ffprobe codec_name.
	VideoCodec string `json:"video_codec"`
	BitDepth   int64  `json:"bit_depth"`
	// HDR is "", "HDR10", "HDR10+", "HLG", "DV", "DV HDR10", ...
	HDR               string           `json:"hdr"`
	AudioCodec        string           `json:"audio_codec"`
	AudioChannels     int64            `json:"audio_channels"`
	AudioLanguages    []string         `json:"audio_languages"`
	AudioTracks       []AudioTrackInfo `json:"audio_tracks,omitempty"`
	Streams           []StreamInfo     `json:"streams,omitempty"`
	SubtitleLanguages []string         `json:"subtitle_languages"`
	// RuntimeSeconds is the duration in seconds, rounded.
	RuntimeSeconds int64 `json:"runtime_seconds"`
}

// Resolution is the resolution bucket derived from the real frame size, used
// to sanity-check (or replace) the filename-derived resolution.
func (m MediaInfo) Resolution() string {
	switch {
	case m.Width >= 3800 || m.Height >= 2000:
		return "2160p"
	case m.Width >= 1900 || m.Height >= 1000:
		return "1080p"
	case m.Width >= 1200 || m.Height >= 700:
		return "720p"
	case m.Height >= 540:
		return "576p"
	case m.Height > 0 || m.Width > 0:
		return "480p"
	default:
		return ""
	}
}

// HasHDR reports whether the probe found an HDR flavour.
func (m MediaInfo) HasHDR() bool { return m.HDR != "" }

// ApplyToQuality enriches a filename-derived quality with ground-truth
// attributes. It is additive: it fills HDR/codec/audio/resolution only when
// the probe found them and the filename value is unknown, so it can never
// clear a value or regress an existing file. Used to make upgrade comparisons
// read the real archived file, not just its name.
func (m MediaInfo) ApplyToQuality(quality *models.Quality) {
	if strings.TrimSpace(m.HDR) != "" {
		if strings.TrimSpace(quality.HDR) == "" {
			quality.HDR = m.HDR
		}
		if strings.Contains(strings.ToLower(m.HDR), "dv") {
			quality.IsDV = true
		}
	}
	if codec := strings.TrimSpace(quality.Codec); codec == "" || codec == "unknown" {
		if label, ok := CodecLabel(m.VideoCodec); ok {
			quality.Codec = label
		}
	}
	if audio := strings.TrimSpace(quality.Audio); audio == "" || audio == "unknown" {
		if label, ok := AudioLabel(m.AudioCodec); ok {
			quality.Audio = label
		}
	}
	if resolution := strings.TrimSpace(quality.Resolution); resolution == "" || resolution == "unknown" {
		if resolution := m.Resolution(); resolution != "" {
			quality.Resolution = resolution
		}
	}
	// The streams know the channel layout even when the archive name dropped it
	// (the standard rename keeps only resolution and codec).
	if channels := strings.TrimSpace(quality.AudioChannels); channels == "" || strings.EqualFold(channels, "unknown") {
		switch {
		case m.AudioChannels >= 8:
			quality.AudioChannels = "7.1"
		case m.AudioChannels >= 6:
			quality.AudioChannels = "5.1"
		case m.AudioChannels > 0:
			quality.AudioChannels = "2.0"
		}
	}
	if len(quality.Languages) == 0 && len(m.AudioLanguages) > 0 {
		quality.Languages = slices.Clone(m.AudioLanguages)
	}
}

// AddsToQuality reports whether the probe found something the given quality does
// not carry: the fields a later upgrade decision takes from the file instead of
// the release name. It is the effect ApplyToQuality would have, without changing
// the caller's value.
func (m MediaInfo) AddsToQuality(quality models.Quality) bool {
	enriched := quality
	m.ApplyToQuality(&enriched)
	return enriched.HDR != quality.HDR ||
		enriched.IsDV != quality.IsDV ||
		enriched.Codec != quality.Codec ||
		enriched.Audio != quality.Audio ||
		enriched.Resolution != quality.Resolution ||
		enriched.AudioChannels != quality.AudioChannels ||
		len(enriched.Languages) != len(quality.Languages)
}

// CodecLabel maps an ffprobe codec name to a Gextto quality codec label.
func CodecLabel(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "h265":
		return "h265", true
	case "h264", "avc":
		return "h264", true
	case "av1":
		return "av1", true
	case "vp9":
		return "vp9", true
	case "mpeg2video":
		return "mpeg2", true
	case "vc1":
		return "vc1", true
	default:
		return "", false
	}
}

// AudioLabel maps an ffprobe codec name to a Gextto quality audio label.
func AudioLabel(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "eac3":
		return "ddp", true
	case "ac3":
		return "ac3", true
	case "aac":
		return "aac", true
	case "dts":
		return "dts", true
	case "truehd":
		return "truehd", true
	case "flac":
		return "flac", true
	case "opus":
		return "opus", true
	case "mp3":
		return "mp3", true
	default:
		return "", false
	}
}

// miText trims a JSON string value, reporting false when it is absent, not a
// string, or empty.
func miText(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func miStreamText(stream map[string]any, key string) (string, bool) {
	return miText(stream[key])
}

// miTextValue is miStreamText without the presence flag, for struct fields.
func miTextValue(stream map[string]any, key string) string {
	value, _ := miStreamText(stream, key)
	return value
}

// miIntTextValue reads an integer that ffprobe encodes as a JSON string
// (`bit_rate`, the `BPS` tag) with a numeric fallback.
func miIntTextValue(value any) int64 {
	if text, ok := miText(value); ok {
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return parsed
		}
	}
	return miAsI64(value)
}

// miAsI64 mirrors encoding/json's `Value::as_i64`: only integer JSON numbers
// qualify. Values decoded with a plain `encoding/json` decoder (float64) are
// accepted when they are integral.
func miAsI64(value any) int64 {
	switch number := value.(type) {
	case json.Number:
		if parsed, err := number.Int64(); err == nil {
			return parsed
		}
	case float64:
		if !math.IsNaN(number) && !math.IsInf(number, 0) && math.Trunc(number) == number {
			return int64(number)
		}
	case int64:
		return number
	case int:
		return int64(number)
	}
	return 0
}

// BitDepthFromPixFmt is a best-effort bit depth from the pixel format:
// `yuv420p10le` -> 10, `yuv420p` -> 8. Falls back to 8 when unknown.
func BitDepthFromPixFmt(pixFmt string) int64 {
	digits := ""
	for i := len(pixFmt) - 1; i >= 0; i-- {
		character := pixFmt[i]
		if character < '0' || character > '9' {
			break
		}
		digits = string(character) + digits
	}
	// `yuv420p10le` ends in `le`, so scan the whole string for `p<depth>` and
	// for a 9/10/12/16 token before the endianness suffix.
	if digits != "" {
		if depth, err := strconv.ParseInt(digits, 10, 64); err == nil {
			switch depth {
			case 9, 10, 12, 14, 16:
				return depth
			}
		}
	}
	lowered := strings.ToLower(pixFmt)
	for _, depth := range []int64{16, 14, 12, 10, 9} {
		if strings.Contains(lowered, fmt.Sprintf("p%d", depth)) {
			return depth
		}
	}
	return 8
}

// HDRFromStream maps ffprobe colour metadata to a display HDR label. Dolby
// Vision is detected from the DOVI side data first, then HLG/PQ from the
// transfer.
func HDRFromStream(stream map[string]any) string {
	var sideData []any
	if entries, ok := stream["side_data_list"].([]any); ok {
		sideData = entries
	}
	dovi := false
	for _, entry := range sideData {
		entry, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if kind, ok := entry["side_data_type"].(string); ok {
			if strings.Contains(strings.ToLower(kind), "dovi") {
				dovi = true
				break
			}
		}
	}
	transfer, _ := miStreamText(stream, "color_transfer")
	transfer = strings.ToLower(transfer)
	primaries, _ := miStreamText(stream, "color_primaries")
	primaries = strings.ToLower(primaries)
	hlg := strings.Contains(transfer, "arib-std-b67")
	pq := strings.Contains(transfer, "smpte2084") || strings.Contains(transfer, "pq")
	isHDR := hlg || pq || strings.Contains(primaries, "bt2020")
	if !isHDR && !dovi {
		return ""
	}
	dynamic := false
	if tags, ok := stream["tags"].(map[string]any); ok {
		if _, ok := tags["NUMBER_OF_DYNAMIC_HDR_LAYERS"]; ok {
			dynamic = true
		} else if _, ok := tags["dynamic_hdr_plus"]; ok {
			dynamic = true
		}
	}
	switch {
	case dovi && hlg:
		return "DV HLG"
	case dovi && !hlg && pq && dynamic:
		return "DV HDR10+"
	case dovi && !hlg && pq && !dynamic:
		return "DV"
	case !dovi && hlg:
		return "HLG"
	case !dovi && !hlg && pq && dynamic:
		return "HDR10+"
	case !dovi && !hlg && pq && !dynamic:
		return "HDR10"
	default:
		return "HDR"
	}
}

// ParseFfprobe parses the JSON produced by
// `ffprobe -v error -print_format json -show_format -show_streams`.
func ParseFfprobe(root map[string]any) MediaInfo {
	streams, _ := root["streams"].([]any)
	isMotionImage := func(codec string) bool {
		return codec == "mjpeg" || codec == "png" || codec == "bmp" || codec == "gif"
	}

	var video map[string]any
	for _, item := range streams {
		stream, ok := item.(map[string]any)
		if !ok {
			continue
		}
		codecType, _ := stream["codec_type"].(string)
		if codecType != "video" {
			continue
		}
		codecName, _ := stream["codec_name"].(string)
		if isMotionImage(codecName) {
			continue
		}
		video = stream
		break
	}

	var info MediaInfo
	if video != nil {
		info.Width = miAsI64(video["width"])
		info.Height = miAsI64(video["height"])
		info.VideoCodec, _ = miStreamText(video, "codec_name")
		if pixFmt, ok := miStreamText(video, "pix_fmt"); ok {
			info.BitDepth = BitDepthFromPixFmt(pixFmt)
		} else {
			info.BitDepth = 8
		}
		info.HDR = HDRFromStream(video)
	}

	for _, item := range streams {
		stream, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := StreamInfo{
			Index: int(miAsI64(stream["index"])),
			Type:  miTextValue(stream, "codec_type"),
			Codec: miTextValue(stream, "codec_name"),
		}
		if start, ok := miText(stream["start_time"]); ok {
			if seconds, err := strconv.ParseFloat(start, 64); err == nil {
				entry.StartTime = seconds
				entry.HasStart = true
			}
		}
		info.Streams = append(info.Streams, entry)
	}

	var firstAudio map[string]any
	for _, item := range streams {
		stream, ok := item.(map[string]any)
		if !ok {
			continue
		}
		codecType, _ := stream["codec_type"].(string)
		if codecType != "audio" {
			continue
		}
		if firstAudio == nil {
			firstAudio = stream
		}
		track := AudioTrackInfo{
			Index:    int(miAsI64(stream["index"])),
			Codec:    miTextValue(stream, "codec_name"),
			Channels: miAsI64(stream["channels"]),
			Bitrate:  miIntTextValue(stream["bit_rate"]),
		}
		if tags, ok := stream["tags"].(map[string]any); ok {
			if language, ok := miText(tags["language"]); ok {
				track.Language = strings.ToLower(language)
				if !slices.Contains(info.AudioLanguages, track.Language) {
					info.AudioLanguages = append(info.AudioLanguages, track.Language)
				}
			}
			if title, ok := miText(tags["title"]); ok {
				track.Title = title
				lower := strings.ToLower(title)
				track.Commentary = strings.Contains(lower, "comment") || strings.Contains(lower, "commento")
			}
			if track.Bitrate == 0 {
				track.Bitrate = miIntTextValue(tags["BPS"])
			}
		}
		if disposition, ok := stream["disposition"].(map[string]any); ok {
			track.Default = miAsI64(disposition["default"]) != 0
			track.Forced = miAsI64(disposition["forced"]) != 0
		}
		if start, ok := miText(stream["start_time"]); ok {
			if seconds, err := strconv.ParseFloat(start, 64); err == nil {
				track.StartTime = seconds
				track.HasStart = true
			}
		}
		info.AudioTracks = append(info.AudioTracks, track)
	}
	if firstAudio != nil {
		info.AudioCodec, _ = miStreamText(firstAudio, "codec_name")
		info.AudioChannels = miAsI64(firstAudio["channels"])
	}

	for _, item := range streams {
		stream, ok := item.(map[string]any)
		if !ok {
			continue
		}
		codecType, _ := stream["codec_type"].(string)
		if codecType != "subtitle" {
			continue
		}
		if tags, ok := stream["tags"].(map[string]any); ok {
			if language, ok := miText(tags["language"]); ok {
				language = strings.ToLower(language)
				if !slices.Contains(info.SubtitleLanguages, language) {
					info.SubtitleLanguages = append(info.SubtitleLanguages, language)
				}
			}
		}
	}

	if format, ok := root["format"].(map[string]any); ok {
		info.Container, _ = miStreamText(format, "format_name")
		if duration, ok := miStreamText(format, "duration"); ok {
			if seconds, err := strconv.ParseFloat(duration, 64); err == nil {
				info.RuntimeSeconds = int64(math.Round(seconds))
			}
		}
	}
	return info
}

var miVideoExtensions = map[string]bool{
	"mkv":  true,
	"mp4":  true,
	"avi":  true,
	"m4v":  true,
	"ts":   true,
	"mov":  true,
	"wmv":  true,
	"webm": true,
}

// ProbeBest probes a video file, or the largest video file directly inside a
// directory (archive entries are sometimes stored as a folder). It returns nil
// on failure.
func ProbeBest(path string) *MediaInfo {
	info, err := ProbeBestResult(path)
	if err != nil {
		return nil
	}
	return &info
}

// ProbeBestResult is like ProbeBest but keeps the failure reason, so the
// backfill can tell the user which file failed and why instead of only
// counting it.
func ProbeBestResult(path string) (MediaInfo, error) {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return ProbeResult(path)
	}
	if err == nil && info.IsDir() {
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return MediaInfo{}, fmt.Errorf("folder not readable: %w", readErr)
		}
		bestSize := int64(-1)
		bestPath := ""
		anyVideo := false
		for _, entry := range entries {
			candidate := filepath.Join(path, entry.Name())
			candidateInfo, statErr := os.Stat(candidate)
			if statErr != nil || !candidateInfo.Mode().IsRegular() {
				continue
			}
			extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(entry.Name()), "."))
			if !miVideoExtensions[extension] {
				continue
			}
			anyVideo = true
			size := candidateInfo.Size()
			if bestSize < 0 || size > bestSize {
				bestSize = size
				bestPath = candidate
			}
		}
		switch {
		case bestSize >= 0:
			return ProbeResult(bestPath)
		case anyVideo:
			return MediaInfo{}, errors.New("no readable video file in the folder")
		default:
			return MediaInfo{}, errors.New("empty folder or no video files")
		}
	}
	if err == nil {
		return MediaInfo{}, errors.New("invalid path: it is neither a file nor a folder")
	}
	return MediaInfo{}, errors.New("file or folder not found")
}

// Available reports whether `ffprobe` can be executed. It lets the scheduler
// skip runs instead of retrying every entry when the binary is missing.
func Available() bool {
	command := exec.Command("ffprobe", "-version")
	return command.Run() == nil
}

// Probe runs `ffprobe` on path. It returns nil when the binary is missing, the
// file is unreadable, or the output is not valid JSON.
func Probe(path string) *MediaInfo {
	info, err := ProbeResult(path)
	if err != nil {
		return nil
	}
	return &info
}

// ProbeResult runs `ffprobe` on path and returns a human-readable reason on
// failure, so the caller can log the exact file that could not be analyzed.
func ProbeResult(path string) (MediaInfo, error) {
	return probeFileWithin(path, 60*time.Second)
}

// probeFileWithin runs `ffprobe` on path with a caller-chosen bound: the
// backfill worker can wait a minute, a decision inside the search cycle cannot.
func probeFileWithin(path string, timeout time.Duration) (MediaInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return MediaInfo{}, errors.New("empty path")
	}
	// Reject a leading dash so the path can never be parsed as an ffprobe
	// option (argument injection). The daemon only probes local media.
	if strings.HasPrefix(path, "-") {
		return MediaInfo{}, errors.New("invalid path")
	}
	// A bounded timeout keeps a hung probe from pinning a worker, and the
	// protocol whitelist keeps ffprobe from following remote URLs (SSRF).
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-protocol_whitelist", "file,pipe,data,crypto",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			stderr := strings.TrimSpace(strings.ToValidUTF8(string(exitError.Stderr), "\uFFFD"))
			if stderr == "" {
				if code := exitError.ExitCode(); code >= 0 {
					return MediaInfo{}, fmt.Errorf("ffprobe exited with status: %d", code)
				}
				return MediaInfo{}, fmt.Errorf("ffprobe exited with status %s", exitError.ProcessState)
			}
			return MediaInfo{}, errors.New(miTruncateRunes(stderr, 300))
		}
		return MediaInfo{}, fmt.Errorf("cannot run ffprobe: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return MediaInfo{}, fmt.Errorf("invalid ffprobe output: %w", err)
	}
	if object, ok := root.(map[string]any); ok {
		return ParseFfprobe(object), nil
	}
	return MediaInfo{}, nil
}

// miTruncateRunes returns the first limit runes of value.
func miTruncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	count := 0
	for index := range value {
		if count == limit {
			return value[:index]
		}
		count++
	}
	return value
}
