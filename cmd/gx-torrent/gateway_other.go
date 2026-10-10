//go:build !linux && !windows

package main

import (
	"errors"
	"net"
)

// defaultGateway is not implemented on this platform: UPnP/NAT-PMP fall back
// to SSDP discovery.
func defaultGateway() (net.IP, error) {
	return nil, errors.New("default gateway lookup is not supported on this platform")
}
