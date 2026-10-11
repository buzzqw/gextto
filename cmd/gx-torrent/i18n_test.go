package main

import (
	"strings"
	"testing"
)

func TestUIDictionaryLanguages(t *testing.T) {
	for _, lang := range []string{"it", "de", "fr", "es", "pl"} {
		if uiDictionary(lang) == nil {
			t.Fatalf("missing dictionary for %q", lang)
		}
	}
	if uiDictionary("en") != nil {
		t.Fatal("English needs no dictionary")
	}
	if uiDictionary("xx") != nil {
		t.Fatal("unknown language must return nil")
	}
}

// TestUICatalogTranslatedInEveryLanguage enforces the rule that every UI field
// is translated in all available languages: each uiCatalog entry must carry a
// non-empty translation in every column (it, de, fr, es, pl), so a new field
// cannot ship translated in Italian only.
func TestUICatalogTranslatedInEveryLanguage(t *testing.T) {
	langs := []string{"it", "de", "fr", "es", "pl"}
	for key, value := range uiCatalog {
		for idx, lang := range langs {
			if strings.TrimSpace(value[idx]) == "" {
				t.Errorf("catalog entry %q has no %s translation", key, lang)
			}
		}
	}
}

func TestUITranslateHTML(t *testing.T) {
	dict := uiDictionary("it")
	in := `<button title="Test ports" onclick="f()">Test ports</button><script>var x = "Test ports";</script>`
	out := uiTranslateHTML(in, dict)
	if !strings.Contains(out, `>Test porte</button>`) {
		t.Fatalf("text node not translated: %s", out)
	}
	if !strings.Contains(out, `title="Test porte"`) {
		t.Fatalf("title attribute not translated: %s", out)
	}
	if !strings.Contains(out, `var x = "Test ports";`) {
		t.Fatalf("script content must stay untouched: %s", out)
	}
}

func TestUITranslateHTMLNoDictionary(t *testing.T) {
	in := `<p>Test ports</p>`
	if got := uiTranslateHTML(in, nil); got != in {
		t.Fatalf("nil dictionary changed the HTML: %s", got)
	}
}
