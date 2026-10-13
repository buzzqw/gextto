package udptracker

import (
	"context"
	"io"
	"math/rand/v2"
)

// transaction pairs one outgoing request with a random id and a child
// context. The transport run loop cancels the context when the matching
// reply arrives (or the exchange fails), releasing everyone waiting on it.
type transaction struct {
	id int32

	// The in-flight exchange: a connect, announce or scrape request.
	request udpRequest

	ctx    context.Context
	cancel func()
}

// udpRequest is everything the transport needs to send an exchange and
// deliver its outcome: wire serialization, addressing, and completion.
type udpRequest interface {
	io.WriterTo
	SetTransactionID(int32)
	SetConnectionID(int64)
	GetContext() context.Context
	GetDest() string
	Done() <-chan struct{}
	GetResponse() (data []byte, err error)
	SetResponse(data []byte, err error)
}

// newTransaction tags req with a random id and scopes it to a child of the
// request context.
func newTransaction(req udpRequest) *transaction {
	t := &transaction{
		id:      rand.Int32(), // nolint: gosec
		request: req,
	}
	req.SetTransactionID(t.id)
	t.ctx, t.cancel = context.WithCancel(req.GetContext())
	return t
}
