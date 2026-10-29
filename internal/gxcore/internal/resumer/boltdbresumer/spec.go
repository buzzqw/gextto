package boltdbresumer

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Spec is the resume record of one torrent: identity, swarm configuration,
// transfer totals and runtime flags. It travels through the bbolt resume
// store and the JSON move-torrent protocol.
type Spec struct {
	InfoHash   []byte
	Port       int
	Name       string
	Trackers   [][]string
	URLList    []string
	FixedPeers []string
	Info       []byte
	// PieceLayers holds the bencoded BEP 52 piece layers of a v2 torrent (nil
	// for v1): the info dict alone cannot rebuild a v2 torrent.
	PieceLayers       []byte
	Bitfield          []byte
	AddedAt           time.Time
	BytesDownloaded   int64
	BytesUploaded     int64
	BytesWasted       int64
	SeededFor         time.Duration
	Started           bool
	StopAfterDownload bool
	StopAfterMetadata bool
	CompleteCmdRun    bool
	Sequential        bool
	FirstLast         bool
	// SuperSeeding arms BEP 16 super-seeding.
	SuperSeeding bool
	// SeedMode trusts the data without the full check: pieces are hashed
	// lazily as they are served (libtorrent seed_mode). The pieces already
	// checked are not recorded, so a restart checks them again.
	SeedMode bool
	// FilePaths overrides the on-disk storage paths of info.Files, in order,
	// when a file or folder was renamed (qBittorrent renameFile/renameFolder).
	// Nil when no rename happened. The info dict itself is never changed (its
	// hash must stay the original).
	FilePaths []string
	// DisplayName overrides the shown name after a torrent rename (qBittorrent
	// torrents/rename). Empty when the torrent was never renamed: the name then
	// follows the metadata.
	DisplayName string
	// ContentLayout is the qBittorrent content layout chosen at add time. A
	// magnet still waiting for its metadata applies it when the info arrives,
	// also after a restart.
	ContentLayout string
	Version       int
}

// jsonSpec is the JSON projection of Spec. Raw bytes ride base64-encoded,
// the duration as plain nanoseconds; field order is wire-significant for
// readers that compare payloads literally.
type jsonSpec struct {
	Port              int
	Name              string
	Trackers          [][]string
	URLList           []string
	FixedPeers        []string
	AddedAt           time.Time
	BytesDownloaded   int64
	BytesUploaded     int64
	BytesWasted       int64
	Started           bool
	StopAfterDownload bool
	StopAfterMetadata bool
	CompleteCmdRun    bool
	Sequential        bool
	FirstLast         bool
	SuperSeeding      bool
	Version           int

	// JSON unsafe types
	InfoHash    string
	Info        string
	PieceLayers string
	Bitfield    string
	SeededFor   int64

	// Rename overrides, appended last and omitted when unset so the payload of
	// a never-renamed torrent is unchanged.
	FilePaths   []string `json:",omitempty"`
	DisplayName string   `json:",omitempty"`
	// Seed mode, appended last and omitted when off for the same reason.
	SeedMode bool `json:",omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (s Spec) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonSpec{
		Port:              s.Port,
		Name:              s.Name,
		Trackers:          s.Trackers,
		URLList:           s.URLList,
		FixedPeers:        s.FixedPeers,
		AddedAt:           s.AddedAt,
		BytesDownloaded:   s.BytesDownloaded,
		BytesUploaded:     s.BytesUploaded,
		BytesWasted:       s.BytesWasted,
		Started:           s.Started,
		StopAfterDownload: s.StopAfterDownload,
		StopAfterMetadata: s.StopAfterMetadata,
		CompleteCmdRun:    s.CompleteCmdRun,
		Sequential:        s.Sequential,
		FirstLast:         s.FirstLast,
		SuperSeeding:      s.SuperSeeding,
		Version:           s.Version,

		InfoHash:    base64.StdEncoding.EncodeToString(s.InfoHash),
		Info:        base64.StdEncoding.EncodeToString(s.Info),
		PieceLayers: base64.StdEncoding.EncodeToString(s.PieceLayers),
		Bitfield:    base64.StdEncoding.EncodeToString(s.Bitfield),
		SeededFor:   int64(s.SeededFor),
		FilePaths:   s.FilePaths,
		DisplayName: s.DisplayName,
		SeedMode:    s.SeedMode,
	})
}

// UnmarshalJSON implements json.Unmarshaler. A corrupt base64 leg aborts
// the whole record — a half-decoded resume is worse than none.
func (s *Spec) UnmarshalJSON(b []byte) error {
	var j jsonSpec
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	var err error
	if s.InfoHash, err = base64.StdEncoding.DecodeString(j.InfoHash); err != nil {
		return err
	}
	if s.Info, err = base64.StdEncoding.DecodeString(j.Info); err != nil {
		return err
	}
	if s.PieceLayers, err = base64.StdEncoding.DecodeString(j.PieceLayers); err != nil {
		return err
	}
	if s.Bitfield, err = base64.StdEncoding.DecodeString(j.Bitfield); err != nil {
		return err
	}
	s.SeededFor = time.Duration(j.SeededFor)
	s.Port = j.Port
	s.Name = j.Name
	s.Trackers = j.Trackers
	s.URLList = j.URLList
	s.FixedPeers = j.FixedPeers
	s.AddedAt = j.AddedAt
	s.BytesDownloaded = j.BytesDownloaded
	s.BytesUploaded = j.BytesUploaded
	s.BytesWasted = j.BytesWasted
	s.Started = j.Started
	s.StopAfterDownload = j.StopAfterDownload
	s.StopAfterMetadata = j.StopAfterMetadata
	s.CompleteCmdRun = j.CompleteCmdRun
	s.Sequential = j.Sequential
	s.FirstLast = j.FirstLast
	s.SuperSeeding = j.SuperSeeding
	s.Version = j.Version
	s.FilePaths = j.FilePaths
	s.DisplayName = j.DisplayName
	s.SeedMode = j.SeedMode
	return nil
}
