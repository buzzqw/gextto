package rpctypes

// This file defines the JSON-RPC surface between the gx-core engine and its
// host (the gx-torrent daemon): one Request/Response pair per Session method
// plus the shared entity and statistics shapes.
//
// Field names and declaration order are wire-critical: responses are encoded
// with encoding/json, so any rename or reorder changes what API clients see.
// Reorganize freely, but never alter a field.

// ---------------------------------------------------------------------------
// Entities
// ---------------------------------------------------------------------------

// Torrent is a torrent registered in the session.
type Torrent struct {
	ID       string
	Name     string
	InfoHash string
	Port     int
	AddedAt  Time
}

// Peer is a peer connected to a torrent.
type Peer struct {
	ID                 string
	Client             string
	Addr               string
	Source             string
	ConnectedAt        Time
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
}

// Webseed is a URL seed attached to a torrent.
type Webseed struct {
	URL           string
	Error         string
	DownloadSpeed int
}

// Tracker is a tracker announced to by a torrent.
type Tracker struct {
	URL           string
	Status        string
	Leechers      int
	Seeders       int
	Warning       string
	Error         string
	ErrorUnknown  bool
	ErrorInternal string
	LastAnnounce  Time
	NextAnnounce  Time
}

// File is a file inside a torrent.
type File struct {
	Path   string
	Length int64
}

// FileStats pairs a file with its verified completion.
type FileStats struct {
	File           File
	BytesCompleted int64
}

// ---------------------------------------------------------------------------
// Statistics snapshots
// ---------------------------------------------------------------------------

// SessionStats is a point-in-time snapshot of the whole session.
type SessionStats struct {
	Uptime         int
	Torrents       int
	Peers          int
	PortsAvailable int

	BlockListRules   int
	BlockListRecency int

	ReadCacheObjects     int
	ReadCacheSize        int64
	ReadCacheUtilization int

	ReadsPerSecond int
	ReadsActive    int
	ReadsPending   int

	WriteCacheObjects     int
	WriteCacheSize        int64
	WriteCachePendingKeys int

	WritesPerSecond int
	WritesActive    int
	WritesPending   int

	SpeedDownload int
	SpeedUpload   int
	SpeedRead     int
	SpeedWrite    int

	BytesDownloaded int64
	BytesUploaded   int64
	BytesRead       int64
	BytesWritten    int64
}

// Stats is a point-in-time snapshot of one torrent.
type Stats struct {
	InfoHash string
	Port     int
	Status   string
	Error    string
	Pieces   struct {
		Checked   uint32
		Have      uint32
		Missing   uint32
		Available uint32
		Total     uint32
	}
	Bytes struct {
		Total      int64
		Padding    int64
		Allocated  int64
		Completed  int64
		Incomplete int64
		Downloaded int64
		Uploaded   int64
		Wasted     int64
	}
	Peers struct {
		Total    int
		Incoming int
		Outgoing int
	}
	Handshakes struct {
		Total    int
		Incoming int
		Outgoing int
	}
	Addresses struct {
		Total   int
		Tracker int
		DHT     int
		PEX     int
	}
	Downloads struct {
		Total   int
		Running int
		Snubbed int
		Choked  int
	}
	MetadataDownloads struct {
		Total   int
		Snubbed int
		Running int
	}
	Name        string
	Private     bool
	FileCount   int
	PieceLength uint32
	SeededFor   uint
	Speed       struct {
		Download int
		Upload   int
	}
	ETA int
}

// ---------------------------------------------------------------------------
// Session methods: one Request/Response pair each, in RPC registration order
// ---------------------------------------------------------------------------

// AddTorrentOptions applies to both AddTorrent and AddURI.
type AddTorrentOptions struct {
	ID                string
	Stopped           bool
	StopAfterDownload bool
	StopAfterMetadata bool
	Sequential        bool
}

// GetMagnetRequest is the request for Session.GetMagnet.
type GetMagnetRequest struct {
	ID string
}

// GetMagnetResponse is the response for Session.GetMagnet.
type GetMagnetResponse struct {
	Magnet string
}

// GetTorrentRequest is the request for Session.GetTorrent.
type GetTorrentRequest struct {
	ID string
}

// GetTorrentResponse is the response for Session.GetTorrent.
type GetTorrentResponse struct {
	Torrent string
}

// ListTorrentsRequest is the request for Session.ListTorrents.
type ListTorrentsRequest struct {
}

// ListTorrentsResponse is the response for Session.ListTorrents.
type ListTorrentsResponse struct {
	Torrents []Torrent
}

// AddTorrentRequest is the request for Session.AddTorrent.
type AddTorrentRequest struct {
	Torrent string
	AddTorrentOptions
}

// AddTorrentResponse is the response for Session.AddTorrent.
type AddTorrentResponse struct {
	Torrent Torrent
}

