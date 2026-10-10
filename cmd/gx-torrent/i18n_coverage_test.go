package main

// i18n_coverage_test.go is the i18n guard for the daemon page: it scans the
// page template (and the t() keys used by its JavaScript) and fails when a
// user-facing string is not a uiCatalog key, so a new string cannot be added
// without a translation. It also fails when a text segment mixes fixed words
// with a printed value: the server translator matches whole text nodes only,
// so such a segment would never be translated.

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

// uiI18nAllowlist holds strings that must not be translated (brand names and
// protocol tokens identical in every language).
var uiI18nAllowlist = map[string]bool{
	"gx-torrent": true,
	"TCP":        true,
	"uTP":        true,
	// Language endonyms in the wizard's language selector.
	"English": true, "Italiano": true, "Deutsch": true, "Français": true, "Español": true, "Polski": true,
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
	// Lower-case single tokens are CSS classes, selectors or field names.
	if regexp.MustCompile(`^[a-z0-9_./:{} -]+$`).MatchString(s) {
		return false
	}
	return true
}

func TestUIPageIsFullyTranslatable(t *testing.T) {
	raw := assets.UITemplate() + "\n" + assets.LoginPage() + "\n" + assets.SetupPage()
	text := raw
	// <script>/<style> content is not translated (the translator skips it) and
	// would only produce JS/CSS false positives; strip it before scanning.
	text = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(text, "")

	keys := map[string]bool{}
	for key := range uiCatalog {
		keys[key] = true
	}

	missing := map[string]string{}
	record := func(source, value string) {
		s := strings.TrimSpace(html.UnescapeString(value))
		if !uiI18nWorth(s) || keys[s] || uiI18nAllowlist[s] {
			return
		}
		missing[s] = source
	}

	for _, m := range regexp.MustCompile(`>([^<>{}]+)<`).FindAllStringSubmatch(text, -1) {
		record("text", m[1])
	}
	for _, m := range regexp.MustCompile(`(?:title|placeholder|aria-label|label)="([^"{}]+)"`).FindAllStringSubmatch(text, -1) {
		record("attr", m[1])
	}

	// A segment between two tags that mixes fixed words with a printed value
	// can never be translated: only a whole text node, or a whole if/else
	// branch, ends up as one node at runtime.
	action := regexp.MustCompile(`\{\{[^}]*\}\}`)
	branchOpen := regexp.MustCompile(`^\{\{-?\s*(?:if|else|range|with)\b`)
	branchClose := regexp.MustCompile(`^\{\{-?\s*(?:else|end)\b`)
	valueOnlyBranch := regexp.MustCompile(`(\{\{-?\s*(?:if|else)[^}]*\}\})\{\{[^}]*\}\}(\{\{-?\s*(?:else|end)[^}]*\}\})`)
	mixed := map[string]string{}
	for _, m := range regexp.MustCompile(`>([^<>]*)<`).FindAllStringSubmatch(text, -1) {
		segment := valueOnlyBranch.ReplaceAllString(m[1], "$1$2")
		if !strings.Contains(segment, "{{") {
			continue
		}
		actions := action.FindAllStringIndex(segment, -1)
		pieces := action.Split(segment, -1)
		trimmed := strings.TrimSpace(segment)
		onlyBlock := branchOpen.MatchString(trimmed) && strings.HasSuffix(trimmed, "}}")
		for _, a := range action.FindAllString(segment, -1) {
			if !branchOpen.MatchString(a) && !branchClose.MatchString(a) {
				onlyBlock = false
			}
		}
		ok := true
		for i, piece := range pieces {
			if !uiI18nWorth(piece) {
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
				record("branch", piece)
			} else {
				ok = false
			}
		}
		if !ok {
			mixed[strings.Join(strings.Fields(action.ReplaceAllString(segment, "{}")), " ")] = "segment"
		}
	}

	// The t() keys of the page's JavaScript must be declared client keys and
	// have a catalog entry, or the client would silently stay English.
	client := map[string]bool{}
	for _, key := range uiClientKeys {
		client[key] = true
		if !keys[key] {
			t.Errorf("uiClientKeys %q has no uiCatalog entry", key)
		}
	}
	for _, m := range regexp.MustCompile(`\bt\('([^']*)'\)`).FindAllStringSubmatch(raw, -1) {
		if !client[m[1]] {
			t.Errorf("t(%q) is not in uiClientKeys", m[1])
		}
	}

	if len(mixed) > 0 {
		lines := make([]string, 0, len(mixed))
		for s := range mixed {
			lines = append(lines, "  "+s)
		}
		sort.Strings(lines)
		t.Errorf("%d text segment(s) mix words with a printed value; wrap the words in their own element (e.g. <span>):\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
	if len(missing) > 0 {
		lines := make([]string, 0, len(missing))
		for s, source := range missing {
			lines = append(lines, "  ["+source+"] "+s)
		}
		sort.Strings(lines)
		t.Fatalf("%d string(s) without a uiCatalog key:\n%s", len(missing), strings.Join(lines, "\n"))
	}
}
