package torrent

import (
	"encoding/hex"
	"time"
)

// EventType classifies a Session event.
type EventType string

const (
	// EventTorrentAdded fires when a torrent was added to the session.
	EventTorrentAdded EventType = "torrent_added"
	// EventTorrentCompleted fires when every wanted piece is present.
	EventTorrentCompleted EventType = "torrent_completed"
	// EventTorrentStopped fires when a torrent stopped without an error.
	EventTorrentStopped EventType = "torrent_stopped"
	// EventTorrentError fires when a torrent stopped on an error (storage,
	// allocation, verification).
	EventTorrentError EventType = "torrent_error"
	// EventTorrentPiece fires for every completed piece. High frequency: a
	// subscriber that cannot keep up drops these events.
	EventTorrentPiece EventType = "torrent_piece"
	// EventPeerConnected fires when a peer finishes the handshake.
	EventPeerConnected EventType = "peer_connected"
	// EventPeerDisconnected fires when a connected peer goes away.
	EventPeerDisconnected EventType = "peer_disconnected"
	// EventTrackerAnnounce fires after every announce attempt (success or
	// failure) with the swarm counts on success.
	EventTrackerAnnounce EventType = "tracker_announce"
)

// Event is a session-level notification. The engine pushes it to every
// subscriber, so a consumer does not have to poll for state changes.
type Event struct {
	Type    EventType
	Torrent string // info hash, hex
	Name    string
	Error   string
	// Piece is set for EventTorrentPiece.
	Piece uint32
	// Peer is the "ip:port" address for the peer events.
	Peer string
	// Seeders/Leechers are set for a successful EventTrackerAnnounce.
	Seeders  int
	Leechers int
	At       time.Time
}

// subscribeBuffer is the per-subscriber queue. A slow subscriber drops events
// rather than stalling a torrent run loop.
const subscribeBuffer = 64

// Subscribe returns a channel carrying session events until cancel is called.
// The engine never blocks on a subscriber: a full channel drops the event.
func (s *Session) Subscribe() (<-chan Event, func()) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.subs == nil {
		s.subs = make(map[int]chan Event)
	}
	id := s.subNext
	s.subNext++
	ch := make(chan Event, subscribeBuffer)
	s.subs[id] = ch
	cancel := func() {
		s.subMu.Lock()
		defer s.subMu.Unlock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
	}
	return ch, cancel
}

func (s *Session) emit(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	s.subMu.Lock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default: // drop on a slow subscriber, never block the engine
		}
	}
	s.subMu.Unlock()
}

// baseEvent builds an event carrying the torrent identity.
func (t *torrent) baseEvent(typ EventType) Event {
	ev := Event{Type: typ, Torrent: hex.EncodeToString(t.infoHash[:])}
	if t.info != nil {
		ev.Name = t.info.Name
	}
	return ev
}

// emit is a helper for torrent-scoped events.
func (t *torrent) emit(typ EventType, err error) {
	if t.session == nil {
		return
	}
	ev := t.baseEvent(typ)
	if err != nil {
		ev.Error = err.Error()
	}
	t.session.emit(ev)
}

func (t *torrent) emitPiece(index uint32) {
	if t.session == nil {
		return
	}
	ev := t.baseEvent(EventTorrentPiece)
	ev.Piece = index
	t.session.emit(ev)
}

func (t *torrent) emitPeer(typ EventType, addr string) {
	if t.session == nil {
		return
	}
	ev := t.baseEvent(typ)
	ev.Peer = addr
	t.session.emit(ev)
}
