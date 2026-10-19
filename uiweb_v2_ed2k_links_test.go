package gextto

import "testing"

func TestExtractEd2kLinks(t *testing.T) {
	raw := `
		ecco i link:
		ed2k://|file|Uno.mkv|1000|0123456789abcdef0123456789abcdef|/
		ed2k://|file|Due%20con%20spazi.avi|2000|fedcba9876543210fedcba9876543210|/ ed2k://|file|Uno.mkv|1000|0123456789ABCDEF0123456789ABCDEF|/
		ed2k://|file|rotto|niente|xxx|/
	`
	links := extractEd2kLinks(raw)
	if len(links) != 2 {
		t.Fatalf("links = %#v", links)
	}
	if links[0] != "ed2k://|file|Uno.mkv|1000|0123456789abcdef0123456789abcdef|/" {
		t.Fatalf("first link = %q", links[0])
	}
	if links[1] != "ed2k://|file|Due%20con%20spazi.avi|2000|fedcba9876543210fedcba9876543210|/" {
		t.Fatalf("second link = %q", links[1])
	}
	if got := extractEd2kLinks("nessun link qui"); len(got) != 0 {
		t.Fatalf("unexpected links = %#v", got)
	}
}
