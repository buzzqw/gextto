package gextto

import (
	"strings"
	"testing"
)

// TestV2TrashDeleteMessage pins the toast shown after emptying the trash: it
// names the removed entries and the freed space, and never reports success on
// an error.
func TestV2TrashDeleteMessage(t *testing.T) {
	msg, failed := v2TrashDeleteMessage([]byte(`{"removed":2,"bytes":1048576,"names":["a.mkv","b.mkv"]}`), 200)
	if failed {
		t.Fatalf("unexpected failure: %q", msg)
	}
	for _, want := range []string{"2 elementi", "a.mkv", "b.mkv", "liberati"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q missing %q", msg, want)
		}
	}
	if msg, _ := v2TrashDeleteMessage([]byte(`{"removed":1,"bytes":2048,"names":["x"]}`), 200); !strings.Contains(msg, "1 elemento") {
		t.Fatalf("singular message = %q", msg)
	}
	if msg, failed := v2TrashDeleteMessage([]byte(`{"removed":0,"bytes":0,"errors":["x"]}`), 200); !failed {
		t.Fatalf("errors must set failed: %q", msg)
	}
	if _, failed := v2TrashDeleteMessage([]byte(`{}`), 500); !failed {
		t.Fatal("HTTP error must set failed")
	}
}
