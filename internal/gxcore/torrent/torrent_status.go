package torrent

// Status is the externally visible torrent lifecycle state.
type Status int

const (
	// Stopped runs nothing: no peers, closed files.
	Stopped Status = iota
	// DownloadingMetadata fetches the info dict from peers (magnet links).
	DownloadingMetadata
	// Allocating creates or opens the files on disk.
	Allocating
	// Verifying hashes existing files against the piece hashes.
	Verifying
	// Downloading fetches the torrent's files from peers.
	Downloading
	// Seeding serves a complete torrent.
	Seeding
	// Stopping disconnects, closes files and announces stopped to the
	// trackers; Stopped follows once they answer.
	Stopping
)

// statusNames renders statuses for the API and UI.
var statusNames = map[Status]string{
	Stopped:             "Stopped",
	DownloadingMetadata: "Downloading Metadata",
	Allocating:          "Allocating",
	Verifying:           "Verifying",
	Downloading:         "Downloading",
	Seeding:             "Seeding",
	Stopping:            "Stopping",
}

func (s Status) String() string {
	return statusNames[s]
}

// status derives the state from the run loop's organs, in precedence order:
// a missing command channel means never started; a stop announcer means
// draining; allocator and verifier mean their phases; completion decides
// between seeding and downloading, with metadata-less torrents still
// fetching their info dict.
func (t *torrent) status() Status {
	switch {
	case t.errC == nil:
		return Stopped
	case t.stoppedEventAnnouncer != nil:
		return Stopping
	case t.allocator != nil:
		return Allocating
	case t.verifier != nil:
		return Verifying
	case t.completed:
		return Seeding
	case t.info() == nil:
		return DownloadingMetadata
	default:
		return Downloading
	}
}
