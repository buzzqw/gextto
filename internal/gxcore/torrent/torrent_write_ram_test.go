package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resourcemanager"
	"github.com/buzzqw/gextto/internal/gxcore/internal/suspendchan"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/rcrowley/go-metrics"
)

// testWritePieceLength is a small stand-in for the real piece length: the
// budget arithmetic is per piece, so the size only has to be big enough to
// tell the pieces apart.
const testWritePieceLength = 1 << 16

// newWriteBudgetTorrent builds the minimum a piece write needs: a session RAM
// budget of cap bytes, a torrent of n pieces and the write-ring channels. No
// run loop starts: the test drives the settle path directly.
func newWriteBudgetTorrent(t *testing.T, cap int64, n int) (*torrent, *Session, *bufferpool.Pool) {
	t.Helper()
	sess := &Session{
		ram:     resourcemanager.New[*peer.Peer](cap),
		metrics: &sessionMetrics{WritesPerSecond: metrics.NilMeter{}, SpeedWrite: metrics.NilMeter{}},
	}
	t.Cleanup(sess.ram.Close)
	pool := bufferpool.New(testWritePieceLength)
	tr := &torrent{
		session:             sess,
		pieceMessagesC:      suspendchan.New[peer.PieceMessage](0),
		webseedPieceResultC: suspendchan.New[*urldownloader.PieceResult](0),
		pieceWriterResultC:  make(chan *piecewriter.PieceWriter),
		pieceDownloaders:    make(map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader),
		doneC:               make(chan struct{}),
	}
	tr.infoPtr.Store(&metainfo.Info{PieceLength: testWritePieceLength})
	for i := 0; i < n; i++ {
		tr.pieces = append(tr.pieces, piece.Piece{Index: uint32(i), Length: testWritePieceLength})
	}
	return tr, sess, pool
}

// chargePiece opens a piece downloader the way startPieceDownloaderFor does:
// one full piece length reserved in the session budget, plus the buffer the
// downloader takes when its first block lands.
func chargePiece(t *testing.T, tr *torrent, pool *bufferpool.Pool, pe *peer.Peer, index uint32) *piecedownloader.PieceDownloader {
	t.Helper()
	units := int64(tr.info().PieceLength)
	if ok := tr.session.ram.Request(tr.id, pe, units, nil, nil); !ok {
		t.Fatalf("piece #%d: RAM request was queued, the test budget is too small", index)
	}
	pd := &piecedownloader.PieceDownloader{Piece: &tr.pieces[index], Peer: pe}
	pd.Buffer = pool.GetUncleared(int(pd.Piece.Length))
	if tr.pieceDownloaders[pe] == nil {
		tr.pieceDownloaders[pe] = make(map[uint32]*piecedownloader.PieceDownloader)
	}
	tr.pieceDownloaders[pe][index] = pd
	pe.ConcurrentPieces++
	pe.Downloading = true
	return pd
}

