package gextto

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

	second, err := db.SeedDefaultTranslations()
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second != 0 {
		t.Errorf("second seed inserted %d rows, want 0 (idempotent)", second)
	}

	// After deleting the language the same defaults can be reseeded. The third
	// seed also restores the bundled value the user edit had shadowed, so it
	// inserts exactly one more row than the first seed.
	if _, err := db.DeleteLang("eng"); err != nil {
		t.Fatalf("DeleteLang(eng): %v", err)
	}
	third, err := db.SeedDefaultTranslations()
	if err != nil {
		t.Fatalf("third seed: %v", err)
	}
	if third != first+1 {
		t.Errorf("third seed inserted %d rows, want %d", third, first+1)
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
