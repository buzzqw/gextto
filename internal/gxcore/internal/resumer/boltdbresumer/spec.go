package boltdbresumer

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Spec contains fields for resuming an existing torrent.
type Spec struct {
	InfoHash   []byte
	Port       int
	Name       string
	Trackers   [][]string
	URLList    []string
	FixedPeers []string
	Info       []byte
	// PieceLayers is the bencoded BEP 52 "piece layers" of a v2 torrent (nil
	// for v1): the info dict alone is not enough to rebuild a v2 torrent.
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
	// SuperSeeding enables BEP 16 super-seeding.
	SuperSeeding bool
	Version      int
}

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

// MarshalJSON converts the Spec to a JSON string.
func (s Spec) MarshalJSON() ([]byte, error) {
	j := jsonSpec{
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
	}
	return json.Marshal(j)
}

// UnmarshalJSON fills the Spec from a JSON string.
func (s *Spec) UnmarshalJSON(b []byte) error {
	var j jsonSpec
	err := json.Unmarshal(b, &j)
	if err != nil {
		return err
	}
	s.InfoHash, err = base64.StdEncoding.DecodeString(j.InfoHash)
	if err != nil {
		return err
	}
	s.Info, err = base64.StdEncoding.DecodeString(j.Info)
	if err != nil {
		return err
	}
	s.PieceLayers, err = base64.StdEncoding.DecodeString(j.PieceLayers)
	if err != nil {
		return err
	}
	s.Bitfield, err = base64.StdEncoding.DecodeString(j.Bitfield)
	if err != nil {
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
