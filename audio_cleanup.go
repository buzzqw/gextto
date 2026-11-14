// audio_cleanup.go removes the audio tracks that the container declares in a
// language other than the configured one, right after a file enters the
// library. It never re-encodes: the video and the kept audio are copied packet
// by packet, so quality, HDR metadata and the A/V timing are exactly the same
// as the source. mkvmerge is tried first for MKV because it keeps the Matroska
// structure; when its copy does not pass the checks, ffmpeg tries again, and it
// is the only tool for the other containers.
//
// Safety, in order:
//   - every file is probed with ffprobe: one audio track, no track tagged in
//     the wanted language, or nothing to remove means the file is left alone;
//     untagged tracks are kept, removed or make the file untouchable, as the
//     operator chose;
//   - the remux writes a hidden temporary in the same directory, only when the
//     filesystem has room for it;
//   - the result is re-probed: the same streams per type, in the same order,
//     with the same codecs, languages and flags (a subtitle cannot switch itself
//     on), the same duration and A/V offsets;
//   - then every packet of every kept stream is compared with the source: the
//     same MD5 of the payload and the same relative timestamps, so the copy is
//     byte-for-byte the source minus the removed tracks. Only then the file is
//     renamed over the library file, atomically;
//   - the rename swaps the directory entry, not the inode: a hardlink shared
//     with a seeding torrent keeps the old file alive, so seeding is not
//     disturbed and the download's data is never written to;
//   - the job runs in one background worker, never in the torrent event loop;
//     the source is re-stat'ed just before the rename so a concurrent upgrade
//     makes the job give up instead of overwriting it;
//   - on a network share the permissions are left to the share (a chmod would
//     strip the ACLs of a NAS): the new file inherits them from the folder.
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
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	audioCleanupSetting         = "audio_cleanup"
	audioCleanupLanguageSetting = "audio_cleanup_language"
	audioCleanupPolicySetting   = "audio_cleanup_policy"
	audioCleanupUntaggedSetting = "audio_cleanup_untagged"

	audioCleanupPolicyAll   = "all"
	audioCleanupPolicyBest  = "best"
	audioCleanupPolicyFirst = "first"

	// What to do with an audio track that declares no language.
	audioCleanupUntaggedKeep   = "keep"
	audioCleanupUntaggedRemove = "remove"
	audioCleanupUntaggedSkip   = "skip"

	audioCleanupDefaultLanguage = "ita"
	// audioCleanupTimeout bounds one file, remux and checks included: a 4K film
	// on a slow share can take a long time, but a hung tool must not pin the
	// worker forever.
	audioCleanupTimeout = 6 * time.Hour
	// audioCleanupOffsetTolerance is the maximum A/V offset change accepted
	// between the source and the result. Container rounding is milliseconds;
	// 250ms is already a visible desync, so anything above it is refused.
	audioCleanupOffsetTolerance   = 0.25
	audioCleanupDurationTolerance = 2
	// audioCleanupSpaceMargin is kept free on top of the temporary copy.
	audioCleanupSpaceMargin = 256 << 20
)

// AudioCleanupOptions is the resolved configuration of one job. It is copied
// when the job is queued so a later settings change cannot alter work already
// scheduled.
type AudioCleanupOptions struct {
	Language string `json:"language"`
	Policy   string `json:"policy"`
	Untagged string `json:"untagged"`
}

// AudioCleanupPlan is the decision taken from the container alone.
type AudioCleanupPlan struct {
	Info          MediaInfo
	Keep          []AudioTrackInfo
	Drop          []AudioTrackInfo
	KeepPositions []int // positions inside Info.AudioTracks, in file order
	DropPositions []int // positions inside Info.AudioTracks, in file order
	// DefaultPosition is the kept track that becomes the default one because
	// the default track was removed; -1 when the flags stay as they are.
	DefaultPosition int
	SkipReason      string
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
	return AudioCleanupOptions{
		Language: language,
		Policy:   policy,
		Untagged: normalizeAudioCleanupUntagged(cfg.Settings[audioCleanupUntaggedSetting]),
	}, true
}

// normalizeAudioCleanupUntagged maps the setting to one of the three modes;
// anything unknown keeps the tracks, the choice that never loses audio.
func normalizeAudioCleanupUntagged(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case audioCleanupUntaggedRemove:
		return audioCleanupUntaggedRemove
	case audioCleanupUntaggedSkip:
		return audioCleanupUntaggedSkip
	default:
		return audioCleanupUntaggedKeep
	}
}

