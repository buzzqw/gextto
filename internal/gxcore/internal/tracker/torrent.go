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
	// IPv6 is the address the HTTP announce publishes in the BEP 7 "ipv6"
	// parameter, so a tracker that only sees our IPv4 address can list us for
	// v6 peers. Empty means "do not publish": there is no global address.
	IPv6 string
}
