package peerprotocol

import (
	"encoding/binary"
	"io"
)

// Message is one outgoing BitTorrent peer message: a numeric ID plus a body
// serialized by Read. Read reports io.EOF once the whole body is emitted,
// which is how the peer writer knows the message is complete.
type Message interface {
	io.Reader
	ID() MessageID
}

// emptyMessage is the body of the fixed single-byte messages (choke,
// interested, ...): no payload, immediately complete.
type emptyMessage struct{}

func (m emptyMessage) Read(b []byte) (int, error) {
	return 0, io.EOF
}

// HaveMessage announces possession of one piece.
type HaveMessage struct {
	Index uint32
}

// ID returns the peer protocol message type.
func (m HaveMessage) ID() MessageID { return Have }

func (m HaveMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	return 4, io.EOF
}

// RequestMessage asks the peer for one block.
type RequestMessage struct {
	Index, Begin, Length uint32
}

// ID returns the peer protocol message type.
func (m RequestMessage) ID() MessageID { return Request }

func (m RequestMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	binary.BigEndian.PutUint32(b[4:8], m.Begin)
	binary.BigEndian.PutUint32(b[8:12], m.Length)
	return 12, io.EOF
}

// PieceMessage carries one uploaded block: header only, the payload follows.
type PieceMessage struct {
	Index, Begin uint32
}

// ID returns the peer protocol message type.
func (m PieceMessage) ID() MessageID { return Piece }

func (m PieceMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	binary.BigEndian.PutUint32(b[4:8], m.Begin)
	return 8, io.EOF
}

// BitfieldMessage exchanges piece availability right after the handshake.
// The payload streams out incrementally, tracking progress in pos.
type BitfieldMessage struct {
	Data []byte
	pos  int
}

// ID returns the peer protocol message type.
func (m *BitfieldMessage) ID() MessageID { return Bitfield }

func (m *BitfieldMessage) Read(b []byte) (n int, err error) {
	n = copy(b, m.Data[m.pos:])
	m.pos += n
	if m.pos == len(m.Data) {
		err = io.EOF
	}
	return
}

// PortMessage advertises the UDP port of the peer's own DHT node.
type PortMessage struct {
	Port uint16
}

// ID returns the peer protocol message type.
func (m PortMessage) ID() MessageID { return Port }

func (m PortMessage) Read(b []byte) (n int, err error) {
	binary.BigEndian.PutUint16(b[0:2], m.Port)
	return 2, io.EOF
}

// AllowedFastMessage grants downloading regardless of choke state (BEP 6).
type AllowedFastMessage struct{ HaveMessage }

// ID returns the peer protocol message type.
func (m AllowedFastMessage) ID() MessageID { return AllowedFast }

// ChokeMessage forbids the peer from requesting.
type ChokeMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m ChokeMessage) ID() MessageID { return Choke }

// UnchokeMessage allows the peer to request.
type UnchokeMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m UnchokeMessage) ID() MessageID { return Unchoke }

// InterestedMessage signals we want blocks from the peer.
type InterestedMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m InterestedMessage) ID() MessageID { return Interested }

// NotInterestedMessage signals we want nothing from the peer.
type NotInterestedMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m NotInterestedMessage) ID() MessageID { return NotInterested }

// HaveAllMessage declares a full seed in one byte (BEP 6 fast extension).
type HaveAllMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m HaveAllMessage) ID() MessageID { return HaveAll }

// HaveNoneMessage declares an empty peer in one byte.
type HaveNoneMessage struct{ emptyMessage }

// ID returns the peer protocol message type.
func (m HaveNoneMessage) ID() MessageID { return HaveNone }

// RejectMessage refuses a previously allowed-fast request.
type RejectMessage struct{ RequestMessage }

// ID returns the peer protocol message type.
func (m RejectMessage) ID() MessageID { return Reject }

// CancelMessage withdraws a pending block request.
type CancelMessage struct{ RequestMessage }

// ID returns the peer protocol message type.
func (m CancelMessage) ID() MessageID { return Cancel }
