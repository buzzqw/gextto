package gextto

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// catalogFile describes one bundled i18n YAML catalog. The bundled catalogs are
// embedded in i18n.go; keeping a table here makes the validation tests below
// apply to every catalog automatically, including new languages.
type catalogFile struct {
	name string
	lang string
	raw  string
}

func bundledCatalogs() []catalogFile {
	return []catalogFile{
		{name: "internal_translations.yml", lang: "eng", raw: defaultTranslations},
		{name: "internal_translations_de.yml", lang: "deu", raw: defaultGermanTranslations},
		{name: "internal_translations_fr.yml", lang: "fra", raw: defaultFrenchTranslations},
		{name: "internal_translations_es.yml", lang: "spa", raw: defaultSpanishTranslations},
		{name: "internal_translations_pl.yml", lang: "pol", raw: defaultPolishTranslations},
	}
}

// parseCatalog decodes one bundled catalog and returns its language code and
// flat key/value entries. It fails the test on malformed YAML, on catalogs that
// do not contain exactly one top-level language, and on duplicate keys.
//
// yaml.v3 already rejects duplicate mapping keys while decoding into a Go map,
// but the error text is terse and we want to keep the guard explicit so a
// future switch to a lenient decoder does not silently drop entries.
func parseCatalog(t *testing.T, file catalogFile) (string, map[string]string) {
	t.Helper()

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(file.raw), &root); err != nil {
		t.Fatalf("%s: parse catalog: %v", file.name, err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("%s: expected a single top-level language mapping", file.name)
	}

	top := root.Content[0]
	if len(top.Content) != 2 {
		t.Fatalf("%s: expected exactly one top-level language, found %d", file.name, len(top.Content)/2)
	}
	lang := strings.TrimSpace(top.Content[0].Value)
	entriesNode := top.Content[1]
	if entriesNode.Kind != yaml.MappingNode {
		t.Fatalf("%s: language %q is not a key/value mapping", file.name, lang)
	}

	entries := make(map[string]string, len(entriesNode.Content)/2)
	for i := 0; i+1 < len(entriesNode.Content); i += 2 {
		key := entriesNode.Content[i].Value
		if _, exists := entries[key]; exists {
			t.Errorf("%s: duplicate key %q", file.name, key)
		}
		entries[key] = entriesNode.Content[i+1].Value
	}
	return lang, entries
}

// TestBundledCatalogsAreWellFormed is the "catalog check" from the technical
// review: every bundled catalog must parse, declare the expected language, and
// contain no duplicate or empty entries.
func TestBundledCatalogsAreWellFormed(t *testing.T) {
	for _, file := range bundledCatalogs() {
		lang, entries := parseCatalog(t, file)
		if lang != file.lang {
			t.Errorf("%s: language = %q, want %q", file.name, lang, file.lang)
		}
		if len(entries) == 0 {
			t.Errorf("%s: no entries", file.name)
		}
		for key, value := range entries {
			if strings.TrimSpace(key) == "" {
				t.Errorf("%s: empty translation key", file.name)
			}
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: empty translation for key %q", file.name, key)
			}
		}
	}
}

// TestBundledCatalogsShareKeys guards the deterministic fallback: the fully
// translated languages (German, French, Spanish, Polish) must expose the same
// key set, and the English catalog must be a subset of each of them. A key that
// exists in only one language is almost always a typo (for example a key
// written in the target language instead of the Italian source string) and
// would otherwise silently never match the UI.
func TestBundledCatalogsShareKeys(t *testing.T) {
	catalogs := map[string]map[string]string{}
	for _, file := range bundledCatalogs() {
		_, entries := parseCatalog(t, file)
		catalogs[file.lang] = entries
	}

	full := []string{"deu", "fra", "spa", "pol"}
	reference := full[0]
	for _, lang := range full[1:] {
		for _, key := range keysOnlyIn(catalogs[reference], catalogs[lang]) {
			t.Errorf("catalog %q is missing key %q present in %q", lang, key, reference)
		}
		for _, key := range keysOnlyIn(catalogs[lang], catalogs[reference]) {
			t.Errorf("catalog %q has extra key %q missing from %q (typo?)", lang, key, reference)
		}
	}

	if english := catalogs["eng"]; english != nil {
		for _, lang := range full {
			for _, key := range keysOnlyIn(english, catalogs[lang]) {
				t.Errorf("catalog %q is missing English key %q", lang, key)
			}
			// English must cover every key of the full catalogs too, otherwise
			// the English UI silently falls back to Italian. This is what keeps
			// the gap closed.
			for _, key := range keysOnlyIn(catalogs[lang], english) {
				t.Errorf("English catalog is missing key %q present in %q", key, lang)
			}
		}
	}
}

// keysOnlyIn returns the keys present in a but absent from b, sorted for stable
// error output.
func keysOnlyIn(a, b map[string]string) []string {
	var only []string
	for key := range a {
		if _, ok := b[key]; !ok {
			only = append(only, key)
		}
	}
	sort.Strings(only)
	return only
}

// placeholderPattern matches one printf verb: the flags, the optional width and
// precision, then the conversion letter. A percent sign followed by a space is
// prose ("100% from"), not a verb, so it cannot match.
var placeholderPattern = regexp.MustCompile(`%(?:\[[0-9]+\])?[+#0\-]*[0-9]*(?:\.[0-9]+)?[vTtbcdoOqxXUeEfFgGsqp]`)

// placeholders returns how many times each verb appears in a string.
func placeholders(text string) map[string]int {
	counts := map[string]int{}
	for _, verb := range placeholderPattern.FindAllString(strings.ReplaceAll(text, "%%", ""), -1) {
		counts[verb]++
	}
	return counts
}

// TestCatalogTranslationsKeepThePlaceholders locks the rule that a translated
// message keeps the verbs of its Italian format string: uiTextf fills them with
// fmt.Sprintf, so a missing %d becomes "%!d(MISSING)" in the interface. Only
// entries whose key carries at least one verb are compared: a bare "100%" in
// prose is not a placeholder.
func TestCatalogTranslationsKeepThePlaceholders(t *testing.T) {
	checked := 0
	for _, file := range bundledCatalogs() {
		_, entries := parseCatalog(t, file)
		for key, value := range entries {
			want := placeholders(key)
			if len(want) == 0 {
				continue
			}
			checked++
			got := placeholders(value)
			if len(got) != len(want) {
				t.Errorf("%s: %q keeps %v, translation %q has %v", file.name, key, want, value, got)
				continue
			}
			for verb, n := range want {
				if got[verb] != n {
					t.Errorf("%s: %q keeps %d×%q, translation %q has %d", file.name, key, n, verb, value, got[verb])
				}
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d formatted entries checked: the guard is not reading the catalogs", checked)
	}
}
