package torrent

import (
	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn"
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"go.etcd.io/bbolt"
)

// SessionStats contains statistics about Session.
type SessionStats struct {
	// peer connections by transport since start.
	OutgoingUTP, OutgoingTCP, IncomingUTP int64
	// incoming TCP connections since start (mirrors IncomingUTP).
	IncomingTCP int64
	// currently connected peers by transport.
	TCPPeers, UTPPeers int
	// UTP and DHT report whether they are active.
	UTP bool
	DHT bool
	// DHTStats are the DHT counters (nodes, peers found, traffic).
	DHTStats map[string]int64

	// Time elapsed after creation of the Session object.
	Uptime time.Duration
	// Number of torrents in Session.
	Torrents int
	// Total number of connected peers.
	Peers int
	// Number of available ports for new torrents.
	PortsAvailable int

	// Number of rules in blocklist.
	BlockListRules int
	// Time elapsed after the last successful update of blocklist.
	BlockListRecency time.Duration

	// Number of objects in piece read cache.
	// Each object is a block whose size is defined in Config.ReadCacheBlockSize.
	ReadCacheObjects int
	// Current size of read cache.
	ReadCacheSize int64
	// Hit ratio of read cache.
	ReadCacheUtilization int

	// Number of reads per second from disk.
	ReadsPerSecond int
	// Number of active read requests from disk.
	ReadsActive int
	// Number of pending read requests from disk.
	ReadsPending int

	// Number of objects in piece write cache.
	// Objects are complete pieces.
	// Piece size differs among torrents.
	WriteCacheObjects int
	// Current size of write cache.
	WriteCacheSize int64
	// Number of pending torrents that is waiting for write cache.
	WriteCachePendingKeys int

	// Number of writes per second to disk.
	// Each write is a complete piece.
	WritesPerSecond int
	// Number of active write requests to disk.
	WritesActive int
	// Number of pending write requests to disk.
	WritesPending int

	// Download speed from peers in bytes/s.
	SpeedDownload int
	// Upload speed to peers in bytes/s.
	SpeedUpload int
	// Read speed from disk in bytes/s.
	SpeedRead int
	// Write speed to disk in bytes/s.
	SpeedWrite int

	// Number of bytes downloaded from peers.
	BytesDownloaded int64
	// Number of bytes uploaded to peers.
	BytesUploaded int64
	// raw peer wire bytes, protocol framing and encryption
	// included (payload bytes are BytesDownloaded/BytesUploaded).
	PeerWireDownloaded int64
	PeerWireUploaded   int64
	// Number of bytes read from disk.
	BytesRead int64
	// Number of bytes written to disk.
	BytesWritten int64

	// cumulative read/write operations since start. Their rate is
	// ReadsPerSecond/WritesPerSecond; the meters' total is what libtorrent
	// reports as disk.num_read_ops / disk.num_write_ops.
	ReadOpsTotal  int64
	WriteOpsTotal int64

	// StorageErrors is the number of piece writes that failed at the storage
	// layer (full disk, unwritable file, broken mount) since start.
	StorageErrors int64

	// MergedPieces and MergedBytes are the pieces and bytes that Swarm Merging
	// reused from other local torrents since start (no network involved).
	MergedPieces int64
	MergedBytes  int64
	// MergedPartialPieces counts the destination pieces reused at block level
	// (Phase 3): part of their 16 KiB blocks came from a partially verified
	// source, the rest from peers.
	MergedPartialPieces int64
}

