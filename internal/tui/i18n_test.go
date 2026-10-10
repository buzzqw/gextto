package tui

import "testing"

// TestCatalogIsComplete fails when a string is missing in any language.
func TestCatalogIsComplete(t *testing.T) {
	names := []string{"Italian", "English", "German", "French", "Spanish", "Polish"}
	for _, key := range CatalogKeys() {
		entry := catalog[key]
		for i, name := range names {
			if entry[i] == "" {
				t.Errorf("missing %s translation for %q", name, key)
			}
		}
	}
}

func TestTranslatorBothLanguages(t *testing.T) {
	it := NewTranslator("it")
	en := NewTranslator("en")
	if got := it.T("tab.torrents"); got != "Torrent" {
		t.Errorf("it tab.torrents = %q", got)
	}
	if got := en.T("tab.torrents"); got != "Torrents" {
		t.Errorf("en tab.torrents = %q", got)
	}
	if got := it.T("prompt.search"); got != "Cerca: " {
		t.Errorf("it prompt.search = %q", got)
	}
	if got := en.T("prompt.search"); got != "Search: " {
		t.Errorf("en prompt.search = %q", got)
	}
	if got := it.Lang(); got != "it" {
		t.Errorf("it.Lang() = %q", got)
	}
	if got := en.Lang(); got != "en" {
		t.Errorf("en.Lang() = %q", got)
	}
}

func TestTranslatorUnknownKey(t *testing.T) {
	it := NewTranslator("it")
	if got := it.T("does.not.exist"); got != "does.not.exist" {
		t.Errorf("unknown key should echo the key, got %q", got)
	}
	if got := it.Format("msg.removedone", 3, 1); got != "completati rimossi: 3, saltati: 1" {
		t.Errorf("Format = %q", got)
	}
}

func TestTranslatorFallbackLanguage(t *testing.T) {
	// German/French/Spanish/Polish are supported now.
	for _, tc := range []struct{ lang, want string }{
		{"de", "de"}, {"fr", "fr"}, {"es", "es"}, {"pl", "pl"},
	} {
		if got := NewTranslator(tc.lang).Lang(); got != tc.want {
			t.Errorf("NewTranslator(%q).Lang() = %q, want %q", tc.lang, got, tc.want)
		}
	}
	// An unknown language falls back to Italian.
	if got := NewTranslator("xx").Lang(); got != "it" {
		t.Errorf("unknown language should fall back to it, got %q", got)
	}
}

func TestStateLabelLocalized(t *testing.T) {
	it := NewTranslator("it")
	en := NewTranslator("en")
	cases := []struct{ state, it, en string }{
		{"downloading", "In scarico", "Downloading"},
		{"seeding", "In seed", "Seeding"},
		{"paused", "In pausa", "Paused"},
		{"stalled", "In attesa di seed", "Stalled"},
	}
	for _, tc := range cases {
		if got := it.StateLabel(tc.state); got != tc.it {
			t.Errorf("it StateLabel(%q) = %q, want %q", tc.state, got, tc.it)
		}
		if got := en.StateLabel(tc.state); got != tc.en {
			t.Errorf("en StateLabel(%q) = %q, want %q", tc.state, got, tc.en)
		}
	}
	// Unknown states pass through unchanged.
	if got := it.StateLabel("weird"); got != "weird" {
		t.Errorf("unknown state = %q", got)
	}
}

func TestResolveLang(t *testing.T) {
	cases := []struct {
		explicit, env, daemon, want string
	}{
		{"en", "it", "it", "en"},
		{"", "en_US", "it", "en"},
		{"", "", "en", "en"},
		{"", "", "", "it"},
		{"", "", "unknown", "it"},
		{"it", "en", "en", "it"},
	}
	for _, tc := range cases {
		if got := ResolveLang(tc.explicit, tc.env, tc.daemon); got != tc.want {
			t.Errorf("ResolveLang(%q,%q,%q) = %q, want %q", tc.explicit, tc.env, tc.daemon, got, tc.want)
		}
	}
}
