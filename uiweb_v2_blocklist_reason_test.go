package gextto

// uiweb_v2_blocklist_reason_test.go covers the "Motivo" column of the blocklist
// page: the program stores a code, the page says in words why the release will
// not be downloaded again.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlocklistReasonLabelSaysWhyInWords(t *testing.T) {
	for code, want := range map[string]string{
		"manual":                        "bloccata a mano",
		"manual_failed":                 "download fallito rimosso a mano",
		"dead_swarm":                    "nessuno la condivideva",
		"stalled_ed2k":                  "il download eD2k non riceveva dati",
		"corrupt_file":                  "il file scaricato era danneggiato",
		"pack_inspection_useless":       "il pack non contiene puntate che servono",
		"season_pack_identity_mismatch": "il pack non contiene la stagione che promette",
		"in_progress_pack_incomplete":   "il pack è incompleto",
		"dead_magnet":                   "il magnet non funzionava",
		"unsafe_payload":                "il contenuto non è sicuro",
		"":                              "motivo non registrato",
		"a_code_from_the_future":        "a_code_from_the_future",
	} {
		if got := blocklistReasonLabel(code); got != want {
			t.Errorf("blocklistReasonLabel(%q) = %q, want %q", code, got, want)
		}
	}
}

// TestBlocklistPageShowsTheReasonInWords reads the real page: the code the
// program writes must never reach the operator.
func TestBlocklistPageShowsTheReasonInWords(t *testing.T) {
	state := newTestAppState(t)
	release := expiryTestRelease()
	if err := state.db.Blocklist(&release, "dead_swarm"); err != nil {
		t.Fatalf("Blocklist: %v", err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()

	code, body := v2Request(t, server, http.MethodGet, "/?view=blocklist", nil)
	if code != http.StatusOK {
		t.Fatalf("blocklist page = %d: %s", code, body)
	}
	if !strings.Contains(body, "nessuno la condivideva") {
		t.Errorf("the reason is not written in words:\n%s", body)
	}
	if strings.Contains(body, "dead_swarm") {
		t.Error("the internal code of the reason reached the page")
	}
}
