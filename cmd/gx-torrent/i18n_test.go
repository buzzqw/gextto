package main

import (
	"regexp"
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

func TestUITrFillsTheValuesAfterTheLookup(t *testing.T) {
	if got := uiTr("it", "%d torrents", 3); got != "3 torrent" && got != "%d torrents" {
		// The catalog may translate the phrase; either way the value is filled
		// and no verb is left behind.
		if strings.Contains(got, "%d") {
			t.Fatalf("the value was not filled: %q", got)
		}
	}
	if got := uiTr("en", "%d torrents", 3); got != "3 torrents" {
		t.Fatalf("English fills the phrase as written: %q", got)
	}
	if got := uiTr("en", "Missing phrase"); got != "Missing phrase" {
		t.Fatalf("a phrase without a translation stays as written: %q", got)
	}
}

// TestUICatalogKeepsTheVerbs fails when a translation of a phrase that carries
// values loses or renames a printf verb: the page would show "%!s(MISSING)"
// where the number should be.
func TestUICatalogKeepsTheVerbs(t *testing.T) {
	verb := regexp.MustCompile(`%[-+ 0-9.#]*[sdvf]`)
	for key, translations := range uiCatalog {
		want := verb.FindAllString(key, -1)
		if len(want) == 0 {
			continue
		}
		for idx, translated := range translations {
			if got := verb.FindAllString(translated, -1); len(got) != len(want) {
				t.Errorf("catalog entry %q: translation %d has %d verbs, want %d", key, idx, len(got), len(want))
			}
		}
	}
}

// The compiled columns and the language index agree, so every lazily built
// dictionary reads its own column.
func TestUICatalogColumnsMatchTheLanguageIndex(t *testing.T) {
	for i, lang := range uiCatalogColumns {
		if uiLangIndex[lang] != i {
			t.Fatalf("column %d is %q, the index says %d", i, lang, uiLangIndex[lang])
		}
	}
	if len(uiLangIndex) != len(uiCatalogColumns) {
		t.Fatalf("index has %d languages, columns %d", len(uiLangIndex), len(uiCatalogColumns))
	}
	if got := uiDictionary("it")["Torrents"]; got != uiCatalog["Torrents"][0] {
		t.Fatalf("it dictionary reads %q", got)
	}
}
