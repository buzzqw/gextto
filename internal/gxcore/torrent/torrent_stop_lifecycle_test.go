package torrent

import (
	"context"
	"crypto/sha1"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/addrlist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/filesection"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/suspendchan"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/verifier"
	"github.com/rcrowley/go-metrics"
	"go.etcd.io/bbolt"
)

// lifecycleWriteFile blocks a piece write until the test releases it. Close is
// observable so the ordering between worker drain and storage teardown is
// checked without relying on scheduler timing alone.
type lifecycleWriteFile struct {
	writeStarted chan struct{}
	releaseWrite chan struct{}
	startOnce    sync.Once
	releaseOnce  sync.Once
	syncStarted  chan struct{}
	syncOnce     sync.Once
	writes       atomic.Int32
	syncs        atomic.Int32
	closed       atomic.Bool
}

func newLifecycleWriteFile() *lifecycleWriteFile {
	return &lifecycleWriteFile{
		writeStarted: make(chan struct{}),
		releaseWrite: make(chan struct{}),
		syncStarted:  make(chan struct{}),
	}
}

func (f *lifecycleWriteFile) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (f *lifecycleWriteFile) WriteAt(p []byte, _ int64) (int, error) {
	f.writes.Add(1)
	f.startOnce.Do(func() { close(f.writeStarted) })
	<-f.releaseWrite
	return len(p), nil
}
func (f *lifecycleWriteFile) Sync() error {
	f.syncs.Add(1)
	f.syncOnce.Do(func() { close(f.syncStarted) })
	return nil
}
func (f *lifecycleWriteFile) Close() error { f.closed.Store(true); return nil }
func (f *lifecycleWriteFile) allowWrite()  { f.releaseOnce.Do(func() { close(f.releaseWrite) }) }

// lifecycleReadFile blocks the first read of a seed hash or v2 tree build.
// Piece sections hold a filesection.ReadWriterAt, so the wrapper embeds that
// interface and forwards Close/Sync when the underlying file is a full
// storage.File.
type lifecycleReadFile struct {
	filesection.ReadWriterAt
	readStarted chan struct{}
	releaseRead chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
}

func newLifecycleReadFile(file filesection.ReadWriterAt) *lifecycleReadFile {
	return &lifecycleReadFile{
		ReadWriterAt: file,
		readStarted:  make(chan struct{}),
		releaseRead:  make(chan struct{}),
	}
}

func (f *lifecycleReadFile) ReadAt(p []byte, off int64) (int, error) {
	f.startOnce.Do(func() { close(f.readStarted) })
	<-f.releaseRead
	return f.ReadWriterAt.ReadAt(p, off)
}

func (f *lifecycleReadFile) allowRead() { f.releaseOnce.Do(func() { close(f.releaseRead) }) }