// audioLanguageAliases maps the spellings found in real containers to one
// canonical code: ISO 639-1, ISO 639-2/B and 639-2/T, and the English and
// native names releases write. It covers every language of the settings list;
// anything else is compared after normalisation.
var audioLanguageAliases = func() map[string]string {
	groups := map[string][]string{
		"it": {"ita", "ital", "italian", "italiano"},
		"en": {"eng", "english"},
		"fr": {"fra", "fre", "french", "francais", "français"},
		"de": {"deu", "ger", "german", "deutsch"},
		"es": {"spa", "spanish", "espanol", "español", "castellano"},
		"pt": {"por", "portuguese", "portugues", "português"},
		"nl": {"nld", "dut", "dutch", "nederlands"},
		"pl": {"pol", "polish", "polski"},
		"sv": {"swe", "swedish", "svenska"},
		"no": {"nor", "nob", "nno", "nb", "nn", "norwegian", "norsk"},
		"da": {"dan", "danish", "dansk"},
		"fi": {"fin", "finnish", "suomi"},
		"cs": {"ces", "cze", "czech", "čeština", "cestina"},
		"hu": {"hun", "hungarian", "magyar"},
		"ro": {"ron", "rum", "romanian", "română", "romana"},
		"el": {"ell", "gre", "greek"},
		"tr": {"tur", "turkish", "türkçe", "turkce"},
		"ru": {"rus", "russian"},
		"uk": {"ukr", "ukrainian"},
		"ar": {"ara", "arabic"},
		"he": {"heb", "hebrew", "iw"},
		"hi": {"hin", "hindi"},
		"ja": {"jpn", "japanese"},
		"ko": {"kor", "korean"},
		"zh": {"zho", "chi", "chinese"},
	}
	aliases := make(map[string]string)
	for canonical, spellings := range groups {
		aliases[canonical] = canonical
		for _, spelling := range spellings {
			aliases[spelling] = canonical
		}
	}
	return aliases
}()

// audioUntaggedLanguages are the values that declare no real language.
var audioUntaggedLanguages = map[string]bool{
	"": true, "und": true, "unk": true, "unknown": true, "zxx": true,
	"mul": true, "mis": true, "none": true, "n/a": true, "nan": true,
}

// canonicalAudioLanguage reduces "it-IT", "ITA", "ital" and "Italiano" to "it".
func canonicalAudioLanguage(value string) string {
	// Some muxing tools write the tag with quotes around it ('ita').
	code := strings.ToLower(strings.Trim(strings.TrimSpace(value), `'"`+"`"+` `))
	code = strings.ReplaceAll(code, "_", "-")
	if index := strings.IndexByte(code, '-'); index > 0 {
		code = code[:index]
	}
	if alias, ok := audioLanguageAliases[code]; ok {
		return alias
	}
	return code
}

// audioLanguageUntagged reports a language value that names no language.
func audioLanguageUntagged(value string) bool {
	return audioUntaggedLanguages[canonicalAudioLanguage(value)]
}

// audioTrackMatchesLanguage reports a track explicitly tagged in the wanted
// language. An untagged track ("und", empty) never matches: what happens to it
// is the operator's choice (AudioCleanupOptions.Untagged), never a guess.
func audioTrackMatchesLanguage(track AudioTrackInfo, wanted string) bool {
	if audioLanguageUntagged(track.Language) {
		return false
	}
	return canonicalAudioLanguage(track.Language) == canonicalAudioLanguage(wanted)
}

