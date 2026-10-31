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

// TestWantedRowMenuIsNotClipped guards the wanted rows' "More" menu: it opens
// over the card, so the list must not clip its children (the remove action sits
// at the bottom of the menu and was cut off by the card's edge), and the menu
// must be able to flip upwards when there is no room below.
func TestWantedRowMenuIsNotClipped(t *testing.T) {
	for _, line := range strings.Split(ThemeCSS(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ul.items {") && strings.Contains(line, "overflow: hidden") {
			t.Errorf("the item list clips the row menu: %s", line)
		}
	}
	for _, marker := range []string{".row-more-menu.up{", "classList.add('up')"} {
		if !strings.Contains(WantedPage(), marker) {
			t.Errorf("the wanted page is missing %q", marker)
		}
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
