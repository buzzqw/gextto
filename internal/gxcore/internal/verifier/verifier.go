package verifier

import (
	"crypto/sha1"
	"hash"
	"runtime"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Verifier hashes every piece on disk into a bitfield. It runs on its own
// goroutine: Run blocks until done, Close aborts mid-run.
type Verifier struct {
	Bitfield *bitfield.Bitfield
	Error    error

	// Skip marks pieces already known-verified (e.g. just re-verified by a
	// swarm merge against this torrent's own hashes): their bit is set
	// without re-reading or re-hashing.
	Skip func(uint32) bool
	// Only restricts the check to accepted pieces; the rest stay unset and
	// unread. For storage created empty this run nothing but pre-written
	// pieces (a merge) can be valid, so hashing the holes is skipped.
	Only func(uint32) bool
	// Prepare runs first on the verifier goroutine for slow preamble work
	// (the merge's reads, hashes and writes) that must stay off the run
	// loop. It may set Skip and Only; stop closes when Close is called.
	Prepare func(stop <-chan struct{})
	// Gentle makes the check give way to the rest of the machine: the reading
	// thread runs at the lowest best-effort I/O priority where the platform
	// has one, a read clearly slower than the check's usual pace (someone else
	// is using the disk) is followed by a pause as long as that read, and the
	// pages of each hashed piece are dropped from the OS cache, so a check of
	// a large library does not push everything else out of memory.
	Gentle bool

	closeC chan struct{}
	doneC  chan struct{}
}

// Progress reports the latest checked piece index (1-based count).
type Progress struct {
	Checked uint32
}

// New prepares a verifier; run it with Run.
func New() *Verifier {
	return &Verifier{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close aborts the run and waits for it to unwind.
func (v *Verifier) Close() {
	close(v.closeC)
	<-v.doneC
}

// Run checks every piece, reporting progress per piece and delivering
// itself on resultC — unless closed first, in which case nobody is sent.
func (v *Verifier) Run(pieces []piece.Piece, progressC chan Progress, resultC chan *Verifier) {
	defer close(v.doneC)
	defer func() {
		select {
		case resultC <- v:
		case <-v.closeC:
		}
	}()

	v.Bitfield = bitfield.New(uint32(len(pieces)))
	if v.Prepare != nil {
		v.Prepare(v.closeC)
		select {
		case <-v.closeC:
			return
		default:
		}
	}
	v.check(pieces, progressC)
}

// readPiece is one piece on its way from the reader to the hasher: its data in
// buf, or skip for a piece already known to be good.
type readPiece struct {
	piece *piece.Piece
	buf   []byte
	skip  bool
}

// check reads the pieces on one goroutine and hashes them on this one, so the
// disk and the CPU work at the same time: a cold check takes as long as the slower
// of the two instead of their sum. Reading stays strictly in order, one piece at a
// time, which is what a hard disk wants, and two buffers are enough: the one being
// read and the one being hashed.
func (v *Verifier) check(pieces []piece.Piece, progressC chan Progress) {
	readC := make(chan readPiece)
	freeC := make(chan []byte, 2)
	// Sized to the widest piece: v2 pieces vary (per-file tails), so the first
	// piece is not necessarily the largest.
	width := widestPiece(pieces)
	freeC <- make([]byte, width)
	freeC <- make([]byte, width)
	stopC := make(chan struct{})
	var readErr error
	go func() {
		defer close(readC)
		readErr = v.readPieces(pieces, readC, freeC, stopC)
	}()
	defer func() {
		close(stopC)
		for range readC { // the reader exits on stopC; wait for it
		}
		if v.Error == nil {
			v.Error = readErr
		}
	}()

	hash := sha1.New()
	for r := range readC {
		if r.skip {
			v.Bitfield.Set(r.piece.Index)
		} else {
			if v.checkPiece(r.piece, r.buf, hash) {
				v.Bitfield.Set(r.piece.Index)
			}
			hash.Reset()
			if v.Gentle {
				dropPieceCache(r.piece)
			}
			freeC <- r.buf[:cap(r.buf)]
		}
		if !v.report(progressC, r.piece.Index+1) {
			return
		}
	}
}

// readPieces feeds the hasher in piece order until the pieces end, a read fails
// or the check stops. A read error ends the check, as it always did.
func (v *Verifier) readPieces(pieces []piece.Piece, readC chan<- readPiece, freeC <-chan []byte, stopC <-chan struct{}) error {
	if v.Gentle {
		// The priority belongs to this OS thread: the goroutine keeps it and
		// exits without unlocking, so the thread is discarded with it and the
		// low priority never leaks to other goroutines.
		runtime.LockOSThread()
		lowerIOPriority()
	}
	var pace readPace
	for i := range pieces {
		p := &pieces[i]
		if v.Only != nil && !v.Only(p.Index) {
			continue
		}
		next := readPiece{piece: p}
		if v.Skip != nil && v.Skip(p.Index) {
			next.skip = true
		} else {
			select {
			case next.buf = <-freeC:
			case <-stopC:
				return nil
			case <-v.closeC:
				return nil
			}
			next.buf = next.buf[:p.Length]
			start := time.Now()
			if _, err := p.Data.ReadAt(next.buf, 0); err != nil {
				return err
			}
			if v.Gentle {
				if wait := pace.after(len(next.buf), time.Since(start)); wait > 0 {
					select {
					case <-time.After(wait):
					case <-stopC:
						return nil
					case <-v.closeC:
						return nil
					}
				}
			}
		}
		select {
		case readC <- next:
		case <-stopC:
			return nil
		case <-v.closeC:
			return nil
		}
	}
	return nil
}

// widestPiece returns the largest piece length for the shared read buffer.
func widestPiece(pieces []piece.Piece) int {
	maxLen := 0
	for _, p := range pieces {
		if int(p.Length) > maxLen {
			maxLen = int(p.Length)
		}
	}
	return maxLen
}

// checkPiece hashes buf: SHA-1 for v1, Merkle verification for v2 pieces. The
// v1 hasher is reused across pieces, so the branch keeps that allocation out
// of the loop instead of calling Piece.Verify.
func (v *Verifier) checkPiece(p *piece.Piece, buf []byte, hash hash.Hash) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, hash)
}

// report publishes one progress step; false means closed meanwhile.
func (v *Verifier) report(progressC chan Progress, checked uint32) bool {
	select {
	case progressC <- Progress{Checked: checked}:
		return true
	case <-v.closeC:
		return false
	}
}

// paceWindow is how many recent reads set the check's usual pace.
const paceWindow = 32

// readPace tells a read slowed by someone else's disk use from the check's
// usual pace: it keeps the time per byte of the last reads and compares each
// new read with their median.
type readPace struct {
	perByte [paceWindow]float64
	n       int
	slow    int // consecutive slow reads
}

// Reads faster than cacheSpeed come from the OS cache and say nothing about
// the disk; reads shorter than minSlowRead are too short to be worth a pause.
// A read is slow when it takes slowFactor times the usual time per byte, and
// the check pauses only from the second slow read in a row: a single one is
// the disk's own jitter (on an idle NVMe drive the read-ahead makes some reads
// several times faster than the rest), while another program keeps the disk
// busy for many reads.
const (
	cacheSpeed  = 4 << 30 // bytes per second
	minSlowRead = 5 * time.Millisecond
	maxPause    = 2 * time.Second
	slowFactor  = 2.5
	slowStreak  = 2
)

// after records one read of size bytes that took d and returns the pause to
// take before the next: as long as the read itself while reads are slow, so
// the check uses at most about half of a disk someone else is using.
func (rp *readPace) after(size int, d time.Duration) time.Duration {
	if size <= 0 || d <= 0 || float64(size)/d.Seconds() > cacheSpeed {
		return 0
	}
	per := d.Seconds() / float64(size)
	usual, known := rp.usual()
	rp.perByte[rp.n%paceWindow] = per
	rp.n++
	if !known || d < minSlowRead || per <= slowFactor*usual {
		rp.slow = 0
		return 0
	}
	if rp.slow++; rp.slow < slowStreak {
		return 0
	}
	if d > maxPause {
		return maxPause
	}
	return d
}

// usual is the median of the recorded times per byte; known is false until
// enough reads were seen.
func (rp *readPace) usual() (float64, bool) {
	k := rp.n
	if k > paceWindow {
		k = paceWindow
	}
	if k < 8 {
		return 0, false
	}
	var tmp [paceWindow]float64
	copy(tmp[:], rp.perByte[:k])
	vals := tmp[:k]
	for i := 1; i < len(vals); i++ { // insertion sort: 32 values at most
		for j := i; j > 0 && vals[j] < vals[j-1]; j-- {
			vals[j], vals[j-1] = vals[j-1], vals[j]
		}
	}
	return vals[k/2], true
}

// cacheDropper is the optional storage file side of a gentle check: the
// pages of a range just hashed leave the OS cache.
type cacheDropper interface {
	DropCache(off, length int64)
}

// dropPieceCache drops the OS cache pages of every file section of p.
func dropPieceCache(p *piece.Piece) {
	for _, sec := range p.Data {
		if sec.Padding {
			continue
		}
		if d, ok := sec.File.(cacheDropper); ok {
			d.DropCache(sec.Offset, sec.Length)
		}
	}
}
