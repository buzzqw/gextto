package torrent

import (
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/stringutil"
)

// Stats snapshots one torrent for the API and UI.
type Stats struct {
	// InfoHash identifies the torrent.
	InfoHash InfoHash
	// Port is the listening port.
	Port int
	// Status is the lifecycle state.
	Status Status
	// Error carries the unexpected stop reason, if any.
	Error  error
	Pieces struct {
		// Checked counts hash-verified pieces (Verifying state).
		Checked uint32
		// Have counts verified pieces.
		Have uint32
		// Missing counts pieces still to fetch (in-flight ones included
		// until hashed).
		Missing uint32
		// Available counts distinct swarm pieces; below Total the download
		// may never finish.
		Available uint32
		// Total counts all pieces.
		Total uint32
	}
	Bytes struct {
		// Completed holds verified bytes.
		Completed int64
		// Incomplete holds the bytes still missing.
		Incomplete int64
		// Total holds all file bytes (Completed + Incomplete).
		Total int64
		// Padding holds filler bytes (Disk = Total - Padding).
		Padding int64
		// Downloaded may exceed Completed on duplicate fetches.
		Downloaded int64
		// Uploaded holds the swarm upload total.
		Uploaded int64
		// Wasted holds duplicate/unsolicited bytes.
		Wasted int64
		// Allocated holds the on-storage bytes.
		Allocated int64
		// Selected/SelectedCompleted cover the wanted-files subset,
		// bitfield-derived so they hold while stopped.
		Selected          int64
		SelectedCompleted int64
	}
	Peers struct {
		// Total counts handshaked peers.
		Total int
		// Incoming counts peers that dialed us.
		Incoming int
		// Outgoing counts peers we dialed.
		Outgoing int
	}
	Handshakes struct {
		// Total counts pending handshakes.
		Total int
		// Incoming counts inbound handshakes.
		Incoming int
		// Outgoing counts outbound handshakes.
		Outgoing int
	}
	Addresses struct {
		// Total counts dialable addresses.
		Total int
		// Tracker/DHT/PEX split them by source.
		Tracker int
		DHT     int
		PEX     int
	}
	Downloads struct {
		// Total counts active piece fetches.
		Total int
		// Running counts normally downloading pieces.
		Running int
		// Snubbed counts stalled pieces.
		Snubbed int
		// Choked counts choked-state fetches.
		Choked int
	}
	MetadataDownloads struct {
		// Total counts active metadata fetches.
		Total int
		// Snubbed counts stalled ones.
		Snubbed int
		// Running counts healthy ones.
		Running int
	}
	// Name follows the metadata once it arrives.
	Name string
	// Private marks private torrents.
	Private bool
	// FileCount counts the files.
	FileCount int
	// PieceLength is the nominal piece size.
	PieceLength uint32
	// SeededFor accumulates Seeding-state time.
	SeededFor time.Duration
	// Speed holds 1-minute moving averages.
	Speed struct {
		// Download bytes/s.
		Download int
		// Upload bytes/s.
		Upload int
	}
	// ETA estimates completion; nil means unknown.
	ETA *time.Duration
}

