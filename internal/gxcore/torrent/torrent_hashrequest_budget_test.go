package torrent

import (
	"testing"
	"time"
)

// TestHashAnswerBudgetPacesDiskReads pins the pacing of the leaf-layer hash
// answers: they run on the torrent loop and read the file from disk, so a peer
// must not be able to drive the disk by asking again and again. The budget is
// in bytes read, so a downloader that needs hashes for a big file is not
// slowed down by an unrelated request count.
func TestHashAnswerBudgetPacesDiskReads(t *testing.T) {
	tr := &torrent{}
	now := time.Now()
	perRequest := 8 << 20 // one full v2ChunkMax request
	for i := 0; i < hashServeBurstBytes/perRequest; i++ {
		if !tr.allowServedHashRequest(now, perRequest) {
			t.Fatalf("request %d was refused inside the burst", i)
		}
	}
	if tr.allowServedHashRequest(now, perRequest) {
		t.Fatal("the burst limit was not enforced")
	}
	// One second later a second of refill is available, no more.
	if !tr.allowServedHashRequest(now.Add(time.Second), hashServePerSecond) {
		t.Fatal("the refill was not granted")
	}
	if tr.allowServedHashRequest(now.Add(time.Second), 1) {
		t.Fatal("the budget went past the refill")
	}
	// The budget never grows past the burst, however long the pause.
	if got := tr.hashReqBudget; got > hashServeBurstBytes {
		t.Fatalf("budget %d is above the burst %d", got, hashServeBurstBytes)
	}
}

// TestHashAnswerBudgetSmallRequestsFlow keeps the common case untouched: small
// requests (the usual chunk) are served for a long time without a refusal.
func TestHashAnswerBudgetSmallRequestsFlow(t *testing.T) {
	tr := &torrent{}
	now := time.Now()
	for i := 0; i < 100; i++ {
		if !tr.allowServedHashRequest(now, 4*int(16<<10)) {
			t.Fatalf("small request %d was refused", i)
		}
	}
}
