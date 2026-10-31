package gextto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

	if status, detail := mediaIntegrityCheckPath(context.Background(), good); status != "ok" {
		t.Fatalf("good file = %q (%s), want ok", status, detail)
	}
	status, detail := mediaIntegrityCheckPath(context.Background(), bad)
	if status != "damaged" {
		t.Fatalf("bad file = %q, want damaged", status)
	}
	if !strings.Contains(detail, "Invalid data") {
		t.Fatalf("detail = %q, want the ffmpeg message", detail)
	}
	if status, _ := mediaIntegrityCheckPath(context.Background(), filepath.Join(dir, "missing.mkv")); status != "unreadable" {
		t.Fatalf("missing file = %q, want unreadable", status)
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

	total, err := state.db.CountMediaIntegrityTargets()
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	targets, next, err := state.db.MediaIntegrityTargets("", 10)
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
	more, next, err := state.db.MediaIntegrityTargets(next, 10)
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
	total, err := state.db.CountMediaIntegrityTargets()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pass interrupted after the first file.
	firstID := int64(0)
	if err := state.db.db.QueryRow("SELECT id FROM episodes ORDER BY id LIMIT 1").Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveMediaIntegrityState(MediaIntegrityState{
		Cursor: "e:" + strconv.FormatInt(firstID, 10), Total: total, Checked: 1,
		StartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := runMediaIntegrityScan(context.Background(), state, "")
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
	if err := state.db.SaveMediaIntegrityProblem(problem); err != nil {
		t.Fatal(err)
	}
	problem.Detail = "second"
	if err := state.db.SaveMediaIntegrityProblem(problem); err != nil {
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
}

func TestV2MaintenanceRendersTheMediaIntegrityPanel(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	_, body := v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if !strings.Contains(body, "Controllo integrità file") {
		t.Fatalf("maintenance page does not render the media integrity panel")
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
		if _, _, err := db.MediaIntegrityTargets("", 1); err != nil {
			b.Fatal(err)
		}
	}
}
