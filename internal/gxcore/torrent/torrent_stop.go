package torrent

import (
	"context"

	"github.com/buzzqw/gextto/internal/gxcore/internal/announcer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/rcrowley/go-metrics"
)

// handleStopped lands the Stopping transition: the stopped announcer is
// gone, the waiter gets the terminal error, and a clean stop with a pending
// manual verification restarts into it. A stop that itself failed (missing
// or unwritable files) never restarts — retrying would log thousands of
// errors a second and recreate empty files on every attempt.
func (t *torrent) handleStopped() {
	t.stoppedEventAnnouncer = nil
	t.errC <- t.lastError
	t.errC = nil
	t.portC = nil
	if t.seedModeRecheckAfterStop() {
		return
	}
	if t.doVerify && t.lastError != nil {
		t.doVerify = false
	}
	if t.doVerify {
		t.mBitfield.Lock()
		t.bitfield = nil
		t.mBitfield.Unlock()
		t.start()
	} else {
		t.log.Info("torrent has stopped")
		if t.lastError != nil {
			t.emit(EventTorrentError, t.lastError)
		} else {
			t.emit(EventTorrentStopped, nil)
		}
	}
}

// stopAndSetStoppedOnComplete records the stop-after-download flag, then stops.
func (t *torrent) stopAndSetStoppedOnComplete() {
	if err := t.session.resumer.HandleStopAfterDownload(t.id); err != nil {
		t.log.Errorf("cannot write status to resume db: %s", err)
	}
	t.stop(nil)
}

// stopAndSetStoppedOnMetadata records the stop-after-metadata flag, then stops.
func (t *torrent) stopAndSetStoppedOnMetadata() {
	if err := t.session.resumer.HandleStopAfterMetadata(t.id); err != nil {
		t.log.Errorf("cannot write status to resume db: %s", err)
	}
	t.stop(nil)
}

// stop unwinds a running torrent in dependency order: reject and cancel
// off-loop data work, then drain it; acceptor, peers, downloaders, webseeds;
// the verifier (its check and any merge Prepare read pieces and files, so it
// must be cancelled and joined before anything snapshots its bitfield or drops
// that data); the bitfield persist; periodical announcers before data files (an
// announcing goroutine touching pieces during close is a data race); data
// before the allocator (its IO must be already cancelled); handshakers; meters;
// and finally the stopped-event announcer over the trackers that actually heard
// from us, which drives the Stopping state. Already stopping or stopped
// torrents ignore the call.
func (t *torrent) stop(err error) {
	if s := t.status(); s == Stopping || s == Stopped {
		return
	}
	t.log.Info("stopping torrent")
	t.lastError = err
	if err != nil && err != errClosed {
		t.log.Error(err)
	}

	t.markStopped()
	// No run-loop work can start while stop runs. Cancel queued hashes, tree
	// builds and write-semaphore waits immediately, and do not touch storage
	// until every worker that already touched it has finished.
	t.stopWorkers()
	t.resetStoppedDataWork()
	t.stopAcceptor()
	t.stopPeers()
	t.stopPiecedownloaders()
	t.stopInfoDownloaders()
	t.stopWebseedDownloads()

	// The verifier's Prepare runs the swarm merge off loop: it reads t.pieces
	// and writes through their file sections. Close cancels it and joins both
	// it and the check/reader goroutines, so the bitfield below is stable and
	// closeData cannot drop storage under them.
	t.stopVerifier()

	if t.bitfield != nil {
		_ = t.writeBitfield()
	}

	announcers := t.announcers
	t.stopPeriodicalAnnouncers()

	t.closeData()
	t.stopAllocator()

	t.stopOutgoingHandshakers()
	t.stopIncomingHandshakers()

	t.resetSpeeds()

	trackers := make([]tracker.Tracker, 0, len(announcers))
	for _, an := range announcers {
		if an.HasAnnounced {
			trackers = append(trackers, an.Tracker)
		}
	}
	if t.stoppedEventAnnouncer != nil {
		t.crash("stopped event announcer exists")
	}
	t.stoppedEventAnnouncer = announcer.NewStopAnnouncer(trackers, t.announcerFields(), t.session.config.TrackerStopTimeout, t.announcersStoppedC, t.log)
	go t.stoppedEventAnnouncer.Run()

	t.addrList.Reset()
	// Closed handshakers never report back, so rebuild the connected-IP set
	// from the live peers only — otherwise their IPs stay pinned and are
	// never dialled again.
	t.connectedPeerIPs = make(map[string]struct{}, len(t.peers))
	for pe := range t.peers {
		t.connectedPeerIPs[pe.Conn.IP()] = struct{}{}
	}
}

