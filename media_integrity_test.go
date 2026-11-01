package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// miInstallFakeMediaTools puts a working ffprobe and a configurable ffmpeg in a
// temporary directory and makes it the whole PATH for the test.
func miInstallFakeMediaTools(t *testing.T, ffmpegScript string) {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "ffprobe"), []byte(miFakeFfprobeSuccess), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ffmpeg"), []byte(ffmpegScript), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	miUsePath(t, binDir)
}

// miFakeFfmpegDetectsBadFile succeeds unless one of its arguments contains
// "bad", which is how a corrupted file is simulated.
const miFakeFfmpegDetectsBadFile = `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    *bad*) echo "Invalid data found when processing input" >&2; exit 1 ;;
  esac
done
exit 0
`

func TestMediaIntegrityCheckPath(t *testing.T) {
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.mkv")
	bad := filepath.Join(dir, "bad.mkv")
	for _, path := range []string{good, bad} {
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if status, detail := mediaIntegrityCheckPath(context.Background(), good, false); status != "ok" {
		t.Fatalf("good file = %q (%s), want ok", status, detail)
	}
	status, detail := mediaIntegrityCheckPath(context.Background(), bad, false)
	if status != "damaged" {
		t.Fatalf("bad file = %q, want damaged", status)
	}
	if !strings.Contains(detail, "Invalid data") {
		t.Fatalf("detail = %q, want the ffmpeg message", detail)
	}
	if status, _ := mediaIntegrityCheckPath(context.Background(), filepath.Join(dir, "missing.mkv"), false); status != "unreadable" {
		t.Fatalf("missing file = %q, want unreadable", status)
	}
}

// miFakeFfmpegRepeatedOutputTimestamps reproduces what ffmpeg writes at error
// level when the source repeats or reorders the decode timestamps: the null
// muxer cannot honour them, while the file plays fine. A whole healthy season
// from the same encoder produced exactly these lines, and reading them as
// damage sent every episode to the re-download.
const miFakeFfmpegRepeatedOutputTimestamps = `#!/bin/sh
echo "[null @ 0x55bd8d99e680] Application provided invalid, non monotonically increasing dts to muxer in stream 0: 2 >= 2" >&2
echo "[null @ 0x55bd8d99e680] Application provided invalid, non monotonically increasing dts to muxer in stream 0: 6 >= 6" >&2
exit 0
`

// miFakeFfmpegDecodeError is a real problem in the file: a stream that cannot be
// decoded. ffmpeg keeps going and exits zero, so the message itself is the only
// evidence.
const miFakeFfmpegDecodeError = `#!/bin/sh
echo "[h264 @ 0x1234] Error while decoding stream #0:0: Invalid data found when processing input" >&2
exit 0
`

// TestMediaIntegrityIgnoresOutputTimestampWarnings pins that only a complaint
// about the file counts: a warning about the timestamps of the output, which
// the check itself creates, is not damage.
func TestMediaIntegrityIgnoresOutputTimestampWarnings(t *testing.T) {
	miInstallFakeMediaTools(t, miFakeFfmpegRepeatedOutputTimestamps)
	file := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, detail := mediaIntegrityCheckPath(context.Background(), file, false)
	if status != "ok" {
		t.Fatalf("status = %q (%s), want ok", status, detail)
	}
	if detail != "" {
		t.Fatalf("detail = %q, want no problem reported", detail)
	}
}

// TestMediaIntegrityDecodeErrorIsStillDamaged keeps the other side of the rule:
// a message about the file is still damage, even when ffmpeg exits zero.
func TestMediaIntegrityDecodeErrorIsStillDamaged(t *testing.T) {
	miInstallFakeMediaTools(t, miFakeFfmpegDecodeError)
	file := filepath.Join(t.TempDir(), "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, detail := mediaIntegrityCheckPath(context.Background(), file, false)
	if status != "damaged" {
		t.Fatalf("status = %q (%s), want damaged", status, detail)
	}
	if !strings.Contains(detail, "Error while decoding") {
		t.Fatalf("detail = %q, want the decode error", detail)
	}
}

func TestMediaIntegrityResolveFilePicksLargestVideo(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.mkv")
	large := filepath.Join(dir, "large.mp4")
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(small, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, []byte("123456"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := mediaIntegrityResolveFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != large {
		t.Fatalf("resolved = %q, want %q", resolved, large)
	}
}

func TestMediaIntegrityTargetsWalkEpisodesThenMovies(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	episodeFile := filepath.Join(dir, "episode.mkv")
	movieFile := filepath.Join(dir, "movie.mkv")
	for _, path := range []string{episodeFile, movieFile} {
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,2,'Show S01E02',?2)", seriesID, episodeFile); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO torrent_meta(hash,status,name,processed_path,updated_at) VALUES ('abc','completed','Film',?1,'2026-01-01 00:00:00')", movieFile); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO movies(name,year,title,magnet_hash,downloaded_at) VALUES ('Film',2024,'Film','abc','2026-01-01')"); err != nil {
		t.Fatal(err)
	}

	total, err := state.db.CountMediaIntegrityTargets(true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	targets, next, err := state.db.MediaIntegrityTargets("", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	if targets[0].Kind != "episode" || targets[0].Label() != "Show S01E02" {
		t.Fatalf("first target = %+v", targets[0])
	}
	if targets[1].Kind != "movie" || targets[1].Label() != "Film (2024)" {
		t.Fatalf("second target = %+v", targets[1])
	}
	if next != "m:"+strconv.FormatInt(targets[1].ID, 10) {
		t.Fatalf("next = %q", next)
	}
	more, next, err := state.db.MediaIntegrityTargets(next, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 || next != "done" {
		t.Fatalf("after the end: %d targets, next %q", len(more), next)
	}
}

func TestRunMediaIntegrityScanRecordsAndResumes(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	oldPause := mediaIntegrityPause
	mediaIntegrityPause = 0
	t.Cleanup(func() { mediaIntegrityPause = oldPause })

	dir := t.TempDir()
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(dir, "first.mkv"),
		filepath.Join(dir, "bad second.mkv"),
		filepath.Join(dir, "third.mkv"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for index, path := range paths {
		if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,?2,?3,?4)",
			seriesID, index+1, "Show S01E0"+strconv.FormatInt(int64(index+1), 10), path); err != nil {
			t.Fatal(err)
		}
	}
	total, err := state.db.CountMediaIntegrityTargets(true)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pass interrupted after the first file: its outcome is
	// already stored, so a resumed pass must not check it again.
	firstID := int64(0)
	if err := state.db.db.QueryRow("SELECT id FROM episodes ORDER BY id LIMIT 1").Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveMediaIntegrityCheck(MediaIntegrityProblem{
		TargetID: firstID, Kind: "episode", Series: "Show", Season: 1, Episode: 1,
		Path: paths[0], Status: "ok", SizeBytes: firstInfo.Size(),
		ModifiedNS: firstInfo.ModTime().UnixNano(), CheckedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveMediaIntegrityState(MediaIntegrityState{
		Cursor: "e:" + strconv.FormatInt(firstID, 10), Total: total, Checked: 1,
		StartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := runMediaIntegrityScan(context.Background(), state, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := report["checked"]; got != 3 {
		t.Fatalf("checked = %v, want 3 (1 resumed + 2 new)", got)
	}
	if got := report["damaged"]; got != 1 {
		t.Fatalf("damaged = %v, want 1", got)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Path, "bad second") {
		t.Fatalf("problems = %+v", problems)
	}
	if problems[0].Status != "damaged" || problems[0].Season != 1 || problems[0].Episode != 2 {
		t.Fatalf("problem = %+v", problems[0])
	}
	saved := state.db.LoadMediaIntegrityState()
	if !saved.Done || saved.Running || saved.Checked != 3 || saved.Cursor != "done" {
		t.Fatalf("state = %+v", saved)
	}
}

func TestMediaIntegritySaveProblemIsIdempotent(t *testing.T) {
	state := newTestAppState(t)
	problem := MediaIntegrityProblem{
		TargetID: 7, Kind: "episode", Series: "Show", Season: 1, Episode: 2,
		Path: "/media/episode.mkv", Status: "damaged", Detail: "first", CheckedAt: "2026-01-01T00:00:00Z",
	}
	if err := state.db.SaveMediaIntegrityCheck(problem); err != nil {
		t.Fatal(err)
	}
	problem.Detail = "second"
	if err := state.db.SaveMediaIntegrityCheck(problem); err != nil {
		t.Fatal(err)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Detail != "second" {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestRedownloadEpisodeAvoidsThePreviousRelease(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "Example - S01E01 - Pilot.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Example')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Example'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 40)
	magnet := "magnet:?xt=urn:btih:" + hash
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,magnet_hash,magnet_link,downloaded_at,archive_path) VALUES (?1,1,1,'Example S01E01',?2,?3,'2026-01-01 00:00:00',?4)",
		seriesID, hash, magnet, file); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodPost, "/api/episodes/Example/1/1/redownload?avoid_previous=1", nil)
	if code != http.StatusOK {
		t.Fatalf("redownload -> %d: %s", code, body)
	}
	var blocked int
	if err := state.db.db.QueryRow("SELECT COUNT(*) FROM blocklist WHERE lower(magnet_hash)=?1 AND reason='corrupt_file'", hash).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked != 1 {
		t.Fatalf("blocklisted rows = %d, want 1", blocked)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the damaged file must leave the library, stat err = %v", err)
	}
	oldTitle, oldPath, found, err := state.db.TakeMediaIntegrityReplacement("episode", "Example", 1, 1, "", 0)
	if err != nil || !found {
		t.Fatalf("replacement marker: found=%v err=%v", found, err)
	}
	if oldTitle != "Example S01E01" || oldPath != file {
		t.Fatalf("replacement marker = %q, %q; want the old release and file", oldTitle, oldPath)
	}
}

// TestV2SettingsMaintenanceHoldsTheMediaIntegrityPanel checks the check lives in
// one place only: the Maintenance tab of the settings, right after the group
// that configures it, and no longer as a separate panel of the Maintenance page.
func TestV2SettingsMaintenanceHoldsTheMediaIntegrityPanel(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	_, settings := v2Request(t, server, http.MethodGet, "/?view=settings&tab=maintenance", nil)
	if !strings.Contains(settings, `id="v2-media-integrity-panel"`) {
		t.Fatalf("the Maintenance settings tab does not render the media integrity panel")
	}
	if !strings.Contains(settings, "Controlla ora") {
		t.Fatalf("the manual check must be usable with the automatic pass off")
	}
	if !strings.Contains(settings, "Riavvia da capo") {
		t.Fatalf("the panel must offer the full pass")
	}
	if !strings.Contains(settings, "Stato del controllo") {
		t.Fatalf("the panel has no title of its own")
	}
	// The panel follows the group of its options.
	group := strings.Index(settings, `id="v2-setting-media_integrity_enabled"`)
	panel := strings.Index(settings, `id="v2-media-integrity-panel"`)
	if group < 0 || panel < 0 || panel < group {
		t.Fatalf("the panel must sit under its settings (group at %d, panel at %d)", group, panel)
	}

	_, maintenance := v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if strings.Contains(maintenance, `id="v2-media-integrity-panel"`) {
		t.Fatalf("the maintenance page must not render the check a second time")
	}
}

// TestMediaIntegrityPanelActionsRunInPlace keeps the partial endpoint of the
// panel working where the panel now lives.
func TestMediaIntegrityPanelActionsRunInPlace(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodGet, server.URL+"/settings/media-integrity", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("HX-Request", "true")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `id="v2-media-integrity-panel"`) {
		t.Fatalf("panel partial -> %d", response.StatusCode)
	}
	if !strings.Contains(body, `hx-post="/settings/media-integrity"`) {
		t.Fatalf("the partial does not post its actions back to itself")
	}
}

func TestMediaIntegrityAPIRefusesWhenToolsAreMissing(t *testing.T) {
	state := newTestAppState(t)
	miUsePath(t, t.TempDir())
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	status, body := webPostJSON(t, server, "/api/maintenance/media-integrity/start", "{}")
	if status != http.StatusConflict {
		t.Fatalf("start without tools -> %d: %s", status, body)
	}
	if !strings.Contains(string(body), "ffprobe") || !strings.Contains(string(body), "ffmpeg") {
		t.Fatalf("missing tools not named: %s", body)
	}
}

func TestMediaIntegrityAPIStartAndStatus(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, "#!/bin/sh\nexit 0\n")
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/maintenance/media-integrity/start", "{}")
	if status != http.StatusAccepted {
		t.Fatalf("start -> %d: %s", status, body)
	}
	if !strings.Contains(string(body), "job_id") {
		t.Fatalf("start answer has no job id: %s", body)
	}
	code, _, payload := webGet(t, server, "/api/maintenance/media-integrity")
	if code != http.StatusOK {
		t.Fatalf("status -> %d", code)
	}
	if !strings.Contains(string(payload), `"available":true`) {
		t.Fatalf("status does not report the tools as available: %s", payload)
	}
	code, stopBody := webPostJSON(t, server, "/api/maintenance/media-integrity/stop", "{}")
	if code != http.StatusOK {
		t.Fatalf("stop -> %d: %s", code, stopBody)
	}
}

// BenchmarkMediaIntegrityTargets measures the query that feeds the one-file-at-
// a-time scan, with a library of five thousand archived episodes.
func BenchmarkMediaIntegrityTargets(b *testing.B) {
	db, err := OpenDatabase(filepath.Join(b.TempDir(), "gextto.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	tx, err := db.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	result, err := tx.Exec("INSERT INTO series(name) VALUES ('Show')")
	if err != nil {
		b.Fatal(err)
	}
	seriesID, err := result.LastInsertId()
	if err != nil {
		b.Fatal(err)
	}
	for index := 1; index <= 5000; index++ {
		if _, err := tx.Exec("INSERT INTO episodes(series_id,season,episode,archive_path) VALUES (?1,1,?2,'/media/show.mkv')", seriesID, index); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := db.MediaIntegrityTargets("", 1, true); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMediaIntegrityDecodeTimeoutGrowsWithTheRuntime(t *testing.T) {
	cases := []struct {
		runtime int64
		want    time.Duration
	}{
		{0, mediaIntegrityDecodeTimeout},
		{60, mediaIntegrityDecodeTimeout},
		{600, mediaIntegrityDecodeTimeout},
		{3724, 3724 * time.Second * mediaIntegrityDecodeSlowFactor},
		{1_000_000, mediaIntegrityDecodeTimeoutMax},
	}
	for _, test := range cases {
		if got := mediaIntegrityDecodeTimeoutFor(test.runtime); got != test.want {
			t.Errorf("timeout for %ds = %s, want %s", test.runtime, got, test.want)
		}
	}
}

func TestMediaIntegrityQuickDecodeArgs(t *testing.T) {
	quick := strings.Join(mediaIntegrityDecodeArgs("/media/movie.mkv", true), " ")
	full := strings.Join(mediaIntegrityDecodeArgs("/media/movie.mkv", false), " ")
	if !strings.Contains(quick, "-threads 1 -skip_frame nokey -i /media/movie.mkv") {
		t.Fatalf("quick args do not decode keyframes on one thread before the input: %s", quick)
	}
	if strings.Contains(full, "-skip_frame") {
		t.Fatalf("the full check must decode every frame: %s", full)
	}
	if !strings.Contains(full, "-map 0:v? -map 0:a? -f null -") {
		t.Fatalf("full args changed shape: %s", full)
	}
}

func TestMediaIntegrityQuickCheckOnlyFor4K(t *testing.T) {
	uhd := MediaInfo{Width: 3840, Height: 1606}
	hd := MediaInfo{Width: 1920, Height: 1080}
	if !mediaIntegrityUsesQuickDecode(true, uhd) {
		t.Fatal("a 4K file must use the quick check when the option is on")
	}
	if mediaIntegrityUsesQuickDecode(true, hd) {
		t.Fatal("a 1080p file must keep the full decode")
	}
	if mediaIntegrityUsesQuickDecode(false, uhd) {
		t.Fatal("the quick check must be off when the option is off")
	}
}

func TestMediaIntegrityQuick4KIsOnByDefault(t *testing.T) {
	if !mediaIntegrityQuick4K(nil) {
		t.Fatal("the quick 4K check must be on by default")
	}
	on := &Config{Settings: map[string]string{mediaIntegrityQuick4KSetting: "true"}}
	if !mediaIntegrityQuick4K(on) {
		t.Fatal("the quick 4K check must read a saved true")
	}
	off := &Config{Settings: map[string]string{mediaIntegrityQuick4KSetting: "false"}}
	if mediaIntegrityQuick4K(off) {
		t.Fatal("the quick 4K check must read a saved false")
	}
}

func TestMediaIntegrityDecodeReportsATimeoutAsUnreadable(t *testing.T) {
	miInstallFakeMediaTools(t, "#!/bin/sh\nexec /bin/sleep 5\n")
	file := filepath.Join(t.TempDir(), "slow.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, detail := mediaIntegrityDecode(context.Background(), file, 100*time.Millisecond, false)
	if status != "unreadable" {
		t.Fatalf("timeout = %q (%s), want unreadable", status, detail)
	}
	if !strings.Contains(detail, "did not finish") {
		t.Fatalf("detail = %q, want the timeout reason", detail)
	}
}

func TestMediaIntegritySummarySeparatesUnreadable(t *testing.T) {
	if got, want := mediaIntegritySummary(3, 0, 1, false), "3 files checked, 0 files found damaged, 1 file could not be read"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if got, want := mediaIntegritySummary(2, 1, 0, true), "2 files checked so far, 1 file found damaged"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestMediaIntegrityRetriesUnreadableFilesAfterTheWait(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show S01E01',?2)", seriesID, file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	problem := MediaIntegrityProblem{
		TargetID: 1, Kind: "episode", Series: "Show", Season: 1, Episode: 1, Path: file,
		Status: "unreadable", Detail: "the check did not finish",
		SizeBytes: info.Size(), ModifiedNS: info.ModTime().UnixNano(),
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	if err := state.db.SaveMediaIntegrityCheck(problem); err != nil {
		t.Fatal(err)
	}
	if total, err := state.db.CountMediaIntegrityTargets(false); err != nil || total != 0 {
		t.Fatalf("a fresh timeout must wait: total = %d (%v)", total, err)
	}
	problem.CheckedAt = time.Now().Add(-mediaIntegrityUnreadableRetry - time.Minute).Format(time.RFC3339)
	if err := state.db.SaveMediaIntegrityCheck(problem); err != nil {
		t.Fatal(err)
	}
	if total, err := state.db.CountMediaIntegrityTargets(false); err != nil || total != 1 {
		t.Fatalf("an old timeout must be retried: total = %d (%v)", total, err)
	}

	// A damaged file is not decoded again by itself: only the operator acts.
	problem.Status = "damaged"
	if err := state.db.SaveMediaIntegrityCheck(problem); err != nil {
		t.Fatal(err)
	}
	if total, err := state.db.CountMediaIntegrityTargets(false); err != nil || total != 0 {
		t.Fatalf("a damaged file must not be rechecked by itself: total = %d (%v)", total, err)
	}
}

func TestMediaIntegritySkipsCheckedUnchangedFiles(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,2,'Show S01E02',?2)", seriesID, file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveMediaIntegrityCheck(MediaIntegrityProblem{
		TargetID: 1, Kind: "episode", Series: "Show", Season: 1, Episode: 2, Path: file,
		Status: "ok", SizeBytes: info.Size(), ModifiedNS: info.ModTime().UnixNano(),
		CheckedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	total, err := state.db.CountMediaIntegrityTargets(false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("unchanged checked files still count: %d", total)
	}
	targets, next, err := state.db.MediaIntegrityTargets("", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 || next != "done" {
		t.Fatalf("targets = %d, next = %q, want none/done", len(targets), next)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("an OK check must not be listed as a problem: %+v", problems)
	}

	// A changed file is checked again.
	if err := os.WriteFile(file, []byte("changed and longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	total, err = state.db.CountMediaIntegrityTargets(false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("changed file not counted: %d", total)
	}
	targets, _, err = state.db.MediaIntegrityTargets("", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("changed file not returned: %d", len(targets))
	}

	// A forced pass returns it even when unchanged.
	targets, _, err = state.db.MediaIntegrityTargets("", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("forced pass did not return the file: %d", len(targets))
	}
}

func TestPruneMediaIntegrityChecksDropsRemovedItems(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show S01E01',?2)", seriesID, file); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveMediaIntegrityCheck(MediaIntegrityProblem{
		Kind: "episode", Series: "Show", Season: 1, Episode: 1, Path: file,
		Status: "damaged", Detail: "broken", CheckedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("DELETE FROM episodes WHERE series_id=?1", seriesID); err != nil {
		t.Fatal(err)
	}
	if err := state.db.PruneMediaIntegrityChecks(); err != nil {
		t.Fatal(err)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("the check of a removed episode was not pruned: %+v", problems)
	}
}

func TestRunMediaIntegrityScanSkipsUnchangedOnTheNextPass(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, "#!/bin/sh\nexit 0\n")
	oldPause := mediaIntegrityPause
	mediaIntegrityPause = 0
	t.Cleanup(func() { mediaIntegrityPause = oldPause })

	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show S01E01',?2)", seriesID, file); err != nil {
		t.Fatal(err)
	}
	total, err := state.db.CountMediaIntegrityTargets(false)
	if err != nil || total != 1 {
		t.Fatalf("first pass total = %d (%v)", total, err)
	}
	if _, err := runMediaIntegrityScan(context.Background(), state, "", 0); err != nil {
		t.Fatal(err)
	}
	saved := state.db.LoadMediaIntegrityState()
	if !saved.Done || saved.Checked != 1 {
		t.Fatalf("first pass state = %+v", saved)
	}
	if total, err := state.db.CountMediaIntegrityTargets(false); err != nil || total != 0 {
		t.Fatalf("next pass total = %d (%v), want 0", total, err)
	}
	// beginMediaIntegrityScan starts a new pass after a completed one; here
	// the fresh state is set directly so the scan can be called synchronously.
	if err := state.db.SaveMediaIntegrityState(MediaIntegrityState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := runMediaIntegrityScan(context.Background(), state, "", 0); err != nil {
		t.Fatal(err)
	}
	saved = state.db.LoadMediaIntegrityState()
	if !saved.Done || saved.Checked != 0 {
		t.Fatalf("next pass state = %+v, want done with nothing checked", saved)
	}
}

func TestLoadMediaIntegrityVerifiedManifestTrustsOnlyVerifiedFiles(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "state.json")
	payload := map[string]any{
		"folders": map[string]any{
			"Show": map[string]any{
				"path": dir,
				"files": map[string]any{
					"remuxed.mkv": map[string]any{"status": "remuxed", "size": 100, "mtime": 11, "size_after": 90, "deep": false},
					"skipped.mkv": map[string]any{"status": "skipped", "size": 50, "mtime": 12, "size_after": 50, "deep": false},
					"deep.mkv":    map[string]any{"status": "skipped", "size": 60, "mtime": 13, "size_after": 60, "deep": true},
					"error.mkv":   map[string]any{"status": "error", "size": 70, "mtime": 14, "size_after": 0, "deep": true},
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := loadMediaIntegrityVerifiedManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("trusted entries = %d, want 2 (remuxed and deep skipped)", len(entries))
	}
	if entry, ok := entries[filepath.Join(dir, "remuxed.mkv")]; !ok || entry.size != 90 || entry.mtime != 11 {
		t.Fatalf("remuxed entry = %+v, ok=%v", entry, ok)
	}
	if _, ok := entries[filepath.Join(dir, "skipped.mkv")]; ok {
		t.Fatalf("a skipped file that was not fully decoded must not be trusted")
	}
	if _, ok := entries[filepath.Join(dir, "error.mkv")]; ok {
		t.Fatalf("an error must never be trusted")
	}
}

func TestApplyMediaIntegrityManifestMarksMatchingFiles(t *testing.T) {
	state := newTestAppState(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, make([]byte, 90), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1700000000, 0)
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := state.db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show S01E01',?2)", seriesID, file); err != nil {
		t.Fatal(err)
	}
	entries := map[string]mediaIntegrityManifestEntry{
		filepath.Clean(file): {size: info.Size(), mtime: info.ModTime().UnixNano()},
	}
	marked, err := state.db.ApplyMediaIntegrityManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 1 {
		t.Fatalf("marked = %d, want 1", marked)
	}
	if total, err := state.db.CountMediaIntegrityTargets(false); err != nil || total != 0 {
		t.Fatalf("after the manifest total = %d (%v), want 0", total, err)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil || len(problems) != 0 {
		t.Fatalf("manifest row must not be a problem: %+v (%v)", problems, err)
	}
}

func TestMediaIntegrityReplacementMarkerRoundTrip(t *testing.T) {
	state := newTestAppState(t)
	problem := MediaIntegrityProblem{Kind: "episode", Series: "Show", Season: 1, Episode: 2, Path: "/media/old.mkv"}
	if err := state.db.MarkMediaIntegrityReplacement(problem, "Old.Release.S01E02"); err != nil {
		t.Fatal(err)
	}
	title, path, found, err := state.db.TakeMediaIntegrityReplacement("episode", "Show", 1, 2, "", 0)
	if err != nil || !found {
		t.Fatalf("take: found=%v err=%v", found, err)
	}
	if title != "Old.Release.S01E02" || path != "/media/old.mkv" {
		t.Fatalf("marker = %q, %q", title, path)
	}
	if _, _, found, err := state.db.TakeMediaIntegrityReplacement("episode", "Show", 1, 2, "", 0); err != nil || found {
		t.Fatalf("second take: found=%v err=%v, want consumed", found, err)
	}
}

func TestMovieIdentityForRelease(t *testing.T) {
	state := newTestAppState(t)
	if _, err := state.db.db.Exec("INSERT INTO movies(name,year,title,downloaded_at) VALUES ('Film','2024','Film.2024.1080p.WEB-DL','2026-01-01')"); err != nil {
		t.Fatal(err)
	}
	year := int64(2024)
	name, movieYear, found, err := state.db.MovieIdentityForRelease("Film.2024.1080p.WEB-DL", &year)
	if err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	if name != "Film" || movieYear != 2024 {
		t.Fatalf("identity = %q, %d", name, movieYear)
	}
}

func TestLogMediaIntegrityReplacementConsumesTheMarker(t *testing.T) {
	state := newTestAppState(t)
	season, episode := int64(1), int64(2)
	series := "Show"
	if err := state.db.MarkMediaIntegrityReplacement(MediaIntegrityProblem{
		Kind: "episode", Series: series, Season: season, Episode: episode, Path: "/media/old.mkv",
	}, "Old.Release.S01E02"); err != nil {
		t.Fatal(err)
	}
	release := models.Release{Kind: "series", Title: "New.Release.S01E02", Series: &series, Season: &season, Episode: &episode}
	logMediaIntegrityReplacementAfterRelease(state.db, "hash", &release, "/media/new.mkv")
	if _, _, found, err := state.db.TakeMediaIntegrityReplacement("episode", series, season, episode, "", 0); err != nil || found {
		t.Fatalf("the marker must be consumed: found=%v err=%v", found, err)
	}
}

// miFakeFfmpegStoppedBySignal reproduces what ffmpeg does when a restart of
// Gextto reaches it: the signal handler ends the process with status 255 and
// nothing on stderr.
const miFakeFfmpegStoppedBySignal = `#!/bin/sh
exit 255
`

// miFakeFfmpegKilledWithoutHandler is the other shape of an interrupted decode:
// the process dies on the signal itself, so it has no exit code at all.
const miFakeFfmpegKilledWithoutHandler = `#!/bin/sh
kill -TERM $$
exit 0
`

// TestMediaIntegrityStoppedProcessIsNotDamaged pins the cause of the false
// "damaged" verdicts: an ffmpeg stopped by a signal (which is what a restart of
// the daemon does to its children) never finished the decode, so the file was
// not read and nothing can be said about it. Recording it as damaged would send
// a healthy file to the re-download.
func TestMediaIntegrityStoppedProcessIsNotDamaged(t *testing.T) {
	for name, script := range map[string]string{
		"exit 255":   miFakeFfmpegStoppedBySignal,
		"no handler": miFakeFfmpegKilledWithoutHandler,
	} {
		t.Run(name, func(t *testing.T) {
			miInstallFakeMediaTools(t, script)
			file := filepath.Join(t.TempDir(), "episode.mkv")
			if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
				t.Fatal(err)
			}
			status, detail := mediaIntegrityCheckPath(context.Background(), file, false)
			if status != "interrupted" {
				t.Fatalf("status = %q (%s), want interrupted", status, detail)
			}
			if !strings.Contains(detail, "stopped") {
				t.Fatalf("detail = %q, want the interruption reason", detail)
			}
		})
	}
}

// TestRunMediaIntegrityScanRecordsNothingForAnInterruptedFile checks the scan
// leaves no outcome at all: the file keeps its position and is checked again in
// a later pass, exactly like a pass stopped at shutdown.
func TestRunMediaIntegrityScanRecordsNothingForAnInterruptedFile(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegStoppedBySignal)
	oldPause := mediaIntegrityPause
	mediaIntegrityPause = 0
	t.Cleanup(func() { mediaIntegrityPause = oldPause })

	dir := t.TempDir()
	file := filepath.Join(dir, "episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES ((SELECT id FROM series WHERE name='Show'),1,1,'Show S01E01',?1)", file); err != nil {
		t.Fatal(err)
	}

	report, err := runMediaIntegrityScan(context.Background(), state, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := report["damaged"]; got != 0 {
		t.Fatalf("damaged = %v, want 0", got)
	}
	if got := report["checked"]; got != 0 {
		t.Fatalf("checked = %v, want 0: an interrupted file was not checked", got)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}
	if state.db.LoadMediaIntegrityState().Cursor == "done" {
		t.Fatal("the interrupted file must keep its position for the next pass")
	}
}

// TestMediaIntegrityRedownloadIsOffByDefault keeps the automatic replacement an
// explicit choice: the check alone never removes a file from the library.
func TestMediaIntegrityRedownloadIsOffByDefault(t *testing.T) {
	if mediaIntegrityRedownloadEnabled(nil) {
		t.Fatal("the automatic re-download must be off without a configuration")
	}
	cfg := &Config{Settings: map[string]string{}}
	if mediaIntegrityRedownloadEnabled(cfg) {
		t.Fatal("the automatic re-download must be off when nothing is saved")
	}
	cfg.Settings[mediaIntegrityRedownloadSetting] = "true"
	if !mediaIntegrityRedownloadEnabled(cfg) {
		t.Fatal("the automatic re-download must read a saved true")
	}
}

// TestRunMediaIntegrityScanRedownloadsADamagedFileWithTheOptionOn is the whole
// automatic path: the damaged episode is blocklisted, the file leaves the
// library, the episode is eligible again and the search starts as soon as the
// run ends.
func TestRunMediaIntegrityScanRedownloadsADamagedFileWithTheOptionOn(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	oldPause := mediaIntegrityPause
	mediaIntegrityPause = 0
	t.Cleanup(func() { mediaIntegrityPause = oldPause })
	oldSearch := mediaIntegrityStartSearch
	var searched []string
	mediaIntegrityStartSearch = func(s *AppState, domain *string) (bool, bool, error) {
		if domain == nil {
			searched = append(searched, "full")
		} else {
			searched = append(searched, *domain)
		}
		return true, false, nil
	}
	t.Cleanup(func() { mediaIntegrityStartSearch = oldSearch })
	if err := SaveSetting(state.cfg.DataDir, mediaIntegrityRedownloadSetting, "true"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "bad episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 40)
	magnet := "magnet:?xt=urn:btih:" + hash
	if _, err := state.db.db.Exec(`INSERT INTO episodes(series_id,season,episode,title,magnet_hash,magnet_link,downloaded_at,archive_path)
		VALUES ((SELECT id FROM series WHERE name='Show'),1,1,'Show S01E01',?1,?2,'2026-01-01 00:00:00',?3)`, hash, magnet, file); err != nil {
		t.Fatal(err)
	}

	if _, err := runMediaIntegrityScan(context.Background(), state, "", 0); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the damaged file must leave the library, stat err = %v", err)
	}
	var episodes int
	if err := state.db.db.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&episodes); err != nil {
		t.Fatal(err)
	}
	if episodes != 0 {
		t.Fatalf("episode rows = %d, want 0: the episode must be eligible again", episodes)
	}
	var blocked int
	if err := state.db.db.QueryRow("SELECT COUNT(*) FROM blocklist WHERE lower(magnet_hash)=?1 AND reason='corrupt_file'", hash).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked != 1 {
		t.Fatalf("blocklisted rows = %d, want 1", blocked)
	}
	var gaps int
	if err := state.db.db.QueryRow("SELECT COUNT(*) FROM forced_gaps WHERE lower(series_name)='show' AND season=1 AND episode=1").Scan(&gaps); err != nil {
		t.Fatal(err)
	}
	if gaps != 1 {
		t.Fatalf("forced gaps = %d, want 1", gaps)
	}
	if _, _, found, err := state.db.TakeMediaIntegrityReplacement("episode", "Show", 1, 1, "", 0); err != nil || !found {
		t.Fatalf("replacement marker: found=%v err=%v", found, err)
	}
	if problems, err := state.db.MediaIntegrityProblems(); err != nil {
		t.Fatal(err)
	} else if len(problems) != 0 {
		t.Fatalf("problems = %+v, want none after the re-download", problems)
	}
	if len(searched) != 1 || searched[0] != "series" {
		t.Fatalf("searches = %v, want one series search as soon as the run ends", searched)
	}
}

// TestRunMediaIntegrityScanLeavesADamagedFileAloneWhenTheOptionIsOff pins the
// default: without the option the file and the episode row stay as they are and
// no search starts.
func TestRunMediaIntegrityScanLeavesADamagedFileAloneWhenTheOptionIsOff(t *testing.T) {
	state := newTestAppState(t)
	miInstallFakeMediaTools(t, miFakeFfmpegDetectsBadFile)
	oldPause := mediaIntegrityPause
	mediaIntegrityPause = 0
	t.Cleanup(func() { mediaIntegrityPause = oldPause })
	oldSearch := mediaIntegrityStartSearch
	searches := 0
	mediaIntegrityStartSearch = func(s *AppState, domain *string) (bool, bool, error) {
		searches++
		return true, false, nil
	}
	t.Cleanup(func() { mediaIntegrityStartSearch = oldSearch })

	dir := t.TempDir()
	file := filepath.Join(dir, "bad episode.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES ((SELECT id FROM series WHERE name='Show'),1,1,'Show S01E01',?1)", file); err != nil {
		t.Fatal(err)
	}

	if _, err := runMediaIntegrityScan(context.Background(), state, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the damaged file must stay where it is: %v", err)
	}
	problems, err := state.db.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || problems[0].Status != "damaged" {
		t.Fatalf("problems = %+v, want the damaged file listed", problems)
	}
	if searches != 0 {
		t.Fatalf("searches = %d, want none with the option off", searches)
	}
}

// TestOpenDatabaseForgetsTheInterruptedDamage clears the rows the old version
// stored for a decode stopped by a signal: they were never evidence of damage
// and would offer a healthy episode for re-download.
func TestOpenDatabaseForgetsTheInterruptedDamage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gextto.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range []MediaIntegrityProblem{
		{Kind: "episode", Series: "Show", Season: 1, Episode: 1, Path: "/media/a.mkv", Status: "damaged", Detail: "exit status 255", CheckedAt: "2026-11-01T00:00:00Z"},
		{Kind: "episode", Series: "Show", Season: 1, Episode: 2, Path: "/media/b.mkv", Status: "damaged", Detail: "signal: terminated", CheckedAt: "2026-11-01T00:00:00Z"},
		{Kind: "episode", Series: "Show", Season: 1, Episode: 3, Path: "/media/c.mkv", Status: "damaged", Detail: "Invalid data found when processing input", CheckedAt: "2026-11-01T00:00:00Z"},
		{Kind: "episode", Series: "Show", Season: 1, Episode: 4, Path: "/media/d.mkv", Status: "unreadable", Detail: "the check did not finish within 30 minutes", CheckedAt: "2026-11-01T00:00:00Z"},
	} {
		if err := db.SaveMediaIntegrityCheck(problem); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	problems, err := reopened.MediaIntegrityProblems()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 2 {
		t.Fatalf("problems = %+v, want only the real damage and the unreadable file", problems)
	}
	for _, problem := range problems {
		if problem.Episode == 1 || problem.Episode == 2 {
			t.Fatalf("the interrupted check of S01E%02d was kept: %+v", problem.Episode, problem)
		}
	}
}
