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
	normalized := "it"
	code := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(code, "en") {
		normalized = "en"
	} else if strings.HasPrefix(code, "de") {
		normalized = "de"
	} else if strings.HasPrefix(code, "fr") {
		normalized = "fr"
	} else if code == "es" || code == "spa" {
		normalized = "es"
	} else if code == "pl" || code == "pol" {
		normalized = "pl"
	}
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

// Pick returns the English variant when the interface language is English,
// German, French, Spanish or Polish. Non-English backend messages currently use
// English as a fallback because these notifications have no separate catalogs.
func Pick(italian, english string) string {
	if IsEnglish() || Language() == "de" || Language() == "fr" || Language() == "es" || Language() == "pl" {
		return english
	}
	return italian
}
