package peersource

// Source indicates how we found the Peer.
type Source int

const (
	// Tracker indicates that the peer is found from tracker by announcing the torrent.
	Tracker Source = iota
	// DHT indicates that the peer is found from DHT node.
	DHT
	// PEX indicates that the peer is found from another peer with PEX messages.
	PEX
	// Manual indicates that the peer is added manually by user.
	Manual
	// Incoming indicates that the peer found us. We did not found the peer.
	Incoming
	// Holepunch indicates that the peer was reached via a BEP 55 rendezvous
	Holepunch
)

func (s Source) String() string {
	switch s {
	case Tracker:
		return "tracker"
	case DHT:
		return "dht"
	case PEX:
		return "pex"
	case Manual:
		return "manual"
	case Incoming:
		return "incoming"
	case Holepunch:
		return "holepunch"
	default:
		panic("unhandled source")
	}
}
