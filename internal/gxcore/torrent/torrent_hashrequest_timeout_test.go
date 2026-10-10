package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
)

func TestExpireV2HashRequestsReleasesStaleRequest(t *testing.T) {
	now := time.Now()
	silent, fresh := &peer.Peer{}, &peer.Peer{}
	staleFile := &v2LayerFile{chunk: 512, chunks: 4, inflight: map[int]bool{0: true}, nextChunk: 1}
	freshFile := &v2LayerFile{chunk: 512, chunks: 4, inflight: map[int]bool{0: true}, nextChunk: 1}
	tr := &torrent{
		v2LayerFiles: []*v2LayerFile{staleFile, freshFile},
		v2Pending: map[*peer.Peer]*v2HashRequest{
			silent: {file: staleFile, index: 0, count: 512, at: now.Add(-v2HashTimeout - time.Second)},
			fresh:  {file: freshFile, index: 0, count: 512, at: now.Add(-time.Second)},
		},
		v2NoHashPeers: map[*peer.Peer]struct{}{},
	}
	tr.expireV2HashRequests(now)
	if _, ok := tr.v2Pending[silent]; ok {
		t.Fatal("silent peer still pending")
	}
	if _, ok := tr.v2NoHashPeers[silent]; !ok {
		t.Fatal("silent peer would be asked again")
	}
	if staleFile.inflight[0] || len(staleFile.retry) != 1 || staleFile.retry[0] != 0 {
		t.Fatal("stale chunk not released for another peer")
	}
	if _, ok := tr.v2Pending[fresh]; !ok {
		t.Fatal("a recent request lost its peer")
	}
	if !freshFile.inflight[0] {
		t.Fatal("a recent chunk was released")
	}
}

// TestV2LayerChunksAreAssignedAndRetried pins the parallel scheduling: chunks
// are handed out one by one, a failed chunk is queued for retry, and the file
// is done only when nothing is left to request or in flight.
func TestV2LayerChunksAreAssignedAndRetried(t *testing.T) {
	lf := &v2LayerFile{chunk: 512, chunks: 3, inflight: map[int]bool{}}
	for want := 0; want < 3; want++ {
		chunk, ok := lf.allocateChunk()
		if !ok || chunk != want {
			t.Fatalf("allocateChunk = %d, %v; want %d", chunk, ok, want)
		}
		lf.inflight[chunk] = true
	}
	if _, ok := lf.allocateChunk(); ok {
		t.Fatal("no chunk should be left")
	}
	if lf.done() {
		t.Fatal("a file with chunks in flight is not done")
	}
	lf.failChunk(1)
	if lf.inflight[1] {
		t.Fatal("a failed chunk stayed in flight")
	}
	if lf.done() {
		t.Fatal("a file with a chunk to retry is not done")
	}
	if chunk, ok := lf.allocateChunk(); !ok || chunk != 1 {
		t.Fatalf("retry chunk = %d, %v; want 1", chunk, ok)
	}
	delete(lf.inflight, 0)
	delete(lf.inflight, 2)
	if !lf.done() {
		t.Fatal("a file with every chunk received is done")
	}
}
