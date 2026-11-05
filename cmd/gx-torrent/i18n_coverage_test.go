package main

// i18n_coverage_test.go is the i18n guard for the daemon pages: every text and
// every translatable attribute is written through `tr`, the catalog key it
// names exists, and the page's JavaScript only uses declared client keys. A new
// phrase written as plain English would ship untranslated, so it fails here.

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// uiI18nAllowlist holds strings that must not be translated (brand names and
// protocol tokens identical in every language).
var uiI18nAllowlist = map[string]bool{
	"gx-torrent": true,
	"TCP":        true,
	"uTP":        true,
	// The peer flag of a torrent that is a complete source.
	"seed": true,
	// Language endonyms in the wizard's language selector.
	"English": true, "Italiano": true, "Deutsch": true, "Français": true, "Español": true, "Polski": true,
	// Acronym kept as-is on the RSS page.
	"RSS": true,
	// Indexer product names used as placeholders.
	"Jackett": true, "Prowlarr": true, "indexarr": true, "MIRCrew": true,
	// Example values used as placeholders on the RSS page.
	"1080p, ita": true, "cam, ts": true, "the office, 1080p": true, "Serie S01": true, "Serie S02": true, "Serie": true,
	// Example values used as placeholders on the settings page.
	"smtp.example.com:587": true, "gx-torrent@example.com": true,
	"/srv/media/Movies\n/srv/media/TV": true,
	// Piece sizes offered by the torrent creator: unit labels, identical in
	// every language.
	"32 KiB": true, "64 KiB": true, "128 KiB": true, "256 KiB": true,
	"512 KiB": true, "1 MiB": true, "2 MiB": true, "4 MiB": true,
	"8 MiB": true, "16 MiB": true,
	// An example version number, identical in every language.
	"5.2.3": true,
}

// uiI18nWorth reports whether a string is user-facing prose worth translating.
func uiI18nWorth(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || !regexp.MustCompile(`[A-Za-zÀ-ÿ]{2,}`).MatchString(s) {
		return false
	}
	if regexp.MustCompile(`[{}<>]|%[sd]|==|!=|&&|\|\||:=|https?://`).MatchString(s) {
		return false
	}
	if strings.HasPrefix(s, "{") {
		return false
	}
	// Units and separators alone are not prose: "12/34 MB" carries no words.
	if uiI18nUnitOnly(s) {
		return false
	}
	// Lower-case single tokens are CSS classes, selectors or field names.
	if regexp.MustCompile(`^[a-z0-9_./:{} -]+$`).MatchString(s) {
		return false
	}
	return true
}

// uiI18nUnitOnly reports whether a string is only numbers, separators and
// units, which read the same in every language.
func uiI18nUnitOnly(s string) bool {
	core := strings.Trim(s, " \t\n0123456789.,:/·%")
	switch core {
	case "", "B", "KB", "MB", "GB", "TB", "KiB", "MiB", "GiB", "TiB",
		"ms", "s", "h", "m", "min", "KiB/s", "MiB/s", "MB/s", "KB/s":
		return true
	}
	return false
}

// uiI18nTranslatableAttrs are the attributes whose value is user-facing text.
var uiI18nTranslatableAttrs = []string{
	"title", "placeholder", "aria-label", "label",
	"data-label-auto", "data-label-light", "data-label-dark",
}

var (
	uiI18nAction     = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	uiI18nTextNode   = regexp.MustCompile(`>([^<>]*)<`)
	uiI18nQuotedText = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
)

// uiI18nPages returns the name and the source of every page the daemon serves.
func uiI18nPages() []struct{ name, source string } {
	var pages []struct{ name, source string }
	for _, name := range []string{"ui", "rss", "settings", "wanted", "archive", "stats", "setup", "login", "token"} {
		pages = append(pages, struct{ name, source string }{name, uiPageSource[name]()})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].name < pages[j].name })
	return pages
}

// uiI18nStrip removes what is not translated: scripts, styles and comments.
func uiI18nStrip(source string) string {
	text := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`).ReplaceAllString(source, "")
	text = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(text, "")
	return regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`).ReplaceAllString(text, "")
}

// uiI18nTrKey returns the catalog key of a `tr` action, or "" when the action
// is not a `tr` call.
func uiI18nTrKey(action string) (key string, isTr bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(action), "{{"), "}}"))
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	if !strings.HasPrefix(trimmed, "tr ") && trimmed != "tr" {
		return "", false
	}
	m := uiI18nQuotedText.FindStringSubmatch(strings.TrimPrefix(trimmed, "tr"))
	if m == nil {
		return "", true
	}
	return uiUnquoteGo(m[1]), true
}

