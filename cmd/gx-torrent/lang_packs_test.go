package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// removeTestLanguagePack drops a pack from the globals so one test cannot leak
// into another.
func removeTestLanguagePack(code string) {
	delete(uiDicts, code)
	delete(uiPackLangs, code)
	out := uiLanguageNames[:0]
	for _, item := range uiLanguageNames {
		if item.Code != code {
			out = append(out, item)
		}
	}
	uiLanguageNames = out
}

// TestLanguagePackLoads proves a JSON pack adds a selectable language and that
// a missing key falls back to English.
func TestLanguagePackLoads(t *testing.T) {
	const code = "pt-br"
	defer removeTestLanguagePack(code)
	dir := t.TempDir()
	pack := `{"code":"pt-br","name":"Português (Brasil)","translations":{"Torrents":"Torrentes","Settings":"Configurações"}}`
	if err := os.WriteFile(filepath.Join(dir, "pt-br.json"), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	loadLanguagePacks(dir)
	if !uiLangKnown(code) {
		t.Fatal("the pack language is not selectable")
	}
	if !validStandaloneLang(code) {
		t.Fatal("the wizard must accept the pack language")
	}
	if got := uiTr(code, "Torrents"); got != "Torrentes" {
		t.Fatalf("Torrents = %q", got)
	}
	if got := uiTr(code, "A key the pack does not have"); got != "A key the pack does not have" {
		t.Fatalf("missing key = %q, want the English key", got)
	}
	found := false
	for _, item := range uiLanguageNames {
		if item.Code == code && item.Name == "Português (Brasil)" {
			found = true
		}
	}
	if !found {
		t.Fatal("the pack is missing from the language picker")
	}
}

// TestLanguagePackRejectsBuiltInAndMalformed keeps a bad file from changing the
// picker or shadowing a compiled language.
func TestLanguagePackRejectsBuiltInAndMalformed(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"it.json":    `{"code":"it","name":"Italiano","translations":{"Torrents":"x"}}`,
		"bad.json":   `{"code":"bad code!","name":"Bad","translations":{"Torrents":"x"}}`,
		"empty.json": `{"code":"zz","name":"Empty","translations":{}}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loadLanguagePacks(dir)
	if uiLangKnown("bad code!") || uiLangKnown("zz") {
		t.Fatal("invalid packs must not be loaded")
	}
	if !strings.Contains(uiTr("it", "Torrents"), "Torrent") {
		t.Fatal("the built-in Italian catalog must not be replaced")
	}
}
