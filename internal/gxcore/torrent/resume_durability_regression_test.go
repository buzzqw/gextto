package torrent

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/logging"
	"github.com/rcrowley/go-metrics"
	"go.etcd.io/bbolt"
)

var errDurabilityInjectedSync = errors.New("injected sync failure")

// durabilitySyncFile is a storage.File whose Sync outcome is controlled by the
// regression tests.
type durabilitySyncFile struct {
	syncs int
	err   error
}

func (f *durabilitySyncFile) ReadAt([]byte, int64) (int, error)      { return 0, io.EOF }
func (f *durabilitySyncFile) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (f *durabilitySyncFile) Close() error                           { return nil }
func (f *durabilitySyncFile) Sync() error {
	f.syncs++
	return f.err
}

func newDurabilityResumeStore(t *testing.T) (*bbolt.DB, *boltdbresumer.Resumer) {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		t.Fatal(err)
	}
	return db, res
}

func durabilityTestBitfield(t *testing.T, raw []byte) *bitfield.Bitfield {
	t.Helper()
	bf, err := bitfield.NewBytes(raw, 8)
	if err != nil {
		t.Fatal(err)
	}
	return bf
}

func TestResumeDurabilitySyncPiecesReturnsErrorAndFlushesEveryFile(t *testing.T) {
	failed := &durabilitySyncFile{err: errDurabilityInjectedSync}
	flushed := &durabilitySyncFile{}
	tr := &torrent{files: []allocator.File{
		{Name: "broken", Storage: failed},
		{Name: "healthy", Storage: flushed},
	}}

	err := tr.syncPieces()
	if !errors.Is(err, errDurabilityInjectedSync) {
		t.Fatalf("syncPieces error = %v, want injected sync failure", err)
	}
	if failed.syncs != 1 || flushed.syncs != 1 {
		t.Fatalf("syncs = %d/%d, want 1/1", failed.syncs, flushed.syncs)
	}
	if tr.syncingSince.Load() != 0 {
		t.Fatal("syncPieces left the durability probe armed after an error")
	}
}

func TestResumeDurabilityWriteBitfieldKeepsPreviousRecordOnSyncFailure(t *testing.T) {
	_, res := newDurabilityResumeStore(t)
	const id = "durability-write-bitfield"
	original := &boltdbresumer.Spec{
		InfoHash:        make([]byte, 20),
		Port:            6881,
		Name:            "durability",
		Bitfield:        []byte{0x80},
		AddedAt:         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		BytesDownloaded: 1,
		BytesUploaded:   2,
		BytesWasted:     3,
		SeededFor:       4 * time.Second,
		Started:         true,
	}
	if err := res.Write(id, original); err != nil {
		t.Fatal(err)
	}
	before, err := res.Read(id)
	if err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	log := logger.New("durability-write-bitfield")
	log.SetHandler(logging.NewWriterHandler(&logs))
	failed := &durabilitySyncFile{err: errDurabilityInjectedSync}
	flushed := &durabilitySyncFile{}
	tr := &torrent{
		id:       id,
		session:  &Session{resumer: res},
		log:      log,
		bitfield: durabilityTestBitfield(t, []byte{0x40}),
		files: []allocator.File{
			{Name: "broken", Storage: failed},
			{Name: "healthy", Storage: flushed},
		},
	}

	err = tr.writeBitfield()
	if !errors.Is(err, errDurabilityInjectedSync) {
		t.Fatalf("writeBitfield error = %v, want injected sync failure", err)
	}
	after, err := res.Read(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed sync changed the resume record:\nbefore: %+v\nafter:  %+v", before, after)
	}
	if failed.syncs != 1 || flushed.syncs != 1 {
		t.Fatalf("syncs = %d/%d, want 1/1", failed.syncs, flushed.syncs)
	}
	message := logs.String()
	for _, want := range []string{"cannot write bitfield to resume db", "could not be synced", errDurabilityInjectedSync.Error()} {
		if !strings.Contains(message, want) {
			t.Fatalf("writeBitfield log %q does not contain %q", message, want)
		}
	}
}

