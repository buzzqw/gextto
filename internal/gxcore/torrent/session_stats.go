package torrent

import (
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"go.etcd.io/bbolt"
)

// SessionStats snapshots the session: transports, torrents, caches,
// throughput, lifetime totals and the merge/durability diagnostics.
type SessionStats struct {
	// Connection counts by transport since start.
	OutgoingUTP, OutgoingTCP, IncomingUTP int64
	// IncomingTCP mirrors IncomingUTP for TCP.
	IncomingTCP int64
	// Live peers by transport.
	TCPPeers, UTPPeers int
	// UTP and DHT report whether they are active.
	UTP bool
	DHT bool
	// UTPDials/UTPAccepts count the socket-level uTP dials and accepts
	// since start; UTPDialErrors/UTPAcceptErrors count their failures.
	// They diagnose transport health (firewalled UDP, CGNAT) where the
	// peer-level counters alone cannot.
	UTPDials        int64
	UTPAccepts      int64
	UTPDialErrors   int64
	UTPAcceptErrors int64
	// UTPResendsTimeout counts RTO-driven packet resends (loss/congestion
	// signal); UTPResendsFast counts selective-ACK fast resends. Fed by the
	// uTP stack, they drive the adaptive RTO and are shown in both UIs.
	UTPResendsTimeout int64
	UTPResendsFast    int64
	// UTPSynDrops counts backlog SYNs evicted under burst. Fed by the uTP
	// stack; shown in both UIs.
	UTPSynDrops int64
	// DHTStats are the DHT counters (nodes, peers found, traffic).
	DHTStats map[string]int64

	// Uptime since the Session object was created.
	Uptime time.Duration
	// Torrents registered in the session.
	Torrents int
	// Connected peers in total.
	Peers int
	// Free ports for new torrents.
	PortsAvailable int

	// BlockListRules counts the filter rules.
	BlockListRules int
	// BlockListRecency is the time since the last filter update.
	BlockListRecency time.Duration

	// ReadCacheObjects counts the cached blocks (sized by ReadCacheBlockSize).
	ReadCacheObjects int
	// ReadCacheSize is the cached bytes.
	ReadCacheSize int64
	// ReadCacheUtilization is the cache hit ratio.
	ReadCacheUtilization int

	// ReadsPerSecond is the disk read rate.
	ReadsPerSecond int
	// ReadsActive counts the in-flight disk reads.
	ReadsActive int
	// ReadsPending counts the queued disk reads.
	ReadsPending int

	// WriteCacheObjects counts the buffered whole pieces (sizes vary).
	WriteCacheObjects int
	// WriteCacheSize is the buffered bytes.
	WriteCacheSize int64
	// WriteCachePendingKeys counts the torrents waiting on the write budget.
	WriteCachePendingKeys int

	// WritesPerSecond is the completed-piece write rate.
	WritesPerSecond int
	// WritesActive counts the in-flight piece writes.
	WritesActive int
	// WritesPending counts the queued piece writes.
	WritesPending int

	// SpeedDownload/SpeedUpload are the peer throughput in bytes/s.
	SpeedDownload int
	SpeedUpload   int
	// SpeedRead/SpeedWrite are the disk throughput in bytes/s.
	SpeedRead  int
	SpeedWrite int

	// BytesDownloaded/BytesUploaded are the payload totals.
	BytesDownloaded int64
	BytesUploaded   int64
	// PeerWireDownloaded/PeerWireUploaded are this session's raw peer-wire
	// bytes, framing included; the difference from the payload totals is the
	// protocol overhead.
	PeerWireDownloaded int64
	PeerWireUploaded   int64
	// BytesRead/BytesWritten are the disk totals.
	BytesRead    int64
	BytesWritten int64

	// ReadOpsTotal/WriteOpsTotal are the lifetime disk operation counts,
	// the libtorrent disk.num_read/write_ops equivalents.
	ReadOpsTotal  int64
	WriteOpsTotal int64

	// StorageErrors counts piece writes failed at the storage layer (full
	// disk, unwritable file, broken mount) since start.
	StorageErrors int64

	// MergedPieces/MergedBytes are the pieces and bytes Swarm Merging reused
	// from sibling torrents without network traffic.
	MergedPieces int64
	MergedBytes  int64
	// MergedPartialPieces counts the destination pieces reused at block level
	// (Phase 3): part of their 16 KiB blocks came from a partially verified
	// source, the rest from peers.
	MergedPartialPieces int64
}

