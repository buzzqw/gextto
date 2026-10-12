package gextto

import "testing"

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
