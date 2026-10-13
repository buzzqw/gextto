package acceptor

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// Acceptor pumps accepted sockets from a listener into newConns until
// Close. A socket that arrives as Close fires is closed on the spot instead
// of leaking into a session nobody drains anymore.
type Acceptor struct {
	listener net.Listener
	newConns chan net.Conn
	closeC   chan struct{}
	doneC    chan struct{}
	log      logger.Logger
}

// New builds an acceptor feeding newConns from lis.
func New(lis net.Listener, newConns chan net.Conn, l logger.Logger) *Acceptor {
	return &Acceptor{
		listener: lis,
		newConns: newConns,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
		log:      l,
	}
}

// Close stops accepting, closes the listener, and waits for Run to return.
func (a *Acceptor) Close() {
	close(a.closeC)
	<-a.doneC
}

// Run accepts until the listener fails. A failure after Close is the normal
// shutdown path (the listener was closed deliberately) and stays silent;
// any other failure is logged and still ends the loop.
func (a *Acceptor) Run() {
	defer close(a.doneC)

	relayDone := make(chan struct{})
	defer close(relayDone)
	go func() {
		select {
		case <-a.closeC:
			a.listener.Close()
		case <-relayDone:
		}
	}()

	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.closeC:
			default:
				a.log.Error(err)
			}
			return
		}
		select {
		case a.newConns <- conn:
		case <-a.closeC:
			conn.Close()
			return
		}
	}
}
