package verifier

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// droppingFile records the ranges a gentle check asks to drop from the cache.
type droppingFile struct {
	countingFile
	dropped [][2]int64
}

func (f *droppingFile) DropCache(off, length int64) {
	f.dropped = append(f.dropped, [2]int64{off, length})
}

// A gentle check drops the cache pages of each piece it hashed.
func TestGentleCheckDropsCache(t *testing.T) {
	data := [][]byte{[]byte("piece-a-16-bytes"), []byte("piece-b-16-bytes")}
	files := make([]*droppingFile, len(data))
	pieces := make([]piece.Piece, len(data))
	for i, d := range data {
		p, cf := makePiece(uint32(i), d)
		files[i] = &droppingFile{countingFile: *cf}
		p.Data[0].File = files[i]
		pieces[i] = p
	}
	v := New()
	v.Gentle = true
	resultC := make(chan *Verifier, 1)
	v.Run(pieces, make(chan Progress, 4), resultC)
	<-resultC
	if !v.Bitfield.All() {
		t.Fatal("a gentle check failed good pieces")
	}
	for i, f := range files {
		if len(f.dropped) != 1 || f.dropped[0] != [2]int64{0, 16} {
			t.Fatalf("piece %d dropped %v, want its whole range", i, f.dropped)
		}
	}
}

// The pace pauses only after a read clearly slower than usual, for as long as
// that read took, and ignores reads served from the cache.
func TestReadPace(t *testing.T) {
	var rp readPace
	const size = 4 << 20
	usual := 20 * time.Millisecond // 200 MB/s
	for i := 0; i < 16; i++ {
		if w := rp.after(size, usual); w != 0 {
			t.Fatalf("read %d at the usual pace paused %v", i, w)
		}
	}
	if w := rp.after(size, 40*time.Millisecond); w != 0 {
		t.Fatalf("a read 2x slower paused %v", w)
	}
	if w := rp.after(size, 80*time.Millisecond); w != 0 {
		t.Fatalf("a single slow read paused %v: one is the disk's jitter", w)
	}
	if w := rp.after(size, 80*time.Millisecond); w != 80*time.Millisecond {
		t.Fatalf("a second slow read in a row paused %v, want as long as the read", w)
	}
	if w := rp.after(size, 10*time.Second); w != maxPause {
		t.Fatalf("a very slow read paused %v, want the cap %v", w, maxPause)
	}
	if w := rp.after(size, 100*time.Microsecond); w != 0 {
		t.Fatalf("a read from the cache paused %v", w)
	}
}
