package tracker

// Torrent contains fields that are sent in an announce request.
type Torrent struct {
	BytesUploaded   int64
	BytesDownloaded int64
	BytesLeft       int64
	InfoHash        [20]byte
	PeerID          [20]byte
	Port            int
	// PartialSeed is true when every wanted piece is present but the torrent is
	// not complete (BEP 21): the announce then carries event=paused.
	PartialSeed bool
}