// Stats returns current statistics about the Session.
func (s *Session) Stats() SessionStats {
	out := s.baseStats()
	out.OutgoingUTP = netx.OutgoingUTP.Load()
	out.OutgoingTCP = netx.OutgoingTCP.Load()
	out.IncomingUTP = netx.IncomingUTP.Load()
	out.IncomingTCP = netx.IncomingTCP.Load()
	out.UTPPeers = netx.UTPPeerCount()
	if out.UTPPeers > out.Peers {
		out.UTPPeers = out.Peers
	}
	out.TCPPeers = out.Peers - out.UTPPeers
	out.ReadOpsTotal = s.metrics.ReadsPerSecond.Count()
	out.WriteOpsTotal = s.metrics.WritesPerSecond.Count()
	out.StorageErrors = s.storageErrors.Load()
	out.MergedPieces = mergedPiecesTotal.Load()
	out.MergedBytes = mergedBytesTotal.Load()
	out.MergedPartialPieces = partialSourcePieces.Load()
	out.UTP = s.utpSocket != nil
	out.DHT = s.config.DHTEnabled
	out.PeerWireDownloaded = peerconn.WireBytesRead.Load()
	out.PeerWireUploaded = peerconn.WireBytesWritten.Load()
	if s.config.DHTEnabled {
		out.DHTStats = dht.Stats()
	}
	return out
}

func (s *Session) baseStats() SessionStats {
	return SessionStats{
		Uptime:         time.Duration(s.metrics.Uptime.Value()) * time.Second,
		Torrents:       int(s.metrics.Torrents.Value()),
		Peers:          int(s.metrics.Peers.Count()),
		PortsAvailable: int(s.metrics.PortsAvailable.Value()),

		BlockListRules:   int(s.metrics.BlockListRules.Value()),
		BlockListRecency: time.Duration(s.metrics.BlockListRecency.Value()) * time.Second,

		ReadCacheObjects:     int(s.metrics.ReadCacheObjects.Value()),
		ReadCacheSize:        s.metrics.ReadCacheSize.Value(),
		ReadCacheUtilization: int(s.metrics.ReadCacheUtilization.Value()),

		ReadsPerSecond: int(s.metrics.ReadsPerSecond.Rate1()),
		ReadsActive:    int(s.metrics.ReadsActive.Value()),
		ReadsPending:   int(s.metrics.ReadsPending.Value()),

		WriteCacheObjects:     int(s.metrics.WriteCacheObjects.Value()),
		WriteCacheSize:        s.metrics.WriteCacheSize.Value(),
		WriteCachePendingKeys: int(s.metrics.WriteCachePendingKeys.Value()),

		WritesPerSecond: int(s.metrics.WritesPerSecond.Rate1()),
		WritesActive:    int(s.metrics.WritesActive.Value()),
		WritesPending:   int(s.metrics.WritesPending.Value()),

		SpeedDownload: int(s.metrics.SpeedDownload.Rate1()),
		SpeedUpload:   int(s.metrics.SpeedUpload.Rate1()),
		SpeedRead:     int(s.metrics.SpeedRead.Rate1()),
		SpeedWrite:    int(s.metrics.SpeedWrite.Rate1()),

		BytesDownloaded: s.metrics.SpeedDownload.Count(),
		BytesUploaded:   s.metrics.SpeedUpload.Count(),
		BytesRead:       s.metrics.SpeedRead.Count(),
		BytesWritten:    s.metrics.SpeedWrite.Count(),
	}
}

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

// statsSnapshot is the durable state of one torrent collected by updateStats
// before the Bolt transaction is opened.
type statsSnapshot struct {
	id              []byte
	bitfield        []byte
	bytesDownloaded int64
	bytesUploaded   int64
	bytesWasted     int64
	seededFor       time.Duration
}

// updateStats persists the counters and the bitfield of every torrent. It runs
// in two phases: it samples the bitfield and fsyncs the data on each torrent's
// run loop (so the sample cannot race with the piece writers or closeData, and
// no Bolt transaction is open during the fsyncs), then writes every snapshot in
// a single durable transaction. The order preserves the durability invariant:
// a persisted bit refers to piece data already synced.
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
			// The torrent was closed before its snapshot could run.
			continue
		}
		snaps = append(snaps, snap)
	}

	err := s.db.Update(func(tx *bbolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		for _, snap := range snaps {
			b := mb.Bucket(snap.id)
			if b == nil {
				// The torrent was removed meanwhile: nothing to persist.
				continue
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