func (f *lifecycleReadFile) Close() error {
	if c, ok := f.ReadWriterAt.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (f *lifecycleReadFile) Sync() error {
	if s, ok := f.ReadWriterAt.(interface{ Sync() error }); ok {
		return s.Sync()
	}
	return nil
}

func lifecyclePiece(file storage.File, data []byte) piece.Piece {
	sum := sha1.Sum(data)
	return piece.Piece{
		Index:  0,
		Length: uint32(len(data)),
		Hash:   sum[:],
		Data:   filesection.Piece{{File: file, Length: int64(len(data)), Name: "data.bin"}},
	}
}

// newStopLifecycleTorrent is the minimum running-shaped torrent needed by the
// real stop path: no peers, but open data and a resume store for the bitfield
// flush.
func newStopLifecycleTorrent(t *testing.T, file storage.File, p piece.Piece) *torrent {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		config:   Config{TrackerStopTimeout: time.Millisecond},
		semWrite: semaphore.New(1),
		metrics:  &sessionMetrics{WritesPerSecond: metrics.NilMeter{}, SpeedWrite: metrics.NilMeter{}},
		resumer:  res,
	}
	tr := &torrent{
		session:             sess,
		log:                 logger.New("stop lifecycle test"),
		errC:                make(chan error, 1),
		files:               []allocator.File{{Storage: file, Name: "data.bin"}},
		pieces:              []piece.Piece{p},
		pieceMessagesC:      suspendchan.New[peer.PieceMessage](0),
		webseedPieceResultC: suspendchan.New[*urldownloader.PieceResult](0),
		pieceWriterResultC:  make(chan *piecewriter.PieceWriter),
		announcersStoppedC:  make(chan struct{}, 1),
		addrList:            addrlist.New(16, nil, 0, nil),
		downloadSpeed:       metrics.NilMeter{},
		uploadSpeed:         metrics.NilMeter{},
		bytesDownloaded:     metrics.NewCounter(),
		bytesUploaded:       metrics.NewCounter(),
		bytesWasted:         metrics.NewCounter(),
		seededFor:           metrics.NewCounter(),
		connectedPeerIPs:    make(map[string]struct{}),
	}
	t.Cleanup(func() {
		if a := tr.stoppedEventAnnouncer; a != nil {
			a.Close()
		}
	})
	return tr
}

func waitForStopSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitForWorkersStopping(t *testing.T, tr *torrent) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tr.workMu.Lock()
		stopping := tr.workStopping
		tr.workMu.Unlock()
		if stopping {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("off-loop data workers were not put into stopping state")
}

// TestStopDrainsPieceWriterBeforeClosingStorage holds a writer inside WriteAt.
// stop must cancel it, wait for the syscall to return, and only then close the
// file. The cancelled result is released by the writer rather than the absent
// run-loop receiver.
func TestStopDrainsPieceWriterBeforeClosingStorage(t *testing.T) {
	data := []byte("piece payload")
	file := newLifecycleWriteFile()
	t.Cleanup(file.allowWrite)
	p := lifecyclePiece(file, data)
	tr := newStopLifecycleTorrent(t, file, p)
	tr.id = "stop-lifecycle"
	tr.infoPtr.Store(&metainfo.Info{Length: int64(len(data))})
	tr.bitfield = bitfield.New(1)
	pool := bufferpool.NewBounded(len(data), 1)
	buf := pool.GetUncleared(len(data))
	copy(buf.Data, data)
	pw := piecewriter.New(&tr.pieces[0], peerPieceWrite{}, buf)
	tr.beginPieceWrite()
	tr.runPieceWrite(pw)
	waitForStopSignal(t, file.writeStarted, "piece write to start")

	stopped := make(chan struct{})
	go func() {
		tr.stop(nil)
		close(stopped)
	}()
	waitForWorkersStopping(t, tr)
	if file.closed.Load() {
		t.Fatal("storage closed while the piece writer was still running")
	}
	if got := file.syncs.Load(); got != 0 {
		t.Fatalf("storage synced %d times before the writer drained", got)
	}
	select {
	case <-stopped:
		t.Fatal("stop returned before the in-flight write drained")
	default:
	}

	file.allowWrite()
	waitForStopSignal(t, stopped, "torrent stop")
	waitForStopSignal(t, file.syncStarted, "bitfield data sync")
	if !file.closed.Load() {
		t.Fatal("storage was not closed after the writer drained")
	}
	if got := file.writes.Load(); got != 1 {
		t.Fatalf("writes = %d, want 1", got)
	}
	if got := file.syncs.Load(); got != 1 {
		t.Fatalf("syncs before bitfield persistence = %d, want 1", got)
	}
	if tr.pieces != nil || tr.files != nil {
		t.Fatal("stop did not release the piece and file state")
	}
	if tr.pendingWrites != 0 {
		t.Fatalf("pendingWrites = %d after stop, want 0", tr.pendingWrites)
	}
	if got := pool.Retained(); got != 1 {
		t.Fatalf("released buffers = %d, want 1", got)
	}
}

