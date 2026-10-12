package assets

import (
	"strings"
	"testing"
)

func TestAssetsAreEmbedded(t *testing.T) {
	tpl := UITemplate()
	if len(tpl) == 0 {
		t.Fatal("UITemplate is empty")
	}
	// The page body and the fragment definitions must be present: the daemon
	// renders both from the same parsed template set.
	for _, marker := range []string{"<!doctype html>", `{{define "fragments"}}`, `{{define "detail"}}`, `{{define "addopts"}}`} {
		if !strings.Contains(tpl, marker) {
			t.Errorf("UITemplate is missing %q", marker)
		}
	}
	if !strings.Contains(TokenPage(), "<form") {
		t.Error("TokenPage is missing its form")
	}
	if !strings.Contains(LoginPage(), "<form") {
		t.Error("LoginPage is missing its form")
	}
	if !strings.Contains(SetupPage(), "<form") {
		t.Error("SetupPage is missing its form")
	}
	if !strings.HasPrefix(FaviconSVG(), "<svg") {
		t.Error("FaviconSVG is not an SVG")
	}
}

// TestArchivePageHasLiveIndexerSearch checks the archive page keeps the
// "search the indexers too" option wired to the live search endpoint.
func TestArchivePageHasLiveIndexerSearch(t *testing.T) {
	page := ArchivePage()
	for _, marker := range []string{
		`id="archive-live"`,
		`id="archive-live-section"`,
		`id="archive-live-rows"`,
		`/ui/search?q=`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("archive page is missing %q", marker)
		}
	}
}
