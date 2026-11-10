package gextto

import "testing"

// The state filter groups the engine states, counts them, hides the empty
// groups except the selected one, and treats a finished torrent as completed
// even when the engine reports it paused.
func TestV2StateChips(t *testing.T) {
	rows := []uiTorrentRow{
		{StateKey: "downloading", Progress: 40},
		{StateKey: "downloading_metadata"},
		{StateKey: "seeding", Progress: 100},
		{StateKey: "pausedDL", Progress: 10},
		{StateKey: "paused", Progress: 100},
	}
	chips := v2StateChips(rows, "error")
	got := map[string]int{}
	for _, c := range chips {
		got[c.Key] = c.Count
	}
	want := map[string]int{"all": 5, "downloading": 2, "seeding": 1, "paused": 1, "completed": 2, "error": 0}
	for k, n := range want {
		if c, ok := got[k]; !ok || c != n {
			t.Errorf("group %s: %d (present %v), want %d", k, c, ok, n)
		}
	}
	if _, ok := got["stalled"]; ok {
		t.Error("an empty, unselected group is shown")
	}
	for _, c := range chips {
		if c.On != (c.Key == "error") {
			t.Errorf("group %s pressed %v", c.Key, c.On)
		}
	}
	if !v2ValidStateGroup("seeding") || v2ValidStateGroup("bogus") {
		t.Error("group validation is wrong")
	}
}
