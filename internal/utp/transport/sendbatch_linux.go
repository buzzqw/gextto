//go:build linux

package transport

// UDP GSO (Generic Segmentation Offload): one write carries a whole burst and
// the kernel splits it into datagrams of the given size. It needs Linux 4.18+
// and a plain UDP socket; anything else falls back to one write per packet.

import (
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

// gsoOOB is the control message every segmented send builds: the same 24 bytes
// each time, so it lives here instead of on the heap. Only gsoSend touches it,
// and every send runs under the package mutex.
var gsoOOB [24]byte // CmsgSpace(2): 16-byte header plus the aligned data

// gsoSend writes data as len(sizes) datagrams, all of the first packet's size
// except the last, with one call. It reports false when the batch does not
// have that shape or the socket is not a plain UDP socket.
func (s *Socket) gsoSend(data []byte, sizes []int, addr net.Addr) bool {
	if len(sizes) < 2 || len(data) == 0 {
		return false
	}
	segment := sizes[0]
	if segment <= 0 {
		return false
	}
	// GSO needs every segment to be the announced size; only the last may be
	// shorter, which is what the tail of a write looks like.
	for _, size := range sizes[1 : len(sizes)-1] {
		if size != segment {
			return false
		}
	}
	if sizes[len(sizes)-1] > segment {
		return false
	}
	conn, ok := s.pc.(*net.UDPConn)
	if !ok {
		return false
	}
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return false
	}
	// A link-local address with a zone needs a scope id this control message
	// does not carry: let the normal path handle it.
	if udpAddr.Zone != "" {
		return false
	}
	// UDP_SEGMENT carries a __u16 and the kernel wants the exact cmsg length.
	// The control message is the same for every send, so it is reused.
	oob := gsoOOB[:]
	h := (*unix.Cmsghdr)(unsafe.Pointer(&oob[0]))
	h.Level = unix.IPPROTO_UDP
	h.Type = unix.UDP_SEGMENT
	h.SetLen(unix.CmsgLen(2))
	*(*uint16)(unsafe.Pointer(&oob[unix.CmsgLen(0)])) = uint16(segment)
	if _, _, err := conn.WriteMsgUDP(data, oob, udpAddr); err != nil {
		return false
	}
	gsoBatches.Add(1)
	gsoPackets.Add(int64(len(sizes)))
	return true
}
