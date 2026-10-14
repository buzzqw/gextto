package gextto

import "testing"

// TestGxFirstLastDefaultReachesEveryAdd checks that the global
// torrent_first_last setting adds first_last to a torrent added without it.
func TestGxFirstLastDefaultReachesEveryAdd(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	cfg := &Config{LibtorrentDir: "/dl"}
	if _, err := engine.AddWithOptions("magnet:?off", cfg, nil, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if fake.seen("POST /api/v1/add?destination=%2Fdl&first_last=1&magnet=magnet%3A%3Foff") {
		t.Fatal("first_last must stay off without the setting")
	}
	engine.SetFirstLastDefault(true)
	if _, err := engine.AddWithOptions("magnet:?on", cfg, nil, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if !fake.seen("POST /api/v1/add?destination=%2Fdl&first_last=1&magnet=magnet%3A%3Fon") {
		t.Fatalf("first_last default not sent: %v", fake.requests)
	}
}

func TestQbittorrentFirstLastDefault(t *testing.T) {
	engine := &qbittorrentEngine{}
	if engine.addOptions(AddOptions{}).FirstLast {
		t.Fatal("first/last must stay off without the setting")
	}
	if !engine.addOptions(AddOptions{FirstLast: true}).FirstLast {
		t.Fatal("the per-torrent option must still work")
	}
	engine.SetFirstLastDefault(true)
	if !engine.addOptions(AddOptions{}).FirstLast {
		t.Fatal("first/last default not applied")
	}
}
