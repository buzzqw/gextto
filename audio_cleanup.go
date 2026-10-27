// audio_cleanup.go removes the audio tracks that the container declares in a
// language other than the configured one, right after a file enters the
// library. It never re-encodes: the video and the kept audio are copied packet
// by packet, so quality, HDR metadata and the A/V timing are exactly the same
// as the source. mkvmerge is preferred for MKV because it copies the Matroska
// structure as it is; ffmpeg is the fallback for the other containers.
//
// Safety, in order:
//   - every file is probed with ffprobe: one audio track, no track tagged in
//     the wanted language, or nothing to remove means the file is left alone;
//   - the remux writes a hidden temporary in the same directory; the result is
//     re-probed (stream list, duration, A/V offsets) and only then renamed over
//     the library file, atomically;
//   - the rename swaps the directory entry, not the inode: a hardlink shared
//     with a seeding torrent keeps the old file alive, so seeding is not
//     disturbed and the download's data is never written to;
//   - the job runs in one background worker, never in the torrent event loop;
//     the source is re-stat'ed just before the rename so a concurrent upgrade
//     makes the job give up instead of overwriting it.
//
// Scores: the file name is not touched, and most of the quality score is
// parsed from the name, so the archive score does not move. Two components can
// change: the size bonus (rules.SizeScoreBonus, capped at 100 points) and the
// audio codec/channels, which groundTruthQuality reads from the first remaining
// track (the kept one). Both stay far below the default 200-point upgrade
// threshold, and technicalScoreGain still requires a real technical gain, so
// cleaning cannot trigger a re-download of the same quality.

package gextto

import (
	"bufio"
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
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	audioCleanupSetting         = "audio_cleanup"
	audioCleanupLanguageSetting = "audio_cleanup_language"
	audioCleanupPolicySetting   = "audio_cleanup_policy"

	audioCleanupPolicyAll   = "all"
	audioCleanupPolicyBest  = "best"
	audioCleanupPolicyFirst = "first"

	audioCleanupDefaultLanguage = "ita"
	audioCleanupQueueSize       = 64
	// audioCleanupTimeout bounds one remux: a 4K film on a slow share can take
	// a long time, but a hung tool must not pin the worker forever.
	audioCleanupTimeout = 6 * time.Hour
	// audioCleanupOffsetTolerance is the maximum A/V offset change accepted
	// between the source and the result. Container rounding is milliseconds;
	// 250ms is already a visible desync, so anything above it is refused.
	audioCleanupOffsetTolerance   = 0.25
	audioCleanupDurationTolerance = 2
)

// AudioCleanupOptions is the resolved configuration of one job. It is copied
// when the job is queued so a later settings change cannot alter work already
// scheduled.
type AudioCleanupOptions struct {
	Language string
	Policy   string
}

// AudioCleanupPlan is the decision taken from the container alone.
type AudioCleanupPlan struct {
	Info          MediaInfo
	Keep          []AudioTrackInfo
	Drop          []AudioTrackInfo
	KeepPositions []int // positions inside Info.AudioTracks
	DropPositions []int // positions inside Info.AudioTracks
	SkipReason    string
}

// Skipped reports a file that must not be touched.
func (p AudioCleanupPlan) Skipped() bool { return p.SkipReason != "" }

// AudioCleanupReport is the outcome of one file.
type AudioCleanupReport struct {
	Path         string
	Applied      bool
	SkipReason   string
	Backend      string
	Kept         []AudioTrackInfo
	Dropped      []AudioTrackInfo
	OriginalSize int64
	NewSize      int64
	SavedBytes   int64
	Elapsed      time.Duration
}

