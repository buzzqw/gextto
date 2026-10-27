package gextto

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/logging"
)

// Two copies of the same old file in different folders are one line, not the
// same line twice.
func TestReplacedCopiesAreOneLine(t *testing.T) {
	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	cfg := &Config{CleanupAction: "move"}
	logRemovedFiles(cfg, []string{"/lib/Show/Show - S01E01 - [1080p].mkv", "/lib/Show/Season 01/Show - S01E01 - [1080p].mkv"}, "/lib/Show/Show - S01E01 - [2160p].mkv")
	out := captured.String()
	if strings.Count(out, "Replaced with a better version") != 1 {
		t.Fatalf("want one line:\n%s", out)
	}
	if !strings.Contains(out, "(2 copies in different folders) are in the trash") {
		t.Fatalf("copies not counted:\n%s", out)
	}
}

// A Torznab error body becomes its code and description, not raw XML.
func TestTorrentFetchFailureReasonReadsTorznabError(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?> <error code="201" description="release not found"/>`
	got := torrentFetchFailureReason(strings.NewReader(body))
	if got != "Torznab 201: release not found" {
		t.Fatalf("reason = %q", got)
	}
}
