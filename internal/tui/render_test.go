package tui

import (
	"reflect"
	"testing"
)

// TestRenderDoesNotChangeTheModel pins the promise in Render's comment: a
// frame may clamp its scroll window, but drawing it must never mutate the
// state it draws. Snapshot tests and repeated draws depend on this.
func TestRenderDoesNotChangeTheModel(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.Overlay = OverlayHelp
	model.HelpScroll = 9999
	model.Tab = TabLogs
	model.UnseenProblems = 3
	model.TorrentScroll = 9999
	model.PageScroll = 9999
	model.LogScroll = 9999
	before := *model
	first := model.Render(80, 24)
	if !reflect.DeepEqual(before, *model) {
		t.Fatal("rendering changed the model")
	}
	second := model.Render(80, 24)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("rendering twice produced a different frame")
	}
	if model.UnseenProblems != 3 {
		t.Fatalf("rendering the log cleared unseen problems: %d", model.UnseenProblems)
	}
}

// TestOpeningLogsClearsUnseenProblems moves the old render side effect into
// the transition that actually changes the view.
func TestOpeningLogsClearsUnseenProblems(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.UnseenProblems = 3
	model.setTab(TabLogs)
	if model.UnseenProblems != 0 {
		t.Fatalf("unseen problems not cleared: %d", model.UnseenProblems)
	}
}

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
