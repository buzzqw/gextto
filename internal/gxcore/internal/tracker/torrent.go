package tracker

// Torrent carries the per-torrent numbers every announce (and scrape)
// request reports to the tracker: transfer totals, identity and port.
type Torrent struct {
	BytesUploaded   int64
	BytesDownloaded int64
	BytesLeft       int64
	InfoHash        [20]byte
	PeerID          [20]byte
	Port            int
	// PartialSeed marks a BEP 21 partial seed — every wanted piece present
	// without full completion — whose announces carry event=paused.
	PartialSeed bool
}
