//go:build linux

package ed2k

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
)

// defaultGateway reads the IPv4 default route from /proc/net/route. It is used
// by NAT-PMP and PCP, which address the router directly instead of discovering
// it over SSDP.
func defaultGateway() (net.IP, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		raw, err := hex.DecodeString(fields[2])
		if err != nil || len(raw) != 4 {
			continue
		}
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]), nil
	}
	return nil, fmt.Errorf("no default gateway")
}
