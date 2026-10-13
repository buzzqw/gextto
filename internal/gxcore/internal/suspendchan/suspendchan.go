package suspendchan

// Chan wraps a channel with a suspend switch. Sends never block on the
// switch itself; receivers use ReceiveC, which yields nil while suspended,
// parking the receiver in the select until Resume is called.
type Chan[T any] struct {
	ch        chan T
	suspended bool
}

// New builds a suspendable channel with the given buffer size.
func New[T any](buflen int) *Chan[T] {
	return &Chan[T]{ch: make(chan T, buflen)}
}

// SendC exposes the channel for sending. Sending is never suspended.
func (c *Chan[T]) SendC() chan T {
	return c.ch
}

// ReceiveC exposes the channel for receiving, or nil while suspended.
// A nil channel in a select blocks forever, which is exactly the parking
// behavior suspend relies on.
func (c *Chan[T]) ReceiveC() chan T {
	if c.suspended {
		return nil
	}
	return c.ch
}

// Suspend parks receivers until Resume is called.
func (c *Chan[T]) Suspend() {
	c.suspended = true
}

// Resume reverts a previous Suspend.
func (c *Chan[T]) Resume() {
	c.suspended = false
}
