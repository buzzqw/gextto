package gextto

import "testing"

// TestSortByScoreComputesOncePerItem: the score runs a series lookup, so it must
// be called once per element and never inside the comparator, where a manual
// search with n results would pay it n log n times.
func TestSortByScoreComputesOncePerItem(t *testing.T) {
	items := []int{5, 3, 9, 1, 7, 2, 8, 4}
	calls := 0
	sortByScore(items, func(v *int) int64 {
		calls++
		return int64(*v)
	})
	if calls != len(items) {
		t.Fatalf("the score ran %d times for %d elements", calls, len(items))
	}
	for i := 1; i < len(items); i++ {
		if items[i-1] < items[i] {
			t.Fatalf("not ordered: %v", items)
		}
	}
}

// TestSortByScoreIsStable: two elements with the same score keep the order they
// arrived in, so a tie does not depend on the sort's internals.
func TestSortByScoreIsStable(t *testing.T) {
	type row struct{ id, score int }
	rows := []row{{1, 2}, {2, 3}, {3, 2}, {4, 3}}
	sortByScore(rows, func(r *row) int64 { return int64(r.score) })
	want := []int{2, 4, 1, 3}
	for i := range want {
		if rows[i].id != want[i] {
			t.Fatalf("order = %v, want %v", rows, want)
		}
	}
}