// TestStopCancelsQueuedPieceWriter occupies the session write slot so the
// writer cannot reach storage. stop must cancel the semaphore wait instead of
// waiting for an unrelated writer forever.
func TestStopCancelsQueuedPieceWriter(t *testing.T) {
	data := []byte("queued payload")
	file := newLifecycleWriteFile()
	p := lifecyclePiece(file, data)
	tr := newStopLifecycleTorrent(t, file, p)
	tr.session.semWrite.Wait()
	defer tr.session.semWrite.Signal()

	pool := bufferpool.NewBounded(len(data), 1)
	buf := pool.GetUncleared(len(data))
	copy(buf.Data, data)
	pw := piecewriter.New(&tr.pieces[0], peerPieceWrite{}, buf)
	tr.beginPieceWrite()
	tr.runPieceWrite(pw)

	deadline := time.Now().Add(2 * time.Second)
	for tr.session.semWrite.Waiting() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := tr.session.semWrite.Waiting(); got != 1 {
		t.Fatalf("queued writers = %d, want 1", got)
	}

	stopped := make(chan struct{})
	go func() {
		tr.stop(nil)
		close(stopped)
	}()
	waitForStopSignal(t, stopped, "stop with a queued writer")
	if !errors.Is(pw.Error, context.Canceled) {
		t.Fatalf("writer error = %v, want context.Canceled", pw.Error)
	}
	if got := file.writes.Load(); got != 0 {
		t.Fatalf("queued writer reached storage %d times", got)
	}
	if !file.closed.Load() {
		t.Fatal("storage was not closed after the queued writer cancelled")
	}
	if got := pool.Retained(); got != 1 {
		t.Fatalf("released buffers = %d, want 1", got)
	}
}

// TestStopWorkersCancelsAndDrainsSeedHash blocks a lazy seed hash in ReadAt.
// Cancellation must refuse another hash, wait for the active read, and never
// deliver a result after the piece slice has been dropped.
func TestStopWorkersCancelsAndDrainsSeedHash(t *testing.T) {
	content := []byte("seed mode payload")
	pieces := seedModePieces(t, content, len(content))
	file := newLifecycleReadFile(pieces[0].Data[0].File)
	t.Cleanup(file.allowRead)
	pieces[0].Data[0].File = file
	tr := &torrent{
		pieces:       pieces,
		seedMode:     true,
		seedVerified: bitfield.New(1),
		seedPending:  make(map[uint32][]seedModeRequest),
		seedParked:   make(map[*peer.Peer]int),
		seedHashing:  make(map[uint32]struct{}),
		seedHashC:    make(chan seedHashResult, 1),
		closeC:       make(chan struct{}),
	}
	tr.startSeedHash(0)
	waitForStopSignal(t, file.readStarted, "seed hash read to start")

	stopped := make(chan struct{})
	go func() {
		tr.stopWorkers()
		close(stopped)
	}()
	waitForWorkersStopping(t, tr)
	tr.startSeedHash(0)
	if got := len(tr.seedHashing); got != 1 {
		t.Fatalf("seed hashes after stop began = %d, want only the active one", got)
	}
	// closeData clears this field. The worker retained its own piece pointer,
	// so unwinding after cancellation must not follow t.pieces.
	tr.pieces = nil
	select {
	case <-stopped:
		t.Fatal("worker drain returned before the active seed read finished")
	default:
	}

	file.allowRead()
	waitForStopSignal(t, stopped, "seed hash worker drain")
	select {
	case result := <-tr.seedHashC:
		t.Fatalf("cancelled seed hash delivered a result: %+v", result)
	default:
	}
}

