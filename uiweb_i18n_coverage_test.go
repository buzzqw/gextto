package gextto

// uiweb_i18n_coverage_test.go is the i18n guard: it scans the user-facing
// strings of the server-rendered UI (templates and Go view-models) and fails
// when a string is not a catalog key, so new UI text cannot be added without a
// translation. Brand names, format tokens, language codes and input examples
// are listed in i18nAllowlist because they are the same in every language.

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// i18nAllowlist holds strings that must not be translated: product/brand names,
// quality and codec tokens, BCP-47 language codes and free-text examples.
var i18nAllowlist = map[string]bool{
	// Formats, codecs, quality tokens (same in every language).
	"AAC": true, "AC3": true, "BluRay": true, "DTS": true, "DTS-HD": true,
	"DVDRip": true, "Dolby Digital Plus": true, "Dolby TrueHD": true, "HDR": true,
	"HDTV": true, "H.264 / AVC": true, "H.265 / HEVC": true, "MP3": true,
	"PROPER": true, "REAL": true, "REPACK": true, "Remux": true,
	"WEB-DL": true, "WEBRip": true,
	// Web search engines (proper names).
	"BT4G": true, "BTDig": true, "BitSearch": true, "EZTV": true, "Knaben": true,
	"LimeTorrents": true, "Nyaa": true, "TorrentsCSV": true, "Torrentz2": true,
	// Notification providers and signature label.
	"Discord": true, "Gotify": true, "Pushover": true, "Slack": true,
	"Gextto (JSON firmato)": true,
	// Prometheus-style name filters: bracketed language/kind tags.
	"[arabo]": true, "[cirillico]": true, "[cjk]": true, "[ebraico]": true,
	"[non-latino]": true, "[porno]": true, "[thai]": true,
	// BCP-47 language codes (values of the interface-language selector).
	"de-DE": true, "en-GB": true, "en-US": true, "es-ES": true, "fr-FR": true,
	"ja-JP": true, "ko-KR": true, "pt-BR": true, "pt-PT": true, "ru-RU": true,
	"zh-CN": true,
	// Free-text examples and dynamic fragments reused at runtime.
	"cam, ts": true, "es. TBK": true, "es. ita,eng": true,
	"-1 globale, 0 illimitato": true,
	"0 selezionati · Azioni:":  true,
	// Language endonyms shown in the interface-language selector, and the
	// "Comic" prefix of a download title.
	"Deutsch": true, "English": true, "Español": true, "Français": true,
	"Italiano": true, "Polski": true, "Comic": true,
}

// i18nWorth reports whether a string is user-facing prose worth translating.
func i18nWorth(s string) bool {
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
	// Lower-case single tokens are field names, CSS classes or selectors.
	if regexp.MustCompile(`^[a-z0-9_./:{} -]+$`).MatchString(s) {
		return false
	}
	return true
}

// i18nCatalogKeys reads the keys of the bundled English catalog.
func i18nCatalogKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	raw, err := os.ReadFile("internal_translations.yml")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	lineRE := regexp.MustCompile(`(?m)^\s*"((?:[^"\\]|\\.)*)"\s*:`)
	for _, m := range lineRE.FindAllStringSubmatch(string(raw), -1) {
		key := strings.ReplaceAll(m[1], `\"`, `"`)
		key = strings.ReplaceAll(key, `\\`, `\`)
		keys[key] = true
	}
	return keys
}

// TestUIIsTranslatable is the guard: every user-facing UI string must be a
// catalog key (or an explicitly allowed non-translatable token).
func TestUIIsTranslatable(t *testing.T) {
	keys := i18nCatalogKeys(t)

	templateText := regexp.MustCompile(`>([^<>{}]+)<`)
	templateAttr := regexp.MustCompile(`(?:title|placeholder|aria-label|label)="([^"{}]+)"`)
	goField := regexp.MustCompile(`(?:Label|Value|Title|Hint|Note|Empty|Placeholder|SearchHint|Description|Caption|Subtitle|Message|Flash):\s*"([^"\\\n]{3,})"`)
	// uiText localizes a string at runtime; its argument must be a catalog key
	// too, otherwise the localized message would stay Italian.
	goUIText := regexp.MustCompile(`uiText\([^,]+,\s*"([^"\\\n]{3,})"\)`)

	missing := map[string]string{}
	record := func(source, raw string) {
		s := strings.TrimSpace(html.UnescapeString(raw))
		if !i18nWorth(s) || keys[s] || i18nAllowlist[s] {
			return
		}
		missing[s] = source
	}

	templates, _ := filepath.Glob("uiweb/v2/templates/*.html")
	for _, path := range templates {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		for _, m := range templateText.FindAllStringSubmatch(text, -1) {
			record(filepath.Base(path), m[1])
		}
		for _, m := range templateAttr.FindAllStringSubmatch(text, -1) {
			record(filepath.Base(path), m[1])
		}
	}

	goFiles, _ := filepath.Glob("uiweb*.go")
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range goField.FindAllStringSubmatch(string(raw), -1) {
			record(path, m[1])
		}
		for _, m := range goUIText.FindAllStringSubmatch(string(raw), -1) {
			record(path, m[1])
		}
	}

	if len(missing) > 0 {
		lines := make([]string, 0, len(missing))
		for s, src := range missing {
			lines = append(lines, "  "+src+": "+s)
		}
		sort.Strings(lines)
		t.Fatalf("%d UI string(s) without a catalog key (add them to internal_translations*.yml or to i18nAllowlist):\n%s",
			len(missing), strings.Join(lines, "\n"))
	}
}

// TestClientI18nKeysTranslated checks every key the client script renders at
// runtime exists in the catalogs, so window.__v2i18n never falls back silently.
func TestClientI18nKeysTranslated(t *testing.T) {
	keys := i18nCatalogKeys(t)
	var missing []string
	for _, key := range v2ClientKeys {
		if !keys[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("client i18n keys without a catalog entry:\n%s", strings.Join(missing, "\n"))
	}
}
