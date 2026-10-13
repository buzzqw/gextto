package peersource

// Source records where a peer address came from, for diagnostics,
// per-source budgeting, and the session stats breakdown.
type Source int

const (
	// Tracker peers arrived in a tracker announce.
	Tracker Source = iota
	// DHT peers arrived through the DHT.
	DHT
	// PEX peers were gossiped by another peer.
	PEX
	// Manual peers were added by the operator.
	Manual
	// Incoming peers dialed us.
	Incoming
	// Holepunch peers arrived via a BEP 55 rendezvous.
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
