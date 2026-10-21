package gextto

import (
	"fmt"
	"strings"
	"testing"
)

// TestUIMergeReuseRow: the Swarm Merging row appears only when something was
// reused, and reports the pieces and the bytes.
func TestUIMergeReuseRow(t *testing.T) {
	if _, ok := uiMergeReuseRow(map[string]any{}); ok {
		t.Fatal("no merged pieces must hide the row")
	}
	if _, ok := uiMergeReuseRow(map[string]any{"merged_pieces": int64(0)}); ok {
		t.Fatal("zero merged pieces must hide the row")
	}
	parts, ok := uiMergeReuseRow(map[string]any{"merged_pieces": int64(3), "merged_bytes": int64(2048)})
	row := strings.Join(parts, "")
	if !ok {
		t.Fatal("merged pieces must show the row")
	}
	if !strings.Contains(row, "3 pezzi") || !strings.Contains(row, "2.0 KB") {
		t.Fatalf("row = %q, want the piece count and the bytes", row)
	}
	withPartialParts, _ := uiMergeReuseRow(map[string]any{
		"merged_pieces": int64(3), "merged_bytes": int64(2048), "merged_partial_pieces": int64(2),
	})
	withPartial := strings.Join(withPartialParts, "")
	if !strings.Contains(withPartial, "2 pezzi riusati a blocchi") {
		t.Fatalf("row = %q, want the block-level reuse count", withPartial)
	}
	if _, ok := uiMergeReuseRow(map[string]any{"merged_bytes": int64(4096), "merged_partial_pieces": int64(1)}); !ok {
		t.Fatal("block-level reuse alone must show the row")
	}
}

// TestUIUTPSocketRow: the socket counters appear only with socket activity;
// dial errors warn only when nothing was ever accepted (UDP likely blocked).
func TestUIUTPSocketRow(t *testing.T) {
	if _, ok, _ := uiUTPSocketRow(map[string]any{}); ok {
		t.Fatal("no socket activity must hide the row")
	}
	rowParts, ok, warn := uiUTPSocketRow(map[string]any{"utp_dials": int64(10), "utp_accepts": int64(3)})
	row := strings.Join(rowParts, "")
	if !ok || warn {
		t.Fatalf("healthy socket must show without warn, got %q warn=%v", row, warn)
	}
	if !strings.Contains(row, "10 dial") || !strings.Contains(row, "3 accept") {
		t.Fatalf("row = %q, want dial/accept counts", row)
	}
	withErrsParts, _, warn := uiUTPSocketRow(map[string]any{
		"utp_dials": int64(10), "utp_accepts": int64(3), "utp_dial_errors": int64(2),
	})
	withErrs := strings.Join(withErrsParts, "")
	if warn {
		t.Fatalf("errors with flowing accepts must not warn: %q", withErrs)
	}
	if !strings.Contains(withErrs, "errori 2 dial") {
		t.Fatalf("row = %q, want the error counts", withErrs)
	}
	blocked, _, warn := uiUTPSocketRow(map[string]any{"utp_dials": int64(10), "utp_dial_errors": int64(10)})
	if !warn {
		t.Fatalf("dial errors without accepts must warn: %q", blocked)
	}
	withResendsParts, _, _ := uiUTPSocketRow(map[string]any{
		"utp_dials": int64(10), "utp_accepts": int64(3),
		"utp_resends_timeout": int64(5), "utp_resends_fast": int64(2), "utp_syn_drops": int64(4),
	})
	withResends := strings.Join(withResendsParts, "")
	if !strings.Contains(withResends, "resend RTO 5 / fast 2") || !strings.Contains(withResends, "SYN scartati 4") {
		t.Fatalf("row = %q, want the resend and SYN-drop counts", withResends)
	}
	quietParts, _, _ := uiUTPSocketRow(map[string]any{"utp_dials": int64(10), "utp_accepts": int64(3)})
	quiet := strings.Join(quietParts, "")
	if strings.Contains(quiet, "resend") || strings.Contains(quiet, "SYN") {
		t.Fatalf("row = %q, must hide the counters when zero", quiet)
	}
}

// fakeStatsEngine feeds a canned /api/v1/stats map to the engine panel; the
// embedded nil interface panics on any method the panel does not use.
type fakeStatsEngine struct {
	TorrentEngine
	stats map[string]any
}

func (fakeStatsEngine) Name() string { return BackendGxTorrent }

func (f fakeStatsEngine) Stats() map[string]any { return f.stats }

// TestUIGxEngineStatsAllTimeTotals: the panel shows the all-time totals when
// the daemon reports them (and only then), and every label it renders is a
// catalog entry, so it is translated in the six languages.
func TestUIGxEngineStatsAllTimeTotals(t *testing.T) {
	engine := fakeStatsEngine{stats: map[string]any{
		"session_loaded": true,
		"lifetime": map[string]any{
			"downloaded_bytes": float64(3 << 40),
			"uploaded_bytes":   float64(1 << 40),
			"ratio":            0.3333,
		},
	}}
	rows := map[string]string{}
	for _, row := range uiGxEngineStats(engine).Rows {
		rows[row.Label] = row.Value
	}
	if !strings.Contains(rows["Scaricato in totale"], "3.0") {
		t.Fatalf("downloaded row = %q", rows["Scaricato in totale"])
	}
	if rows["Inviato in totale"] == "" {
		t.Fatalf("uploaded row missing: %+v", rows)
	}
	if rows["Rapporto totale"] != "0.33" {
		t.Fatalf("ratio row = %q, want 0.33", rows["Rapporto totale"])
	}

	keys := i18nCatalogKeys(t)
	for _, key := range []string{"Scaricato in totale", "Inviato in totale", "Rapporto totale"} {
		if !keys[key] {
			t.Errorf("no catalog entry for %q", key)
		}
	}

	// A daemon that predates the lifetime counters shows no all-time row.
	old := fakeStatsEngine{stats: map[string]any{"session_loaded": true}}
	for _, row := range uiGxEngineStats(old).Rows {
		if row.Label == "Rapporto totale" || row.Label == "Scaricato in totale" || row.Label == "Inviato in totale" {
			t.Fatalf("all-time row shown without lifetime data: %+v", row)
		}
	}
}