// Stats snapshots the session: meter-backed gauges plus the transport, wire,
// merge and DHT counters.
func (s *Session) Stats() SessionStats {
	out := s.baseStats()
	out.OutgoingUTP = netx.OutgoingUTP.Load()
	out.OutgoingTCP = netx.OutgoingTCP.Load()
	out.IncomingUTP = netx.IncomingUTP.Load()
	out.IncomingTCP = netx.IncomingTCP.Load()
	out.UTPPeers = min(netx.UTPPeerCount(), out.Peers)
	out.TCPPeers = out.Peers - out.UTPPeers
	out.ReadOpsTotal = s.metrics.ReadsPerSecond.Count()
	out.WriteOpsTotal = s.metrics.WritesPerSecond.Count()
	out.StorageErrors = s.storageErrors.Load()
	out.MergedPieces = mergedPiecesTotal.Load()
	out.MergedBytes = mergedBytesTotal.Load()
	out.MergedPartialPieces = partialSourcePieces.Load()
	out.UTP = s.utpSocket != nil
	if s.utpSocket != nil {
		ust := s.utpSocket.Stats()
		out.UTPDials = int64(ust.Dials)
		out.UTPAccepts = int64(ust.Accepts)
		out.UTPDialErrors = int64(ust.DialErrors)
		out.UTPAcceptErrors = int64(ust.AcceptErrors)
		out.UTPResendsTimeout = int64(ust.ResendsTimeout)
		out.UTPResendsFast = int64(ust.ResendsFast)
		out.UTPSynDrops = int64(ust.SynDrops)
	}
	out.DHT = s.config.DHTEnabled
	out.PeerWireDownloaded = s.wire.Read.Load()
	out.PeerWireUploaded = s.wire.Written.Load()
	if s.config.DHTEnabled {
		out.DHTStats = dht.Stats()
	}
	return out
}

// baseStats reads the meter-backed gauges into the snapshot.
func (s *Session) baseStats() SessionStats {
	m := s.metrics
	return SessionStats{
		Uptime:         time.Duration(m.Uptime.Value()) * time.Second,
		Torrents:       int(m.Torrents.Value()),
		Peers:          int(m.Peers.Count()),
		PortsAvailable: int(m.PortsAvailable.Value()),

		BlockListRules:   int(m.BlockListRules.Value()),
		BlockListRecency: time.Duration(m.BlockListRecency.Value()) * time.Second,

		ReadCacheObjects:     int(m.ReadCacheObjects.Value()),
		ReadCacheSize:        m.ReadCacheSize.Value(),
		ReadCacheUtilization: int(m.ReadCacheUtilization.Value()),

		ReadsPerSecond: int(m.ReadsPerSecond.Rate1()),
		ReadsActive:    int(m.ReadsActive.Value()),
		ReadsPending:   int(m.ReadsPending.Value()),

		WriteCacheObjects:     int(m.WriteCacheObjects.Value()),
		WriteCacheSize:        m.WriteCacheSize.Value(),
		WriteCachePendingKeys: int(m.WriteCachePendingKeys.Value()),

		WritesPerSecond: int(m.WritesPerSecond.Rate1()),
		WritesActive:    int(m.WritesActive.Value()),
		WritesPending:   int(m.WritesPending.Value()),

		SpeedDownload: int(m.SpeedDownload.Rate1()),
		SpeedUpload:   int(m.SpeedUpload.Rate1()),
		SpeedRead:     int(m.SpeedRead.Rate1()),
		SpeedWrite:    int(m.SpeedWrite.Rate1()),

		BytesDownloaded: m.SpeedDownload.Count(),
		BytesUploaded:   m.SpeedUpload.Count(),
		BytesRead:       m.SpeedRead.Count(),
		BytesWritten:    m.SpeedWrite.Count(),
	}
}

// updateStatsLoop persists stats and bitfields every resume interval.
func (s *Session) updateStatsLoop() {
	ticker := time.NewTicker(s.config.ResumeWriteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.updateStats()
		case <-s.closeC:
			return
		}
	}
}

// statsSnapshot is one torrent's durable state, sampled before the Bolt
// transaction opens.
type statsSnapshot struct {
	id              []byte
	bitfield        []byte
	bytesDownloaded int64
	bytesUploaded   int64
	bytesWasted     int64
	seededFor       time.Duration
}

// updateStats persists every torrent's counters and bitfield in two phases:
// sample the bitfield and fsync the data on each run loop (no race with
// writers or closeData, no open Bolt transaction during fsyncs), then write
// all snapshots in one durable transaction. A persisted bit always describes
// already-synced data.
func (s *Session) updateStats() {
	s.mTorrents.RLock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	s.mTorrents.RUnlock()

	snaps := make([]statsSnapshot, 0, len(torrents))
	for _, t := range torrents {
		snap := query(t.torrent, func() statsSnapshot {
			t.torrent.mBitfield.RLock()
			var bits []byte
			if t.torrent.bitfield != nil {
				bits = append([]byte(nil), t.torrent.bitfield.Bytes()...)
			}
			t.torrent.mBitfield.RUnlock()
			t.torrent.syncPieces()
			return statsSnapshot{
				id:              []byte(t.torrent.id),
				bitfield:        bits,
				bytesDownloaded: t.torrent.bytesDownloaded.Count(),
				bytesUploaded:   t.torrent.bytesUploaded.Count(),
				bytesWasted:     t.torrent.bytesWasted.Count(),
				seededFor:       time.Duration(t.torrent.seededFor.Count()),
			}
		})
		if snap.id == nil {
			continue // closed before its snapshot ran
		}
		snaps = append(snaps, snap)
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		for _, snap := range snaps {
			b := mb.Bucket(snap.id)
			if b == nil {
				continue // removed meanwhile: nothing to persist
			}
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(snap.bytesDownloaded, 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(snap.bytesUploaded, 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(snap.bytesWasted, 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(snap.seededFor.String()))
			if snap.bitfield != nil {
				_ = b.Put(boltdbresumer.Keys.Bitfield, snap.bitfield)
			}
		}
		return nil
	})
	if err != nil {
		s.log.Errorln("cannot update stats:", err.Error())
	}
}
