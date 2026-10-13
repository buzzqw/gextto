package peer

import (
	"math"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/fast"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerreader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerwriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/pexlist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/sliceset"
	"github.com/buzzqw/gextto/internal/gxcore/internal/stringutil"
	"github.com/rcrowley/go-metrics"
)

// Peer wraps one handshaked connection with its choke/interest state,
// speeds, PEX exchange and snub detection.
type Peer struct {
	*peerconn.Conn

	ConnectedAt time.Time

	Source peersource.Source

	Bitfield            *bitfield.Bitfield
	ReceivedAllowedFast sliceset.SliceSet[piece.Piece]
	SentAllowedFast     sliceset.SliceSet[piece.Piece]

	ID                [20]byte
	ExtensionsEnabled bool
	FastEnabled       bool
	DHTEnabled        bool
	// ProtocolV2 marks peers advertising BitTorrent v2 in the handshake
	// reserved bits (BEP 52).
	ProtocolV2       bool
	EncryptionCipher mse.CryptoMethod

	ClientInterested bool
	ClientChoking    bool
	PeerInterested   bool
	PeerChoking      bool

	OptimisticUnchoked bool

	// Snubbed flags peers delivering too slowly.
	Snubbed bool

	Downloading bool

	// ConcurrentPieces counts this peer's active pieces, capped by
	// MaxConcurrentPieces (multi-piece pipelining keeps the request pipe
	// full on small pieces). Torrent goroutine only.
	ConcurrentPieces    int
	MaxConcurrentPieces int
	// RAMQueued marks a queued session-RAM request for this peer, so every
	// HAVE does not pile on duplicates. Torrent goroutine only.
	RAMQueued bool
	// PipeRefillTried bounds the extra-piece attempt on a dry request pipe to
	// once per piece instead of once per block. Torrent goroutine only.
	PipeRefillTried bool

	// RequestQueue is the adaptive in-flight depth: DefaultRequestsOut (or
	// the peer's reqq) growing toward MaxRequestsOut on delivery, reset on
	// choke. Torrent goroutine only.
	RequestQueue int

	downloadSpeed metrics.Meter
	uploadSpeed   metrics.Meter

	// Messages buffers protocol traffic received before metadata arrived.
	Messages []any

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	PEX *pex

	snubTimeout time.Duration
	snubTimer   *time.Timer

	closeC chan struct{}
	doneC  chan struct{}

	Closed bool
}

// Message is a protocol message read from a peer.
type Message struct {
	*Peer
	Message any
}

// PieceMessage is a block payload read from a peer.
type PieceMessage struct {
	*Peer
	Piece peerreader.Piece
}

// New wraps conn, decoding the capability bits (fast 61, extension 43, DHT
// 63, v2 59) out of the handshake extensions.
func New(conn net.Conn, source peersource.Source, id [20]byte, extensions [8]byte, cipher mse.CryptoMethod, pieceReadTimeout, snubTimeout time.Duration, maxRequestsIn, maxMsgSize int, br, bw *bandwidth.Limiter) *Peer {
	bf, _ := bitfield.NewBytes(extensions[:], 64)
	snub := time.NewTimer(math.MaxInt64)
	snub.Stop()
	return &Peer{
		Conn:              peerconn.New(conn, newPeerLogger(source, conn), pieceReadTimeout, maxRequestsIn, maxMsgSize, bf.Test(61), br, bw),
		Source:            source,
		ConnectedAt:       time.Now(),
		ID:                id,
		ClientChoking:     true,
		PeerChoking:       true,
		ExtensionsEnabled: bf.Test(43),
		FastEnabled:       bf.Test(61),
		DHTEnabled:        bf.Test(63),
		ProtocolV2:        bf.Test(59),
		EncryptionCipher:  cipher,
		snubTimeout:       snubTimeout,
		snubTimer:         snub,
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
		downloadSpeed:     metrics.NewMeter(),
		uploadSpeed:       metrics.NewMeter(),
	}
}

// newPeerLogger tags peer logs with direction and address.
func newPeerLogger(src peersource.Source, conn net.Conn) logger.Logger {
	if src == peersource.Incoming {
		return logger.New("peer <- " + conn.RemoteAddr().String())
	}
	return logger.New("peer -> " + conn.RemoteAddr().String())
}

// Close stops the pumps, the PEX exchange and the meters, then waits for
// the run loop to exit.
func (p *Peer) Close() {
	p.snubTimer.Stop()
	if p.PEX != nil {
		p.PEX.close()
	}
	close(p.closeC)
	p.Conn.Close()
	p.downloadSpeed.Stop()
	p.uploadSpeed.Stop()
	<-p.doneC
}

// Done closes when the run loop exits.
func (p *Peer) Done() chan struct{} {
	return p.doneC
}

