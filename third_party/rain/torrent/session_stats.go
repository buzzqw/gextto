package torrent

import (
	"github.com/cenkalti/rain/v2/internal/netx"
	"github.com/cenkalti/rain/v2/internal/peerconn"
	"github.com/nictuku/dht"
	"strconv"
	"time"

	"github.com/cenkalti/rain/v2/internal/resumer/boltdbresumer"
	"go.etcd.io/bbolt"
)

// SessionStats contains statistics about Session.
type SessionStats struct {
	// gextto fork: peer connections by transport since start.
	OutgoingUTP, OutgoingTCP, IncomingUTP int64
	// gextto fork: incoming TCP connections since start (mirrors IncomingUTP).
	IncomingTCP int64
	// gextto fork: currently connected peers by transport.
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
	// gextto fork: raw peer wire bytes, protocol framing and encryption
	// included (payload bytes are BytesDownloaded/BytesUploaded).
	PeerWireDownloaded int64
	PeerWireUploaded   int64
	// Number of bytes read from disk.
	BytesRead int64
	// Number of bytes written to disk.
	BytesWritten int64

	// gextto fork: cumulative read/write operations since start. Their rate is
	// ReadsPerSecond/WritesPerSecond; the meters' total is what libtorrent
	// reports as disk.num_read_ops / disk.num_write_ops.
	ReadOpsTotal  int64
	WriteOpsTotal int64
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

func (s *Session) updateStats() {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	err := s.db.Update(func(tx *bbolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		for _, t := range s.torrents {
			b := mb.Bucket([]byte(t.torrent.id))
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.bytesDownloaded.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.bytesUploaded.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.bytesWasted.Count(), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.seededFor.Count()).String()))

			t.torrent.mBitfield.RLock()
			if t.torrent.bitfield != nil {
				_ = b.Put(boltdbresumer.Keys.Bitfield, t.torrent.bitfield.Bytes())
			}
		}
		return nil
	})
	for _, t := range s.torrents {
		t.torrent.mBitfield.RUnlock()
	}
	if err != nil {
		s.log.Errorln("cannot update stats:", err.Error())
	}
}
