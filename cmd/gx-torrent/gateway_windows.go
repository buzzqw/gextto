//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi          = windows.NewLazySystemDLL("iphlpapi.dll")
	getIPForwardTable = iphlpapi.NewProc("GetIpForwardTable")
)

// mibIPForwardRowSize is sizeof(MIB_IPFORWARDROW) on both 32- and 64-bit
// Windows (14 DWORDs).
const mibIPForwardRowSize = 56

// defaultGateway returns the IPv4 default route from the Windows routing table
// (the route with destination and mask 0.0.0.0), choosing the one with the
// lowest metric. It is not available on every system, so callers fall back to
// letting UPnP/NAT-PMP pick the interface.
func defaultGateway() (net.IP, error) {
	var size uint32
	// The first call sizes the table (ERROR_INSUFFICIENT_BUFFER = 122). A route
	// added in between (VPN, DHCP renewal) makes the table grow, so the sized
	// call is retried a few times with the new size.
	const insufficientBuffer = 122
	ret, _, _ := getIPForwardTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if ret != insufficientBuffer || size == 0 {
		return nil, fmt.Errorf("GetIpForwardTable failed: %d", ret)
	}
	var buffer []byte
	for attempt := 0; ; attempt++ {
		buffer = make([]byte, size)
		ret, _, _ = getIPForwardTable.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0)
		if ret == 0 {
			break
		}
		if ret != insufficientBuffer || attempt == 2 {
			return nil, fmt.Errorf("GetIpForwardTable failed: %d", ret)
		}
	}
	if len(buffer) < 4 {
		return nil, fmt.Errorf("no default gateway")
	}
	count := binary.LittleEndian.Uint32(buffer[0:4])
	bestMetric := ^uint32(0)
	var best net.IP
	for i := uint32(0); i < count; i++ {
		off := 4 + int(i)*mibIPForwardRowSize
		if off+mibIPForwardRowSize > len(buffer) {
			break
		}
		dest := binary.LittleEndian.Uint32(buffer[off : off+4])
		mask := binary.LittleEndian.Uint32(buffer[off+4 : off+8])
		nextHop := binary.LittleEndian.Uint32(buffer[off+12 : off+16])
		metric := binary.LittleEndian.Uint32(buffer[off+36 : off+40])
		if dest != 0 || mask != 0 {
			continue
		}
		if metric < bestMetric {
			bestMetric = metric
			ip := make(net.IP, 4)
			binary.LittleEndian.PutUint32(ip, nextHop)
			best = ip
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no default gateway")
	}
	return best, nil
}
