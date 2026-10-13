package trackermanager

import (
	"testing"
	"time"
)

// TestGetSchemes locks in the supported tracker schemes: http(s), udp and the
// WebSocket ones (ws/wss). An unknown scheme is rejected so the tier can drop
// it instead of keeping a dead tracker.
func TestGetSchemes(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()

	for _, scheme := range []string{"http", "https", "udp", "ws", "wss"} {
		raw := scheme + "://tracker.example/announce"
		tr, err := m.Get(raw, time.Second, "test", 1<<20)
		if err != nil {
			t.Errorf("%s: %v", scheme, err)
			continue
		}
		if tr.URL() != raw {
			t.Errorf("%s: URL = %q", scheme, tr.URL())
		}
	}
	if _, err := m.Get("ftp://tracker.example/announce", time.Second, "test", 1<<20); err == nil {
		t.Fatal("ftp must be rejected")
	}
}
