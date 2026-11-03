package gextto

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// templateLiteralAllowlist holds the words a template may print outside `tr`:
// product and service names, protocols and units, which read the same in every
// language.
var templateLiteralAllowlist = map[string]bool{
	"Gextto": true, "gx-torrent": true, "gx-core": true, "EXTTO": true,
	"EXpert": true, "Torrent": true, "Transfer": true, "Orchestrator": true,
	"TMDB": true, "TVDB": true, "IMDb": true, "eD2k": true, "aMule": true, "Kad": true,
	"Plex": true, "Jellyfin": true, "Simkl": true, "Trakt": true, "qBittorrent": true,
	"libtorrent": true, "HTTP": true, "API": true, "NAS": true, "CPU": true, "RAM": true,
	"SQL": true, "JSON": true, "URL": true, "OK": true, "ETA": true, "DL": true, "UL": true,
	"KiB": true, "MiB": true, "GiB": true, "KB": true, "MB": true, "GB": true, "TB": true,
	"ms": true, "s": true, "h": true, "m": true, "min": true, "v": true, "S": true, "E": true,
	"GHz": true,
}

var (
	templateComment       = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)
	templateRawBlock      = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>|<!--.*?-->`)
	templateActionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	templateTextNode      = regexp.MustCompile(`>([^<>]*)<`)
	templateAttrValue     = regexp.MustCompile(`\s(?:title|placeholder|aria-label|label|hx-confirm|data-v2-toast-title|data-v2-toast-message)="([^"]*)"`)
	templateWord          = regexp.MustCompile(`[\p{L}_][\p{L}\d_\-]*`)
)

// templateUntranslatedWords returns the words of a text that are printed as
// they are: neither inside an action nor in the allowlist.
func templateUntranslatedWords(text string) []string {
	var words []string
	for _, word := range templateWord.FindAllString(text, -1) {
		// A word with an underscore is an identifier (a setting key shown in
		// <code>), not prose.
		if !templateLiteralAllowlist[word] && !strings.Contains(word, "_") {
			words = append(words, word)
		}
	}
	return words
}

// TestTemplatesTranslateEveryText is the static guard of the render-time
// translation: every text node and every translatable attribute of the v2
// templates goes through `tr` (or `trTitle`). A new phrase written as plain
// text would stay Italian in every language, so it fails here instead.
func TestTemplatesTranslateEveryText(t *testing.T) {
	templates, err := filepath.Glob("uiweb/v2/templates/*.html")
	if err != nil || len(templates) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	var problems []string
	for _, path := range templates {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := templateComment.ReplaceAllString(string(raw), "")
		text = templateRawBlock.ReplaceAllString(text, "")
		// Actions become a blank: a quote inside {{tr "…"}} must not end the
		// attribute value, and the action itself is never printed text.
		stripped := templateActionPattern.ReplaceAllString(text, " ")
		for _, m := range templateAttrValue.FindAllStringSubmatch(stripped, -1) {
			if words := templateUntranslatedWords(m[1]); len(words) > 0 {
				problems = append(problems, filepath.Base(path)+": attribute "+strings.Join(strings.Fields(m[0]), " "))
			}
		}
		for _, m := range templateTextNode.FindAllStringSubmatch(stripped, -1) {
			if words := templateUntranslatedWords(m[1]); len(words) > 0 {
				problems = append(problems, filepath.Base(path)+": text "+strings.Join(strings.Fields(m[1]), " "))
			}
		}
	}
	sort.Strings(problems)
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestTranslateFuncLooksUpThenFills(t *testing.T) {
	dict := map[string]string{"Trovati %d file": "Found %d files", "Salva": "Save"}
	tr := v2TranslateFunc(dict, nil)
	if got := tr("  Salva\n"); got != "  Save\n" {
		t.Errorf("tr keeps the surrounding whitespace: got %q", got)
	}
	if got := tr("Trovati %d file", 3); got != "Found 3 files" {
		t.Errorf("tr fills after the lookup: got %q", got)
	}
	if got := tr("Assente"); got != "Assente" {
		t.Errorf("a missing key renders the Italian phrase: got %q", got)
	}
	if got := tr(42); got != 42 {
		t.Errorf("a number passes through unchanged: got %v", got)
	}
	markup := template.HTML("<b>Salva</b>")
	if got := tr(markup); got != markup {
		t.Errorf("typed markup passes through unchanged: got %v", got)
	}
}

func TestTitleFuncMarksOnlyTranslatedTitles(t *testing.T) {
	trTitle := v2TitleFunc(map[string]string{"Apri %s su TMDB": "Open %s on TMDB"}, nil)
	if got := string(trTitle("Apri %s su TMDB", `A "quoted" name`)); got != `title="Open A &#34;quoted&#34; name on TMDB" data-v2-title-translated="true"` {
		t.Errorf("translated title: got %s", got)
	}
	if got := string(trTitle("Senza traduzione")); got != `title="Senza traduzione"` {
		t.Errorf("an untranslated title carries no marker: got %s", got)
	}
}

// TestTemplatesRenderInTheActiveLanguage renders a fragment through the cached
// per-language set and checks that a catalog change is picked up.
func TestTemplatesRenderInTheActiveLanguage(t *testing.T) {
	state := newTestAppState(t)
	if err := state.i18n.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if err := state.i18n.Set("en", "Impossibile caricare il pannello", "Panel test text"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := v2TemplatesFor(state).ExecuteTemplate(&out, "v2_unavailable", map[string]any{"Title": "Impossibile caricare il pannello"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Panel test text") {
		t.Fatalf("the page is not rendered in English: %s", out.String())
	}
	if err := state.i18n.Set("en", "Impossibile caricare il pannello", "Changed text"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := v2TemplatesFor(state).ExecuteTemplate(&out, "v2_unavailable", map[string]any{"Title": "Impossibile caricare il pannello"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Changed text") {
		t.Fatalf("the cached template set served a stale translation: %s", out.String())
	}
}
