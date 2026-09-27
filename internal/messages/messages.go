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

// SetLanguage sets the active language from a public code (it, en, ita, eng, ...).
func SetLanguage(lang string) {
	normalized := "it"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		normalized = "en"
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
// otherwise the Italian one.
func Pick(italian, english string) string {
	if IsEnglish() {
		return english
	}
	return italian
}
