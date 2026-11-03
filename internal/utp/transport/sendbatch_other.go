//go:build !linux

package transport

// No UDP GSO outside Linux: a burst is written one datagram at a time.

import "net"

// gsoSend reports false so the batch takes the one-write-per-packet path.
func (s *Socket) gsoSend([]byte, []int, net.Addr) bool {
	return false
}
