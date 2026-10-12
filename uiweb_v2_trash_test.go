package gextto

import (
	"strings"
	"testing"
)

// TestV2TrashDeleteMessage pins the toast shown after emptying the trash: it
// names the removed entries and the freed space, and never reports success on
// an error.
func TestV2TrashDeleteMessage(t *testing.T) {
	msg, failed := v2TrashDeleteMessage([]byte(`{"removed":2,"bytes":1048576,"names":["a.mkv","b.mkv"]}`), 200, v2Identity)
	if failed {
		t.Fatalf("unexpected failure: %q", msg)
	}
	for _, want := range []string{"2 elementi", "a.mkv", "b.mkv", "liberati"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	if msg, _ := v2TrashDeleteMessage([]byte(`{"removed":1,"bytes":2048,"names":["x"]}`), 200, v2Identity); !strings.Contains(msg, "1 elemento") {
		t.Fatalf("singular message = %q", msg)
	}
	if msg, failed := v2TrashDeleteMessage([]byte(`{"removed":0,"bytes":0,"errors":["x"]}`), 200, v2Identity); !failed {
		t.Fatalf("errors must set failed: %q", msg)
	}
	if _, failed := v2TrashDeleteMessage([]byte(`{}`), 500, v2Identity); !failed {
		t.Fatal("HTTP error must set failed")
	}
}

// v2Identity keeps the Italian catalog keys as they are.
func v2Identity(key string) string { return key }

// TestV2TrashDeleteMessageIsTranslated: the toast is built from catalog phrases,
// so an English interface gets an English message with the numbers filled in.
func TestV2TrashDeleteMessageIsTranslated(t *testing.T) {
	en := map[string]string{
		"%d elementi rimossi · %s liberati": "%d items removed · %s freed",
		"%d non rimossi":                    "%d not removed",
		"Cestino già vuoto.":                "Trash already empty.",
	}
	tr := func(key string) string { return v2TranslateText(key, en, nil) }
	msg, failed := v2TrashDeleteMessage([]byte(`{"removed":2,"bytes":1048576,"names":["a.mkv","b.mkv"],"errors":["c"]}`), 200, tr)
	if !failed || !strings.HasPrefix(msg, "2 items removed · ") || !strings.Contains(msg, "freed: a.mkv, b.mkv") || !strings.HasSuffix(msg, "(1 not removed)") {
		t.Fatalf("translated message = %q", msg)
	}
	if msg, _ := v2TrashDeleteMessage([]byte(`{"removed":0}`), 200, tr); msg != "Trash already empty." {
		t.Fatalf("empty trash message = %q", msg)
	}
}
