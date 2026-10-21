package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestV2ArchiveSQLMaintenance(t *testing.T) {
	state := newTestAppState(t)
	state.cfg.DryRun = false
	if _, err := state.archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show ITA 1080p','magnet:?xt=urn:btih:sql-ui-1',gextto_magnet_key('magnet:?xt=urn:btih:sql-ui-1'),'feed',datetime('now','-8 months')),
		('Old Show ENG 1080p','magnet:?xt=urn:btih:sql-ui-2',gextto_magnet_key('magnet:?xt=urn:btih:sql-ui-2'),'feed',datetime('now','-8 months'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance page -> %d", code)
	}
	for _, want := range []string{"Generale", "Manutenzione SQL dell", "tab=archive-sql"} {
		if !strings.Contains(body, want) {
			t.Fatalf("maintenance page missing %q", want)
		}
	}
	if strings.Contains(body, `id="v2-archive-sql-panel"`) {
		t.Fatal("the SQL panel should live in its own tab, not in the general tab")
	}

	code, body = v2Request(t, server, http.MethodGet, "/?view=maintenance&tab=archive-sql", nil)
	if code != http.StatusOK {
		t.Fatalf("SQL tab -> %d", code)
	}
	for _, want := range []string{
		`id="v2-archive-sql-panel"`,
		"Lingua per le operazioni",
		`id="v2-archive-sql-language"`,
		`value="ita" selected`,
		`value="eng"`,
		"Release nella lingua selezionata più vecchie di 6 mesi",
		"Release recenti",
		"Duplicati per titolo e fonte",
		`hx-post="/maintenance/archive-sql/preview"`,
		"Eliminazione",
		"Solo lettura",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SQL tab missing %q", want)
		}
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/preview", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"eng"}})
	if code != http.StatusOK {
		t.Fatalf("preview English -> %d", code)
	}
	for _, want := range []string{"righe corrispondenti", "Old Show ENG 1080p", "Elimina definitivamente", "Query SQL", "English"} {
		if !strings.Contains(body, want) {
			t.Fatalf("English preview missing %q", want)
		}
	}
	if strings.Contains(body, "Old Show ITA 1080p") {
		t.Fatal("English preview should not contain the Italian release")
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/preview", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"ita"}})
	if code != http.StatusOK || !strings.Contains(body, "Old Show ITA 1080p") || strings.Contains(body, "Old Show ENG 1080p") || !strings.Contains(body, "Italiano") {
		t.Fatalf("Italian preview -> %d: %s", code, body)
	}
	if count, err := state.archive.Count(); err != nil || count != 2 {
		t.Fatalf("preview changed the archive: %d, %v", count, err)
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/delete", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"eng"}})
	if code != http.StatusOK || !strings.Contains(body, "Conferma richiesta per eliminare.") {
		t.Fatalf("delete without confirmation -> %d: %s", code, body)
	}
	if count, err := state.archive.Count(); err != nil || count != 2 {
		t.Fatalf("unconfirmed delete changed the archive: %d, %v", count, err)
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/delete", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"eng"}, "confirm": {"1"}})
	if code != http.StatusOK {
		t.Fatalf("delete -> %d", code)
	}
	for _, want := range []string{"righe eliminate", "righe corrispondenti"} {
		if !strings.Contains(body, want) {
			t.Fatalf("delete result missing %q", want)
		}
	}
	if count, err := state.archive.Count(); err != nil || count != 1 {
		t.Fatalf("after delete count = %d, %v; want 1", count, err)
	}
	var remaining string
	if err := state.archive.db.QueryRow("SELECT title FROM archive").Scan(&remaining); err != nil || !strings.Contains(remaining, "ITA") {
		t.Fatalf("remaining row = %q, %v; want the Italian release", remaining, err)
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/preview", url.Values{"op": {"archive_health"}, "language": {"ita"}})
	if code != http.StatusOK || !strings.Contains(body, "Query SQL") || strings.Contains(body, "Elimina definitivamente") {
		t.Fatalf("read-only preview -> %d: %s", code, body)
	}

	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/preview", url.Values{"op": {"does-not-exist"}})
	if code != http.StatusOK || !strings.Contains(body, "Operazione non valida.") {
		t.Fatalf("invalid operation -> %d: %s", code, body)
	}
}

func TestV2ArchiveSQLDryRunDisablesDeletion(t *testing.T) {
	state := newTestAppState(t)
	if _, err := state.archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show ITA 1080p','magnet:?xt=urn:btih:sql-dry-1',gextto_magnet_key('magnet:?xt=urn:btih:sql-dry-1'),'feed',datetime('now','-8 months')),
		('Old Show ENG 1080p','magnet:?xt=urn:btih:sql-dry-2',gextto_magnet_key('magnet:?xt=urn:btih:sql-dry-2'),'feed',datetime('now','-8 months'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/preview", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"eng"}})
	if code != http.StatusOK || !strings.Contains(body, "La modalità prova è attiva") || strings.Contains(body, "Elimina definitivamente") {
		t.Fatalf("dry-run preview -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/maintenance/archive-sql/delete", url.Values{"op": {"archive_language_older_than_6_months"}, "language": {"eng"}, "confirm": {"1"}})
	if code != http.StatusOK || !strings.Contains(body, "La modalità prova è attiva") {
		t.Fatalf("dry-run delete -> %d: %s", code, body)
	}
	if count, err := state.archive.Count(); err != nil || count != 2 {
		t.Fatalf("dry-run changed the archive: %d, %v", count, err)
	}
}
