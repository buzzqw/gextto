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
		{"queue", Torrent{AutoManaged: true}, "sched.managed"},
		{"user pause", Torrent{State: "paused"}, "sched.paused"},
		{"queued", Torrent{State: "queued"}, "sched.paused"},
		{"parked", Torrent{State: "stalled", Stalled: true}, "sched.parked"},
		{"pinned", Torrent{Pinned: true}, "sched.pinned"},
		{"forced running", Torrent{State: "downloading"}, "sched.pinned"},
		{"error outside the queue", Torrent{State: "error"}, "sched.paused"},
		{"checking outside the queue", Torrent{State: "checking_files"}, "sched.paused"},
		{"forced seeding", Torrent{State: "seeding"}, "sched.pinned"},
	}
	for _, tc := range cases {
		if got := autoManagedLabel(tc.in); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