// PlanAudioCleanup decides what to keep from the probed container. No file
// name, release tag or external metadata is consulted.
func PlanAudioCleanup(info MediaInfo, opts AudioCleanupOptions) AudioCleanupPlan {
	plan := AudioCleanupPlan{Info: info, DefaultPosition: -1}
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

	var chosen []int
	switch opts.Policy {
	case audioCleanupPolicyFirst:
		chosen = []int{candidates[0]}
	case audioCleanupPolicyBest:
		chosen = []int{bestAudioCleanupTrack(info.AudioTracks, candidates)}
	default:
		chosen = append([]int(nil), candidates...)
	}

	kept := make(map[int]bool, len(info.AudioTracks))
	for _, position := range chosen {
		kept[position] = true
	}
	untaggedMode := normalizeAudioCleanupUntagged(opts.Untagged)
	untagged := 0
	for position, track := range info.AudioTracks {
		if kept[position] || !audioLanguageUntagged(track.Language) {
			continue
		}
		untagged++
		if untaggedMode == audioCleanupUntaggedKeep {
			kept[position] = true
		}
	}
	if untagged > 0 && untaggedMode == audioCleanupUntaggedSkip {
		plan.SkipReason = fmt.Sprintf("%d audio track(s) declare no language", untagged)
		return plan
	}

	keptDefault, droppedDefault := false, false
	for position, track := range info.AudioTracks {
		if kept[position] {
			plan.Keep = append(plan.Keep, track)
			plan.KeepPositions = append(plan.KeepPositions, position)
			keptDefault = keptDefault || track.Default
			continue
		}
		plan.Drop = append(plan.Drop, track)
		plan.DropPositions = append(plan.DropPositions, position)
		droppedDefault = droppedDefault || track.Default
	}
	if len(plan.Drop) == 0 {
		plan.SkipReason = "there is no audio track to remove"
		return plan
	}
	// The default track was removed: the chosen language track takes its place,
	// so a player that follows the flag does not start on no track at all.
	if droppedDefault && !keptDefault {
		plan.DefaultPosition = chosen[0]
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

// RunAudioCleanup probes path, decides, remuxes, checks and publishes the
// result. A skipped file returns no error: skipping is a normal outcome. On any
// error the library file is exactly as it was.
func RunAudioCleanup(ctx context.Context, path string, opts AudioCleanupOptions) (report AudioCleanupReport, err error) {
	report.Path = path
	started := time.Now()
	defer func() { report.Elapsed = time.Since(started) }()

	fileInfo, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Upgraded, renamed or deleted while it waited in the queue.
		report.SkipReason = "the file is no longer there"
		return report, nil
	}
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

	media, err := probeFileWithinContext(ctx, path, 5*time.Minute)
	if err != nil {
		return report, fmt.Errorf("cannot analyse the file: %w", err)
	}
	report.OriginalSize = fileInfo.Size()

	plan := PlanAudioCleanup(media, opts)
	if plan.Skipped() {
		report.SkipReason = plan.SkipReason
		return report, nil
	}

	// A crash or a restart in the middle of an earlier attempt leaves its
	// hidden copy behind: only this worker writes them, so it is safe to go.
	removeAudioCleanupLeftovers(path)
	if err := audioCleanupCheckSpace(path, fileInfo.Size(), plan); err != nil {
		return report, err
	}

	backends := audioCleanupBackends(path)
	if len(backends) == 0 {
		return report, errors.New("neither mkvmerge nor ffmpeg is installed")
	}
	temporary := ""
	failures := make([]string, 0, len(backends))
	refusals := make([]string, 0, len(backends))
	// The copy was refused by a check, not by a tool that broke: when every
	// tool ends that way the file cannot be cleaned, now or later.
	wouldDiffer := true
	for attempt, backend := range backends {
		candidate := audioCleanupTemporaryPath(path)
		attemptErr := audioCleanupAttempt(ctx, backend, path, candidate, media, plan, opts)
		if attemptErr == nil {
			temporary = candidate
			report.Backend = backend
			break
		}
		_ = os.Remove(candidate)
		if ctx.Err() != nil {
			return report, fmt.Errorf("%s: %w", backend, attemptErr)
		}
		if !errors.Is(attemptErr, errAudioCleanupWouldDiffer) {
			wouldDiffer = false
		} else {
			refusals = append(refusals, audioCleanupRefusal(attemptErr))
		}
		failures = append(failures, fmt.Sprintf("%s: %v", backend, attemptErr))
		if attempt+1 < len(backends) {
			logging.Debug("the remuxed copy did not pass the checks, trying the next tool",
				"path", path, "tool", backend, "error", attemptErr.Error())
		}
	}
	if temporary == "" {
		logging.Debug("the audio cleanup produced no copy that could replace the file", "path", path,
			"attempts", strings.Join(failures, "; "))
		if wouldDiffer {
			return report, audioCleanupRefusedCopy(refusals)
		}
		return report, errors.New(strings.Join(failures, "; "))
	}
	published := false
	defer func() {
		if !published {
			_ = os.Remove(temporary)
		}
	}()

	temporaryInfo, err := os.Stat(temporary)
	if err != nil {
		return report, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return report, err
	}
	if !os.SameFile(fileInfo, after) || fileInfo.Size() != after.Size() || !fileInfo.ModTime().Equal(after.ModTime()) {
		return report, errors.New("the file changed while it was being cleaned")
	}
	audioCleanupCopyPermissions(path, temporary, fileInfo, temporaryInfo)
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

// audioCleanupAttempt writes the cleaned copy with one tool and proves it is
// the source minus the dropped tracks. Any error means the copy is not used.
func audioCleanupAttempt(ctx context.Context, backend, source, temporary string, media MediaInfo, plan AudioCleanupPlan, opts AudioCleanupOptions) error {
	argv, err := audioCleanupCommand(backend, source, temporary, plan)
	if err != nil {
		return err
	}
	if err := runAudioCleanupCommand(ctx, argv, media.RuntimeSeconds, filepath.Base(source)); err != nil {
		return err
	}
	temporaryInfo, err := os.Stat(temporary)
	if err != nil {
		return err
	}
	if temporaryInfo.Size() == 0 {
		return errors.New("the remux produced an empty file")
	}
	output, err := probeFileWithinContext(ctx, temporary, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("cannot analyse the remuxed file: %w", err)
	}
	if err := verifyAudioCleanupOutput(media, output, plan, opts, isMatroskaPath(temporary)); err != nil {
		return &audioCleanupDiffError{reason: err.Error()}
	}
	if err := verifyAudioCleanupPackets(ctx, source, temporary, media, output, plan, isMatroskaPath(temporary)); err != nil {
		if errors.Is(err, errAudioCleanupWouldDiffer) {
			// Which stream, which packet: a person cannot act on the detail, so
			// it goes to DEBUG and the log line says only that the copy is not
			// the original minus the removed tracks.
			logging.Debug("the remuxed copy differs from the source packet by packet",
				"path", source, "tool", backend, "error", err.Error())
			return &audioCleanupDiffError{}
		}
		return err
	}
	return nil
}

// errAudioCleanupWouldDiffer marks a remux that one of the checks refused
// because the copy is not exactly the source minus the dropped tracks. The
// same file would be refused in the same way at every attempt — it is a
// permanent outcome, not a failure to retry — so the library remembers it and
// stops probing the file unless it changes.
var errAudioCleanupWouldDiffer = errors.New("the cleaned copy would not be identical to the source")

// audioCleanupDiffError says why the copy was refused. Error is one sentence a
// person can follow ("the cleaned copy would be 10 seconds shorter"): the callers put
// it in their own sentence, so the general "would not be identical" is not
// repeated. reason is empty when the difference has no short explanation.
type audioCleanupDiffError struct {
	reason string
}

func (e *audioCleanupDiffError) Error() string {
	if e.reason != "" {
		return e.reason
	}
	return errAudioCleanupWouldDiffer.Error()
}

// Unwrap keeps errors.Is(err, errAudioCleanupWouldDiffer) working.
func (e *audioCleanupDiffError) Unwrap() error { return errAudioCleanupWouldDiffer }

// audioCleanupRefusal returns the reason of a copy the checks refused, or "".
func audioCleanupRefusal(err error) string {
	var diff *audioCleanupDiffError
	if errors.As(err, &diff) {
		return diff.reason
	}
	return ""
}

// audioCleanupRefusedNote is the parenthesis a log line puts after "would
// change the file", or "" when the difference has no explanation worth
// reading.
func audioCleanupRefusedNote(err error) string {
	if reason := audioCleanupRefusal(err); reason != "" {
		return " (" + reason + ")"
	}
	return ""
}

// audioCleanupRefusedCopy builds the error for a file no tool could clean
// without changing it. The parenthesis appears only when every tool refused
// for the same reason and that reason is readable by a person.
func audioCleanupRefusedCopy(reasons []string) error {
	unique := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if !slices.Contains(unique, reason) {
			unique = append(unique, reason)
		}
	}
	if len(unique) == 1 && unique[0] != "" {
		return &audioCleanupDiffError{reason: unique[0]}
	}
	return &audioCleanupDiffError{}
}

// audioCleanupBackends lists the tools to try, in order. For Matroska
// mkvmerge comes first and ffmpeg is the second chance: mkvmerge rewrites some
// codec headers and subtitle packets, which the packet check refuses, while
// ffmpeg copies the bytes as they are.
func audioCleanupBackends(path string) []string {
	backends := make([]string, 0, 2)
	if isMatroskaPath(path) {
		if _, err := exec.LookPath("mkvmerge"); err == nil {
			backends = append(backends, "mkvmerge")
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		backends = append(backends, "ffmpeg")
	}
	return backends
}

func isMatroskaPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mk3d", ".webm":
		return true
	}
	return false
}

