package main

// lang_packs.go loads optional interface languages from the data folder:
// <data>/lang/<anything>.json. A pack is a JSON object with a code, a display
// name and a map from the English catalog key to the translation. Missing keys
// fall back to English, so a partial pack is still usable.
//
// The six bundled catalogs (it, de, fr, es, pl) stay compiled in; a pack is the
// way to add a language without rebuilding the daemon.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// languagePack is the JSON shape of a pack file.
type languagePack struct {
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Translations map[string]string `json:"translations"`
}

// uiPackLangs holds the codes of the languages loaded from packs.
var uiPackLangs = map[string]bool{}

// loadLanguagePacks reads every *.json in dir and registers the valid packs.
// A malformed file or a code that collides with a bundled language is reported
// and skipped, never fatal: a broken pack must not keep the daemon from
// starting.
func loadLanguagePacks(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			logfError("cannot read the language packs in %s: %v", dir, err)
		}
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fi, err := entry.Info()
		if err != nil {
			logfError("language pack %s ignored: %v", entry.Name(), err)
			continue
		}
		if fi.Size() > 8<<20 {
			logfError("language pack %s ignored: the file is larger than 8 MiB", entry.Name())
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			logfError("language pack %s ignored: %v", entry.Name(), err)
			continue
		}
		var pack languagePack
		if err := json.Unmarshal(data, &pack); err != nil {
			logfError("language pack %s ignored: %v", entry.Name(), err)
			continue
		}
		code := normalizeLanguageCode(pack.Code)
		if code == "" {
			logfError("language pack %s ignored: the code must be 2-12 letters, digits, '-' or '_'", entry.Name())
			continue
		}
		if code == "en" {
			logfError("language pack %s ignored: English is built in", entry.Name())
			continue
		}
		if _, bundled := uiLangIndex[code]; bundled {
			logfError("language pack %s ignored: the language %s is built in", entry.Name(), code)
			continue
		}
		if uiPackLangs[code] {
			logfError("language pack %s ignored: the language %s is already loaded", entry.Name(), code)
			continue
		}
		if strings.TrimSpace(pack.Name) == "" {
			logfError("language pack %s ignored: the name is empty", entry.Name())
			continue
		}
		if len(pack.Translations) == 0 {
			logfError("language pack %s ignored: it has no translations", entry.Name())
			continue
		}
		uiDicts[code] = pack.Translations
		uiPackLangs[code] = true
		uiLanguageNames = append(uiLanguageNames, struct{ Code, Name string }{Code: code, Name: strings.TrimSpace(pack.Name)})
		logf("language pack %s (%s) loaded: %d strings", strings.TrimSpace(pack.Name), code, len(pack.Translations))
	}
}

// normalizeLanguageCode keeps a short, lower-case code that can be stored in
// settings.json and used in ?lang=.
func normalizeLanguageCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) < 2 || len(code) > 12 {
		return ""
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return code
}

// uiLangKnown reports whether code is English, a bundled language or a loaded
// pack.
func uiLangKnown(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "en" {
		return true
	}
	if _, ok := uiLangIndex[code]; ok {
		return true
	}
	return uiPackLangs[code]
}
