package messages

import "testing"

func TestPicksVariantByLanguage(t *testing.T) {
	SetLanguage("it")
	if got := Pick("ciao", "hello"); got != "ciao" {
		t.Fatalf("italian pick = %q", got)
	}
	if IsEnglish() {
		t.Fatal("italian is not english")
	}
	SetLanguage("en")
	if got := Pick("ciao", "hello"); got != "hello" {
		t.Fatalf("english pick = %q", got)
	}
	if Language() != "en" || !IsEnglish() {
		t.Fatalf("language = %q english=%v", Language(), IsEnglish())
	}
	// Public codes alias to the two supported languages.
	SetLanguage("eng")
	if !IsEnglish() {
		t.Fatal("eng should map to english")
	}
	SetLanguage("ita")
	if IsEnglish() {
		t.Fatal("ita should map to italian")
	}
	SetLanguage("it")
}
