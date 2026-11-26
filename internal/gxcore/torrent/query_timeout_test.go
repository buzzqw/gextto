package torrent

import (
	"testing"
	"time"
)

func TestQueryWithinReturnsWhenRunLoopDoesNotAnswer(t *testing.T) {
	tr := &torrent{commandC: make(chan func()), closeC: make(chan struct{})}
	start := time.Now()
	_, ok := queryWithin(tr, 20*time.Millisecond, func() Status { return tr.status() })
	if ok {
		t.Fatal("queryWithin reported an answer from a run loop that never ran")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("queryWithin took %s, want the 20 ms deadline", elapsed)
	}
}
