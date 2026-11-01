package main

// ui_mobile_test.go guards the phone layout of the standalone pages. There is
// no browser suite for the daemon page (the Playwright end-to-end tests drive
// Gextto's interface), so these are static checks: remove one of the rules and
// a phone goes back to a table squeezed into a few pixels.

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

func TestArchiveIsACardListOnAPhone(t *testing.T) {
	page := assets.ArchivePage()
	for _, want := range []string{
		"@media(max-width:760px)",
		"td.size::before,td.seeds::before",            // the labels the header would give
		"content:attr(data-label)",                    // …read from the cell
		"sz.dataset.label=t('Size')",                  // set by the rows
		"sd.dataset.label=t('Seeds')",                 //
		"table,tbody,tr,td{display:block;width:100%}", // one card per item
	} {
		if !strings.Contains(page, want) {
			t.Errorf("archive page is missing the phone rule %q", want)
		}
	}
}

func TestWantedLegendWrapsAsWholeItems(t *testing.T) {
	page := assets.WantedPage()
	if !strings.Contains(page, `class="lgitem"`) || !strings.Contains(page, ".legend .lgitem{display:inline-flex") {
		t.Fatal("the wanted legend must keep each icon together with its label when it wraps")
	}
}

func TestPhoneToolbarHasNoDuplicateButtons(t *testing.T) {
	// "Resume all"/"Pause all" and "Resume"/"Pause" have the same icon: with
	// the labels hidden on a phone the selected ones are indistinguishable.
	page := assets.UITemplate()
	for _, want := range []string{
		`class="ghost hide-m" data-bulk="resume"`,
		`class="ghost hide-m" data-bulk="pause"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the toolbar button %q must be hidden on a phone", want)
		}
	}
	if strings.Contains(page, `class="ghost" id="btn-resume-all"`) == false {
		t.Error("Resume all must stay on the phone toolbar")
	}
}

func TestCurrentSectionIsScrolledIntoView(t *testing.T) {
	// The section nav and the tab bars scroll sideways: the page you are on
	// must not be the item cut off at the edge.
	js := assets.CommonJS()
	if !strings.Contains(js, "revealActive") || !strings.Contains(js, `[aria-current="page"], [aria-selected="true"]`) {
		t.Fatal("common.js must reveal the current section in a scrolling nav or tab bar")
	}
}