// audioCleanupTemporaryMarker names the hidden copies, so a leftover of a
// crashed attempt can be recognised and removed.
const audioCleanupTemporaryMarker = ".gxt-audio-"

func audioCleanupTemporaryPath(path string) string {
	extension := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), extension)
	return filepath.Join(filepath.Dir(path), "."+stem+audioCleanupTemporaryMarker+randomToken()+extension)
}

// removeAudioCleanupLeftovers deletes the hidden copies an interrupted attempt
// left next to path. The match is literal (no glob): release names are full of
// brackets.
func removeAudioCleanupLeftovers(path string) {
	directory := filepath.Dir(path)
	extension := filepath.Ext(path)
	prefix := "." + strings.TrimSuffix(filepath.Base(path), extension) + audioCleanupTemporaryMarker
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, extension) {
			continue
		}
		leftover := filepath.Join(directory, name)
		if err := os.Remove(leftover); err == nil {
			logging.Info(fmt.Sprintf("🎧 «%s»: removed the unfinished copy left by an interrupted audio cleanup",
				filepath.Base(path)), "path", leftover)
		}
	}
}

// audioCleanupCheckSpace refuses to start when the folder cannot hold the
// temporary copy plus a margin: a full disk would stop downloads and imports.
func audioCleanupCheckSpace(path string, size int64, plan AudioCleanupPlan) error {
	free := FreeSpaceBytes(filepath.Dir(path))
	if free == nil {
		return nil
	}
	need := audioCleanupEstimatedSize(size, plan)
	if *free < uint64(need)+audioCleanupSpaceMargin {
		return fmt.Errorf("not enough free space for the cleaned copy: about %s needed, %s free",
			logging.HumanBytesI64(need+audioCleanupSpaceMargin), logging.HumanBytesI64(clampFreeBytes(*free)))
	}
	return nil
}

