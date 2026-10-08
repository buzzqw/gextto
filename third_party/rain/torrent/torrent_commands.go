package torrent

import (
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/cenkalti/rain/v2/internal/magnet"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/cenkalti/rain/v2/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"go.etcd.io/bbolt"
)

// sendCommand sends f to the torrent's run loop, where it executes on the
// run() goroutine. It gives up and returns false if the torrent is closed
// first.
func (t *torrent) sendCommand(f func()) bool {
	select {
	case t.commandC <- f:
		return true
	case <-t.closeC:
		return false
	}
}

// query runs f on the torrent's run loop and returns what it produced.
// It returns the zero value if the torrent is closed first.
//
// It is a function rather than a method because methods cannot be generic.
func query[T any](t *torrent, f func() T) T {
	var zero T
	resp := make(chan T, 1) // buffered, so the run loop never waits on the caller
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

// Start downloading.
// After all files are downloaded, seeding continues until the torrent is stopped.
func (t *torrent) Start() {
	t.sendCommand(t.start)
}

// Stop downloading and seeding.
// Stop closes all peer connections.
func (t *torrent) Stop() {
	t.sendCommand(func() { t.stop(nil) })
}

// Announce torrent to trackers and DHT manually.
func (t *torrent) Announce() {
	t.sendCommand(func() { t.setNeedMorePeers(true) })
}

// Verify pieces by checking files.
func (t *torrent) Verify() {
	t.sendCommand(t.handleVerifyCommand)
}

// Close this torrent and release all resources.
// Close must be called before discarding the torrent.
func (t *torrent) Close() {
	close(t.closeC)
	<-t.doneC
}

func (t *torrent) NotifyClose() <-chan struct{} {
	return t.closeC
}

func (t *torrent) NotifyComplete() <-chan struct{} {
	return t.completeC
}

func (t *torrent) NotifyMetadata() <-chan struct{} {
	return t.completeMetadataC
}

// NotifyError returns the channel that carries the error that stopped the
// torrent. It is nil unless the torrent is running, because start() creates the
// channel and stop() drops it, so this has to be read on the run loop.
func (t *torrent) NotifyError() <-chan error {
	return query(t, func() chan error { return t.errC })
}

// NotifyListen returns a new channel that is signalled after torrent has started to listen on peer port.
// NotifyListen must be called after calling Start().
func (t *torrent) NotifyListen() <-chan int {
	return query(t, func() chan int { return t.portC })
}

func (t *torrent) Magnet() (string, error) {
	if t.info != nil && t.info.Private {
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

func (t *torrent) Torrent() ([]byte, error) {
	if t.info == nil {
		return nil, errors.New("torrent metadata not ready")
	}
	webseeds := make([]string, len(t.webseedSources))
	for i, ws := range t.webseedSources {
		webseeds[i] = ws.URL
	}
	return metainfo.NewBytes(t.info.Bytes, t.getTieredTrackers(), webseeds, "")
}

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

// Stats returns statistics about the Torrent.
func (t *torrent) Stats() Stats {
	return query(t, t.stats)
}

func (t *torrent) AddPeers(peers []*net.TCPAddr) {
	t.sendCommand(func() { t.handleNewPeers(peers, peersource.Manual) })
}

func (t *torrent) AddTrackers(trackers []tracker.Tracker) {
	t.sendCommand(func() { t.handleNewTrackers(trackers) })
}

// unlimitedUnchokedPeers is used when the per-torrent upload slots are set to
// "unlimited" (gextto fork).
const unlimitedUnchokedPeers = 1000

// enforceConnectionLimit closes peers over the per-torrent cap (gextto fork).
// It runs in the torrent goroutine.
func (t *torrent) enforceConnectionLimit() {
	if t.maxConnections <= 0 {
		return
	}
	for len(t.peers) > t.maxConnections {
		closed := false
		for pe := range t.peers {
			t.closePeer(pe)
			closed = true
			break
		}
		if !closed {
			return
		}
	}
}

// applyMaxUploads sets the unchoker slots from maxUploads (gextto fork): zero
// unchokes every interested peer. It runs in the torrent goroutine.
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

// setOrder changes the piece order at runtime and persists it (gextto fork).
// It runs in the torrent goroutine.
func (t *torrent) setOrder(sequential, firstLast bool) {
	t.sequential = sequential
	t.firstLast = firstLast
	if t.piecePicker != nil {
		t.piecePicker.SetOrder(sequential, firstLast)
	}
	t.persistOrder()
}

// persistOrder stores the piece order flags in the resume database, so a
// runtime change survives a session reload (gextto fork).
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

// TrackerStatus is status of the Tracker.
type TrackerStatus int

const (
	// NotContactedYet indicates that no announce request has been made to the tracker.
	NotContactedYet TrackerStatus = iota
	// Contacting the tracker. Sending request or waiting response from the tracker.
	Contacting
	// Working indicates that the tracker has responded as expected.
	Working
	// NotWorking indicates that the tracker didn't respond or returned an error.
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

// Tracker is a server that tracks the peers of torrents.
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

func (t *torrent) Trackers() []Tracker {
	return query(t, t.getTrackers)
}

// Peer is a remote peer that is connected and completed protocol handshake.
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
	// UTP is set for peers connected over uTP (gextto fork).
	UTP bool
	// Progress (0-1) and Seed come from the peer's bitfield (gextto fork).
	Progress float64
	Seed     bool
}

// PeerSource indicates that how the peer is found.
type PeerSource int

const (
	// SourceTracker indicates that the peer is found from one of the trackers.
	SourceTracker PeerSource = iota
	// SourceDHT indicates that the peer is found from Decentralised Hash Table.
	SourceDHT
	// SourcePEX indicates that the peer is found from another peer.
	SourcePEX
	// SourceIncoming indicates that the peer found us.
	SourceIncoming
	// SourceManual indicates that the peer is added manually via AddPeer method.
	SourceManual
)

func (t *torrent) Peers() []Peer {
	return query(t, t.getPeers)
}

// Webseed is a HTTP source defined in Torrent.
// Client can download from these sources along with peers from the swarm.
type Webseed struct {
	URL           string
	Error         error
	DownloadSpeed int
}

func (t *torrent) Webseeds() []Webseed {
	return query(t, t.getWebseeds)
}
