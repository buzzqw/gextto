package peerprotocol

import "strconv"

// MessageID identifies peer wire messages: 0-9 are the base protocol,
// 13-17 the fast extension (BEP 6), 20 the extension protocol (BEP 10),
// 21-23 the v2 hash transfer (BEP 52).
type MessageID uint8

// Peer message ids, in wire order.
const (
	Choke MessageID = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
	Port
	Suggest     = 13
	HaveAll     = 14
	HaveNone    = 15
	Reject      = 16
	AllowedFast = 17
	Extension   = 20
	HashRequest = 21
	Hashes      = 22
	HashReject  = 23
)

// messageIDStrings names the known ids for logs and diagnostics.
var messageIDStrings = map[MessageID]string{
	Choke:         "choke",
	Unchoke:       "unchoke",
	Interested:    "interested",
	NotInterested: "not interested",
	Have:          "have",
	Bitfield:      "bitfield",
	Request:       "request",
	Piece:         "piece",
	Cancel:        "cancel",
	Port:          "port",
	Suggest:       "suggest",
	HaveAll:       "have all",
	HaveNone:      "have none",
	Reject:        "reject",
	AllowedFast:   "allowed fast",
	Extension:     "extension",
	HashRequest:   "hash request",
	Hashes:        "hashes",
	HashReject:    "hash reject",
}

// String names the message, or its number when unknown.
func (m MessageID) String() string {
	if s, ok := messageIDStrings[m]; ok {
		return s
	}
	return strconv.FormatInt(int64(m), 10)
}