func clampFreeBytes(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

// audioCleanupEstimatedSize is the size of the cleaned copy: the file minus
// the bitrate of the removed tracks when the container declares it.
func audioCleanupEstimatedSize(size int64, plan AudioCleanupPlan) int64 {
	removed := int64(0)
	if plan.Info.RuntimeSeconds > 0 {
		for _, track := range plan.Drop {
			removed += track.Bitrate / 8 * plan.Info.RuntimeSeconds
		}
	}
	if removed <= 0 || removed >= size {
		return size
	}
	return size - removed
}

// audioCleanupCopyPermissions gives the cleaned copy the source's mode on a
// local disk. On a network share it does nothing: the server decides, and on a
// NAS with ACLs a chmod would replace them. A failure is not fatal either: the
// copy keeps the mode the folder gives new files.
func audioCleanupCopyPermissions(path, temporary string, source, copied os.FileInfo) {
	if source.Mode().Perm() == copied.Mode().Perm() || networkFilesystem(filepath.Dir(path)) {
		return
	}
	if err := os.Chmod(temporary, source.Mode().Perm()); err != nil {
		logging.Debug("the cleaned copy keeps the permissions of the folder", "path", path, "error", err.Error())
	}
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
		argv := []string{
			"mkvmerge", "--gui-mode", "--output", target,
			"--audio-tracks", strings.Join(keep, ","),
		}
		if plan.DefaultPosition >= 0 && plan.DefaultPosition < len(ids) {
			argv = append(argv, "--default-track-flag", fmt.Sprintf("%d:1", ids[plan.DefaultPosition]))
		}
		return append(argv, source), nil
	}

	matroska := isMatroskaPath(target)
	argv := []string{
		"ffmpeg", "-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-progress", "pipe:1", "-nostats",
		"-i", source,
	}
	// Every kept stream is mapped by its index, so the copy keeps the order of
	// the source. Data streams (timecodes, teletext) and, outside Matroska,
	// attachments have no place in the output container.
	mapped := audioCleanupMappedStreams(plan, matroska)
	for _, stream := range mapped {
		argv = append(argv, "-map", fmt.Sprintf("0:%d", stream.Index))
	}
	argv = append(argv,
		"-map_metadata", "0", "-map_chapters", "0",
		"-c", "copy", "-max_muxing_queue_size", "9999",
	)
	if matroska {
		// Matroska accepts negative timestamps: forcing a shift could alter the
		// per-track offsets.
		argv = append(argv, "-avoid_negative_ts", "disabled", "-default_mode", "passthrough")
		// Explicit flags for every stream: without them ffmpeg marks the first
		// track of a type as default when none is, and a subtitle would switch
		// itself on in the player.
		promoted := -1
		if plan.DefaultPosition >= 0 && plan.DefaultPosition < len(plan.Info.AudioTracks) {
			promoted = plan.Info.AudioTracks[plan.DefaultPosition].Index
		}
		for output, stream := range mapped {
			if stream.Type == "attachment" {
				continue
			}
			flags := slices.Clone(stream.Disposition)
			if stream.Index == promoted && !slices.Contains(flags, "default") {
				flags = append(flags, "default")
				slices.Sort(flags)
			}
			value := strings.Join(flags, "+")
			if value == "" {
				value = "0"
			}
			argv = append(argv, fmt.Sprintf("-disposition:%d", output), value)
		}
	} else {
		argv = append(argv, "-avoid_negative_ts", "make_zero")
	}
	argv = append(argv, target)
	return argv, nil
}