// AddURIRequest is the request for Session.AddURI.
type AddURIRequest struct {
	URI string
	AddTorrentOptions
}

// AddURIResponse is the response for Session.AddURI.
type AddURIResponse struct {
	Torrent Torrent
}

// RemoveTorrentRequest is the request for Session.RemoveTorrent.
type RemoveTorrentRequest struct {
	ID       string
	KeepData bool
}

// RemoveTorrentResponse is the response for Session.RemoveTorrent.
type RemoveTorrentResponse struct {
}

// CleanDatabaseRequest is the request for Session.CleanDatabase.
type CleanDatabaseRequest struct {
}

// CleanDatabaseResponse is the response for Session.CleanDatabase.
type CleanDatabaseResponse struct {
}

// GetSessionStatsRequest is the request for Session.GetSessionStats.
type GetSessionStatsRequest struct {
}

// GetSessionStatsResponse is the response for Session.GetSessionStats.
type GetSessionStatsResponse struct {
	Stats SessionStats
}

// GetTorrentStatsRequest is the request for Session.GetTorrentStats.
type GetTorrentStatsRequest struct {
	ID string
}

// GetTorrentStatsResponse is the response for Session.GetTorrentStats.
type GetTorrentStatsResponse struct {
	Stats Stats
}

// GetTorrentTrackersRequest is the request for Session.GetTorrentTrackers.
type GetTorrentTrackersRequest struct {
	ID string
}

// GetTorrentTrackersResponse is the response for Session.GetTorrentTrackers.
type GetTorrentTrackersResponse struct {
	Trackers []Tracker
}

// GetTorrentPeersRequest is the request for Session.GetTorrentPeers.
type GetTorrentPeersRequest struct {
	ID string
}

// GetTorrentPeersResponse is the response for Session.GetTorrentPeers.
type GetTorrentPeersResponse struct {
	Peers []Peer
}

// GetTorrentWebseedsRequest is the request for Session.GetTorrentWebseeds.
type GetTorrentWebseedsRequest struct {
	ID string
}

// GetTorrentWebseedsResponse is the response for Session.GetTorrentWebseeds.
type GetTorrentWebseedsResponse struct {
	Webseeds []Webseed
}

// GetTorrentFilesRequest is the request for Session.GetTorrentFiles.
type GetTorrentFilesRequest struct {
	ID string
}

// GetTorrentFilesResponse is the response for Session.GetTorrentFiles.
type GetTorrentFilesResponse struct {
	Files []File
}

// GetTorrentFileStatsRequest is the request for Session.GetTorrentFileStats.
type GetTorrentFileStatsRequest struct {
	ID string
}

// GetTorrentFileStatsResponse is the response for Session.GetTorrentFileStats.
type GetTorrentFileStatsResponse struct {
	FileStats []FileStats
}

// StartTorrentRequest is the request for Session.StartTorrent.
type StartTorrentRequest struct {
	ID string
}

// StartTorrentResponse is the response for Session.StartTorrent.
type StartTorrentResponse struct {
}

// StopTorrentRequest is the request for Session.StopTorrent.
type StopTorrentRequest struct {
	ID string
}

// StopTorrentResponse is the response for Session.StopTorrent.
type StopTorrentResponse struct {
}

// AnnounceTorrentRequest is the request for Session.AnnounceTorrent.
type AnnounceTorrentRequest struct {
	ID string
}

// AnnounceTorrentResponse is the response for Session.AnnounceTorrent.
type AnnounceTorrentResponse struct {
}

// VerifyTorrentRequest is the request for Session.VerifyTorrent.
type VerifyTorrentRequest struct {
	ID string
}

// VerifyTorrentResponse is the response for Session.VerifyTorrent.
type VerifyTorrentResponse struct {
}

// MoveTorrentRequest is the request for Session.MoveTorrent.
type MoveTorrentRequest struct {
	ID     string
	Target string
}

// MoveTorrentResponse is the response for Session.MoveTorrent.
type MoveTorrentResponse struct {
}

// AddPeerRequest is the request for Session.AddPeer.
type AddPeerRequest struct {
	ID   string
	Addr string
}

// AddPeerResponse is the response for Session.AddPeer.
type AddPeerResponse struct {
}

// AddTrackerRequest is the request for Session.AddTracker.
type AddTrackerRequest struct {
	ID  string
	URL string
}

// AddTrackerResponse is the response for Session.AddTracker.
type AddTrackerResponse struct {
}

// StartAllTorrentsRequest is the request for Session.StartAllTorrents.
type StartAllTorrentsRequest struct {
}

// StartAllTorrentsResponse is the response for Session.StartAllTorrents.
type StartAllTorrentsResponse struct {
}

// StopAllTorrentsRequest is the request for Session.StopAllTorrents.
type StopAllTorrentsRequest struct {
}

// StopAllTorrentsResponse is the response for Session.StopAllTorrents.
type StopAllTorrentsResponse struct {
}
