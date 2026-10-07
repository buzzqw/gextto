package gextto

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestLogTextFormatsNumbersAndDurations(t *testing.T) {
	cases := map[string]string{
		logCount(3612):                              "3,612",
		logCount(int64(-1250000)):                   "-1,250,000",
		countLabel(1, "download", "downloads"):      "1 download",
		countLabel(5, "download", "downloads"):      "5 downloads",
		logPercent(9.02559980750084):                "9%",
		logPercent(99.57):                           "99.5%",
		logDuration(4*time.Minute + 17*time.Second): "4 minutes",
		logDuration(2*time.Hour + 34*time.Minute):   "2 hours 34 minutes",
		logDuration(6 * time.Hour):                  "6 hours",
		logDuration(3 * time.Second):                "3 seconds",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestLogTargetReadsLikeAPerson(t *testing.T) {
	series := func(name string, season, episode int64, episodes ...int64) *models.Release {
		return &models.Release{Kind: "series", Series: &name, Season: &season, Episode: &episode, EpisodeRange: episodes}
	}
	year := int64(2021)
	cases := []struct {
		release *models.Release
		want    string
	}{
		{series("Silo", 3, 4, 4), "Silo S03E04"},
		{series("From", 1, 0, 0), "From season 1"},
		{series("Slow Horses", 1, 1, 1, 2, 3, 4, 5, 6), "Slow Horses S01E01-E06"},
		{&models.Release{Kind: "movie", Title: "Dune", Year: &year}, "Dune (2021)"},
	}
	for _, tc := range cases {
		if got := logTarget(tc.release); got != tc.want {
			t.Fatalf("logTarget = %q, want %q", got, tc.want)
		}
	}
	if got := friendlyQuality(models.Quality{Resolution: "2160p", Source: "webdl", IsDV: true, Language: "ita"}); got != "2160p WEB-DL Dolby Vision ITA" {
		t.Fatalf("friendlyQuality = %q", got)
	}
}

func TestCycleReportTextIsReadable(t *testing.T) {
	stats := &models.CycleStats{Scraped: 3612, Candidates: 183, DownloadsStarted: 5, GapsFilled: 1}
	want := "📊 Search finished in 5 minutes: 3,612 releases checked, 183 matched your titles, 5 downloads started (5 upgrades, 1 missing episode filled)"
	if got := cycleReportText("5 minutes", stats, 5, 0, cycleSkipCounts{}); got != want {
		t.Fatalf("report =\n %q\nwant\n %q", got, want)
	}
	stats = &models.CycleStats{Scraped: 10, Candidates: 0, Errors: 2}
	want = "📊 Search finished in 3 seconds: 10 releases checked, 0 matched your titles, nothing new to download; 2 problems (see the warnings above)"
	if got := cycleReportText("3 seconds", stats, 0, 0, cycleSkipCounts{}); got != want {
		t.Fatalf("report =\n %q\nwant\n %q", got, want)
	}
	stats = &models.CycleStats{Scraped: 6406, Candidates: 183}
	want = "📊 Search finished in 1 minute: 6,406 releases checked, 183 matched your titles, nothing new to download: 170 already in the library, 13 already downloading"
	if got := cycleReportText("1 minute", stats, 0, 0, cycleSkipCounts{InLibrary: 170, Downloading: 13}); got != want {
		t.Fatalf("report =\n %q\nwant\n %q", got, want)
	}
	stats = &models.CycleStats{Scraped: 100, Candidates: 4, DownloadsStarted: 1}
	want = "📊 Search finished in 1 minute: 100 releases checked, 4 matched your titles, 1 download started (1 new); the others: 2 already in the library, 1 waiting for a better version"
	if got := cycleReportText("1 minute", stats, 0, 1, cycleSkipCounts{InLibrary: 2, Waiting: 1}); got != want {
		t.Fatalf("report =\n %q\nwant\n %q", got, want)
	}
}

func TestHashFailuresAlertOnceAfterThreshold(t *testing.T) {
	hash := "ABCDEF0123456789abcdef0123456789abcdef01"
	alerts := 0
	for i := 0; i < hashFailureAlertThreshold*3; i++ {
		if _, alert := recordHashFailure(hash); alert {
			alerts++
		}
	}
	if alerts != 1 {
		t.Fatalf("alerts = %d, want exactly 1", alerts)
	}
	if _, alert := recordHashFailure("another-hash"); alert {
		t.Fatal("a single damaged piece must not alert")
	}
}

func TestTorrentErrorNoticeOncePerCooldown(t *testing.T) {
	hash := "1111111111111111111111111111111111111111"
	now := time.Now()
	if !recordTorrentErrorNotice(hash, "file_error", now) {
		t.Fatal("first error must be reported")
	}
	if recordTorrentErrorNotice(hash, "file_error", now.Add(time.Second)) {
		t.Fatal("a repeated error in the same burst must not be reported again")
	}
	if !recordTorrentErrorNotice(hash, "torrent_error", now.Add(time.Second)) {
		t.Fatal("a different kind of error must be reported")
	}
	if !recordTorrentErrorNotice(hash, "file_error", now.Add(torrentErrorNoticeCooldown+time.Second)) {
		t.Fatal("the error must be reported again after the cooldown")
	}
}

func TestComicPendingNoticeOncePerPost(t *testing.T) {
	url := "https://example.invalid/comic-pending-test"
	if !firstComicPendingNotice(url) {
		t.Fatal("first sighting must notify")
	}
	if firstComicPendingNotice(url) {
		t.Fatal("the next cycles must not notify again")
	}
}

func TestMediaInfoBackfillTextNamesTheFiles(t *testing.T) {
	got := mediaInfoBackfillText([]string{"Wolf Like Me S01E01", "Wolf Like Me S01E02", "Silo S03E04"}, 0, 0)
	want := "🔬 Read the real quality (resolution, HDR, audio) of 3 library files: Wolf Like Me S01E01, Wolf Like Me S01E02, Silo S03E04. Future upgrades will be compared with what is actually on disk"
	if got != want {
		t.Fatalf("got %q", got)
	}
	got = mediaInfoBackfillText([]string{"A", "B", "C", "D", "E", "F"}, 1, 2)
	if !strings.Contains(got, "A, B, C, D and 2 more") || !strings.Contains(got, "1 file could not be read, 2 files are no longer on disk") {
		t.Fatalf("got %q", got)
	}
	if mediaInfoBackfillText(nil, 0, 0) != "" {
		t.Fatal("an empty run must not log")
	}
}

func TestSeedFinishedIsAnnouncedOnce(t *testing.T) {
	reason := "seed time reached"
	if got := seedFinishedLead("FBI.S07E11.mkv", &reason); got != "«FBI.S07E11.mkv» has finished seeding (seed time reached)" {
		t.Fatalf("first lead = %q", got)
	}
	if got := seedFinishedLead("FBI.S07E11.mkv", &reason); got != "«FBI.S07E11.mkv»" {
		t.Fatalf("second lead = %q, the end of seeding must be announced once", got)
	}
}

func TestBackupLogSummaryMentionsOnlyConfiguredSteps(t *testing.T) {
	steps := gh5_backupSteps{path: "/data/backups/b.zip", sizeBytes: 186974379, ftpHost: "192.168.1.119", ftpUploaded: true}
	got := gh5_backupLogSummary(steps, true, 0, "")
	want := "💾 Scheduled backup saved (178.3 MB): /data/backups/b.zip · FTP 192.168.1.119 ✓"
	if got != want {
		t.Fatalf("summary =\n %q\nwant\n %q", got, want)
	}
	failed := "denied"
	steps.cloudError = &failed
	got = gh5_backupLogSummary(steps, false, 2, "part 3/4: HTTP 413")
	if !strings.Contains(got, "cloud copy ✗") || !strings.Contains(got, "Telegram ✗") || strings.HasPrefix(got, "💾 Scheduled") {
		t.Fatalf("summary with failures = %q", got)
	}
}

func TestTorznabShortReasonDropsTheURL(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://192.168.1.161:9118/api?t=tvsearch&apikey=secret", Err: io.EOF}
	if got := torznabShortReason(err); got != "connection closed by the indexer (EOF)" {
		t.Fatalf("reason = %q", got)
	}
	if got := torznabShortReason(errors.New("dial tcp: connection refused")); got != "dial tcp: connection refused" {
		t.Fatalf("reason = %q", got)
	}
}

func TestRenameRepairReportText(t *testing.T) {
	got := renameRepairReportText(37, 1234, 0, 0, 0, 0)
	want := "🗂 Library check: 37 series, 1,234 episodes — all correctly named, no inferior or duplicate copies"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = renameRepairReportText(37, 1200, 3, 1, 0, 0)
	want = "🗂 Library check: 37 series · 3 episodes renamed · 1 inferior copy removed"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDedupeReleasesByHashKeepsFirstCopy(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	releases := []models.Release{
		{Title: "feed", Magnet: magnet, Source: "TGx"},
		{Title: "no-hash"},
		{Title: "archive", Magnet: magnet + "&dn=other.name", Source: "archive:x"},
		{Title: "no-hash-2"},
	}
	got := dedupeReleasesByHash(releases)
	if len(got) != 3 || got[0].Title != "feed" || got[1].Title != "no-hash" || got[2].Title != "no-hash-2" {
		t.Fatalf("deduped = %+v", got)
	}
}

func TestRenameItemLogTextSaysWhatChanged(t *testing.T) {
	renamed := map[string]any{"season": int64(2), "episode": int64(8), "from": "/lib/Agenzia/S02/agenzia.s02e08.1080p.mkv", "to": "/lib/Agenzia/S02/Agenzia S02E08 - Titolo.mkv", "executed": true}
	want := "✏️ Agenzia S02E08 renamed: «agenzia.s02e08.1080p.mkv» → «Agenzia S02E08 - Titolo.mkv»"
	if got := renameItemLogText("Agenzia", renamed, ""); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	discarded := map[string]any{"season": int64(1), "episode": int64(3), "from": "/lib/X/S01/x.720p.mkv", "discarded": true}
	want = "🗑️ X S01E03: «x.720p.mkv» deleted — the library already has a better copy"
	if got := renameItemLogText("X", discarded, "delete"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := renameItemLogText("X", map[string]any{"from": "a", "error": "boom"}, ""); got != "" {
		t.Fatalf("failed item must not log, got %q", got)
	}
}
