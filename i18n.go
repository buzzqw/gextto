package gextto

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// defaultTranslations holds the bundled English translations for strings added
// after the first dictionary release. It is merged with `INSERT OR IGNORE`, so
// user edits always win.
//
//go:embed internal_translations.yml
var defaultTranslations string

//go:embed internal_translations_de.yml
var defaultGermanTranslations string

//go:embed internal_translations_fr.yml
var defaultFrenchTranslations string

// I18nDb is the persisted interface-translation database. It shares the
// `gextto_config.db` schema with the daemon.
type I18nDb struct {
	db *sql.DB
}

// Close closes the underlying SQLite connection (see Database.Close).
func (d *I18nDb) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// RowCount returns the number of rows stored in the translation database.
func (d *I18nDb) RowCount() int64 {
	if d == nil {
		return 0
	}
	return ConnectionRowCount(d.db)
}

// Translation is one persisted key/value pair for a language.
type Translation struct {
	Lang  string `json:"lang"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

// OpenI18nDb opens (creating if needed) the translation database at path.
func OpenI18nDb(path string) (*I18nDb, error) {
	db, err := OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS translations (lang TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(lang,key));"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create translations table: %w", err)
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create settings table: %w", err)
	}
	return &I18nDb{db: db}, nil
}

// Checkpoint truncates the WAL after a checkpoint. It mirrors
// the database checkpoint.
func (i *I18nDb) Checkpoint() error {
	_, err := i.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// QuickCheck runs `PRAGMA quick_check` and returns its rows (`["ok"]` when the
// database is healthy). It mirrors the database quick check.
func (i *I18nDb) QuickCheck() ([]string, error) {
	rows, err := i.db.Query("PRAGMA quick_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		result = append(result, line)
	}
	return result, rows.Err()
}

func storageLanguage(lang string) string {
	switch lang {
	case "it", "ita":
		return "ita"
	case "en", "eng":
		return "eng"
	case "de", "deu":
		return "deu"
	case "fr", "fra":
		return "fra"
	case "es", "spa":
		return "spa"
	default:
		return lang
	}
}

func publicLanguage(lang string) string {
	switch lang {
	case "ita":
		return "it"
	case "eng":
		return "en"
	case "deu":
		return "de"
	case "fra":
		return "fr"
	case "spa":
		return "es"
	default:
		return lang
	}
}

// Language returns the active UI language (public two-letter code).
func (i *I18nDb) Language() (string, error) {
	var value string
	if err := i.db.QueryRow("SELECT value FROM settings WHERE key='ui_language'").Scan(&value); err != nil {
		value = "ita"
	}
	return publicLanguage(value), nil
}

// SetLanguage persists the active UI language.
func (i *I18nDb) SetLanguage(lang string) error {
	_, err := i.db.Exec("INSERT INTO settings(key,value) VALUES ('ui_language',?1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", storageLanguage(lang))
	return err
}

// List returns every translation stored for a language, ordered by key.
func (i *I18nDb) List(lang string) ([]Translation, error) {
	rows, err := i.db.Query("SELECT lang,key,value FROM translations WHERE lang=?1 ORDER BY key", storageLanguage(lang))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Translation{}
	for rows.Next() {
		var storedLang, key, value string
		if err := rows.Scan(&storedLang, &key, &value); err != nil {
			return nil, err
		}
		result = append(result, Translation{Lang: publicLanguage(storedLang), Key: key, Value: value})
	}
	return result, rows.Err()
}

// Set stores (or replaces) a single translation.
func (i *I18nDb) Set(lang, key, value string) error {
	_, err := i.db.Exec("INSERT INTO translations(lang,key,value) VALUES (?1,?2,?3) ON CONFLICT(lang,key) DO UPDATE SET value=excluded.value", storageLanguage(lang), key, value)
	return err
}

// SetBulk stores (or replaces) many translations in a single transaction.
func (i *I18nDb) SetBulk(lang string, values map[string]string) error {
	storage := storageLanguage(lang)
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, err := tx.Exec("INSERT INTO translations(lang,key,value) VALUES (?1,?2,?3) ON CONFLICT(lang,key) DO UPDATE SET value=excluded.value", storage, key, value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SeedDefaultTranslations merges the bundled default translations into the
// database without overwriting existing entries. It returns how many rows were
// added.
func (i *I18nDb) SeedDefaultTranslations() (int, error) {
	// Older installations may still contain translations for the removed
	// aMule/eD2k integration. They are not part of the current UI and would
	// otherwise keep obsolete options visible in the translation editor.
	if _, err := i.db.Exec(`DELETE FROM translations
		WHERE lower(key) LIKE '%ed2k%'
		   OR lower(value) LIKE '%ed2k%'
		   OR lower(key) LIKE '%amule%'
		   OR lower(value) LIKE '%amule%'`); err != nil {
		return 0, fmt.Errorf("remove obsolete aMule/eD2k translations: %w", err)
	}
	inserted := 0
	for _, catalog := range []struct {
		name string
		raw  string
	}{
		{name: "default", raw: defaultTranslations},
		{name: "German", raw: defaultGermanTranslations},
		{name: "French", raw: defaultFrenchTranslations},
	} {
		var defaults map[string]map[string]string
		if err := yaml.Unmarshal([]byte(catalog.raw), &defaults); err != nil {
			return inserted, fmt.Errorf("parse %s translations: %w", catalog.name, err)
		}
		langs := make([]string, 0, len(defaults))
		for lang := range defaults {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		for _, lang := range langs {
			storage := storageLanguage(lang)
			entries := defaults[lang]
			keys := make([]string, 0, len(entries))
			for key := range entries {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			tx, err := i.db.Begin()
			if err != nil {
				return inserted, fmt.Errorf("begin %s translations: %w", catalog.name, err)
			}
			for _, key := range keys {
				res, err := tx.Exec("INSERT OR IGNORE INTO translations(lang,key,value) VALUES (?1,?2,?3)", storage, key, entries[key])
				if err != nil {
					_ = tx.Rollback()
					return inserted, err
				}
				affected, err := res.RowsAffected()
				if err != nil {
					_ = tx.Rollback()
					return inserted, err
				}
				inserted += int(affected)
			}
			if err := tx.Commit(); err != nil {
				return inserted, err
			}
		}
	}
	return inserted, nil
}

// DeleteLang removes every translation row for a language. It returns how many
// rows were deleted.
func (i *I18nDb) DeleteLang(lang string) (int, error) {
	res, err := i.db.Exec("DELETE FROM translations WHERE lang=?1", storageLanguage(lang))
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// I18n holds the in-memory key/value dictionary loaded from a YAML file.
type I18n struct {
	values map[string]string
}

// Load reads a flat key/value YAML translation file. A missing path yields an
// empty dictionary, matching the `Path::exists` guard.
func (i *I18n) Load(path string) (*I18n, error) {
	if _, err := os.Stat(path); err != nil {
		return &I18n{values: map[string]string{}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return &I18n{values: values}, nil
}

// T returns the translation for key, falling back to the key itself.
func (i *I18n) T(key string) string {
	if value, ok := i.values[key]; ok {
		return value
	}
	return key
}
