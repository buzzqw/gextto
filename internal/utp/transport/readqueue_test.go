package transport

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// Bytes come out in the order they went in, across chunk boundaries, whatever
// the sizes of the writes and reads.
func TestReadQueueKeepsTheOrderAcrossChunks(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var q readQueue
	var want, got []byte
	for range 2000 {
		p := make([]byte, r.IntN(3*readChunkSize/2))
		for i := range p {
			p[i] = byte(r.Uint32())
		}
		q.Write(p)
		want = append(want, p...)
		b := make([]byte, r.IntN(2*readChunkSize))
		n := q.Read(b)
		got = append(got, b[:n]...)
		if q.Len() != len(want)-len(got) {
			t.Fatalf("Len %d, want %d", q.Len(), len(want)-len(got))
		}
	}
	rest := make([]byte, q.Len())
	got = append(got, rest[:q.Read(rest)]...)
	if !bytes.Equal(got, want) {
		t.Fatal("bytes read differ from bytes written")
	}
	if q.Len() != 0 || len(q.chunks) != 0 {
		t.Fatalf("drained queue keeps %d bytes in %d chunks", q.Len(), len(q.chunks))
	}
}

// A drained connection holds no read memory.
func TestReadQueueDrainedHoldsNoChunk(t *testing.T) {
	var q readQueue
	q.Write(make([]byte, readChunkSize+10))
	q.Read(make([]byte, readChunkSize+10))
	if len(q.chunks) != 0 {
		t.Fatalf("%d chunks kept after the queue drained", len(q.chunks))
	}
	if q.Read(make([]byte, 1)) != 0 {
		t.Fatal("an empty queue returned bytes")
	}
}

// A queued out-of-order packet gives its pooled buffer back when consumed.
func TestQueuedPacketReleasesItsBuffer(t *testing.T) {
	var r recv
	r.own([]byte("payload"))
	if r.owned == nil || string(r.data) != "payload" {
		t.Fatal("payload not copied into a pooled buffer")
	}
	r.release()
	if r.owned != nil {
		t.Fatal("buffer still owned after release")
	}
	var big recv
	big.own(make([]byte, pooledDatagramSize+1))
	if big.owned != nil || len(big.data) != pooledDatagramSize+1 {
		t.Fatal("an oversized payload must get its own buffer")
	}
}

// A queue consumed from the front keeps its order and, in the steady state,
// its array: no allocation per append.
func TestAppendFIFOReusesItsArray(t *testing.T) {
	var base, q []int
	next, want := 0, 0
	for range 5 {
		q = appendFIFO(&base, q, next)
		next++
	}
	allocs := testing.AllocsPerRun(10000, func() {
		q = appendFIFO(&base, q, next)
		next++
		if q[0] != want {
			t.Fatalf("head %d, want %d", q[0], want)
		}
		q[0] = 0
		q = q[1:]
		want++
	})
	if allocs != 0 {
		t.Fatalf("%.2f allocations per append, want 0", allocs)
	}
	for i, v := range q {
		if v != want+i {
			t.Fatalf("q[%d] = %d, want %d", i, v, want+i)
		}
	}
}
