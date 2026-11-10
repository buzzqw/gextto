package main

import "testing"

// The downloaded bar groups pieces into at most barCells stretches and tells
// complete, partial, missing and skipped stretches apart.
func TestPieceBarCells(t *testing.T) {
	states := []string{"have", "have", "have", "", "skipped", "skipped", "", ""}
	cells := pieceBarCells(states)
	want := []string{"have", "have", "have", "missing", "skip", "skip", "missing", "missing"}
	if len(cells) != len(want) {
		t.Fatalf("%d cells for %d pieces", len(cells), len(states))
	}
	for i, c := range cells {
		if c.Class != want[i] {
			t.Errorf("cell %d is %q, want %q", i, c.Class, want[i])
		}
	}
	many := make([]string, 10000)
	for i := 0; i < 5000; i++ {
		many[i] = "have"
	}
	cells = pieceBarCells(many)
	if len(cells) != barCells {
		t.Fatalf("%d cells for 10,000 pieces, want %d", len(cells), barCells)
	}
	if cells[0].Class != "have" || cells[barCells-1].Class != "missing" {
		t.Fatalf("first %q, last %q", cells[0].Class, cells[barCells-1].Class)
	}
	half := pieceBarCells([]string{"have", ""})
	if len(half) != 2 {
		t.Fatal("two pieces should give two cells")
	}
}

// The availability bar shows the fewest holders of the missing pieces, red
// where nobody connected has one, and counts those pieces.
func TestAvailBarCells(t *testing.T) {
	states := []string{"have", "", "", "skipped"}
	holders := []uint32{0, 4, 0, 0}
	cells, missing, most := availBarCells(states, holders)
	if missing != 1 || most != 4 {
		t.Fatalf("missing %d, most %d; want 1 and 4", missing, most)
	}
	want := []string{"done", "avail", "none", "done"}
	for i, c := range cells {
		if c.Class != want[i] {
			t.Errorf("cell %d is %q, want %q", i, c.Class, want[i])
		}
	}
	if cells[1].Op != "1.00" {
		t.Errorf("the best-held piece has opacity %s, want 1.00", cells[1].Op)
	}
	if c, _, _ := availBarCells(states, holders[:2]); c != nil {
		t.Fatal("mismatched lengths gave cells")
	}
}