// audioCleanupMappedStreams lists the source streams that must reach the
// output, in file order.
func audioCleanupMappedStreams(plan AudioCleanupPlan, matroska bool) []StreamInfo {
	dropped := make(map[int]bool, len(plan.Drop))
	for _, track := range plan.Drop {
		dropped[track.Index] = true
	}
	streams := make([]StreamInfo, 0, len(plan.Info.Streams))
	for _, stream := range plan.Info.Streams {
		switch stream.Type {
		case "video", "audio", "subtitle":
		case "attachment":
			if !matroska {
				continue
			}
		default:
			continue
		}
		if stream.Type == "audio" && dropped[stream.Index] {
			continue
		}
		streams = append(streams, stream)
	}
	return streams
}

// mkvmergeIdentifyTimeout bounds "mkvmerge -J", which reads only the headers.
const mkvmergeIdentifyTimeout = 2 * time.Minute

// mkvmergeAudioTrackIDs reads the real mkvmerge track IDs (0-based). They are
// not assumed to equal the ffprobe indexes: the count is checked and the order
// is the container order, the same both tools use.
func mkvmergeAudioTrackIDs(path string, want int) ([]int, error) {
	// Identifying reads only the headers. The bound keeps a share that stops
	// answering from holding the night maintenance forever on this call.
	ctx, cancel := context.WithTimeout(context.Background(), mkvmergeIdentifyTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "mkvmerge", "-J", path)
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
	// mkvmerge returns 1 for "finished with warnings", which is not a failure:
	// the checks that follow decide whether the copy is good.
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

// audioCleanupStreamsByType groups the streams the cleanup compares, in file
// order. Data streams are not compared: they are not copied.
func audioCleanupStreamsByType(streams []StreamInfo, matroska bool) map[string][]StreamInfo {
	grouped := make(map[string][]StreamInfo, 4)
	for _, stream := range streams {
		switch stream.Type {
		case "video", "audio", "subtitle":
		case "attachment":
			if !matroska {
				continue
			}
		default:
			continue
		}
		grouped[stream.Type] = append(grouped[stream.Type], stream)
	}
	return grouped
}

// verifyAudioCleanupOutput proves, from the container headers, that the remux
// kept every stream it had to keep, unchanged, and that the A/V offsets did
// not move. The packet comparison follows it.
func verifyAudioCleanupOutput(source, output MediaInfo, plan AudioCleanupPlan, opts AudioCleanupOptions, matroska bool) error {
	expected := audioCleanupStreamsByType(audioCleanupMappedStreams(plan, matroska), matroska)
	actual := audioCleanupStreamsByType(output.Streams, matroska)
	promoted := -1
	if plan.DefaultPosition >= 0 && plan.DefaultPosition < len(plan.Info.AudioTracks) {
		promoted = plan.Info.AudioTracks[plan.DefaultPosition].Index
	}
	for _, kind := range []string{"video", "audio", "subtitle", "attachment"} {
		want, got := expected[kind], actual[kind]
		if len(want) != len(got) {
			return fmt.Errorf("the remux kept %d %s stream(s) instead of %d", len(got), kind, len(want))
		}
		if kind == "attachment" {
			continue
		}
		for position := range want {
			if want[position].Codec != got[position].Codec {
				return fmt.Errorf("%s stream %d changed codec from %s to %s", kind, position+1, want[position].Codec, got[position].Codec)
			}
			if kind != "video" && !audioCleanupSameLanguage(want[position].Language, got[position].Language) {
				return fmt.Errorf("%s stream %d changed language from %q to %q", kind, position+1, want[position].Language, got[position].Language)
			}
			if !matroska {
				continue
			}
			flags := want[position].Disposition
			if want[position].Index == promoted && !slices.Contains(flags, "default") {
				flags = append(slices.Clone(flags), "default")
				slices.Sort(flags)
			}
			if !slices.Equal(flags, got[position].Disposition) {
				return fmt.Errorf("%s stream %d changed flags from [%s] to [%s]", kind, position+1,
					strings.Join(flags, " "), strings.Join(got[position].Disposition, " "))
			}
		}
	}

	untaggedMode := normalizeAudioCleanupUntagged(opts.Untagged)
	for _, track := range output.AudioTracks {
		if audioTrackMatchesLanguage(track, opts.Language) {
			continue
		}
		if untaggedMode == audioCleanupUntaggedKeep && audioLanguageUntagged(track.Language) {
			continue
		}
		return fmt.Errorf("audio track %d is still not tagged %q", track.Index, opts.Language)
	}
	if source.RuntimeSeconds > 0 && output.RuntimeSeconds > 0 {
		delta := output.RuntimeSeconds - source.RuntimeSeconds
		if math.Abs(float64(delta)) > audioCleanupDurationTolerance {
			if delta > 0 {
				return fmt.Errorf("the cleaned copy would be %d seconds longer", delta)
			}
			return fmt.Errorf("the cleaned copy would be %d seconds shorter", -delta)
		}
	}

	outputAudio := actual["audio"]
	sourceVideo, sourceVideoOK := audioCleanupFirstVideo(source.Streams)
	outputVideo, outputVideoOK := audioCleanupFirstVideo(output.Streams)
	if sourceVideoOK && outputVideoOK && sourceVideo.HasStart && outputVideo.HasStart {
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

// audioCleanupSameLanguage compares two language tags as written, treating
// the spellings of "no language" as one value.
func audioCleanupSameLanguage(left, right string) bool {
	if audioLanguageUntagged(left) && audioLanguageUntagged(right) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
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

func audioCleanupLastLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}

// audioCleanupNotificationData is the payload of the per-file notification.
func audioCleanupNotificationData(path string, report AudioCleanupReport) map[string]any {
	name := filepath.Base(path)
	return map[string]any{
		"title":       name,
		"file":        name,
		"path":        path,
		"removed":     len(report.Dropped),
		"saved_bytes": report.SavedBytes,
		"size_bytes":  report.NewSize,
		"backend":     report.Backend,
	}
}

// notifyAudioCleanup sends one event per cleaned file on every configured
// channel (Telegram, webhook, email), so a series pack produces one message
// per episode.
func notifyAudioCleanup(notifier *Notifier, path string, report AudioCleanupReport) {
	if notifier == nil {
		return
	}
	if err := notifier.NotifyEvent("audio_cleanup", audioCleanupNotificationData(path, report)); err != nil {
		logging.Warn("audio cleanup notification could not be sent",
			"path", path, "error", err.Error())
	}
}

// audioCleanupUnavailableNote is shown (and translated) when the option cannot
// be enabled because one of the external tools is missing.
const audioCleanupUnavailableNote = "Servono ffprobe, ffmpeg e mkvmerge: installali e riavvia Gextto per attivare la pulizia."

// audioCleanupLookPath is a variable so tests can simulate a missing tool.
var audioCleanupLookPath = exec.LookPath

// AudioCleanupMissingTools lists the external binaries the feature needs and
// that are not on PATH. All three are required: ffprobe decides and checks,
// ffmpeg is the second remuxer, mkvmerge is the first one for Matroska.
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