// torrentDataWork is a comparable registration for one off-loop operation. A
// context.CancelFunc itself is a function and cannot be used as a map key.
type torrentDataWork struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// beginDataWork registers one off-loop operation before its goroutine starts.
// The per-operation context is what a stop cancels; keeping the registration
// and the stopping flag under one lock makes WaitGroup.Add safe against a
// concurrent drain.
func (t *torrent) beginDataWork() (*torrentDataWork, bool) {
	t.workMu.Lock()
	defer t.workMu.Unlock()
	if t.workStopping {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	work := &torrentDataWork{ctx: ctx, cancel: cancel}
	if t.workCancels == nil {
		t.workCancels = make(map[*torrentDataWork]struct{})
	}
	t.workCancels[work] = struct{}{}
	t.workWG.Add(1)
	return work, true
}

// endDataWork retires a worker only after its storage access is over. Cancelling
// here also releases a context that finished before the torrent stopped.
func (t *torrent) endDataWork(work *torrentDataWork) {
	work.cancel()
	t.workMu.Lock()
	delete(t.workCancels, work)
	t.workMu.Unlock()
	t.workWG.Done()
}

// stopWorkers refuses new workers, cancels the registered ones and drains
// them. PieceWriter and the lazy hash/tree loops check their contexts between
// blocking operations; an IO already in progress is allowed to finish before
// closeData can unmap or close its file.
func (t *torrent) stopWorkers() {
	t.workMu.Lock()
	t.workStopping = true
	cancels := make([]*torrentDataWork, 0, len(t.workCancels))
	for work := range t.workCancels {
		cancels = append(cancels, work)
	}
	t.workMu.Unlock()

	for _, work := range cancels {
		work.cancel()
	}
	t.workWG.Wait()

	t.workMu.Lock()
	t.workCancels = nil
	t.workStopping = false
	t.workMu.Unlock()
}

// resetStoppedDataWork clears run-loop bookkeeping for writes and seed hashes
// whose workers were cancelled instead of delivering a result. The next start
// begins with an empty ring and cannot inherit a parked request from a peer
// that is gone.
func (t *torrent) resetStoppedDataWork() {
	t.pendingWrites = 0
	if t.pieceMessagesC != nil {
		t.pieceMessagesC.Resume()
	}
	if t.webseedPieceResultC != nil {
		t.webseedPieceResultC.Resume()
	}
	t.resetSeedHashWork()
}

func (t *torrent) stopAllocator() {
	t.log.Debugln("stopping allocator")
	if t.allocator != nil {
		t.allocator.Close()
		t.allocator = nil
	}
}

func (t *torrent) stopVerifier() {
	t.log.Debugln("stopping verifier")
	if t.verifier == nil {
		return
	}
	v := t.verifier
	// Close cancels the check and the merge Prepare (which reads and writes
	// t.pieces off loop) and waits for the whole run to unwind. Reading the
	// bitfield or dropping the files before that join races with the verifier
	// goroutine, so the snapshot must come after it.
	v.Close()
	// Keep what the interrupted check already proved: on the next start
	// those pieces are skipped instead of being read and hashed again.
	if b := v.Bitfield; b != nil {
		t.verifiedPrefix = append([]byte(nil), b.Bytes()...)
	}
	t.verifier = nil
	t.mergeOut = nil
}

// stopWebseedDownloads closes every webseed downloader and zeroes the
// active counter: their teardown bypasses the piece-result path, so the
// slots would otherwise leak across stop/restart cycles.
func (t *torrent) stopWebseedDownloads() {
	for _, src := range t.webseedSources {
		t.closeWebseedDownloader(src)
	}
	t.webseedActiveDownloads = 0
}

func (t *torrent) resetSpeeds() {
	t.downloadSpeed.Stop()
	t.downloadSpeed = metrics.NilMeter{}
	t.uploadSpeed.Stop()
	t.uploadSpeed = metrics.NilMeter{}
}

func (t *torrent) stopOutgoingHandshakers() {
	t.log.Debugln("stopping outgoing handshakers")
	for oh := range t.outgoingHandshakers {
		oh.Close()
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
	t.holepunchDials = 0
}

func (t *torrent) stopIncomingHandshakers() {
	t.log.Debugln("stopping incoming handshakers")
	for ih := range t.incomingHandshakers {
		ih.Close()
	}
	t.incomingHandshakers = make(map[*incominghandshaker.IncomingHandshaker]struct{})
}

// closeData closes every open file and drops the in-memory piece state.
func (t *torrent) closeData() {
	t.log.Debugln("closing open files")
	for _, f := range t.files {
		if err := f.Storage.Close(); err != nil {
			t.log.Error(err)
		}
	}
	t.files = nil
	t.pieces = nil
	t.piecePicker = nil
	t.bytesAllocated = 0
	t.checkedPieces = 0
}

func (t *torrent) stopPeriodicalAnnouncers() {
	t.log.Debugln("stopping announcers")
	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil
	if t.dhtAnnouncer != nil {
		t.dhtAnnouncer.Close()
		t.dhtAnnouncer = nil
	}
}

func (t *torrent) stopAcceptor() {
	t.log.Debugln("stopping acceptor")
	if t.acceptor != nil {
		t.acceptor.Close()
	}
	t.acceptor = nil
}

func (t *torrent) stopPeers() {
	t.updateUploadClasses(true)
	t.log.Debugln("closing peer connections")
	for p := range t.peers {
		t.closePeer(p)
	}
}

func (t *torrent) stopInfoDownloaders() {
	t.log.Debugln("stopping info downloaders")
	for _, id := range t.infoDownloaders {
		t.closeInfoDownloader(id)
	}
}

func (t *torrent) stopPiecedownloaders() {
	t.log.Debugln("stopping piece downloaders")
	for _, m := range t.pieceDownloaders {
		for _, pd := range m {
			t.closePieceDownloader(pd)
		}
	}
}