// newDurabilityQueryTorrent supplies just enough of a run loop for updateStats
// to take its snapshot through the production query helper.
func newDurabilityQueryTorrent(t *testing.T, session *Session, id string, raw []byte, files []allocator.File) *torrent {
	t.Helper()
	tr := &torrent{
		id:              id,
		session:         session,
		log:             logger.New("durability-stats-" + id),
		commandC:        make(chan func()),
		closeC:          make(chan struct{}),
		doneC:           make(chan struct{}),
		bitfield:        durabilityTestBitfield(t, raw),
		files:           files,
		bytesDownloaded: metrics.NewCounter(),
		bytesUploaded:   metrics.NewCounter(),
		bytesWasted:     metrics.NewCounter(),
		seededFor:       metrics.NewCounter(),
	}
	go func() {
		defer close(tr.doneC)
		for {
			select {
			case f := <-tr.commandC:
				f()
			case <-tr.closeC:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(tr.closeC)
		<-tr.doneC
	})
	return tr
}

func TestResumeDurabilityUpdateStatsWithholdsOnlyTheUnsyncedBitfield(t *testing.T) {
	db, res := newDurabilityResumeStore(t)
	const (
		failedID  = "durability-stats-failed"
		healthyID = "durability-stats-healthy"
	)
	for _, id := range []string{failedID, healthyID} {
		if err := res.Write(id, &boltdbresumer.Spec{
			InfoHash:        make([]byte, 20),
			Port:            6881,
			Name:            id,
			Bitfield:        []byte{0x80},
			BytesDownloaded: 1,
			BytesUploaded:   2,
			BytesWasted:     3,
			SeededFor:       4 * time.Second,
		}); err != nil {
			t.Fatal(err)
		}
	}

	session := &Session{
		db:       db,
		resumer:  res,
		log:      logger.New("durability-stats"),
		torrents: make(map[string]*Torrent),
	}
	failedFile := &durabilitySyncFile{err: errDurabilityInjectedSync}
	flushedFile := &durabilitySyncFile{}
	failed := newDurabilityQueryTorrent(t, session, failedID, []byte{0x40}, []allocator.File{
		{Name: "broken", Storage: failedFile},
		{Name: "healthy", Storage: flushedFile},
	})
	healthyFile := &durabilitySyncFile{}
	healthy := newDurabilityQueryTorrent(t, session, healthyID, []byte{0x20}, []allocator.File{
		{Name: "healthy", Storage: healthyFile},
	})
	for _, tr := range []*torrent{failed, healthy} {
		tr.bytesDownloaded.Inc(101)
		tr.bytesUploaded.Inc(102)
		tr.bytesWasted.Inc(103)
		tr.seededFor.Inc(int64(7 * time.Second))
		session.torrents[tr.id] = &Torrent{torrent: tr}
	}

	session.updateStats()

	failedSpec, err := res.Read(failedID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(failedSpec.Bitfield, []byte{0x80}) {
		t.Fatalf("unsynced bitfield = %x, want previous 80", failedSpec.Bitfield)
	}
	healthySpec, err := res.Read(healthyID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(healthySpec.Bitfield, []byte{0x20}) {
		t.Fatalf("healthy bitfield = %x, want new 20", healthySpec.Bitfield)
	}
	for _, tr := range []*torrent{failed, healthy} {
		spec, err := res.Read(tr.id)
		if err != nil {
			t.Fatal(err)
		}
		if spec.BytesDownloaded != 101 || spec.BytesUploaded != 102 || spec.BytesWasted != 103 || spec.SeededFor != 7*time.Second {
			t.Fatalf("torrent %s counters = %d/%d/%d/%s, want 101/102/103/7s",
				tr.id, spec.BytesDownloaded, spec.BytesUploaded, spec.BytesWasted, spec.SeededFor)
		}
	}
	if failedFile.syncs != 1 || flushedFile.syncs != 1 || healthyFile.syncs != 1 {
		t.Fatalf("syncs = %d/%d/%d, want 1/1/1", failedFile.syncs, flushedFile.syncs, healthyFile.syncs)
	}
}

func TestResumeDurabilityLoadDropsMalformedBitfieldForRehash(t *testing.T) {
	var logs bytes.Buffer
	log := logger.New("durability-load")
	log.SetHandler(logging.NewWriterHandler(&logs))
	session := &Session{log: log}

	if got := session.loadResumeBitfield("durability-load", []byte{0x80, 0x00}, 8); got != nil {
		t.Fatalf("malformed bitfield was adopted: %x", got.Bytes())
	}
	if !strings.Contains(logs.String(), "ignoring invalid resume bitfield") {
		t.Fatalf("malformed bitfield was not reported: %q", logs.String())
	}
	if got := session.loadResumeBitfield("durability-load", nil, 8); got != nil {
		t.Fatalf("empty bitfield = %v, want nil", got)
	}
}

func TestResumeDurabilityLoadKeepsValidBitfieldAndClearsPaddingBits(t *testing.T) {
	session := &Session{log: logger.New("durability-load")}
	got := session.loadResumeBitfield("durability-load", []byte{0xff}, 5)
	if got == nil {
		t.Fatal("valid bitfield was discarded")
	}
	if got.Len() != 5 || !bytes.Equal(got.Bytes(), []byte{0xf8}) {
		t.Fatalf("loaded bitfield = %d bits %x, want 5 bits f8", got.Len(), got.Bytes())
	}
}
