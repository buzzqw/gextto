package torrent

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// run is the torrent event loop: every subsystem (peers, trackers, storage,
// timers) reports here over channels, and the loop mutates the torrent
// state single-threaded. Public API calls arrive as commands carrying their
// own behavior — torrent_commands.go lists what can come through commandC.
func (t *torrent) run() {
	t.seedDurationTicker = time.NewTicker(time.Second)
	defer t.seedDurationTicker.Stop()

	t.unchokeTicker = time.NewTicker(10 * time.Second)
	defer t.unchokeTicker.Stop()

	for {
		select {
		case <-t.closeC:
			t.close()
			close(t.doneC)
			return
		case f := <-t.commandC:
			f()
		case <-t.announcersStoppedC:
			t.handleStopped()
		case p := <-t.allocatorProgressC:
			t.bytesAllocated = p.AllocatedSize
		case al := <-t.allocatorResultC:
			t.handleAllocationDone(al)
		case p := <-t.verifierProgressC:
			t.checkedPieces = p.Checked
		case ve := <-t.verifierResultC:
			t.handleVerificationDone(ve)
		case data := <-t.ramNotifyC:
			data.RAMQueued = false
			t.startSinglePieceDownloader(data)
		case addrs := <-t.addrsFromTrackers:
			t.handleNewPeers(addrs, peersource.Tracker)
		case addrs := <-t.dhtPeersC:
			t.handleNewPeers(addrs, peersource.DHT)
		case conn := <-t.incomingConnC:
			t.handleNewConnection(conn)
		case h := <-t.routedConnC:
			t.handleRoutedConnection(h)
		case res := <-t.webseedPieceResultC.ReceiveC():
			t.handleWebseedPieceResult(res)
		case src := <-t.webseedRetryC:
			t.startPieceDownloaderForWebseed(src)
		case r := <-t.seedHashC:
			t.handleSeedHashDone(r)
		case pw := <-t.pieceWriterResultC:
			t.handlePieceWriteDone(pw)
			t.maybeStartDeferredMerge()
		case <-t.laterMergeC:
			t.handleLaterMergeRequest()
		case now := <-t.seedDurationTicker.C:
			t.updateSeedDuration(now)
		case pe := <-t.peerSnubbedC:
			t.handlePeerSnubbed(pe)
		case <-t.unchokeTicker.C:
			t.prunePeers()
			t.unchoker.TickUnchoke(t.getPeersForUnchoker(), t.completed)
			t.superSeedTick(time.Now())
			t.expireV2HashRequests(time.Now())
		case ih := <-t.incomingHandshakerResultC:
			t.handleIncomingHandshakeDone(ih)
		case oh := <-t.outgoingHandshakerResultC:
			t.handleOutgoingHandshakeDone(oh)
		case pe := <-t.peerDisconnectedC:
			t.closePeer(pe)
		case pm := <-t.pieceMessagesC.ReceiveC():
			t.handlePieceMessage(pm)
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}
