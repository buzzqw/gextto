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
	Version      int
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
	return nil
}
