package tui

import (
	"strings"
	"testing"
)

// TestApplyRefreshDropsStaleResults keeps a slow poll from rewinding a view
// the user has changed while it was in flight.
func TestApplyRefreshDropsStaleResults(t *testing.T) {
	model := NewModel(NewTranslator("en"))

	model.applyRefresh(refreshResult{seq: 1, hasArchive: true, archivePage: 1})
	if model.ArchivePage != 1 {
		t.Fatalf("first result not applied: page %d", model.ArchivePage)
	}

	// The user turned to page 5. A late answer from the old request must not
	// move the page back, even though it carries a full archive snapshot.
	model.ArchivePage = 5
	model.applyRefresh(refreshResult{seq: 1, hasArchive: true, archivePage: 1})
	if model.ArchivePage != 5 {
		t.Fatalf("stale result overwrote the current page: %d", model.ArchivePage)
	}

	// A newer answer is still applied normally.
	model.applyRefresh(refreshResult{seq: 2, hasArchive: true, archivePage: 2})
	if model.ArchivePage != 2 {
		t.Fatalf("new result not applied: page %d", model.ArchivePage)
	}
}

// TestDerivedViewsAreCachedAndInvalidated keeps the download and log views
// stable across the several reads of one frame, while still following every
// setter and filter change.
func TestDerivedViewsAreCachedAndInvalidated(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.SetTorrents([]Torrent{{Hash: "a", Name: "A"}, {Hash: "b", Name: "B"}})
	first := model.VisibleDownloads()
	if len(first) != 2 {
		t.Fatalf("downloads = %d, want 2", len(first))
	}
	if second := model.VisibleDownloads(); &second[0] != &first[0] {
		t.Fatal("unchanged download view was rebuilt")
	}
	model.Filter = "B"
	if second := model.VisibleDownloads(); len(second) != 1 {
		t.Fatalf("filtered downloads = %d, want 1", len(second))
	}
	model.SetTorrents([]Torrent{{Hash: "a", Name: "A"}, {Hash: "b", Name: "B"}, {Hash: "c", Name: "C"}})
	model.Filter = ""
	if second := model.VisibleDownloads(); len(second) != 3 {
		t.Fatalf("downloads after reload = %d, want 3", len(second))
	}

	model.SetLogs([]string{"INFO a", "WARN b"})
	model.LogFilter = "WARN"
	if lines := model.FilteredLogs(); len(lines) != 1 {
		t.Fatalf("filtered logs = %d, want 1", len(lines))
	}
	model.AppendLog("WARN c")
	if lines := model.FilteredLogs(); len(lines) != 2 {
		t.Fatalf("filtered logs after append = %d, want 2", len(lines))
	}
	model.LogFilter = ""
	if lines := model.FilteredLogs(); len(lines) != 3 {
		t.Fatalf("all logs = %d, want 3", len(lines))
	}
}

// TestFetchErrorsAreTrackedAndCleared keeps a failure visible until the
// endpoint answers again, instead of showing old data as if it were fresh.
func TestFetchErrorsAreTrackedAndCleared(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.applyRefresh(refreshResult{seq: 1, fetchErrors: []FetchError{{Source: "archive", Message: "timeout"}}})
	if len(model.FetchErrors) != 1 || model.FetchErrors[0].Message != "timeout" {
		t.Fatalf("failure not recorded: %+v", model.FetchErrors)
	}
	model.applyRefresh(refreshResult{seq: 2, fetchErrors: []FetchError{{Source: "archive", Message: ""}}})
	if len(model.FetchErrors) != 0 {
		t.Fatalf("failure not cleared after a successful poll: %+v", model.FetchErrors)
	}
}

// TestFetchErrorsAreVisible pins the user-facing signal of a stale endpoint.
func TestFetchErrorsAreVisible(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.SetFetchError("archive", "timeout")
	screen := model.Render(120, 24)
	var text strings.Builder
	for _, line := range screen.Lines {
		text.WriteString(line.Text)
		text.WriteString("\n")
	}
	if !strings.Contains(text.String(), "stale sources") {
		t.Fatalf("stale data not reported in the frame: %q", text.String())
	}
}