// audioCleanupSettings resolves the feature switch and its options.
func audioCleanupSettings(cfg *Config) (AudioCleanupOptions, bool) {
	if cfg == nil || !settingsBool(cfg, audioCleanupSetting, false) {
		return AudioCleanupOptions{}, false
	}
	language := strings.TrimSpace(cfg.Settings[audioCleanupLanguageSetting])
	if language == "" {
		language = audioCleanupDefaultLanguage
	}
	policy := strings.ToLower(strings.TrimSpace(cfg.Settings[audioCleanupPolicySetting]))
	switch policy {
	case audioCleanupPolicyBest, audioCleanupPolicyFirst:
	default:
		policy = audioCleanupPolicyAll
	}
	return AudioCleanupOptions{Language: language, Policy: policy}, true
}

// audioLanguageAliases maps the spellings found in real containers to one
// canonical code. Only the languages a release commonly declares are listed;
// everything else is compared after normalisation.
var audioLanguageAliases = map[string]string{
	"it": "it", "ita": "it", "ital": "it", "italian": "it", "italiano": "it",
	"en": "en", "eng": "en", "english": "en",
	"fr": "fr", "fra": "fr", "fre": "fr", "french": "fr", "francais": "fr", "français": "fr",
	"de": "de", "deu": "de", "ger": "de", "german": "de", "deutsch": "de",
	"es": "es", "spa": "es", "spanish": "es", "espanol": "es", "español": "es",
}

// canonicalAudioLanguage reduces "it-IT", "ITA", "ital" and "Italiano" to "it".
func canonicalAudioLanguage(value string) string {
	code := strings.ToLower(strings.TrimSpace(value))
	code = strings.ReplaceAll(code, "_", "-")
	if index := strings.IndexByte(code, '-'); index > 0 {
		code = code[:index]
	}
	if alias, ok := audioLanguageAliases[code]; ok {
		return alias
	}
	return code
}

// audioTrackMatchesLanguage reports a track explicitly tagged in the wanted
// language. An untagged track ("und", empty) never matches: the caller wants to
// leave such files alone rather than guess.
func audioTrackMatchesLanguage(track AudioTrackInfo, wanted string) bool {
	code := canonicalAudioLanguage(track.Language)
	if code == "" || code == "und" || code == "unk" || code == "unknown" {
		return false
	}
	return code == canonicalAudioLanguage(wanted)
}

// PlanAudioCleanup decides what to keep from the probed container. No file
// name, release tag or external metadata is consulted.
func PlanAudioCleanup(info MediaInfo, opts AudioCleanupOptions) AudioCleanupPlan {
	plan := AudioCleanupPlan{Info: info}
	if len(info.Streams) == 0 && len(info.AudioTracks) == 0 {
		plan.SkipReason = "the file could not be analysed"
		return plan
	}
	if len(info.AudioTracks) == 0 {
		plan.SkipReason = "the file has no audio tracks"
		return plan
	}
	if len(info.AudioTracks) == 1 {
		plan.SkipReason = "the file has a single audio track"
		return plan
	}
	language := strings.TrimSpace(opts.Language)
	if language == "" {
		language = audioCleanupDefaultLanguage
	}
	candidates := make([]int, 0, len(info.AudioTracks))
	for position, track := range info.AudioTracks {
		if audioTrackMatchesLanguage(track, language) {
			candidates = append(candidates, position)
		}
	}
	if len(candidates) == 0 {
		plan.SkipReason = fmt.Sprintf("no audio track is tagged %q", language)
		return plan
	}

	switch opts.Policy {
	case audioCleanupPolicyFirst:
		plan.KeepPositions = []int{candidates[0]}
	case audioCleanupPolicyBest:
		plan.KeepPositions = []int{bestAudioCleanupTrack(info.AudioTracks, candidates)}
	default:
		plan.KeepPositions = append([]int(nil), candidates...)
	}

	kept := make(map[int]bool, len(plan.KeepPositions))
	for _, position := range plan.KeepPositions {
		kept[position] = true
		plan.Keep = append(plan.Keep, info.AudioTracks[position])
	}
	for position, track := range info.AudioTracks {
		if kept[position] {
			continue
		}
		plan.Drop = append(plan.Drop, track)
		plan.DropPositions = append(plan.DropPositions, position)
	}
	if len(plan.Drop) == 0 {
		plan.SkipReason = "every audio track is already in the wanted language"
	}
	return plan
}

