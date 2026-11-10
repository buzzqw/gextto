package gextto

import "strings"

// v2StateChip is one button of the Downloads view's state filter.
type v2StateChip struct {
	Key   string
	Label string
	Count int
	On    bool
}

// v2StateGroups are the state filter's groups, in button order, with their
// labels (catalog keys shared with the State column).
var v2StateGroups = []struct{ Key, Label string }{
	{"all", "Tutti i torrent"},
	{"downloading", "In scarico"},
	{"seeding", "In seed"},
	{"paused", "In pausa"},
	{"completed", "Completati"},
	{"stalled", "In attesa di seed"},
	{"error", "Errore"},
}

func v2ValidStateGroup(key string) bool {
	for _, g := range v2StateGroups {
		if g.Key == key {
			return true
		}
	}
	return false
}

// v2RowInStateGroup reports whether a torrent row belongs to a state group.
// A finished torrent counts as completed whatever the engine says (paused
// after its seed limit, archived), so "paused" holds only unfinished ones.
func v2RowInStateGroup(row uiTorrentRow, group string) bool {
	state := strings.ToLower(row.StateKey)
	done := row.Archived || row.Progress >= 100
	switch group {
	case "all":
		return true
	case "downloading":
		return state == "downloading" || state == "downloading_metadata"
	case "seeding":
		return state == "seeding"
	case "paused":
		return strings.Contains(state, "paus") && !done
	case "completed":
		return done
	case "stalled":
		return state == "stalled"
	case "error":
		return state == "error"
	}
	return false
}

// v2StateChips counts the rows of every group. A group with no torrent is left
// out, unless it is the one selected (so the operator can leave it); "all" is
// always there.
func v2StateChips(rows []uiTorrentRow, selected string) []v2StateChip {
	chips := make([]v2StateChip, 0, len(v2StateGroups))
	for _, g := range v2StateGroups {
		n := 0
		for _, row := range rows {
			if v2RowInStateGroup(row, g.Key) {
				n++
			}
		}
		on := g.Key == selected || (selected == "" && g.Key == "all")
		if n == 0 && !on && g.Key != "all" {
			continue
		}
		chips = append(chips, v2StateChip{Key: g.Key, Label: g.Label, Count: n, On: on})
	}
	return chips
}
