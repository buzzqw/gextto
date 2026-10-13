package torrent

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/verifier"
)

// awaitVerifier drains the progress channel until the verifier delivers its
// result, so a test can inspect the bitfield the merge-and-skip produced.
func awaitVerifier(t *testing.T, tr *torrent) *verifier.Verifier {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-tr.verifierProgressC:
		case ve := <-tr.verifierResultC:
			return ve
		case <-deadline:
			t.Fatal("verifier did not finish")
		}
	}
}

// startResume wires the pieces/bitfield/allocator of a torrent reloaded from
// resume so handleAllocationDone takes the resume branch, then runs it and
// returns the verifier result. The caller has already written or left the
// on-disk data as it wants it.
func startResume(t *testing.T, tr *torrent, resume *bitfield.Bitfield, files []allocator.File, session *Session) *verifier.Verifier {
	t.Helper()
	tr.session = session
	tr.bitfield = resume
	tr.verifierProgressC = make(chan verifier.Progress)
	tr.verifierResultC = make(chan *verifier.Verifier)
	al := &allocator.Allocator{Files: files, HasExisting: true, HasMissing: false}
	tr.allocator = al
	tr.handleAllocationDone(al)
	if tr.verifier == nil {
		t.Fatal("the resume path did not start a merge verifier")
	}
	return awaitVerifier(t, tr)
}

// BenchmarkMergeCandidatesUnrelated measures the cheap filter the resume path
// pays on every restart when nothing can be reused: a field comparison per
// session torrent, no read, no bitfield copy (hasBitfield, not haveBits).
func BenchmarkMergeCandidatesUnrelated(b *testing.B) {
	content := make([]byte, 40000)
	info := testInfoBytes(b, content, 16384)
	dst := &torrent{pieces: piece.NewPieces(info, testFiles(b, b.TempDir(), info))}
	dst.setInfo(info)

	const n = 32
	cands := make([]*Torrent, 0, n)
	for i := 0; i < n; i++ {
		other := make([]byte, 40000+int64(i)*16384)
		oi := testInfoBytes(b, other, 16384)
		o := &torrent{pieces: piece.NewPieces(oi, testFiles(b, b.TempDir(), oi))}
		o.setInfo(oi)
		o.publishLocalData()
		cands = append(cands, &Torrent{torrent: o})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := dst.mergeCandidates(cands); len(got) != 0 {
			b.Fatalf("candidates = %d, want 0", len(got))
		}
	}
}

// TestResumeMergeFillsMissingPieces: a torrent reloaded with a partial bitfield
// runs the swarm merge on the resume path, so it completes from a sibling's
// local data instead of waiting for peers, and the pieces the reloaded bitfield
// already verified are kept without being re-read or re-hashed.
func TestResumeMergeFillsMissingPieces(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, pieceLength*3) // three pieces
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)

	// B holds the full content and reports every piece as verified.
	filesB := testFiles(t, t.TempDir(), info)
	if _, err := filesB[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bfB := bitfield.New(info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		bfB.Set(i)
	}
	b := &torrent{pieces: piece.NewPieces(info, filesB), bitfield: bfB}
	b.setInfo(info)
	b.publishLocalData()

	// A resumes with piece 0 already verified and 1..2 missing. Its storage
	// holds nothing (zeros): if the resume path hashed piece 0 it would clear
	// its bit, so a set bit proves the reloaded pieces were skipped.
	filesA := testFiles(t, t.TempDir(), info)
	a := &torrent{id: "a", pieces: nil}
	a.setInfo(info)
	resume := bitfield.New(info.NumPieces)
	resume.Set(0)

	session := &Session{torrents: map[string]*Torrent{"b": {torrent: b}}}
	ve := startResume(t, a, resume, filesA, session)

	for i := uint32(0); i < info.NumPieces; i++ {
		if !ve.Bitfield.Test(i) {
			t.Fatalf("piece %d not set after the resume merge", i)
		}
	}
	if got := a.mergeOut; got == nil || len(got.pieces) != 2 {
		t.Fatalf("mergeOut = %+v, want pieces 1 and 2 reused", got)
	}
	if _, ok := a.mergeOut.pieces[0]; ok {
		t.Fatal("piece 0 came from resume, the merge must not touch it")
	}
	// The merged pieces are on disk and correct.
	got := make([]byte, 2*pieceLength)
	if _, err := filesA[0].Storage.ReadAt(got, pieceLength); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content[pieceLength:]) {
		t.Fatal("the resumed torrent does not hold the reused pieces")
	}
}