// bestAudioCleanupTrack picks the most complete track among the candidates:
// not a commentary, more channels, higher bitrate, better codec, default flag,
// then the first in file order.
func bestAudioCleanupTrack(tracks []AudioTrackInfo, candidates []int) int {
	best := candidates[0]
	for _, position := range candidates[1:] {
		if audioCleanupTrackBetter(tracks[position], tracks[best]) {
			best = position
		}
	}
	return best
}

func audioCleanupTrackBetter(left, right AudioTrackInfo) bool {
	if left.Commentary != right.Commentary {
		return !left.Commentary
	}
	if left.Channels != right.Channels {
		return left.Channels > right.Channels
	}
	if left.Bitrate != right.Bitrate {
		return left.Bitrate > right.Bitrate
	}
	if leftRank, rightRank := audioCleanupCodecRank(left.Codec), audioCleanupCodecRank(right.Codec); leftRank != rightRank {
		return leftRank > rightRank
	}
	if left.Default != right.Default {
		return left.Default
	}
	return left.Index < right.Index
}

func audioCleanupCodecRank(codec string) int {
	codec = strings.ToLower(strings.TrimSpace(codec))
	switch {
	case strings.HasPrefix(codec, "truehd"), strings.HasPrefix(codec, "mlp"):
		return 100
	case strings.HasPrefix(codec, "dts"):
		return 90
	case strings.HasPrefix(codec, "flac"), strings.HasPrefix(codec, "alac"):
		return 85
	case strings.HasPrefix(codec, "eac3"):
		return 70
	case strings.HasPrefix(codec, "ac3"):
		return 60
	case strings.HasPrefix(codec, "opus"):
		return 55
	case strings.HasPrefix(codec, "aac"):
		return 50
	case strings.HasPrefix(codec, "mp3"):
		return 40
	default:
		return 10
	}
}

// RunAudioCleanup probes path, decides, remuxes and publishes the result.
// A skipped file returns no error: skipping is a normal outcome.
func RunAudioCleanup(ctx context.Context, path string, opts AudioCleanupOptions) (report AudioCleanupReport, err error) {
	report.Path = path
	started := time.Now()
	defer func() { report.Elapsed = time.Since(started) }()

	fileInfo, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	if !fileInfo.Mode().IsRegular() {
		report.SkipReason = "not a regular file"
		return report, nil
	}
	if !Available() {
		report.SkipReason = "ffprobe is not installed"
		return report, nil
	}

	media, err := ProbeResult(path)
	if err != nil {
		return report, fmt.Errorf("cannot analyse the file: %w", err)
	}
	report.OriginalSize = fileInfo.Size()

	plan := PlanAudioCleanup(media, opts)
	if plan.Skipped() {
		report.SkipReason = plan.SkipReason
		return report, nil
	}

	extension := filepath.Ext(path)
	temporary := filepath.Join(
		filepath.Dir(path),
		fmt.Sprintf(".%s.gxt-audio-%s%s", strings.TrimSuffix(filepath.Base(path), extension), randomToken(), extension),
	)
	published := false
	defer func() {
		if !published {
			_ = os.Remove(temporary)
		}
	}()

	backend, err := audioCleanupBackend(temporary)
	if err != nil {
		return report, err
	}
	report.Backend = backend

	argv, err := audioCleanupCommand(backend, path, temporary, plan)
	if err != nil && backend == "mkvmerge" {
		// The Matroska ids could not be read: ffmpeg can still do the same copy
		// without touching the track order.
		logging.Debug("mkvmerge cannot be used for this file, falling back to ffmpeg", "path", path, "error", err.Error())
		backend = "ffmpeg"
		report.Backend = backend
		argv, err = audioCleanupCommand(backend, path, temporary, plan)
	}
	if err != nil {
		return report, err
	}
	if err := runAudioCleanupCommand(ctx, argv, media.RuntimeSeconds, filepath.Base(path)); err != nil {
		return report, err
	}
	temporaryInfo, err := os.Stat(temporary)
	if err != nil {
		return report, err
	}
	if temporaryInfo.Size() == 0 {
		return report, errors.New("the remux produced an empty file")
	}

	output, err := ProbeResult(temporary)
	if err != nil {
		return report, fmt.Errorf("cannot analyse the remuxed file: %w", err)
	}
	if err := verifyAudioCleanupOutput(media, output, plan, opts.Language, isMatroskaPath(temporary)); err != nil {
		return report, err
	}

	after, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	if !os.SameFile(fileInfo, after) || fileInfo.Size() != after.Size() || !fileInfo.ModTime().Equal(after.ModTime()) {
		return report, errors.New("the file changed while it was being cleaned")
	}
	if err := os.Chmod(temporary, fileInfo.Mode().Perm()); err != nil {
		return report, err
	}
	if err := os.Rename(temporary, path); err != nil {
		return report, err
	}
	published = true

	report.Applied = true
	report.Kept = plan.Keep
	report.Dropped = plan.Drop
	report.NewSize = temporaryInfo.Size()
	report.SavedBytes = report.OriginalSize - report.NewSize
	return report, nil
}

