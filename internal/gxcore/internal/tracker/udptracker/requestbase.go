package udptracker

import "context"

// requestBase is the shared plumbing of every UDP tracker exchange:
// caller context, destination, and the single-shot completion channel the
// transport closes when the reply (or its error) lands.
type requestBase struct {
	ctx  context.Context
	dest string

	response []byte
	err      error
	done     chan struct{}
}

// newRequestBase prepares an exchange toward dest, cancellable via ctx.
func newRequestBase(ctx context.Context, dest string) *requestBase {
	return &requestBase{ctx: ctx, dest: dest, done: make(chan struct{})}
}

// GetContext returns the caller's context.
func (r *requestBase) GetContext() context.Context {
	return r.ctx
}

// GetDest returns the "host:port" destination.
func (r *requestBase) GetDest() string {
	return r.dest
}

// Done closes when the transport records the outcome.
func (r *requestBase) Done() <-chan struct{} {
	return r.done
}

// GetResponse returns the raw reply (or the failure) after Done closes.
func (r *requestBase) GetResponse() (data []byte, err error) {
	return r.response, r.err
}

// SetResponse records the outcome and wakes the waiter. Called once by the
// transport run loop.
func (r *requestBase) SetResponse(data []byte, err error) {
	r.response, r.err = data, err
	close(r.done)
}
