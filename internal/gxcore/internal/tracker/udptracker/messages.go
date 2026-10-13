package udptracker

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// udpMessageHeader opens every UDP tracker packet: action, then the
// transaction id echoed back in replies so responses match requests.
type udpMessageHeader struct {
	Action        action
	TransactionID int32
}

// SetTransactionID implements udpRequest.
func (h *udpMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

// udpRequestHeader extends the head with the session connection id carried
// by every post-connect request. The transport stamps it once the connect
// transaction succeeds.
type udpRequestHeader struct {
	ConnectionID int64
	udpMessageHeader
}

// SetConnectionID implements udpRequest.
func (h *udpRequestHeader) SetConnectionID(id int64) { h.ConnectionID = id }

// connectRequest opens a session (action 0) with the protocol magic.
type connectRequest struct {
	udpRequestHeader
}

// newConnectRequest builds the session opener.
func newConnectRequest() *connectRequest {
	req := new(connectRequest)
	req.Action = actionConnect
	req.ConnectionID = connectionIDMagic
	return req
}

func (r *connectRequest) WriteTo(w io.Writer) (int64, error) {
	return 0, binary.Write(w, binary.BigEndian, r)
}

// connectResponse answers a session opener with the negotiated id.
type connectResponse struct {
	udpMessageHeader
	ConnectionID int64
}

// announceRequest is the full UDP announce body (BEP 15): identity,
// counters, event, and dial-back details.
type announceRequest struct {
	udpRequestHeader
	InfoHash   [20]byte
	PeerID     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      tracker.Event
	IP         uint32
	Key        uint32
	NumWant    int32
	Port       uint16
	Extensions uint16
}

// transferAnnounceRequest pairs an announce body with the tracker's URL
// suffix, appended as type-length-value options for trackers (e.g. passkey
// hosts) that route on it.
type transferAnnounceRequest struct {
	*announceRequest
	urlData string
}

func (r *transferAnnounceRequest) WriteTo(w io.Writer) (int64, error) {
	// Most tracker URLs with keys fit in 255 suffix bytes; the buffer
	// pre-sizes the 98-byte body plus that headroom.
	buf := bytes.NewBuffer(make([]byte, 0, 98+2+255))

	if err := binary.Write(buf, binary.BigEndian, r.announceRequest); err != nil {
		return 0, err
	}
	for rest := r.urlData; len(rest) > 0; {
		chunk := min(len(rest), 255)
		if _, err := buf.Write([]byte{0x2, byte(chunk)}); err != nil {
			return 0, err
		}
		if _, err := buf.WriteString(rest[:chunk]); err != nil {
			return 0, err
		}
		rest = rest[chunk:]
	}
	return buf.WriteTo(w)
}

// scrapeRequest asks for one info hash's swarm totals (BEP 15, action 2).
// The protocol allows several hashes per packet; the client scrapes one
// torrent at a time.
type scrapeRequest struct {
	udpRequestHeader
	InfoHash [20]byte
}

func (r *scrapeRequest) WriteTo(w io.Writer) (int64, error) {
	return 0, binary.Write(w, binary.BigEndian, r)
}