// Run fans connection traffic out: piece payloads to pieces (metering the
// download), everything else — with upload completions metered — to
// messages. A closed stream reports the disconnect instead.
func (p *Peer) Run(messages chan Message, pieces chan PieceMessage, snubbed, disconnect chan *Peer) {
	defer close(p.doneC)
	go p.Conn.Run()

	for {
		select {
		case pm, ok := <-p.Conn.Messages():
			if !ok {
				select {
				case disconnect <- p:
				case <-p.closeC:
				}
				return
			}
			if m, ok := pm.(peerreader.Piece); ok {
				p.downloadSpeed.Mark(int64(len(m.Buffer.Data)))
				select {
				case pieces <- PieceMessage{Peer: p, Piece: m}:
				case <-p.closeC:
					return
				}
			} else {
				if m, ok := pm.(peerwriter.BlockUploaded); ok {
					p.uploadSpeed.Mark(int64(m.Length))
				}
				select {
				case messages <- Message{Peer: p, Message: pm}:
				case <-p.closeC:
					return
				}
			}
		case <-p.snubTimer.C:
			select {
			case snubbed <- p:
			case <-p.closeC:
				return
			}
		case <-p.closeC:
			return
		}
	}
}

// StartPEX launches the periodic PEX exchange, once.
func (p *Peer) StartPEX(initialPeers map[*Peer]struct{}, recentlySeen *pexlist.RecentlySeen) {
	if p.PEX == nil {
		p.PEX = newPEX(p.Conn, p.ExtensionHandshake.M[peerprotocol.ExtensionKeyPEX], initialPeers, recentlySeen)
		go p.PEX.run()
	}
}

// ResetSnubTimer restarts the stall detector on activity.
func (p *Peer) ResetSnubTimer() {
	p.snubTimer.Reset(p.snubTimeout)
}

// StopSnubTimer disarms the stall detector.
func (p *Peer) StopSnubTimer() {
	p.snubTimer.Stop()
}

// DownloadSpeed is the 1-minute average in bytes/s.
func (p *Peer) DownloadSpeed() int {
	return int(p.downloadSpeed.Rate1())
}

// UploadSpeed is the 1-minute average in bytes/s.
func (p *Peer) UploadSpeed() int {
	return int(p.uploadSpeed.Rate1())
}

// Choke chokes the peer.
func (p *Peer) Choke() {
	p.ClientChoking = true
	p.SendMessage(peerprotocol.ChokeMessage{})
}

// Unchoke unchokes the peer.
func (p *Peer) Unchoke() {
	p.ClientChoking = false
	p.SendMessage(peerprotocol.UnchokeMessage{})
}

// Choking reports our choke state toward the peer.
func (p *Peer) Choking() bool {
	return p.ClientChoking
}

// Interested reports the peer's interest in our pieces.
func (p *Peer) Interested() bool {
	return p.PeerInterested
}

// Optimistic reports the optimistic-unchoke flag.
func (p *Peer) Optimistic() bool {
	return p.OptimisticUnchoked
}

// SetOptimistic sets the optimistic-unchoke flag.
func (p *Peer) SetOptimistic(value bool) {
	p.OptimisticUnchoked = value
}

// MetadataSize is the info-dict size from the extension handshake.
func (p *Peer) MetadataSize() uint32 {
	return uint32(p.ExtensionHandshake.MetadataSize)
}

// RequestMetadataPiece asks for one metadata block.
func (p *Peer) RequestMetadataPiece(index uint32) {
	p.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata],
		Payload: peerprotocol.ExtensionMetadataMessage{
			Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
			Piece: index,
		},
	})
}

// RequestPiece asks for one block.
func (p *Peer) RequestPiece(index, begin, length uint32) {
	p.SendMessage(peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length})
}

// CancelPiece withdraws one block request.
func (p *Peer) CancelPiece(index, begin, length uint32) {
	p.SendMessage(peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length}})
}

// EnabledFast reports fast-extension support.
func (p *Peer) EnabledFast() bool {
	return p.FastEnabled
}

// Client names the peer: the handshake version string when present,
// otherwise the asciified peer id.
func (p *Peer) Client() string {
	if p.ExtensionHandshake != nil && p.ExtensionHandshake.V != "" {
		return stringutil.Printable(p.ExtensionHandshake.V)
	}
	return stringutil.Asciify(clientID(string(p.ID[:])))
}

// GenerateAndSendAllowedFastMessages deals the BEP 6 fast set once per
// connection, skipping peers already served.
func (p *Peer) GenerateAndSendAllowedFastMessages(k int, numPieces uint32, infoHash [20]byte, pieces []piece.Piece) {
	if k == 0 {
		return
	}
	if p.SentAllowedFast.Len() > 0 {
		return
	}
	for _, index := range fast.GenerateFastSet(k, numPieces, infoHash, p.Conn.Addr().IP) {
		p.SentAllowedFast.Add(&pieces[index])
		p.SendMessage(peerprotocol.AllowedFastMessage{HaveMessage: peerprotocol.HaveMessage{Index: index}})
	}
}
