package gextto

// The READMEs show a complete engine capability matrix. It is generated from
// capabilityLevels (torrent_engine.go), the single source of truth the UI and
// the API already use, so the table can never drift from the code.
//
// When capabilityLevels changes, refresh the docs with:
//
//	UPDATE_README=1 go test -run TestReadmeCapabilityMatrix .
//
// capabilityDocRows fixes the human order and the Italian/English labels; it
// must list every capability of the matrix.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

var capabilityDocRows = []struct{ Key, IT, EN string }{
	{"add", "Aggiunta", "Add"},
	{"list", "Elenco", "List"},
	{"pause", "Pausa", "Pause"},
	{"resume", "Riprendi", "Resume"},
	{"remove", "Rimozione", "Remove"},
	{"recheck", "Ricontrollo dei dati", "Recheck"},
	{"move", "Spostamento", "Move"},
	{"sequential", "Download sequenziale", "Sequential download"},
	{"first_last", "Prima/ultima parte", "First/last piece"},
	{"files", "Selezione dei file", "File selection"},
	{"limits", "Limiti per torrent", "Per-torrent limits"},
	{"peers", "Peer", "Peers"},
	{"trackers", "Tracker", "Trackers"},
	{"events", "Eventi", "Events"},
	{"stats", "Statistiche del torrent", "Torrent stats"},
	{"seed_policy", "Policy di seed", "Seed policy"},
	{"super_seeding", "Super-seeding (BEP 16)", "Super-seeding (BEP 16)"},
	{"upload_mode", "Upload/share mode", "Upload/share mode"},
	{"ramdisk", "RAM disk", "RAM disk"},
	{"fastresume", "Fast resume", "Fast resume"},
	{"piece_diagnostics", "Diagnostica dei pezzi", "Per-piece diagnostics"},
	{"preferences", "Preferenze del motore", "Engine preferences"},
	{"session_stats", "Statistiche di sessione", "Session stats"},
	{"sync", "Sincronizzazione della sessione", "Session sync"},
	{"ip_filter", "Filtro IP", "IP filter"},
	{"web_seeds", "Web seed", "Web seeds"},
	{"categories", "Categorie", "Categories"},
	{"tags", "Tag", "Tags"},
}

// capabilityDocColumns is the engine order and labels of the generated table.
var capabilityDocColumns = []struct{ Backend, IT, EN string }{
	{BackendGxTorrent, "gx-torrent", "gx-torrent"},
	{BackendEmbedded, "libtorrent integrato", "libtorrent (embedded)"},
	{BackendQbittorrent, "qBittorrent-nox", "qBittorrent-nox"},
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
func renderCapabilityMatrix(lang string) string {
	parity := CapabilityParity()
	header := []string{map[string]string{"it": "Funzione", "en": "Capability"}[lang]}
	align := []string{"---"}
	for _, col := range capabilityDocColumns {
		label := col.EN
		if lang == "it" {
			label = col.IT
		}
		header = append(header, label)
		align = append(align, ":--:")
	}
	lines := []string{
		"| " + strings.Join(header, " | ") + " |",
		"| " + strings.Join(align, " | ") + " |",
	}
	for _, row := range capabilityDocRows {
		label := row.EN
		if lang == "it" {
			label = row.IT
		}
		cells := []string{label}
		for _, col := range capabilityDocColumns {
			cells = append(cells, capabilityDocLevel(parity[row.Key][col.Backend], lang))
		}
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
			t.Fatalf("capability %q has no README row; add it to capabilityDocRows", name)
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
