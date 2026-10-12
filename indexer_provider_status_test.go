package gextto

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestHealthyIndexerStatusesAlwaysListed: the provider list must show every
// configured indexer, not only the failing ones, so a healthy Jackett is not
// invisible. A name that already has a failure or live row is left to it.
func TestHealthyIndexerStatusesAlwaysListed(t *testing.T) {
	cfg := &Config{Indexers: []IndexerConfig{
		{Name: "Jackett", URL: "http://host:9117", Enabled: true, Manager: ManagerJackett},
		{Name: "disabled-one", URL: "http://host:9118", Enabled: false},
	}}
	known := map[string]struct{}{"prowlarr": {}}
	rows := gh5_healthyIndexerStatuses(cfg, known)
	if len(rows) != 1 || rows[0].Provider != "Jackett" || rows[0].Kind != "indexer" {
		t.Fatalf("rows = %+v, want one Jackett indexer row", rows)
	}
	if rows[0].UserMessage == "" {
		t.Fatalf("healthy row without a message: %+v", rows[0])
	}
	if _, ok := known["jackett"]; !ok {
		t.Fatal("the added name must be recorded so it is not duplicated")
	}

	// A name already present (a Prowlarr manager, a saved failure, ...) is not
	// duplicated.
	known = map[string]struct{}{"jackett": {}}
	if rows := gh5_healthyIndexerStatuses(cfg, known); len(rows) != 0 {
		t.Fatalf("known name duplicated: %+v", rows)
	}
}

// TestProviderStatusMessagesAreTranslated guards the "Stato provider" table:
// every fixed message and suggested action of web_handlers_g5.go is a catalog
// key in every language, so the rows never stay in Italian in another
// interface language (the table cells are translated by exact match).
func TestProviderStatusMessagesAreTranslated(t *testing.T) {
	source, err := os.ReadFile("web_handlers_g5.go")
	if err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`(?:UserMessage|SuggestedAction)\s*[:=]\s*"([^"]+)"|return "([^"]+\.)", "([^"]+\.)"`)
	var messages []string
	for _, m := range literal.FindAllStringSubmatch(string(source), -1) {
		for _, group := range m[1:] {
			if group != "" {
				messages = append(messages, group)
			}
		}
	}
	if len(messages) < 20 {
		t.Fatalf("found only %d provider messages: the pattern no longer matches the source", len(messages))
	}
	for _, file := range bundledCatalogs() {
		_, entries := parseCatalog(t, file)
		for _, message := range messages {
			if strings.TrimSpace(entries[message]) == "" {
				t.Errorf("%s: provider message %q has no translation", file.name, message)
			}
		}
	}
}
