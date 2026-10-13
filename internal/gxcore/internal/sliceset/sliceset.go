package sliceset

import "slices"

// SliceSet is an unordered pointer set backed by a slice. It suits small
// sets (peers per piece) where a map's hashing and bookkeeping would cost
// more than a linear scan, and removal is swap-with-last, O(1).
type SliceSet[T any] struct {
	Items []*T
}

// Add inserts pe, reporting false when it was already present.
func (l *SliceSet[T]) Add(pe *T) bool {
	if l.Has(pe) {
		return false
	}
	l.Items = append(l.Items, pe)
	return true
}

// Remove deletes pe, reporting false when it was absent.
// Order is not preserved.
func (l *SliceSet[T]) Remove(pe *T) bool {
	i := slices.Index(l.Items, pe)
	if i < 0 {
		return false
	}
	last := len(l.Items) - 1
	l.Items[i] = l.Items[last]
	l.Items = l.Items[:last]
	return true
}

// Has reports whether pe is in the set.
func (l *SliceSet[T]) Has(pe *T) bool {
	return slices.Contains(l.Items, pe)
}

// Len returns the number of items in the set.
func (l *SliceSet[T]) Len() int {
	return len(l.Items)
}