// TestUIGxEngineStatsAllTimeKeysReachTheRuntimeDictionary: the all-time labels
// go through the same whole-text-node translation as the rest of the panel, so
// the runtime dictionary of every language must carry them.
func TestUIGxEngineStatsAllTimeKeysReachTheRuntimeDictionary(t *testing.T) {
	state := newTestAppState(t)
	if _, err := state.i18n.SeedDefaultTranslations(); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "de", "fr", "es", "pl"} {
		dict := v2Dictionary(state, lang)
		if dict == nil {
			t.Fatalf("no runtime dictionary for %s", lang)
		}
		for _, key := range []string{"Scaricato in totale", "Inviato in totale", "Rapporto totale"} {
			if dict[key] == "" {
				t.Errorf("%s dictionary is missing %q", lang, key)
			}
		}
	}
}

// TestEngineParts: the pieces join back into the Sprintf result, values stand
// alone and the literal text is cut at the "·" separators.
func TestEngineParts(t *testing.T) {
	parts := engineParts("%d · in download %d · hit %d%% · %s", 6, 2, 40, "x")
	if got, want := strings.Join(parts, ""), fmt.Sprintf("%d · in download %d · hit %d%% · %s", 6, 2, 40, "x"); got != want {
		t.Fatalf("joined = %q, want %q", got, want)
	}
	want := []string{"6", " ", "·", " in download ", "2", " ", "·", " hit ", "40", "% ", "·", " ", "x"}
	if strings.Join(parts, "|") != strings.Join(want, "|") {
		t.Fatalf("parts = %q, want %q", parts, want)
	}
}

// TestUIGxEngineStatsPhrasesAreTranslated: the Health page translates whole
// text nodes, so every phrase of a composed engine row must be a piece of its
// own and a catalog key; otherwise it stays Italian in every language.
func TestUIGxEngineStatsPhrasesAreTranslated(t *testing.T) {
	engine := fakeStatsEngine{stats: map[string]any{
		"session_loaded": true, "version": "1.1.249", "peer_port": float64(51600), "listen_address": "0.0.0.0",
		"dht": true, "utp": true, "proxy": true, "encryption": float64(1), "cache_auto": true, "preallocate": true,
		"port_mapping": map[string]any{"method": "upnp", "external_ip": "203.0.113.1"},
		"lsd":          map[string]any{"enabled": true},
		"session": map[string]any{
			"peers_incoming_tcp": float64(2), "utp_dials": float64(3), "utp_dial_errors": float64(1),
			"utp_syn_drops": float64(1), "merged_pieces": float64(2), "merged_partial_pieces": float64(1),
		},
	}}
	pieces := map[string]bool{}
	labels := map[string]bool{}
	for _, row := range uiGxEngineStats(engine).Rows {
		labels[row.Label] = true
		if strings.Join(row.Parts, "") != row.Value {
			t.Errorf("row %q: parts %q do not join into %q", row.Label, row.Parts, row.Value)
		}
		for _, part := range row.Parts {
			pieces[strings.TrimSpace(part)] = true
		}
	}
	keys := i18nCatalogKeys(t)
	for label := range labels {
		if !keys[label] {
			t.Errorf("no catalog entry for the label %q", label)
		}
	}
	for _, phrase := range []string{
		"attivo da", "in download", "in seed", "in coda", "bloccati", "lenti", "download", "seed", "totale",
		"TCP e UDP", "indirizzo", "porta aperta con", "indirizzo esterno", "attivo", "nodi",
		"connessioni in uscita uTP", "in entrata uTP", "errori", "SYN scartati", "peer trovati in rete locale",
		"sì", "attiva", "attivo (DHT e tracker UDP spenti)", "regole", "automatica (dalla RAM)", "lettura",
		"scrittura", "preallocazione", "job in coda", "letture", "scritture", "cache lettura", "hit",
		"buffer scrittura", "scaricati", "inviati", "overhead protocollo", "peer connessi", "pezzi",
		"riusati da altri torrent (senza rete)", "pezzi riusati a blocchi",
	} {
		if !pieces[phrase] {
			t.Errorf("phrase %q is not a piece of its own in the engine rows", phrase)
		}
		if !keys[phrase] {
			t.Errorf("no catalog entry for %q", phrase)
		}
	}
	for _, phrase := range []string{"spento", "disattivata", "obbligatoria", "nessuno", "impostata a mano",
		"nessuna finora (porta forse chiusa sul router)",
		"porta non aperta automaticamente (né UPnP né NAT-PMP): aprila a mano, TCP e UDP"} {
		if !keys[phrase] {
			t.Errorf("no catalog entry for %q", phrase)
		}
	}
}