// TestStopWorkersCancelsV2BlockBuildWithoutPieces gives a v2 tree build a local
// piece snapshot, then clears t.pieces while its first read is blocked. The
// cancelled build must finish and release all cache bookkeeping without a nil
// dereference or a failure cooldown.
func TestStopWorkersCancelsV2BlockBuildWithoutPieces(t *testing.T) {
	tr, root := v2ServeFixture(t, 0, 1, 2, 3, 4, 5)
	file := newLifecycleReadFile(tr.pieces[0].Data[0].File)
	t.Cleanup(file.allowRead)
	tr.pieces[0].Data[0].File = file
	if tree := tr.v2BlockTree(0); tree != nil {
		t.Fatal("a missing block tree must start a background build")
	}
	waitForStopSignal(t, file.readStarted, "v2 block-tree read to start")

	stopped := make(chan struct{})
	go func() {
		tr.stopWorkers()
		close(stopped)
	}()
	waitForWorkersStopping(t, tr)
	tr.pieces = nil
	select {
	case <-stopped:
		t.Fatal("v2 build drain returned before the active read finished")
	default:
	}

	file.allowRead()
	waitForStopSignal(t, stopped, "v2 block-tree worker drain")
	tr.v2BlockMu.Lock()
	defer tr.v2BlockMu.Unlock()
	key := string(root[:])
	if tr.v2BlockBuilds != 0 || tr.v2BlockBusy[key] || tr.v2BlockBytes != 0 {
		t.Fatalf("v2 build bookkeeping after cancel: builds=%d busy=%v bytes=%d", tr.v2BlockBuilds, tr.v2BlockBusy[key], tr.v2BlockBytes)
	}
	if _, failed := tr.v2BlockFailed[key]; failed {
		t.Fatal("a cancelled v2 build must not leave a failure cooldown")
	}
}

func TestHashStoredPieceHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := hashStoredPieceContext(ctx, &piece.Piece{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("hash error = %v, want context.Canceled", err)
	}
}

// TestStopJoinsVerifierBeforeSnapshotAndCloseData keeps a merge verifier's
// Prepare inside its off-loop read/write window while stop runs. Stop must
// cancel and join that verifier (and its check) before it snapshots the
// bitfield or drops the pieces and files the merge was using.
func TestStopJoinsVerifierBeforeSnapshotAndCloseData(t *testing.T) {
	data := []byte("merge payload")
	file := newLifecycleWriteFile()
	p := lifecyclePiece(file, data)
	tr := newStopLifecycleTorrent(t, file, p)
	tr.id = "stop-verifier"
	tr.infoPtr.Store(&metainfo.Info{Length: int64(len(data))})
	tr.bitfield = bitfield.New(1)

	v := verifier.New()
	prepareStarted := make(chan struct{})
	releasePrepare := make(chan struct{})
	var piecesLive, filesLive atomic.Bool
	v.Prepare = func(stop <-chan struct{}) {
		close(prepareStarted)
		<-releasePrepare // deliberately ignore stop: Close must join us
		piecesLive.Store(len(tr.pieces) > 0)
		filesLive.Store(len(tr.files) > 0)
		v.Bitfield.Set(0) // the final bit stop has to snapshot
	}
	resultC := make(chan *verifier.Verifier, 1)
	tr.verifier = v
	go v.Run(tr.pieces, make(chan verifier.Progress, 1), resultC)
	waitForStopSignal(t, prepareStarted, "merge Prepare to start")

	stopped := make(chan struct{})
	go func() {
		tr.stop(nil)
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stop returned while the merge verifier was still writing data")
	default:
	}

	close(releasePrepare)
	waitForStopSignal(t, stopped, "torrent stop")

	if !piecesLive.Load() || !filesLive.Load() {
		t.Fatal("stop dropped pieces/files before the merge verifier joined")
	}
	if !verifiedPrefixSkip(tr.verifiedPrefix)(0) {
		t.Fatal("stop did not snapshot the bitfield finished by the verifier")
	}
	if tr.verifier != nil || tr.pieces != nil || tr.files != nil {
		t.Fatal("stop did not release the verifier and data state")
	}
}
