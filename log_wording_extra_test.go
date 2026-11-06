package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestFriendlyQualityLeavesOutUnknown: the parser's "unknown" placeholder never
// reaches the log ("unknown unknown ITA").
func TestFriendlyQualityLeavesOutUnknown(t *testing.T) {
	if got := friendlyQuality(models.Quality{Resolution: "unknown", Source: "unknown", Language: "ita"}); got != "ITA" {
		t.Fatalf("quality = %q, want ITA", got)
	}
	if got := friendlyQuality(models.Quality{Resolution: "unknown"}); got != "unknown quality" {
		t.Fatalf("all unknown = %q", got)
	}
}

// TestCycleGapsTextIsPartOfTheDownloads: filled gaps are some of the started
// downloads, not one more kind.
func TestCycleGapsTextIsPartOfTheDownloads(t *testing.T) {
	cases := []struct {
		gaps, started int
		want          string
	}{
		{0, 3, ""},
		{1, 1, "it fills a missing episode"},
		{2, 2, "all of them fill missing episodes"},
		{1, 3, "1 of them fills a missing episode"},
		{2, 5, "2 of them fill missing episodes"},
	}
	for _, c := range cases {
		if got := cycleGapsText(c.gaps, c.started); got != c.want {
			t.Errorf("cycleGapsText(%d, %d) = %q, want %q", c.gaps, c.started, got, c.want)
		}
	}
}

func TestMediaIntegrityProgressDue(t *testing.T) {
	if mediaIntegrityProgressDue(120, 121) {
		t.Fatal("one more file is not worth a line")
	}
	if !mediaIntegrityProgressDue(149, 150) || !mediaIntegrityProgressDue(40, 120) {
		t.Fatal("crossing a multiple of 50 must be logged")
	}
}

func TestJobKindTextUsesWords(t *testing.T) {
	if got := jobKindText(mediaIntegrityJobKind); got != "media integrity check" {
		t.Fatalf("media integrity = %q", got)
	}
	if got := jobKindText("some-new-job"); got != "some new job" {
		t.Fatalf("unknown kind = %q", got)
	}
}
