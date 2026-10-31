// Package messages is the active-language helper for non-UI messages
// (notifications and the human-readable log summaries). The UI itself is
// translated in the frontend; this keeps backend messages aligned with the
// chosen interface language.
package messages

import (
	"strings"
	"sync"
)

var (
	mu       sync.RWMutex
	language = "it"
)

// SetLanguage sets the active language from a public code (it, en, de, fr, es,
// pl, ita, eng, ...).
func SetLanguage(lang string) {
	normalized := Normalize(lang)
	mu.Lock()
	language = normalized
	mu.Unlock()
}

// Language returns the active language code.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return language
}

// IsEnglish reports whether the active language is English.
func IsEnglish() bool {
	return Language() == "en"
}

// Pick returns the message in the active interface language. Italian and
// English come from the arguments; German, French, Spanish and Polish come from
// catalogIT keyed by the Italian source, with English as the fallback.
func Pick(italian, english string) string {
	return PickLang(Language(), italian, english)
}

// PickLang is Pick in an explicit language, without changing the active one.
// Notifications may be sent in a language of their own (the «message language»
// option), different from the interface the daemon is showing.
func PickLang(lang, italian, english string) string {
	switch Normalize(lang) {
	case "en":
		return english
	case "de":
		return pickCatalog(italian, english, 0)
	case "fr":
		return pickCatalog(italian, english, 1)
	case "es":
		return pickCatalog(italian, english, 2)
	case "pl":
		return pickCatalog(italian, english, 3)
	default:
		return italian
	}
}

// Normalize maps a public language code (it, en, de, fr, es, pl, ita, eng, …)
// to the code the catalogs use, with Italian as the fallback.
func Normalize(lang string) string {
	code := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(code, "en"):
		return "en"
	case strings.HasPrefix(code, "de"):
		return "de"
	case strings.HasPrefix(code, "fr"):
		return "fr"
	case code == "es" || code == "spa":
		return "es"
	case code == "pl" || code == "pol":
		return "pl"
	default:
		return "it"
	}
}

// pickCatalog returns the indexed translation for the Italian source, or the
// English fallback when the string is not in the catalog.
func pickCatalog(italian, english string, index int) string {
	if entry, ok := catalogIT[italian]; ok {
		if translated := entry[index]; translated != "" {
			return translated
		}
	}
	return english
}
