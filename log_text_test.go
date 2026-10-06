package gextto

import (
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
	if got := cycleReportText("5 minutes", stats, 5, 0); got != want {
		t.Fatalf("report =\n %q\nwant\n %q", got, want)
	}
	stats = &models.CycleStats{Scraped: 10, Candidates: 0, Errors: 2}
	want = "📊 Search finished in 3 seconds: 10 releases checked, 0 matched your titles, nothing new to download; 2 problems (see the warnings above)"
	if got := cycleReportText("3 seconds", stats, 0, 0); got != want {
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
