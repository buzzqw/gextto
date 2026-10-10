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
	// Language endonyms shown in the interface-language selector, and the
	// "Comic" prefix of a download title.
	"Deutsch": true, "English": true, "Español": true, "Français": true,
	"Italiano": true, "Polski": true, "Comic": true,
	// Transport protocol name.
	"TCP": true,
}

// i18nMixedAllowlist holds template texts that mix a printed value with words
// that are the same in every language (brand names, identifiers).
var i18nMixedAllowlist = map[string]bool{
	"Gextto · {}": true,
	"EXpert Torrent Transfer Orchestrator · v{}": true,
	"libtorrent {}":          true,
	"TMDB {}":                true,
	"TVDB {}":                true,
	"{} torrent · {} HTTP ·": true,
	"Auto: {}on{}off{}":      true,
	"CPU{}":                  true,
	"{}TMDB {}{}TVDB {}{}{} · {}{}{} · ★ {}{}{} ·": true,
	// The inline <script> that defines the client dictionary.
	"window.__v2i18n={}{}{}{}{};": true,
	"window.__v2i18n={}{}{}{};":   true,
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
	templateAttr := regexp.MustCompile(`(?:title|placeholder|aria-label|label|hx-confirm|data-v2-toast-title|data-v2-toast-message)="([^"{}]+)"`)
	goField := regexp.MustCompile(`(?:Label|Value|Title|Hint|Note|Empty|Placeholder|SearchHint|Description|Caption|Subtitle|Message|Flash|Confirm|Intro|Submit|TestLabel|RefreshHint|GeneralNote|CustomPlaceholder|Initial):\s*"([^"\\\n]{3,})"`)
	// A text node at runtime is everything between two tags. Template actions
	// do not split it: a printed value ({{.X}}) or a word sitting next to an
	// {{if}} block ends up in the same node as the surrounding words, so that
	// node can never match a catalog key. Only a text that fills the whole
	// segment, or the whole body of an if/else branch, is translatable.
	action := regexp.MustCompile(`\{\{[^}]*\}\}`)
	branchOpen := regexp.MustCompile(`^\{\{-?\s*(?:if|else|range|with)\b`)
	branchClose := regexp.MustCompile(`^\{\{-?\s*(?:else|end)\b`)
	// A branch that only prints a value ({{if .X}}{{.X}}{{else}}Default{{end}})
	// leaves the other branch alone in its node: drop the value so the
	// default text is checked as a branch body.
	valueOnlyBranch := regexp.MustCompile(`(\{\{-?\s*(?:if|else)[^}]*\}\})\{\{[^}]*\}\}(\{\{-?\s*(?:else|end)[^}]*\}\})`)
	mixed := map[string]string{}
	// uiText localizes a string at runtime; its argument must be a catalog key
	// too, otherwise the localized message would stay Italian.
	// Assignments (data.Empty = "...") and the field helpers that take the
	// label and hint as arguments.
	goAssign := regexp.MustCompile(`\.(?:Empty|Message|Hint|Title|Label|Note|Initial)\s*=\s*"([^"\\\n]{3,})"`)
	goHelper := regexp.MustCompile(`(?:boolField|boolFieldHint|sectionSettingsActions)\(\s*"[^"]*",\s*"([^"\\\n]{3,})"(?:,\s*"([^"\\\n]{3,})")?`)
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
		// Text between template actions, and text mixed with printed values.
		for _, m := range regexp.MustCompile(`>([^<>]*)<`).FindAllStringSubmatch(text, -1) {
			segment := valueOnlyBranch.ReplaceAllString(m[1], "$1$2")
			if !strings.Contains(segment, "{{") || strings.Contains(segment, `="`) || strings.Contains(segment, "&amp;") {
				continue // plain text (checked above) or an attribute cut by an action
			}
			actions := action.FindAllStringIndex(segment, -1)
			pieces := action.Split(segment, -1)
			// Branch bodies are separate text nodes only when the segment is
			// nothing but the if/else block: no printed value and no text
			// around it.
			trimmed := strings.TrimSpace(segment)
			onlyBlock := branchOpen.MatchString(trimmed) && strings.HasSuffix(trimmed, "}}")
			for _, a := range action.FindAllString(segment, -1) {
				if !branchOpen.MatchString(a) && !branchClose.MatchString(a) {
					onlyBlock = false
				}
			}
			ok := true
			for i, piece := range pieces {
				if !i18nWorth(piece) {
					continue
				}
				left, right := "", ""
				if i > 0 {
					left = segment[actions[i-1][0]:actions[i-1][1]]
				}
				if i < len(actions) {
					right = segment[actions[i][0]:actions[i][1]]
				}
				if onlyBlock && branchOpen.MatchString(left) && branchClose.MatchString(right) {
					record(filepath.Base(path), piece)
				} else {
					ok = false
				}
			}
			if !ok {
				key := strings.Join(strings.Fields(action.ReplaceAllString(segment, "{}")), " ")
				if !i18nMixedAllowlist[key] {
					mixed[key] = filepath.Base(path)
				}
			}
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
		for _, m := range goAssign.FindAllStringSubmatch(string(raw), -1) {
			record(path, m[1])
		}
		for _, m := range goHelper.FindAllStringSubmatch(string(raw), -1) {
			record(path, m[1])
			if m[2] != "" {
				record(path, m[2])
			}
		}
	}

	if len(mixed) > 0 {
		lines := make([]string, 0, len(mixed))
		for s, src := range mixed {
			lines = append(lines, "  "+src+": "+s)
		}
		sort.Strings(lines)
		t.Errorf("%d template text(s) mix words with a printed value, so they can never be translated: wrap the words in their own element (e.g. <span>):\n%s",
			len(lines), strings.Join(lines, "\n"))
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

// TestClientScriptKeysAreSent checks every t()/th() literal in the client
// script is in v2ClientKeys: only those keys reach window.__v2i18n, any other
// one would silently stay Italian. The lookup trims spaces like
// v2TranslateText does, so the trimmed key must be in the catalog.
func TestClientScriptKeysAreSent(t *testing.T) {
	raw, err := os.ReadFile("uiweb/v2/static/v2-core.js")
	if err != nil {
		t.Fatalf("read client script: %v", err)
	}
	sent := map[string]bool{}
	for _, key := range v2ClientKeys {
		sent[key] = true
	}
	keys := i18nCatalogKeys(t)
	var problems []string
	for _, m := range regexp.MustCompile(`\bth?\("((?:[^"\\]|\\.)*)"\)`).FindAllStringSubmatch(string(raw), -1) {
		key := m[1]
		if !sent[key] {
			problems = append(problems, "not in v2ClientKeys: "+key)
		}
		if trimmed := strings.TrimSpace(key); !keys[trimmed] {
			problems = append(problems, "no catalog entry for: "+trimmed)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("client script keys:\n%s", strings.Join(problems, "\n"))
	}
}
