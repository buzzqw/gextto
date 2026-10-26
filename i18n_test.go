package gextto

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// openTestI18nDb creates a fresh translation database in a temp directory and
// closes it when the test ends.
func openTestI18nDb(t *testing.T) *I18nDb {
	t.Helper()
	db, err := OpenI18nDb(filepath.Join(t.TempDir(), "gextto_config.db"))
	if err != nil {
		t.Fatalf("open i18n db: %v", err)
	}
	t.Cleanup(func() { _ = db.db.Close() })
	return db
}

func TestI18nQuickCheckAndCheckpoint(t *testing.T) {
	db := openTestI18nDb(t)

	rows, err := db.QuickCheck()
	if err != nil {
		t.Fatalf("quick check: %v", err)
	}
	if !reflect.DeepEqual(rows, []string{"ok"}) {
		t.Fatalf("quick check rows = %v, want [ok]", rows)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
}

func TestSeedDefaultTranslationsRemovesObsoleteEd2kEntries(t *testing.T) {
	db := openTestI18nDb(t)
	if err := db.SetBulk("it", map[string]string{
		"Gap Filling — Fallback eD2k (cerca su eD2k se Jackett non trova nulla)": "legacy",
		"aMule / ed2k":     "legacy",
		"Ricerca eD2k/Kad": "keep-ed2k",
		"current.key":      "keep",
	}); err != nil {
		t.Fatalf("seed legacy translations: %v", err)
	}
	if _, err := db.SeedDefaultTranslations(); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM translations WHERE key IN ('aMule / ed2k', 'Gap Filling — Fallback eD2k (cerca su eD2k se Jackett non trova nulla)')").Scan(&count); err != nil {
		t.Fatalf("count obsolete translations: %v", err)
	}
	if count != 0 {
		t.Fatalf("obsolete translation rows remaining: %d", count)
	}
	// The strings of the current aMule integration are bundled and must stay.
	if err := db.db.QueryRow("SELECT COUNT(*) FROM translations WHERE key = 'Scarica con aMule'").Scan(&count); err != nil || count == 0 {
		t.Fatalf("current aMule translations removed: %d, %v", count, err)
	}
	var value string
	if err := db.db.QueryRow("SELECT value FROM translations WHERE lang='ita' AND key='current.key'").Scan(&value); err != nil {
		t.Fatalf("read current translation: %v", err)
	}
	if value != "keep" {
		t.Fatalf("current translation = %q, want keep", value)
	}
	// The native eD2k/Kad provider is current UI: its rows must survive the
	// cleanup that removes the obsolete aMule integration.
	if err := db.db.QueryRow("SELECT value FROM translations WHERE lang='ita' AND key='Ricerca eD2k/Kad'").Scan(&value); err != nil {
		t.Fatalf("eD2k translation was removed: %v", err)
	}
	if value != "keep-ed2k" {
		t.Fatalf("eD2k translation = %q, want keep-ed2k", value)
	}
}

func TestI18nLanguageRoundTripsAliases(t *testing.T) {
	db := openTestI18nDb(t)

	// A fresh database defaults to Italian (stored as "ita").
	defaultLang, err := db.Language()
	if err != nil {
		t.Fatalf("language: %v", err)
	}
	if defaultLang != "it" {
		t.Fatalf("default language = %q, want it", defaultLang)
	}

	cases := []struct {
		input      string
		wantPublic string
		wantStored string
	}{
		{"it", "it", "ita"},
		{"ita", "it", "ita"},
		{"en", "en", "eng"},
		{"eng", "en", "eng"},
		{"de", "de", "deu"},
		{"deu", "de", "deu"},
		{"fr", "fr", "fra"},
		{"fra", "fr", "fra"},
		{"es", "es", "spa"},
		{"spa", "es", "spa"},
		{"pl", "pl", "pol"},
		{"pol", "pl", "pol"},
		// Unknown codes pass through unchanged in both directions.
		{"pt", "pt", "pt"},
	}
	for _, testCase := range cases {
		if err := db.SetLanguage(testCase.input); err != nil {
			t.Fatalf("SetLanguage(%q): %v", testCase.input, err)
		}
		got, err := db.Language()
		if err != nil {
			t.Fatalf("Language after %q: %v", testCase.input, err)
		}
		if got != testCase.wantPublic {
			t.Errorf("Language after SetLanguage(%q) = %q, want %q", testCase.input, got, testCase.wantPublic)
		}
		var stored string
		if err := db.db.QueryRow("SELECT value FROM settings WHERE key='ui_language'").Scan(&stored); err != nil {
			t.Fatalf("read stored language: %v", err)
		}
		if stored != testCase.wantStored {
			t.Errorf("stored language for %q = %q, want %q", testCase.input, stored, testCase.wantStored)
		}
	}
}