// TestResumeMergeReusesPartialBlocks: on the resume path the merge also
// re-derives the Phase 3 block-level reuse of a destination piece only partly
// covered by a verified source piece, and the reloaded pieces are kept without
// being hashed.
func TestResumeMergeReusesPartialBlocks(t *testing.T) {
	block := merkle.BlockSize
	content := make([]byte, 4*block)
	for i := range content {
		content[i] = byte(i*9 + 2)
	}
	specs := []v2FileSpec{{name: "a.bin", data: content}}

	// Source: one block per piece, only piece 0 verified (blocks 0).
	srcInfo := testV2Info(t, block, specs)
	filesSrc := testFiles(t, t.TempDir(), srcInfo)
	if _, err := filesSrc[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bfB := bitfield.New(srcInfo.NumPieces)
	bfB.Set(0)
	src := &torrent{pieces: piece.NewPieces(srcInfo, filesSrc), bitfield: bfB}
	src.setInfo(srcInfo)
	src.publishLocalData()

	// Destination: two blocks per piece. It resumes with piece 1 verified and
	// piece 0 missing; piece 0 straddles source piece 0 (verified) and 1 (not).
	dstInfo := testV2Info(t, 2*block, specs)
	filesDst := testFiles(t, t.TempDir(), dstInfo)
	dst := &torrent{id: "d"}
	dst.setInfo(dstInfo)
	resume := bitfield.New(dstInfo.NumPieces)
	resume.Set(1)

	session := &Session{torrents: map[string]*Torrent{"s": {torrent: src}}}
	ve := startResume(t, dst, resume, filesDst, session)

	if !ve.Bitfield.Test(1) {
		t.Fatal("the reloaded piece lost its bit (it must not be re-hashed)")
	}
	if ve.Bitfield.Test(0) {
		t.Fatal("the still-incomplete piece was marked verified")
	}
	begins := dst.mergeOut.partial[0]
	if len(begins) != 1 || begins[0] != 0 {
		t.Fatalf("partial blocks of piece 0 = %v, want [0]", begins)
	}
	got := make([]byte, block)
	if _, err := dst.pieces[0].Data.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content[:block]) {
		t.Fatal("the reused block is not on the destination storage")
	}
}

// TestNotifySiblingsOnlyOnNewPieces: a torrent wakes its siblings only when it
// has gained verified pieces, so two incomplete torrents cannot ping each other
// forever with merges that reuse nothing.
func TestNotifySiblingsOnlyOnNewPieces(t *testing.T) {
	b := &torrent{laterMergeC: make(chan struct{}, 1)}
	a := &torrent{session: &Session{torrents: map[string]*Torrent{"b": {torrent: b}}}}
	a.bitfield = bitfield.New(4)

	// Nothing verified: no wake-up.
	a.notifySiblings()
	if len(b.laterMergeC) != 0 {
		t.Fatal("notified with no verified piece")
	}

	a.bitfield.Set(0)
	a.notifySiblings()
	if len(b.laterMergeC) != 1 {
		t.Fatal("no wake-up after the first verified piece")
	}
	<-b.laterMergeC

	// Same count: no repeated wake-up.
	a.notifySiblings()
	if len(b.laterMergeC) != 0 {
		t.Fatal("notified without gaining a piece")
	}

	a.bitfield.Set(1)
	a.notifySiblings()
	if len(b.laterMergeC) != 1 {
		t.Fatal("no wake-up after a new verified piece")
	}
}

// TestLaterMergeRequestQueuesAndDrains: a downloading torrent woken by a
// sibling's new data queues a later merge, waiting for the in-flight piece
// writes to drain first (an adoption during the verifier's bitfield swap would
// be lost), and merges the sibling once it can.
func TestLaterMergeRequestQueuesAndDrains(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, pieceLength*3)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)

	// B holds the full content and reports every piece as verified.
	filesB := testFiles(t, t.TempDir(), info)
	if _, err := filesB[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bfB := bitfield.New(info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		bfB.Set(i)
	}
	b := &torrent{pieces: piece.NewPieces(info, filesB), bitfield: bfB}
	b.setInfo(info)
	b.publishLocalData()

	// A is downloading: a non-nil errC (running), the picker up, empty storage.
	filesA := testFiles(t, t.TempDir(), info)
	a := &torrent{id: "a", errC: make(chan error, 1), pieces: piece.NewPieces(info, filesA), log: logger.New("test")}
	a.setInfo(info)
	a.bitfield = bitfield.New(info.NumPieces)
	a.piecePicker = piecepicker.New(a.pieces, 0, nil, false, false)
	a.verifierProgressC = make(chan verifier.Progress)
	a.verifierResultC = make(chan *verifier.Verifier)
	a.session = &Session{torrents: map[string]*Torrent{"b": {torrent: b}}}

	// With a piece write in flight, the request is queued, not started.
	a.pendingWrites = 1
	a.handleLaterMergeRequest()
	if !a.mergeWaiting || a.verifier != nil {
		t.Fatalf("mergeWaiting=%v verifier=%v, want queued and not running", a.mergeWaiting, a.verifier != nil)
	}
	if len(a.mergePending) != 1 {
		t.Fatalf("mergePending = %d, want 1 candidate", len(a.mergePending))
	}

	// Once the write drains, the merge starts.
	a.pendingWrites = 0
	a.maybeStartDeferredMerge()
	if a.verifier == nil {
		t.Fatal("the merge did not start after the drain")
	}
	ve := awaitVerifier(t, a)
	for i := uint32(0); i < info.NumPieces; i++ {
		if !ve.Bitfield.Test(i) {
			t.Fatalf("piece %d not set after the later merge", i)
		}
	}
}
