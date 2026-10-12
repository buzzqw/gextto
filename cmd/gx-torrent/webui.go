package main

// webui.go prints the address a person should open in a browser. The daemon
// binds a wildcard by default ([::] / 0.0.0.0), which is not an address anyone
// can type; the startup log therefore lists the machine's LAN address
// (reachable from other devices) and loopback, so the web UI can be opened
// without hunting for the IP. When a specific address is bound, only that one is
// listed.

import (
	"net"
	"strings"
)

// webUIURLs returns the browser URLs for a bound listen address (host, port):
// the LAN address first, then loopback, deduplicated. A concrete host yields a
// single URL.
func webUIURLs(boundHost, port string) []string {
	host := strings.TrimSpace(boundHost)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{"http://" + net.JoinHostPort(host, port) + "/"}
	}
	var urls []string
	seen := map[string]bool{}
	for _, ip := range []net.IP{primaryLocalIP(), net.IPv4(127, 0, 0, 1)} {
		if ip == nil {
			continue
		}
		url := "http://" + net.JoinHostPort(ip.String(), port) + "/"
		if !seen[url] {
			seen[url] = true
			urls = append(urls, url)
		}
	}
	return urls
}

// primaryLocalIP is the address of the interface that reaches the default
// gateway, i.e. the machine's LAN address. It never sends a packet (a UDP dial
// only picks the route). When the gateway is unknown it falls back to the first
// non-loopback IPv4 interface address; nil when nothing is found (isolated
// host), in which case only loopback is used.
func primaryLocalIP() net.IP {
	if gateway, err := defaultGateway(); err == nil && gateway != nil {
		if ip := localIPTowards(gateway); ip != nil {
			return ip
		}
	}
	return firstInterfaceIPv4()
}

// localIPTowards returns the local IPv4 the kernel would use to reach dst.
func localIPTowards(dst net.IP) net.IP {
	if dst == nil || dst.To4() == nil {
		return nil
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: dst, Port: 53})
	if err != nil {
		return nil
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		if ip := addr.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			return ip
		}
	}
	return nil
}

// firstInterfaceIPv4 returns the first usable (up, non-loopback, non-link-local)
// IPv4 address of the host.
func firstInterfaceIPv4() net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ip := ipnet.IP.To4(); ip != nil && !ip.IsLinkLocalUnicast() {
					return ip
				}
			}
		}
	}
	return nil
}