// audioCleanupBackend chooses mkvmerge only when the output is Matroska and
// the binary exists: there it copies the container structure exactly.
func audioCleanupBackend(temporary string) (string, error) {
	if isMatroskaPath(temporary) {
		if _, err := exec.LookPath("mkvmerge"); err == nil {
			return "mkvmerge", nil
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return "ffmpeg", nil
	}
	return "", errors.New("neither mkvmerge nor ffmpeg is installed")
}

func isMatroskaPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mk3d", ".webm":
		return true
	}
	return false
}

// audioCleanupCommand builds the remux command. In both cases the video, the
// kept audio and everything else are stream-copied: no frame is decoded or
// encoded again.
func audioCleanupCommand(backend, source, target string, plan AudioCleanupPlan) ([]string, error) {
	if backend == "mkvmerge" {
		ids, err := mkvmergeAudioTrackIDs(source, len(plan.Info.AudioTracks))
		if err != nil {
			return nil, err
		}
		keep := make([]string, 0, len(plan.KeepPositions))
		for _, position := range plan.KeepPositions {
			if position < 0 || position >= len(ids) {
				return nil, errors.New("mkvmerge and ffprobe disagree on the audio tracks")
			}
			keep = append(keep, strconv.Itoa(ids[position]))
		}
		return []string{
			"mkvmerge", "--gui-mode", "--output", target,
			"--audio-tracks", strings.Join(keep, ","),
			source,
		}, nil
	}

	argv := []string{
		"ffmpeg", "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-progress", "pipe:1", "-nostats",
		"-i", source,
		"-map", "0:v?", "-map", "0:a",
	}
	// Negative mappings are relative to the audio streams of the input, which
	// is exactly the position ffprobe reported.
	for _, position := range plan.DropPositions {
		argv = append(argv, "-map", fmt.Sprintf("-0:a:%d", position))
	}
	argv = append(argv, "-map", "0:s?")
	if isMatroskaPath(target) {
		argv = append(argv, "-map", "0:t?")
	}
	argv = append(argv,
		"-map_metadata", "0", "-map_chapters", "0",
		"-c", "copy", "-max_muxing_queue_size", "9999",
	)
	if isMatroskaPath(target) {
		// Matroska accepts negative timestamps: forcing a shift could alter the
		// per-track offsets.
		argv = append(argv, "-avoid_negative_ts", "disabled")
	} else {
		argv = append(argv, "-avoid_negative_ts", "make_zero")
	}
	argv = append(argv, target)
	return argv, nil
}

