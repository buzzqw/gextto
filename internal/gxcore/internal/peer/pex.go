package peer

import (
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/pexlist"
)

// pex trades peer addresses with one connection (BEP 11): discovered
// addresses queue up and flush once a minute. Recently seen peers seed the
// dropped set so we do not get our own swarm echoed straight back.
type pex struct {
	conn  *peerconn.Conn
	extID uint8

	pexList *pexlist.PEXList

	pexAddPeerC  chan *net.TCPAddr
	pexDropPeerC chan *net.TCPAddr

	closeC chan struct{}
	doneC  chan struct{}
}

// newPEX starts an exchange on conn, announcing extID as our PEX extension
// number and preloading everyone already connected (except conn itself).
func newPEX(conn *peerconn.Conn, extID uint8, initialPeers map[*Peer]struct{}, recentlySeen *pexlist.RecentlySeen) *pex {
	pl := pexlist.NewWithRecentlySeen(recentlySeen.Peers())
	self := conn.Addr().String()
	for pe := range initialPeers {
		if addr := pe.Addr().String(); addr != self {
			pl.Add(pe.Addr())
		}
	}
	return &pex{
		conn:         conn,
		extID:        extID,
		pexList:      pl,
		pexAddPeerC:  make(chan *net.TCPAddr),
		pexDropPeerC: make(chan *net.TCPAddr),
		closeC:       make(chan struct{}),
		doneC:        make(chan struct{}),
	}
}

func (p *pex) close() {
	close(p.closeC)
	<-p.doneC
}

func (p *pex) run() {
	defer close(p.doneC)

	p.pexFlushPeers()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case addr := <-p.pexAddPeerC:
			p.pexList.Add(addr)
		case addr := <-p.pexDropPeerC:
			p.pexList.Drop(addr)
		case <-ticker.C:
			p.pexFlushPeers()
		case <-p.closeC:
			return
		}
	}
}

// Add queues a discovered address. Dropped silently once closed.
func (p *pex) Add(addr *net.TCPAddr) {
	select {
	case p.pexAddPeerC <- addr:
	case <-p.doneC:
	}
}

// Drop queues a gone address. Dropped silently once closed.
func (p *pex) Drop(addr *net.TCPAddr) {
	select {
	case p.pexDropPeerC <- addr:
	case <-p.doneC:
	}
}

// pexFlushPeers sends the queued addresses, if any, as one extension message.
func (p *pex) pexFlushPeers() {
	added, added6, dropped, dropped6 := p.pexList.Flush()
	if len(added) == 0 && len(added6) == 0 && len(dropped) == 0 && len(dropped6) == 0 {
		return
	}
	p.conn.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.extID,
		Payload: peerprotocol.ExtensionPEXMessage{
			Added:    added,
			Added6:   added6,
			Dropped:  dropped,
			Dropped6: dropped6,
		},
	})
}