// stats builds the snapshot: swarm census, counters, info-bound totals,
// have/missing from the bitfield, and a rounded ETA while downloading.
func (t *torrent) stats() Stats {
	t.updateSeedDuration(time.Now())

	var s Stats
	s.InfoHash = t.infoHash
	s.Port = t.port
	s.Status = t.status()
	s.Error = t.lastError
	s.Addresses.Total = t.addrList.Len()
	s.Addresses.Tracker = t.addrList.LenSource(peersource.Tracker)
	s.Addresses.DHT = t.addrList.LenSource(peersource.DHT)
	s.Addresses.PEX = t.addrList.LenSource(peersource.PEX)
	s.Handshakes.Incoming = len(t.incomingHandshakers)
	s.Handshakes.Outgoing = len(t.outgoingHandshakers)
	s.Handshakes.Total = len(t.incomingHandshakers) + len(t.outgoingHandshakers)
	s.Peers.Total = len(t.peers)
	s.Peers.Incoming = len(t.incomingPeers)
	s.Peers.Outgoing = len(t.outgoingPeers)
	s.MetadataDownloads.Total = len(t.infoDownloaders)
	s.MetadataDownloads.Snubbed = len(t.infoDownloadersSnubbed)
	s.MetadataDownloads.Running = len(t.infoDownloaders) - len(t.infoDownloadersSnubbed)
	for pe, m := range t.pieceDownloaders {
		n := len(m)
		s.Downloads.Total += n
		switch {
		case pe.PeerChoking:
			s.Downloads.Choked += n
		case pe.Snubbed:
			s.Downloads.Snubbed += n
		}
	}
	s.Downloads.Running = s.Downloads.Total - s.Downloads.Choked - s.Downloads.Snubbed
	s.Pieces.Available = t.avaliablePieceCount()
	s.Bytes.Downloaded = t.bytesDownloaded.Count()
	s.Bytes.Uploaded = t.bytesUploaded.Count()
	s.Bytes.Wasted = t.bytesWasted.Count()
	s.SeededFor = time.Duration(t.seededFor.Count())
	s.Bytes.Allocated = t.bytesAllocated
	s.Pieces.Checked = t.checkedPieces
	s.Speed.Download = int(t.downloadSpeed.Rate1())
	s.Speed.Upload = int(t.uploadSpeed.Rate1())

	if t.info() != nil {
		s.Bytes.Total = t.info().Length
		s.Bytes.Padding = t.info().Padding
		s.Bytes.Completed = t.bytesComplete()
		s.Bytes.Incomplete = s.Bytes.Total - s.Bytes.Completed
		s.Bytes.Selected, s.Bytes.SelectedCompleted = t.selectedBytes()

		s.Name = t.info().Name
		s.Private = t.info().Private
		s.FileCount = len(t.info().Files)
		s.PieceLength = t.info().PieceLength
		s.Pieces.Total = t.info().NumPieces
	} else {
		s.Name = t.Name()
	}
	s.Name = stringutil.Printable(s.Name)
	if t.bitfield != nil {
		s.Pieces.Have = t.bitfield.Count()
		s.Pieces.Missing = s.Pieces.Total - s.Pieces.Have
	}
	if s.Status == Downloading {
		s.ETA = downloadETA(s.Bytes.Incomplete, s.Speed.Download)
	}
	return s
}

// downloadETA estimates the remaining time at bps bytes/s, rounded
// coarser the further away it is; nil at zero speed.
func downloadETA(incomplete int64, bps int) *time.Duration {
	if bps == 0 {
		return nil
	}
	eta := time.Duration(incomplete/int64(bps)) * time.Second
	switch {
	case eta > 8*time.Hour:
		eta = eta.Round(time.Hour)
	case eta > 4*time.Hour:
		eta = eta.Round(30 * time.Minute)
	case eta > 2*time.Hour:
		eta = eta.Round(15 * time.Minute)
	case eta > time.Hour:
		eta = eta.Round(5 * time.Minute)
	case eta > 30*time.Minute:
		eta = eta.Round(time.Minute)
	case eta > 15*time.Minute:
		eta = eta.Round(30 * time.Second)
	case eta > 5*time.Minute:
		eta = eta.Round(15 * time.Second)
	case eta > time.Minute:
		eta = eta.Round(5 * time.Second)
	}
	return &eta
}

// avaliablePieceCount counts distinct swarm pieces (zero without a picker).
func (t *torrent) avaliablePieceCount() uint32 {
	if t.piecePicker == nil {
		return 0
	}
	return t.piecePicker.Available()
}

