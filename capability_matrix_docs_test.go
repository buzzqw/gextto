package gextto

// The READMEs show an engine capability matrix generated from capabilityLevels
// (torrent_engine.go), the single source of truth the UI and the API use, so the
// table can never drift from the code.
//
// Capabilities supported by every engine (add, remove, pause, ...) are collapsed
// into one "basic" row; the others get one row each. Nothing here is edited by
// hand except the labels: when capabilityLevels changes, refresh the docs with
//
//	UPDATE_README=1 go test -run TestReadmeCapabilityMatrix .

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// capabilityDocRows gives the label of every capability (Italian and English)
// and the order in which the rows are rendered. It must cover the whole matrix;
// an empty label keeps an internal capability (API plumbing every engine has)
// out of the table.
var capabilityDocRows = []struct{ Key, IT, EN string }{
	{"add", "Aggiungi", "Add"},
	{"remove", "Rimuovi", "Remove"},
	{"pause", "Pausa", "Pause"},
	{"resume", "Riprendi", "Resume"},
	{"list", "Elenca", "List"},
	{"recheck", "Ricontrolla", "Recheck"},
	{"move", "Sposta", "Move"},
	{"sequential", "Download sequenziale", "Sequential download"},
	{"files", "Selezione file", "File selection"},
	{"limits", "Limiti per torrent", "Per-torrent limits"},
	{"peers", "Peer", "Peers"},
	{"trackers", "Tracker", "Trackers"},
	{"events", "", ""},
	{"stats", "", ""},
	{"categories", "Categorie", "Categories"},
	{"tags", "Tag", "Tags"},
	{"first_last", "Prima/ultima parte", "First/last piece"},
	{"seed_policy", "Policy di seed", "Seed policy"},
	{"super_seeding", "Super-seeding (BEP 16)", "Super-seeding (BEP 16)"},
	{"ramdisk", "RAM disk", "RAM disk"},
	{"fastresume", "Fast resume", "Fast resume"},
	{"piece_diagnostics", "Diagnostica dei pezzi", "Per-piece diagnostics"},
	{"session_stats", "Statistiche di sessione", "Session stats"},
	{"sync", "Sincronizzazione della sessione", "Session sync"},
	{"ip_filter", "Filtro IP", "IP filter"},
	{"web_seeds", "Web seed", "Web seeds"},
	{"holepunch", "Holepunching (BEP 55)", "Holepunching (BEP 55)"},
}

// capabilityDocColumns is the engine order and labels of the generated table.
var capabilityDocColumns = []struct{ Backend, IT, EN string }{
	{BackendGxTorrent, "gx-torrent", "gx-torrent"},
	{BackendQbittorrent, "qBittorrent-nox", "qBittorrent-nox"},
	{BackendEmbedded, "libtorrent (build opzionale)", "libtorrent (optional build)"},
}

const (
	capabilityMatrixStart = "<!-- capability-matrix:start -->"
	capabilityMatrixEnd   = "<!-- capability-matrix:end -->"
)

func capabilityDocLevel(level, lang string) string {
	switch level {
	case "full":
		if lang == "it" {
			return "sì"
		}
		return "yes"
	case "partial":
		if lang == "it" {
			return "parziale"
		}
		return "partial"
	default:
		return "—"
	}
}

// renderCapabilityMatrix builds the markdown table for one language.
// Capabilities with the same level on every engine share one row, in the order
// of their first appearance in capabilityDocRows.
func renderCapabilityMatrix(lang string) string {
	parity := CapabilityParity()
	label := func(it, en string) string {
		if lang == "it" {
			return it
		}
		return en
	}
	header := []string{label("Capacità", "Capability")}
	align := []string{"---"}
	for _, col := range capabilityDocColumns {
		header = append(header, label(col.IT, col.EN))
		align = append(align, ":--:")
	}
	lines := []string{
		"| " + strings.Join(header, " | ") + " |",
		"| " + strings.Join(align, " | ") + " |",
	}

	type group struct {
		names  []string
		levels []string
	}
	var groups []*group
	byLevels := map[string]*group{}
	for _, row := range capabilityDocRows {
		if row.EN == "" {
			continue
		}
		levels := make([]string, 0, len(capabilityDocColumns))
		for _, col := range capabilityDocColumns {
			levels = append(levels, capabilityDocLevel(parity[row.Key][col.Backend], lang))
		}
		key := strings.Join(levels, "|")
		g, ok := byLevels[key]
		if !ok {
			g = &group{levels: levels}
			byLevels[key] = g
			groups = append(groups, g)
		}
		g.names = append(g.names, label(row.IT, row.EN))
	}
	for _, g := range groups {
		cells := append([]string{strings.Join(g.names, ", ")}, g.levels...)
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

// replaceCapabilityMatrix swaps the block between the two markers with table.
func replaceCapabilityMatrix(content, table string) (string, error) {
	start := strings.Index(content, capabilityMatrixStart)
	end := strings.Index(content, capabilityMatrixEnd)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("markers %s / %s not found", capabilityMatrixStart, capabilityMatrixEnd)
	}
	prefix := content[:start+len(capabilityMatrixStart)]
	suffix := content[end:]
	return prefix + "\n" + table + "\n" + suffix, nil
}

// TestReadmeCapabilityMatrix keeps the READMEs' capability matrix identical to
// capabilityLevels. Run with UPDATE_README=1 to regenerate the block.
func TestReadmeCapabilityMatrix(t *testing.T) {
	known := map[string]bool{}
	for _, name := range capabilityNames() {
		known[name] = true
	}
	documented := map[string]bool{}
	for _, row := range capabilityDocRows {
		if documented[row.Key] {
			t.Fatalf("duplicate capability row %q", row.Key)
		}
		if !known[row.Key] {
			t.Fatalf("capabilityDocRows lists unknown capability %q", row.Key)
		}
		documented[row.Key] = true
	}
	for _, name := range capabilityNames() {
		if !documented[name] {
			t.Fatalf("capability %q has no label; add it to capabilityDocRows", name)
		}
	}

	update := os.Getenv("UPDATE_README") != ""
	for _, doc := range []struct{ path, lang string }{{"README.md", "en"}, {"README.it.md", "it"}} {
		content, err := os.ReadFile(doc.path)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := replaceCapabilityMatrix(string(content), renderCapabilityMatrix(doc.lang))
		if err != nil {
			t.Fatalf("%s: %v", doc.path, err)
		}
		if update {
			if updated != string(content) {
				if err := os.WriteFile(doc.path, []byte(updated), 0o644); err != nil {
					t.Fatalf("%s: %v", doc.path, err)
				}
			}
			continue
		}
		if updated != string(content) {
			t.Fatalf("%s capability matrix is out of date; refresh with: UPDATE_README=1 go test -run TestReadmeCapabilityMatrix .", doc.path)
		}
	}
}
