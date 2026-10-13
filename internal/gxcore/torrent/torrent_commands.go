package torrent

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/magnet"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"go.etcd.io/bbolt"
)

// ---------------------------------------------------------------------------
// Run-loop plumbing: every public method below executes on the torrent's own
// goroutine through these two helpers.
// ---------------------------------------------------------------------------

// sendCommand runs f on the run loop, reporting false when the torrent
// closed first.
func (t *torrent) sendCommand(f func()) bool {
	select {
	case t.commandC <- f:
		return true
	case <-t.closeC:
		return false
	}
}

// query runs f on the run loop and returns its result, or the zero value
// when the torrent closed first. Free function (not a method) because
// methods cannot introduce type parameters. The buffered reply channel keeps
// the run loop from ever waiting on the caller.
func query[T any](t *torrent, f func() T) T {
	var zero T
	resp := make(chan T, 1)
	if !t.sendCommand(func() { resp <- f() }) {
		return zero
	}
	select {
	case v := <-resp:
		return v
	case <-t.closeC:
		return zero
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start downloads (seeding continues after completion until Stop).
func (t *torrent) Start() {
	t.sendCommand(t.start)
}

// Stop halts downloading and seeding, closing every peer connection.
func (t *torrent) Stop() {
	t.sendCommand(func() { t.stop(nil) })
}

// Announce asks trackers and DHT for peers right now.
func (t *torrent) Announce() {
	t.sendCommand(func() { t.setNeedMorePeers(true) })
}

// Verify re-checks the files against the piece hashes.
func (t *torrent) Verify() {
	t.sendCommand(t.handleVerifyCommand)
}

// Close releases every resource. Call it before dropping the torrent.
func (t *torrent) Close() {
	close(t.closeC)
	<-t.doneC
}

// NotifyClose fires when the torrent closes.
func (t *torrent) NotifyClose() <-chan struct{} {
	return t.closeC
}

// NotifyComplete fires on download completion.
func (t *torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

// NotifyMetadata fires once the info dict arrives.
func (t *torrent) NotifyMetadata() <-chan struct{} {
	return t.completeMetadataC
}

// NotifyError carries the stopping error. Read it on the run loop: the
// channel exists only while running (start creates it, stop drops it).
func (t *torrent) NotifyError() <-chan error {
	return query(t, func() chan error { return t.errC })
}

// NotifyListen fires with the peer port once listening. Call after Start.
func (t *torrent) NotifyListen() <-chan int {
	return query(t, func() chan int { return t.portC })
}

// ---------------------------------------------------------------------------
// Data access
// ---------------------------------------------------------------------------

// Magnet exports the magnet link. Private torrents refuse (they must not
// leak into the DHT swarm).
func (t *torrent) Magnet() (string, error) {
	if t.info() != nil && t.info().Private {
		return "", errors.New("torrent is private")
	}
	m := magnet.Magnet{
		InfoHash: t.infoHash,
		Name:     t.Name(),
		Trackers: t.getTieredTrackers(),
		Peers:    t.fixedPeers,
	}
	return m.String(), nil
}

// Torrent exports the .torrent, preserving layers and BEP 38 hints.
func (t *torrent) Torrent() ([]byte, error) {
	if t.info() == nil {
		return nil, errors.New("torrent metadata not ready")
	}
	webseeds := make([]string, len(t.webseedSources))
	for i, ws := range t.webseedSources {
		webseeds[i] = ws.URL
	}
	return metainfo.NewBytesWithHints(t.info().Bytes, t.getTieredTrackers(), webseeds, t.pieceLayers, "", t.info().Collections, t.info().Similar)
}

// getTieredTrackers groups tracker URLs back into tiers.
func (t *torrent) getTieredTrackers() [][]string {
	var trackers [][]string
	for _, tr := range t.trackers {
		if tier, ok := tr.(*tracker.Tier); ok {
			urls := make([]string, len(tier.Trackers))
			for i, tt := range tier.Trackers {
				urls[i] = tt.URL()
			}
			trackers = append(trackers, urls)
		} else {
			trackers = append(trackers, []string{tr.URL()})
		}
	}
	return trackers
}

// Stats snapshots the torrent.
func (t *torrent) Stats() Stats {
	return query(t, t.stats)
}

// ---------------------------------------------------------------------------
// Swarm membership
// ---------------------------------------------------------------------------

// AddPeers queues manual peers.
func (t *torrent) AddPeers(peers []*net.TCPAddr) {
	t.sendCommand(func() { t.handleNewPeers(peers, peersource.Manual) })
}

// AddTrackers announces to additional trackers.
func (t *torrent) AddTrackers(trackers []tracker.Tracker) {
	t.sendCommand(func() { t.handleNewTrackers(trackers) })
}

// unlimitedUnchokedPeers emulates "unlimited" upload slots.
const unlimitedUnchokedPeers = 1000

// applyMaxUploads maps maxUploads onto unchoker slots (zero = everyone).
// Runs on the torrent goroutine.
func (t *torrent) applyMaxUploads() {
	if t.unchoker == nil {
		return
	}
	if t.maxUploads == 0 {
		t.unchoker.SetNumUnchoked(unlimitedUnchokedPeers)
		return
	}
	t.unchoker.SetNumUnchoked(t.maxUploads)
}

// setOrder switches the piece order live and persists it. Runs on the
// torrent goroutine.
func (t *torrent) setOrder(sequential, firstLast bool) {
	t.sequential = sequential
	t.firstLast = firstLast
	if t.piecePicker != nil {
		t.piecePicker.SetOrder(sequential, firstLast)
	}
	t.persistOrder()
}

// persistOrder stores the order flags in resume, surviving reloads.
func (t *torrent) persistOrder() {
	_ = t.session.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.id))
		if b == nil {
			return nil
		}
		if err := b.Put(boltdbresumer.Keys.Sequential, []byte(strconv.FormatBool(t.sequential))); err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.FirstLast, []byte(strconv.FormatBool(t.firstLast)))
	})
}