// waitedAllocated polls the budget until it reaches want or the deadline
// passes: the close-path refund happens on the writer's goroutine.
func waitedAllocated(t *testing.T, sess *Session, want int64, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := sess.ram.Stats().AllocatedSize; got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: allocated = %d, want %d", what, sess.ram.Stats().AllocatedSize, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestWriteCacheBudgetHoldsUntilBufferReleased pins the invariant the write
// ring used to break: a piece handed to a writer keeps its write-cache
// reservation until its buffer is really released, so maxPendingWrites piece
// buffers per torrent can never sit outside the budget. It covers the handoff,
// the refund on success/hash-failure/write-error (all through
// releaseWrittenPiece), the immediate refund of a downloader whose buffer dies
// with it, and the stopped-torrent case that must return to zero.
func TestWriteCacheBudgetHoldsUntilBufferReleased(t *testing.T) {
	const pieces = maxPendingWrites
	cap := int64(pieces) * testWritePieceLength
	tr, sess, pool := newWriteBudgetTorrent(t, cap, pieces+1)
	pe := &peer.Peer{}

	// Every write slot busy: charged at open, then handed over to the writer.
	writers := make([]*piecewriter.PieceWriter, 0, pieces)
	for i := 0; i < pieces; i++ {
		pd := chargePiece(t, tr, pool, pe, uint32(i))
		tr.handOffPieceDownloader(pd)
		tr.pieces[i].Writing = true
		tr.beginPieceWrite()
		writers = append(writers, piecewriter.New(&tr.pieces[i], peerPieceWrite{peer: pe, units: tr.pieceRAMUnits()}, pd.Buffer))
	}
	if got := sess.ram.Stats().AllocatedSize; got != cap {
		t.Fatalf("allocated after %d handoffs = %d, want the full %d still held", pieces, got, cap)
	}
	if got := sess.ram.Stats().AllocatedObjects; got != pieces {
		t.Fatalf("allocated objects = %d, want %d", got, pieces)
	}
	if got := tr.pendingWrites; got != maxPendingWrites {
		t.Fatalf("pendingWrites = %d, want %d", got, maxPendingWrites)
	}

	// The writes settle: buffer back to the pool first, charge second.
	for i, pw := range writers {
		pw.HashOK = i%2 == 0 // success and hash failure take the same path
		pw.Error = nil
		tr.releaseWrittenPiece(pw)
	}
	if got := sess.ram.Stats().AllocatedSize; got != 0 {
		t.Fatalf("allocated after the writes settled = %d, want 0", got)
	}

	// A downloader whose buffer dies with it (choke, cancel, peer loss) still
	// refunds at once: that is the stopped-torrent path.
	pd := chargePiece(t, tr, pool, pe, pieces)
	if got := sess.ram.Stats().AllocatedSize; got != testWritePieceLength {
		t.Fatalf("allocated with one open downloader = %d, want %d", got, testWritePieceLength)
	}
	tr.closePieceDownloader(pd)
	if got := sess.ram.Stats().AllocatedSize; got != 0 {
		t.Fatalf("allocated after the downloader closed = %d, want 0 (a stopped torrent returns to zero)", got)
	}

	// The tail piece is shorter than a piece length but reserved a whole one:
	// refunding its own length would drift the budget, so the reservation must
	// be the unit on both sides.
	tr.pieces[pieces].Length = 1000
	tail := chargePiece(t, tr, pool, pe, pieces)
	tr.handOffPieceDownloader(tail)
	tr.pieces[pieces].Writing = true
	tr.beginPieceWrite()
	tr.releaseWrittenPiece(piecewriter.New(&tr.pieces[pieces], peerPieceWrite{peer: pe, units: tr.pieceRAMUnits()}, tail.Buffer))
	if got := sess.ram.Stats().AllocatedSize; got != 0 {
		t.Fatalf("allocated after the tail piece settled = %d, want 0", got)
	}
}

// TestHandlePieceWriteDoneRefundsOnAdopt drives the real settle path: a
// verified piece returns its buffer to the pool and its reservation to the
// budget in the same step, and the write slot frees with them.
func TestHandlePieceWriteDoneRefundsOnAdopt(t *testing.T) {
	tr, sess, pool := newWriteBudgetTorrent(t, testWritePieceLength, 2)
	tr.bitfield = bitfield.New(2)
	pe := &peer.Peer{}
	pd := chargePiece(t, tr, pool, pe, 0)
	tr.handOffPieceDownloader(pd)
	tr.pieces[0].Writing = true
	tr.beginPieceWrite()

	pw := piecewriter.New(&tr.pieces[0], peerPieceWrite{peer: pe, units: tr.pieceRAMUnits()}, pd.Buffer)
	pw.HashOK = true
	tr.handlePieceWriteDone(pw)

	if got := sess.ram.Stats().AllocatedSize; got != 0 {
		t.Fatalf("allocated after the piece settled = %d, want 0", got)
	}
	if tr.pendingWrites != 0 {
		t.Fatalf("pendingWrites = %d, want 0", tr.pendingWrites)
	}
	if !tr.pieces[0].Done || !tr.bitfield.Test(0) {
		t.Fatal("the verified piece was not adopted")
	}
}

// TestWriteCacheBudgetReturnedWhenTorrentClosesWithWriteInFlight: a torrent
// removed while a piece is still being written must not keep its reservation
// forever. The run loop is gone, so the writer's forwarding goroutine releases
// the buffer and the charge itself.
func TestWriteCacheBudgetReturnedWhenTorrentClosesWithWriteInFlight(t *testing.T) {
	tr, sess, pool := newWriteBudgetTorrent(t, testWritePieceLength, 1)
	pe := &peer.Peer{}
	pd := chargePiece(t, tr, pool, pe, 0)
	tr.handOffPieceDownloader(pd)
	tr.pieces[0].Writing = true
	tr.beginPieceWrite()
	close(tr.doneC) // the run loop has already exited

	pw := piecewriter.New(&tr.pieces[0], peerPieceWrite{peer: pe, units: tr.pieceRAMUnits()}, pd.Buffer)
	tr.runPieceWrite(pw)
	waitedAllocated(t, sess, 0, "closed torrent with a write in flight")
}

// TestWebseedWriteRefundsItsCharge: a web seed's buffer now takes a charge of
// its own, and settling its write returns exactly that charge: the peer's
// reservation stays untouched and the budget comes back whole.
func TestWebseedWriteRefundsItsCharge(t *testing.T) {
	tr, sess, pool := newWriteBudgetTorrent(t, 2*testWritePieceLength, 1)
	pe := &peer.Peer{}
	chargePiece(t, tr, pool, pe, 0) // one piece held by a peer

	units := int64(testWritePieceLength)
	if ok := sess.ram.Request(tr.id, nil, units, nil, nil); !ok {
		t.Fatalf("the webseed charge was refused")
	}
	buf := pool.GetUncleared(testWritePieceLength)
	tr.releaseWrittenPiece(piecewriter.New(&tr.pieces[0], webseedPieceWrite{downloader: &urldownloader.URLDownloader{URL: "http://seed.example/piece"}, units: units}, buf))
	if got := sess.ram.Stats().AllocatedSize; got != testWritePieceLength {
		t.Fatalf("allocated after a webseed write = %d, want only the peer reservation (%d)", got, testWritePieceLength)
	}
}

// TestStaleWebseedPieceReleasesItsCharge: a webseed result for a piece another
// copy already completed drops its buffer, and the budget with it.
func TestStaleWebseedPieceReleasesItsCharge(t *testing.T) {
	tr, sess, pool := newWriteBudgetTorrent(t, 2*testWritePieceLength, 1)
	tr.log = logger.New("test")
	tr.bytesWasted = metrics.NewCounter()
	units := int64(testWritePieceLength)
	if ok := sess.ram.Request(tr.id, nil, units, nil, nil); !ok {
		t.Fatalf("the webseed charge was refused")
	}
	tr.pieces[0].Done = true
	buf := pool.GetUncleared(testWritePieceLength)
	tr.discardStaleWebseedPiece(&urldownloader.PieceResult{
		Downloader: &urldownloader.URLDownloader{URL: "http://seed.example/piece"},
		Buffer:     buf, Index: 0, Units: units,
	})
	if got := sess.ram.Stats().AllocatedSize; got != 0 {
		t.Fatalf("allocated after a stale webseed piece = %d, want 0", got)
	}
}