func TestI18nSetSetBulkListDelete(t *testing.T) {
	db := openTestI18nDb(t)

	if err := db.Set("en", "b", "B"); err != nil {
		t.Fatalf("Set b: %v", err)
	}
	if err := db.Set("en", "a", "A"); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	// A second Set replaces the value.
	if err := db.Set("en", "a", "A2"); err != nil {
		t.Fatalf("Set a (update): %v", err)
	}
	if err := db.SetBulk("it", map[string]string{"y": "Y", "x": "X"}); err != nil {
		t.Fatalf("SetBulk: %v", err)
	}

	wantEnglish := []Translation{
		{Lang: "en", Key: "a", Value: "A2"},
		{Lang: "en", Key: "b", Value: "B"},
	}
	got, err := db.List("en")
	if err != nil {
		t.Fatalf("List(en): %v", err)
	}
	if !reflect.DeepEqual(got, wantEnglish) {
		t.Errorf("List(en) = %+v, want %+v", got, wantEnglish)
	}
	// The long alias resolves to the same language.
	gotAlias, err := db.List("eng")
	if err != nil {
		t.Fatalf("List(eng): %v", err)
	}
	if !reflect.DeepEqual(gotAlias, wantEnglish) {
		t.Errorf("List(eng) = %+v, want %+v", gotAlias, wantEnglish)
	}

	wantItalian := []Translation{
		{Lang: "it", Key: "x", Value: "X"},
		{Lang: "it", Key: "y", Value: "Y"},
	}
	gotItalian, err := db.List("it")
	if err != nil {
		t.Fatalf("List(it): %v", err)
	}
	if !reflect.DeepEqual(gotItalian, wantItalian) {
		t.Errorf("List(it) = %+v, want %+v", gotItalian, wantItalian)
	}

	// The values are persisted under the canonical storage code.
	var storedLang string
	if err := db.db.QueryRow("SELECT lang FROM translations WHERE key='x'").Scan(&storedLang); err != nil {
		t.Fatalf("read stored lang: %v", err)
	}
	if storedLang != "ita" {
		t.Errorf("stored lang = %q, want ita", storedLang)
	}

	deleted, err := db.DeleteLang("ita")
	if err != nil {
		t.Fatalf("DeleteLang(ita): %v", err)
	}
	if deleted != 2 {
		t.Errorf("DeleteLang(ita) = %d, want 2", deleted)
	}
	remaining, err := db.List("it")
	if err != nil {
		t.Fatalf("List(it) after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("List(it) after delete = %+v, want empty", remaining)
	}
	deletedAgain, err := db.DeleteLang("it")
	if err != nil {
		t.Fatalf("DeleteLang(it) again: %v", err)
	}
	if deletedAgain != 0 {
		t.Errorf("second DeleteLang = %d, want 0", deletedAgain)
	}
	stillEnglish, err := db.List("en")
	if err != nil {
		t.Fatalf("List(en) after it delete: %v", err)
	}
	if len(stillEnglish) != 2 {
		t.Errorf("List(en) after it delete = %d rows, want 2", len(stillEnglish))
	}
}

func TestI18nSeedDefaultTranslationsIsIdempotentAndPreservesEdits(t *testing.T) {
	db := openTestI18nDb(t)

	// A user edit that collides with a bundled key must survive the merge.
	if err := db.Set("eng", "Consenti aggiornamenti", "MY VALUE"); err != nil {
		t.Fatalf("Set user edit: %v", err)
	}
	first, err := db.SeedDefaultTranslations()
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if first <= 0 {
		t.Fatalf("first seed inserted %d rows, want > 0", first)
	}

	find := func(key string) (string, bool) {
		list, err := db.List("en")
		if err != nil {
			t.Fatalf("List(en): %v", err)
		}
		for _, entry := range list {
			if entry.Key == key {
				return entry.Value, true
			}
		}
		return "", false
	}
	if value, ok := find("Consenti aggiornamenti"); !ok || value != "MY VALUE" {
		t.Errorf("user edit = %q (present=%v), want MY VALUE", value, ok)
	}
	if value, ok := find("Cartelle osservate"); !ok || value != "Watched folders" {
		t.Errorf("bundled translation = %q (present=%v), want Watched folders", value, ok)
	}
	findGerman := func(key string) (string, bool) {
		list, err := db.List("de")
		if err != nil {
			t.Fatalf("List(de): %v", err)
		}
		for _, entry := range list {
			if entry.Key == key {
				return entry.Value, true
			}
		}
		return "", false
	}
	if value, ok := findGerman("Configurazione"); !ok || value != "Konfiguration" {
		t.Errorf("German bundled translation = %q (present=%v), want Konfiguration", value, ok)
	}
	findFrench := func(key string) (string, bool) {
		list, err := db.List("fr")
		if err != nil {
			t.Fatalf("List(fr): %v", err)
		}
		for _, entry := range list {
			if entry.Key == key {
				return entry.Value, true
			}
		}
		return "", false
	}
	if value, ok := findFrench("Configurazione"); !ok || value != "Configuration" {
		t.Errorf("French bundled translation = %q (present=%v), want Configuration", value, ok)
	}
	findLanguage := func(lang, key string) (string, bool) {
		list, err := db.List(lang)
		if err != nil {
			t.Fatalf("List(%s): %v", lang, err)
		}
		for _, entry := range list {
			if entry.Key == key {
				return entry.Value, true
			}
		}
		return "", false
	}
	if value, ok := findLanguage("es", "Configurazione"); !ok || value != "Configuración" {
		t.Errorf("Spanish bundled translation = %q (present=%v), want Configuración", value, ok)
	}
	if value, ok := findLanguage("pl", "Configurazione"); !ok || value != "Konfiguracja" {
		t.Errorf("Polish bundled translation = %q (present=%v), want Konfiguracja", value, ok)
	}
	deItems, err := db.List("de")
	if err != nil {
		t.Fatalf("List(de): %v", err)
	}
	frItems, err := db.List("fr")
	if err != nil {
		t.Fatalf("List(fr): %v", err)
	}
	esItems, err := db.List("es")
	if err != nil {
		t.Fatalf("List(es): %v", err)
	}
	plItems, err := db.List("pl")
	if err != nil {
		t.Fatalf("List(pl): %v", err)
	}
	localizedCount := len(deItems) + len(frItems) + len(esItems) + len(plItems)

	second, err := db.SeedDefaultTranslations()
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second != 0 {
		t.Errorf("second seed inserted %d rows, want 0 (idempotent)", second)
	}

	// After deleting English, the same English defaults can be reseeded. The
	// localized catalogs are still present, so the third seed inserts the
	// original English portion plus one row for the edited key.
	if _, err := db.DeleteLang("eng"); err != nil {
		t.Fatalf("DeleteLang(eng): %v", err)
	}
	third, err := db.SeedDefaultTranslations()
	if err != nil {
		t.Fatalf("third seed: %v", err)
	}
	wantThird := first - localizedCount + 1
	if third != wantThird {
		t.Errorf("third seed inserted %d rows, want %d", third, wantThird)
	}
	if value, ok := find("Consenti aggiornamenti"); !ok || value == "MY VALUE" {
		t.Errorf("reseeded translation = %q (present=%v), want the bundled value", value, ok)
	}
}

func TestI18nLoadAndTFallsBackToKey(t *testing.T) {
	dir := t.TempDir()

	// A missing file yields an empty dictionary and the key as fallback.
	empty, err := (&I18n{}).Load(filepath.Join(dir, "missing.yml"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if empty == nil {
		t.Fatal("Load missing returned nil")
	}
	if len(empty.values) != 0 {
		t.Errorf("Load missing values = %v, want empty", empty.values)
	}
	if got := empty.T("hello"); got != "hello" {
		t.Errorf("T(hello) = %q, want hello", got)
	}

	path := filepath.Join(dir, "it.yml")
	if err := os.WriteFile(path, []byte("hello: ciao\nnested.key: valore\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	loaded, err := (&I18n{}).Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded.T("hello"); got != "ciao" {
		t.Errorf("T(hello) = %q, want ciao", got)
	}
	if got := loaded.T("nested.key"); got != "valore" {
		t.Errorf("T(nested.key) = %q, want valore", got)
	}
	if got := loaded.T("missing"); got != "missing" {
		t.Errorf("T(missing) = %q, want missing", got)
	}
}

// TestDictionaryCacheAndInvalidate checks the renderer dictionary is built once
// per language and rebuilt after a translation changes.
func TestDictionaryCacheAndInvalidate(t *testing.T) {
	db := openTestI18nDb(t)
	defer db.Close()
	if err := db.Set("de", "Salva", "Speichern"); err != nil {
		t.Fatal(err)
	}
	first, err := db.Dictionary("de")
	if err != nil {
		t.Fatal(err)
	}
	if first["Salva"] != "Speichern" {
		t.Fatalf("dictionary value = %q", first["Salva"])
	}
	// The same map is returned (cached) across calls: compare identity, not
	// just the length, so a silent rebuild on every call fails the test.
	second, err := db.Dictionary("de")
	if err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(first).Pointer() != reflect.ValueOf(second).Pointer() {
		t.Fatal("cache not reused: Dictionary returned a different map")
	}
	// A write invalidates the cache: a new map reflects the change.
	if err := db.Set("de", "Salva", "Sichern"); err != nil {
		t.Fatal(err)
	}
	after, err := db.Dictionary("de")
	if err != nil {
		t.Fatal(err)
	}
	if after["Salva"] != "Sichern" {
		t.Fatalf("cache not invalidated: %q", after["Salva"])
	}
}

// TestSeedAppliesTranslationFixesOnlyToUntouchedRows covers the correction of a
// bundled translation on a database seeded before the fix: the old bundled
// value is replaced, a value edited by the user is kept.
func TestSeedAppliesTranslationFixesOnlyToUntouchedRows(t *testing.T) {
	db, err := OpenI18nDb(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fixes map[string]map[string][2]string
	if err := yaml.Unmarshal([]byte(translationFixes), &fixes); err != nil {
		t.Fatal(err)
	}
	// Both keys come from the same language: picking them across languages
	// (map order) could pick the same key twice and overwrite the fixed row.
	var lang, fixedKey, keptKey string
	var fixedValues [2]string
	for l, entries := range fixes {
		if len(entries) < 2 {
			continue
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lang, fixedKey, fixedValues, keptKey = l, keys[0], entries[keys[0]], keys[1]
		break
	}
	if keptKey == "" {
		t.Skip("fewer than two fixes in one language")
	}
	if err := db.Set(lang, fixedKey, fixedValues[0]); err != nil {
		t.Fatal(err)
	}
	if err := db.Set(lang, keptKey, "valore scelto dall'utente"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SeedDefaultTranslations(); err != nil {
		t.Fatal(err)
	}
	dict, err := db.Dictionary(lang)
	if err != nil {
		t.Fatal(err)
	}
	if got := dict[fixedKey]; got != fixedValues[1] {
		t.Fatalf("old bundled value not corrected: %q, want %q", got, fixedValues[1])
	}
	if got := dict[keptKey]; got != "valore scelto dall'utente" {
		t.Fatalf("user translation overwritten: %q", got)
	}
}

// TestSeedDefaultTranslationsSkipsUnchangedCatalogs pins the restart fast path:
// the same catalogs are not merged again, a new catalog version is.
func TestSeedDefaultTranslationsSkipsUnchangedCatalogs(t *testing.T) {
	db := openTestI18nDb(t)
	if _, err := db.SeedDefaultTranslations(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("DELETE FROM translations WHERE lang = 'eng' AND key = 'Cartelle osservate'"); err != nil {
		t.Fatal(err)
	}
	if added, err := db.SeedDefaultTranslations(); err != nil || added != 0 {
		t.Fatalf("seed with unchanged catalogs = %d, %v; want 0 rows, no merge", added, err)
	}
	if _, err := db.db.Exec("UPDATE translation_seed SET digest = 'older catalogs'"); err != nil {
		t.Fatal(err)
	}
	if added, err := db.SeedDefaultTranslations(); err != nil || added != 1 {
		t.Fatalf("seed after a catalog change = %d, %v; want the missing row back", added, err)
	}
	dict, err := db.Dictionary("en")
	if err != nil {
		t.Fatal(err)
	}
	if got := dict["Cartelle osservate"]; got != "Watched folders" {
		t.Fatalf("merged translation = %q, want Watched folders", got)
	}
}

// BenchmarkSeedDefaultTranslationsReseed measures the seeding a restart pays on
// a database that already holds the bundled catalogs.
func BenchmarkSeedDefaultTranslationsReseed(b *testing.B) {
	db, err := OpenI18nDb(filepath.Join(b.TempDir(), "gextto_config.db"))
	if err != nil {
		b.Fatalf("open i18n db: %v", err)
	}
	b.Cleanup(func() { _ = db.db.Close() })
	if _, err := db.SeedDefaultTranslations(); err != nil {
		b.Fatalf("first seed: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.SeedDefaultTranslations(); err != nil {
			b.Fatalf("reseed: %v", err)
		}
	}
}