// bytesComplete totals verified bytes, prorating the tail piece.
func (t *torrent) bytesComplete() int64 {
	if t.bitfield == nil || len(t.pieces) == 0 {
		return 0
	}
	n := int64(t.info().PieceLength) * int64(t.bitfield.Count())
	if t.bitfield.Test(t.bitfield.Len() - 1) {
		n -= int64(t.info().PieceLength)
		n += int64(t.pieces[t.bitfield.Len()-1].Length)
	}
	return n
}

// getTrackers snapshots every announcer's status.
func (t *torrent) getTrackers() []Tracker {
	trackers := make([]Tracker, len(t.announcers))
	for i, an := range t.announcers {
		st := an.Stats()
		trackers[i] = Tracker{
			URL:          an.Tracker.URL(),
			Status:       TrackerStatus(st.Status),
			Seeders:      st.Seeders,
			Leechers:     st.Leechers,
			Warning:      st.Warning,
			LastAnnounce: st.LastAnnounce,
			NextAnnounce: st.NextAnnounce,
		}
		if st.Error != nil {
			trackers[i].Error = &AnnounceError{st.Error}
		}
	}
	return trackers
}

// getPeers snapshots the connected peers, mapping internal sources to API
// ones; an unmapped source is a bug and crashes.
func (t *torrent) getPeers() []Peer {
	peers := make([]Peer, 0, len(t.peers))
	for pe := range t.peers {
		var source PeerSource
		switch pe.Source {
		case peersource.Tracker:
			source = SourceTracker
		case peersource.DHT:
			source = SourceDHT
		case peersource.PEX:
			source = SourcePEX
		case peersource.Incoming:
			source = SourceIncoming
		case peersource.Manual:
			source = SourceManual
		case peersource.Holepunch:
			source = SourceHolepunch
		default:
			t.crash("unhandled peer source")
		}
		p := Peer{
			ID:                 pe.ID,
			Client:             pe.Client(),
			Addr:               pe.Addr(),
			ConnectedAt:        pe.ConnectedAt,
			Downloading:        pe.Downloading,
			ClientInterested:   pe.ClientInterested,
			ClientChoking:      pe.ClientChoking,
			PeerInterested:     pe.PeerInterested,
			PeerChoking:        pe.PeerChoking,
			OptimisticUnchoked: pe.OptimisticUnchoked,
			Snubbed:            pe.Snubbed,
			EncryptedHandshake: pe.EncryptionCipher != 0,
			EncryptedStream:    pe.EncryptionCipher == mse.RC4,
			Source:             source,
			DownloadSpeed:      pe.DownloadSpeed(),
			UploadSpeed:        pe.UploadSpeed(),
			UTP:                netx.IsUTPAddr(pe.Addr().String()),
		}
		if pe.Bitfield != nil && t.info() != nil && t.info().NumPieces > 0 {
			have := pe.Bitfield.Count()
			p.Progress = float64(have) / float64(t.info().NumPieces)
			p.Seed = have == t.info().NumPieces
		}
		peers = append(peers, p)
	}
	return peers
}

// getWebseeds snapshots the webseed sources.
func (t *torrent) getWebseeds() []Webseed {
	webseeds := make([]Webseed, 0, len(t.webseedSources))
	for _, src := range t.webseedSources {
		webseeds = append(webseeds, Webseed{
			URL:           src.URL,
			Error:         src.LastError,
			DownloadSpeed: int(src.DownloadSpeed.Rate1()),
		})
	}
	return webseeds
}

// updateSeedDuration accrues Seeding-state time; leaving the state clears
// the baseline.
func (t *torrent) updateSeedDuration(now time.Time) {
	if t.status() != Seeding {
		t.seedDurationUpdatedAt = time.Time{}
		return
	}
	if t.seedDurationUpdatedAt.IsZero() {
		t.seedDurationUpdatedAt = now
		return
	}
	t.seededFor.Inc(int64(now.Sub(t.seedDurationUpdatedAt)))
	t.seedDurationUpdatedAt = now
}
