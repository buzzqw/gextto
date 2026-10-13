package ctxutil

import "context"

// FromChan bridges a done-style channel to a context: the context is
// cancelled when ch closes, or when the returned cancel is called.
// Call cancel to release the bridging goroutine when ch outlives its use
// (e.g. a session-level stop channel surviving one torrent's shutdown).
func FromChan(ch <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
