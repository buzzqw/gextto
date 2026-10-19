//go:build !linux

package ed2k

import (
	"errors"
	"net"
)

// defaultGateway is only implemented on Linux. Other platforms keep UPnP,
// which does not need the gateway address.
func defaultGateway() (net.IP, error) {
	return nil, errors.New("default gateway lookup is not supported on this platform")
}