// uiUnquoteGo decodes the escapes a template string literal may carry.
func uiUnquoteGo(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// TestUIPageIsFullyTranslatable is the static guard of the render-time
// translation: every text node and every translatable attribute of every page
// goes through `tr`, and the key it names is in the catalog. A phrase written
// as plain text would stay English in every language, so it fails here.
func TestUIPageIsFullyTranslatable(t *testing.T) {
	keys := map[string]bool{}
	for key := range uiCatalog {
		keys[key] = true
	}
	var problems []string
	report := func(where, what string) { problems = append(problems, where+": "+what) }

	for _, page := range uiI18nPages() {
		text := uiI18nStrip(page.source)
		// A page must parse in a language other than English too: the parsed
		// set binds `tr` to that dictionary.
		uiTemplatesFor(page.name, "de")

		// Every action that carries a phrase (other than tr) would print a
		// literal the catalog cannot see.
		for _, action := range uiI18nAction.FindAllString(text, -1) {
			key, isTr := uiI18nTrKey(action)
			if !isTr {
				for _, m := range uiI18nQuotedText.FindAllStringSubmatch(action, -1) {
					literal := html.UnescapeString(uiUnquoteGo(m[1]))
					if uiI18nWorth(literal) && !uiI18nAllowlist[literal] {
						report(page.name, "phrase outside tr: "+action)
					}
				}
				continue
			}
			// A brand, a protocol token or a unit needs no translation: `tr`
			// renders it as written when the catalog has no entry.
			if key != "" && !keys[key] && !uiI18nAllowlist[key] {
				report(page.name, "tr key missing from the catalog: "+key)
			}
		}

		// Text outside an action is printed as written. A literal that is a
		// catalog key must be translated even when it is a single lower-case
		// word ("off", "none", "paused"), which is why the check is not limited
		// to prose.
		for _, m := range uiI18nTextNode.FindAllStringSubmatch(uiI18nAction.ReplaceAllString(text, " "), -1) {
			literal := html.UnescapeString(m[1])
			trimmed := strings.TrimSpace(literal)
			if uiI18nAllowlist[trimmed] {
				continue
			}
			if keys[trimmed] || (uiI18nWorth(literal) && !uiI18nUnitOnly(trimmed)) {
				report(page.name, "text outside tr: "+strings.Join(strings.Fields(literal), " "))
			}
		}

		// The same for the attributes a reader sees.
		for _, name := range uiI18nTranslatableAttrs {
			attr := regexp.MustCompile(`(?s)\s` + regexp.QuoteMeta(name) + `="([^"]*)"`)
			for _, m := range attr.FindAllStringSubmatch(text, -1) {
				value := uiI18nAction.ReplaceAllString(m[1], " ")
				literal := html.UnescapeString(value)
				trimmed := strings.TrimSpace(literal)
				if uiI18nAllowlist[trimmed] {
					continue
				}
				if keys[trimmed] || (uiI18nWorth(literal) && !uiI18nUnitOnly(trimmed)) {
					report(page.name, name+" outside tr: "+strings.Join(strings.Fields(literal), " "))
				}
			}
		}
	}
	sort.Strings(problems)
	for _, problem := range problems {
		t.Error(problem)
	}
}

// TestUIPageTranslationCoversEveryLanguage parses every page in every language:
// a template that cannot be parsed for one dictionary would fail at the first
// request in that language, not at build time.
func TestUIPageTranslationCoversEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en", "it", "de", "fr", "es", "pl"} {
		for _, page := range uiI18nPages() {
			if uiTemplatesFor(page.name, lang) == nil {
				t.Fatalf("%s page has no %s template set", page.name, lang)
			}
		}
	}
}

// TestWantedRowsAreTranslated guards the wanted table rows: they are built in
// Go (uiWantedRowsHTML), so they are not part of the page assets the test above
// scans. Every label they print must still be a catalog key, or a new row
// button would ship an untranslated title.
func TestWantedRowsAreTranslated(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"Show","kind":"series","seasons":"1+"},{"name":"Film","kind":"movie","paused":true}]`)
	// The source-language row carries catalog keys as tooltips, so a new button
	// cannot ship a title without a translation.
	rows := d.uiWantedRowsHTML("")
	for _, m := range regexp.MustCompile(`title="([^"{}]+)"`).FindAllStringSubmatch(rows, -1) {
		if _, ok := uiCatalog[m[1]]; !ok {
			t.Errorf("wanted row tooltip %q has no uiCatalog entry", m[1])
		}
	}
	// A translated row names the chips and the actions in the page language:
	// the Italian dictionary carries them, so none of these may stay English.
	translated := d.uiWantedRowsHTML("it")
	for _, want := range []string{"Cerca ora", "Perché no?", "Modifica"} {
		if !strings.Contains(translated, want) {
			t.Errorf("wanted row is not translated: %q missing", want)
		}
	}
	// A wanted title is data: it must be printed as it is, even when it happens
	// to be an English label.
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"All","kind":"series","seasons":"1+"}]`)
	if rows := d.uiWantedRowsHTML("it"); !strings.Contains(rows, `>All</button>`) {
		t.Error("the wanted title was translated like a label")
	}
}

// TestUIClientKeysAreTranslated checks the strings the page's JavaScript looks
// up at runtime: each one is a declared client key and a catalog entry.
func TestUIClientKeysAreTranslated(t *testing.T) {
	for _, key := range uiClientKeys {
		if _, ok := uiCatalog[key]; !ok {
			t.Errorf("uiClientKeys %q has no uiCatalog entry", key)
		}
	}
	client := map[string]bool{}
	for _, key := range uiClientKeys {
		client[key] = true
	}
	for _, page := range uiI18nPages() {
		for _, m := range regexp.MustCompile(`\bt\('([^']*)'\)`).FindAllStringSubmatch(page.source, -1) {
			if !client[m[1]] {
				t.Errorf("%s: t(%q) is not in uiClientKeys", page.name, m[1])
			}
		}
	}
}