// ---------------------------------------------------------------------------
// Tracker view
// ---------------------------------------------------------------------------

// TrackerStatus is a tracker's contact state.
type TrackerStatus int

const (
	// NotContactedYet means no announce went out yet.
	NotContactedYet TrackerStatus = iota
	// Contacting means a request is in flight.
	Contacting
	// Working answered as expected.
	Working
	// NotWorking failed or errored.
	NotWorking
)

func trackerStatusToString(s TrackerStatus) string {
	m := map[TrackerStatus]string{
		NotContactedYet: "Not contacted yet",
		Contacting:      "Contacting",
		Working:         "Working",
		NotWorking:      "Not working",
	}
	return m[s]
}

// Tracker is one tracker's live status.
type Tracker struct {
	URL          string
	Status       TrackerStatus
	Leechers     int
	Seeders      int
	Error        *AnnounceError
	Warning      string
	LastAnnounce time.Time
	NextAnnounce time.Time
}

// Trackers snapshots every tracker's status.
func (t *torrent) Trackers() []Tracker {
	return query(t, t.getTrackers)
}

// ---------------------------------------------------------------------------
// Peer and webseed views
// ---------------------------------------------------------------------------

// Peer is a handshaked connection: origin, choke/interest both ways,
// speeds, encryption, and v2-relevant progress.
type Peer struct {
	ID                 [20]byte
	Client             string
	Addr               net.Addr
	Source             PeerSource
	ConnectedAt        time.Time
	Downloading        bool
	ClientInterested   bool
	ClientChoking      bool
	PeerInterested     bool
	PeerChoking        bool
	OptimisticUnchoked bool
	Snubbed            bool
	EncryptedHandshake bool
	EncryptedStream    bool
	DownloadSpeed      int
	UploadSpeed        int
	// UTP marks uTP connections.
	UTP bool
	// Progress (0-1) and Seed derive from the peer's bitfield.
	Progress float64
	Seed     bool
}

// PeerSource tells how a peer was found.
type PeerSource int

const (
	// SourceTracker came from a tracker announce.
	SourceTracker PeerSource = iota
	// SourceDHT came from the DHT.
	SourceDHT
	// SourcePEX was gossiped by a peer.
	SourcePEX
	// SourceIncoming dialed us.
	SourceIncoming
	// SourceManual was added via AddPeer.
	SourceManual
	// SourceHolepunch arrived via a BEP 55 rendezvous.
	SourceHolepunch
)

// Peers snapshots the connected peers.
func (t *torrent) Peers() []Peer {
	return query(t, t.getPeers)
}

// Webseed is one HTTP source: URL, last error and live speed.
type Webseed struct {
	URL           string
	Error         error
	DownloadSpeed int
}

// Webseeds snapshots the webseed sources.
func (t *torrent) Webseeds() []Webseed {
	return query(t, t.getWebseeds)
}
