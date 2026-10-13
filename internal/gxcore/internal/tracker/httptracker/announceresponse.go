package httptracker

import (
	"github.com/zeebo/bencode"
)

// announceResponse mirrors the bencoded dictionary of an HTTP announce
// reply (BEP 23). Keys use the tracker's own spellings, including the
// spaced "failure reason" / "min interval" forms; Peers may arrive in
// compact binary or dictionary-list form, hence RawMessage.
type announceResponse struct {
	FailureReason  string             `bencode:"failure reason"`
	RetryIn        string             `bencode:"retry in"`
	WarningMessage string             `bencode:"warning message"`
	Interval       int32              `bencode:"interval"`
	MinInterval    int32              `bencode:"min interval"`
	TrackerID      string             `bencode:"tracker id"`
	Complete       int32              `bencode:"complete"`
	Incomplete     int32              `bencode:"incomplete"`
	Peers          bencode.RawMessage `bencode:"peers"`
	Peers6         bencode.RawMessage `bencode:"peers6"`
	ExternalIP     []byte             `bencode:"external ip"`
}
