package main

// settings_form_test.go guards the browser-side constraint check of the daemon's
// settings page. An input whose seeded value is below its own `min` makes the
// whole form invalid, and the browser then refuses to submit it at all: every
// value on the page (the Web interface switch included) silently stops being
// saved. The seeding limits are seeded with -1 (disabled), the same sentinel the
// qBittorrent API uses, so their inputs must accept it.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

func TestSettingsFormAcceptsItsDisabledDefaults(t *testing.T) {
	page := assets.SettingsPage()
	for _, key := range []string{"max-ratio", "max-seeding-time"} {
		tag := regexp.MustCompile(`<input[^>]*name="` + key + `"[^>]*>`).FindString(page)
		if tag == "" {
			t.Fatalf("no input for %q in the settings page", key)
		}
		if !strings.Contains(tag, `min="-1"`) {
			t.Errorf("input %q must accept its -1 default, or it blocks the whole form: %s", key, tag)
		}
	}
	// A fixed step rejects ordinary ratios (0.01, 2.55) with the same effect:
	// the browser refuses to submit the page.
	tag := regexp.MustCompile(`<input[^>]*name="max-ratio"[^>]*>`).FindString(page)
	if !strings.Contains(tag, `step="any"`) {
		t.Errorf(`the share-ratio input needs step="any": a fixed step rejects 0.01 and blocks the form: %s`, tag)
	}
}

// TestTorrentDetailSeedRatioAcceptsAnyRatio: the per-torrent ratio is a decimal
// the operator types freely (0.01, 1.25 …); a fixed step rejects some of them
// and blocks the detail form the same way.
func TestTorrentDetailSeedRatioAcceptsAnyRatio(t *testing.T) {
	tag := regexp.MustCompile(`<input[^>]*name="seed_ratio"[^>]*>`).FindString(assets.UITemplate())
	if tag == "" {
		t.Fatal("no seed_ratio input in the torrent detail")
	}
	if !strings.Contains(tag, `step="any"`) {
		t.Errorf(`the torrent seed-ratio input needs step="any": %s`, tag)
	}
}