// mkvmergeAudioTrackIDs reads the real mkvmerge track IDs (0-based). They are
// not assumed to equal the ffprobe indexes: the count is checked and the order
// is the container order, the same both tools use.
func mkvmergeAudioTrackIDs(path string, want int) ([]int, error) {
	command := exec.Command("mkvmerge", "-J", path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("mkvmerge cannot identify the file: %s", audioCleanupLastLine(stderr.String()))
	}
	var document struct {
		Tracks []struct {
			ID   int    `json:"id"`
			Type string `json:"type"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, fmt.Errorf("invalid mkvmerge output: %w", err)
	}
	ids := make([]int, 0, len(document.Tracks))
	for _, track := range document.Tracks {
		if strings.EqualFold(track.Type, "audio") {
			ids = append(ids, track.ID)
		}
	}
	if len(ids) != want {
		return nil, fmt.Errorf("mkvmerge reports %d audio tracks, ffprobe %d", len(ids), want)
	}
	return ids, nil
}

// runAudioCleanupCommand runs the tool and logs coarse progress at DEBUG.
func runAudioCleanupCommand(ctx context.Context, argv []string, duration int64, label string) error {
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lastLogged := -1
	for scanner.Scan() {
		percent := audioCleanupProgress(scanner.Text(), duration)
		if percent >= 0 && percent >= lastLogged+25 {
			lastLogged = percent
			logging.Debug("audio cleanup progress", "file", label, "percent", percent)
		}
	}

	waitErr := command.Wait()
	if waitErr == nil {
		return nil
	}
	var exitError *exec.ExitError
	// mkvmerge returns 1 for "finished with warnings", which is not a failure.
	if errors.As(waitErr, &exitError) && argv[0] == "mkvmerge" && exitError.ExitCode() == 1 {
		return nil
	}
	detail := audioCleanupLastLine(stderr.String())
	if detail == "" {
		detail = waitErr.Error()
	}
	return fmt.Errorf("%s: %s", filepath.Base(argv[0]), detail)
}

// audioCleanupProgress reads mkvmerge --gui-mode and ffmpeg -progress output.
func audioCleanupProgress(line string, duration int64) int {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "#GUI#progress"):
		value := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(line, "#GUI#progress")), "%")
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 && parsed <= 100 {
			return parsed
		}
	case strings.HasPrefix(line, "out_time=") && duration > 0:
		if seconds, ok := parseClockSeconds(strings.TrimPrefix(line, "out_time=")); ok {
			return int(seconds / float64(duration) * 100)
		}
	}
	return -1
}

// parseClockSeconds parses the "HH:MM:SS.micro" value ffmpeg writes.
func parseClockSeconds(value string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, false
	}
	hours, errH := strconv.ParseInt(parts[0], 10, 64)
	minutes, errM := strconv.ParseInt(parts[1], 10, 64)
	seconds, errS := strconv.ParseFloat(parts[2], 64)
	if errH != nil || errM != nil || errS != nil {
		return 0, false
	}
	return float64(hours*3600+minutes*60) + seconds, true
}

// verifyAudioCleanupOutput proves that the remux kept every stream it had to
// keep and that the A/V offsets did not move.
func verifyAudioCleanupOutput(source, output MediaInfo, plan AudioCleanupPlan, wanted string, matroska bool) error {
	dropped := make(map[int]bool, len(plan.Drop))
	for _, track := range plan.Drop {
		dropped[track.Index] = true
	}

	expected := make([]string, 0, len(source.Streams))
	for _, stream := range source.Streams {
		switch stream.Type {
		case "video", "audio", "subtitle", "attachment":
		default:
			continue
		}
		if stream.Type == "audio" && dropped[stream.Index] {
			continue
		}
		if !matroska && stream.Type == "attachment" {
			continue
		}
		expected = append(expected, audioCleanupStreamSignature(stream))
	}
	actual := make([]string, 0, len(output.Streams))
	for _, stream := range output.Streams {
		if stream.Type == "data" {
			continue
		}
		actual = append(actual, audioCleanupStreamSignature(stream))
	}
	// The stream order can differ between muxers and container families; the
	// set is what matters, the audio offset check below covers the timing.
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("the remux changed the stream set (expected %v, got %v)", expected, actual)
	}

	outputAudio := make([]StreamInfo, 0, len(plan.Keep))
	for _, stream := range output.Streams {
		if stream.Type == "audio" {
			outputAudio = append(outputAudio, stream)
		}
	}
	if len(outputAudio) != len(plan.Keep) {
		return fmt.Errorf("the remux kept %d audio tracks instead of %d", len(outputAudio), len(plan.Keep))
	}
	for _, track := range output.AudioTracks {
		if !audioTrackMatchesLanguage(track, wanted) {
			return fmt.Errorf("audio track %d is still not tagged %q", track.Index, wanted)
		}
	}
	if source.RuntimeSeconds > 0 && output.RuntimeSeconds > 0 {
		delta := math.Abs(float64(source.RuntimeSeconds - output.RuntimeSeconds))
		if delta > audioCleanupDurationTolerance {
			return fmt.Errorf("the duration changed by %.0f seconds", delta)
		}
	}

	sourceVideo, sourceVideoOK := audioCleanupFirstVideo(source.Streams)
	outputVideo, outputVideoOK := audioCleanupFirstVideo(output.Streams)
	if sourceVideoOK && outputVideoOK {
		for position, track := range plan.Keep {
			if position >= len(outputAudio) || !track.HasStart || !outputAudio[position].HasStart {
				continue
			}
			before := track.StartTime - sourceVideo.StartTime
			after := outputAudio[position].StartTime - outputVideo.StartTime
			if math.Abs(before-after) > audioCleanupOffsetTolerance {
				return fmt.Errorf("the audio offset moved from %.3fs to %.3fs", before, after)
			}
		}
	}
	return nil
}

// audioCleanupFirstVideo returns the first video stream, whose start time is
// the reference every audio offset is measured against.
func audioCleanupFirstVideo(streams []StreamInfo) (StreamInfo, bool) {
	for _, stream := range streams {
		if stream.Type == "video" {
			return stream, true
		}
	}
	return StreamInfo{}, false
}

func audioCleanupStreamSignature(stream StreamInfo) string {
	switch stream.Type {
	case "video", "audio":
		return stream.Type + ":" + stream.Codec
	default:
		return stream.Type
	}
}

func audioCleanupLastLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Background queue
// ---------------------------------------------------------------------------

type audioCleanupJob struct {
	path   string
	key    string
	opts   AudioCleanupOptions
	series *string
	db     *Database
	hash   string
}

var (
	audioCleanupWorkerOnce sync.Once
	audioCleanupQueue      = make(chan audioCleanupJob, audioCleanupQueueSize)
	audioCleanupQueued     sync.Map
	audioCleanupToolWarn   sync.Once
)

// audioCleanupUnavailableNote is shown (and translated) when the option cannot
// be enabled because one of the external tools is missing.
const audioCleanupUnavailableNote = "Servono ffprobe, ffmpeg e mkvmerge: installali e riavvia Gextto per attivare la pulizia."

// audioCleanupLookPath is a variable so tests can simulate a missing tool.
var audioCleanupLookPath = exec.LookPath

// AudioCleanupMissingTools lists the external binaries the feature needs and
// that are not on PATH. All three are required: ffprobe decides, ffmpeg is the
// fallback remuxer, mkvmerge is the preferred one for Matroska.
func AudioCleanupMissingTools() []string {
	missing := make([]string, 0, 3)
	for _, tool := range []string{"ffprobe", "ffmpeg", "mkvmerge"} {
		if _, err := audioCleanupLookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

// AudioCleanupAvailable reports whether the feature can run at all.
func AudioCleanupAvailable() bool {
	return len(AudioCleanupMissingTools()) == 0
}

// WarnIfAudioCleanupUnavailable logs once at startup when the feature is on but
// a tool vanished, so the operator knows why nothing is being cleaned.
func WarnIfAudioCleanupUnavailable(cfg *Config) {
	if cfg == nil || !settingsBool(cfg, audioCleanupSetting, false) {
		return
	}
	if missing := AudioCleanupMissingTools(); len(missing) > 0 {
		logging.Warn(fmt.Sprintf("audio cleanup is enabled but %s is missing, so no file will be cleaned until it is installed and Gextto restarted",
			strings.Join(missing, ", ")))
	}
}

// scheduleAudioCleanup queues the files of an imported release. It is a no-op
// unless the feature is enabled in the settings, and it never blocks the
// caller: a slow remux must not stall the torrent event loop.
func scheduleAudioCleanup(cfg *Config, db *Database, path, hash string, series *string) {
	opts, enabled := audioCleanupSettings(cfg)
	if !enabled || strings.TrimSpace(path) == "" {
		return
	}
	if !AudioCleanupAvailable() {
		audioCleanupToolWarn.Do(func() {
			logging.Warn(fmt.Sprintf("audio cleanup is enabled but %s is missing, so no file will be cleaned",
				strings.Join(AudioCleanupMissingTools(), ", ")))
		})
		return
	}

	files := []string{}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			listed, listErr := VideoFiles(path)
			if listErr != nil {
				logging.Warn("audio cleanup could not list the folder", "path", path, "error", listErr.Error())
				return
			}
			files = listed
		} else if info.Mode().IsRegular() {
			files = []string{path}
		}
	}

	audioCleanupWorkerOnce.Do(func() { go audioCleanupWorker() })
	for _, file := range files {
		key, err := filepath.Abs(file)
		if err != nil {
			key = file
		}
		if _, already := audioCleanupQueued.LoadOrStore(key, struct{}{}); already {
			continue
		}
		job := audioCleanupJob{path: file, key: key, opts: opts, series: series, db: db, hash: hash}
		select {
		case audioCleanupQueue <- job:
		default:
			audioCleanupQueued.Delete(key)
			logging.Warn("audio cleanup queue is full; this file was not cleaned", "path", file)
		}
	}
}

func audioCleanupWorker() {
	for job := range audioCleanupQueue {
		audioCleanupProcess(job)
	}
}

func audioCleanupProcess(job audioCleanupJob) {
	defer audioCleanupQueued.Delete(job.key)
	guard := AcquireArchiveImport(job.series)
	defer guard.Release()

	ctx, cancel := context.WithTimeout(context.Background(), audioCleanupTimeout)
	defer cancel()
	report, err := RunAudioCleanup(ctx, job.path, job.opts)
	switch {
	case err != nil:
		logging.Warn(fmt.Sprintf("🎧 «%s»: audio cleanup failed, the file was left untouched: %v",
			filepath.Base(job.path), err), "path", job.path)
	case report.Applied:
		if job.db != nil {
			if err := job.db.UpdateArchivedSizeAfterCleanup(job.hash, job.path, report.NewSize); err != nil {
				logging.Warn("audio cleanup removed the tracks but the archived size was not updated",
					"path", job.path, "error", err.Error())
			}
		}
		logging.Info(fmt.Sprintf("🎧 «%s»: %d audio track(s) in another language removed, %s saved",
			filepath.Base(job.path), len(report.Dropped), logging.HumanBytesI64(report.SavedBytes)),
			"path", job.path, "backend", report.Backend)
	default:
		logging.Debug("audio cleanup skipped", "path", job.path, "reason", report.SkipReason)
	}
}
