package tui

import "testing"

// TestAutoManagedLabel pins the terms the detail view shows for what controls a
// torrent's scheduling; they must match the web UI (v2AutoManagedLabel).
func TestAutoManagedLabel(t *testing.T) {
	cases := []struct {
		name string
		in   Torrent
		want string
	}{
		{"queue", Torrent{AutoManaged: true}, "managed"},
		{"user pause", Torrent{State: "paused"}, "paused"},
		{"queued", Torrent{State: "queued"}, "paused"},
		{"parked", Torrent{State: "stalled", Stalled: true}, "parked"},
		{"pinned", Torrent{Pinned: true}, "pinned"},
		{"forced running", Torrent{State: "downloading"}, "pinned"},
		{"error outside the queue", Torrent{State: "error"}, "paused"},
		{"checking outside the queue", Torrent{State: "checking_files"}, "paused"},
		{"forced seeding", Torrent{State: "seeding"}, "pinned"},
	}
	for _, tc := range cases {
		if got := autoManagedLabel(tc.in); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
