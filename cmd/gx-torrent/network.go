package main

// network.go: listening port, interfaces, proxy, encryption and IP filter.

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/cenkalti/rain/torrent"
)

// NetworkOptions mirror Gextto's libtorrent network settings.
type NetworkOptions struct {
	// PortBegin..PortEnd: the first free port is the single peer port (TCP
	// for peers, UDP for the DHT). Zero keeps rain's port per torrent.
	PortBegin uint16
	PortEnd   uint16
	// ListenInterface is an IP or an interface name for incoming peers.
	ListenInterface string
	// OutgoingInterface binds all outgoing traffic (VPN killswitch).
	OutgoingInterface string
	// Proxy: socks5:// or http:// URL. Disables DHT and UDP trackers.
	Proxy string
	// Encryption: 0 disabled, 1 enabled (default), 2 forced.
	Encryption int
	DHT        bool
	PEX        bool
	// UTP adds uTP (UDP) next to TCP for peers; the UDP port is shared with
	// the DHT. UTPOnly dials uTP alone (tests).
	UTP     bool
	UTPOnly bool
	// LSD finds peers on the local network (BEP 14 multicast).
	LSD    bool
	UPnP   bool
	NATPMP bool
	// IPFilter is a local file (CIDR, ranges, P2P or eMule format).
	IPFilter string
	// IPFilterTrackers applies the filter to trackers too.
	IPFilterTrackers bool
}

// resolveInterface turns an interface name or IP into an IPv4 address.
func resolveInterface(name string) (net.IP, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	if ip := net.ParseIP(name); ip != nil {
		return ip, nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", name, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			return ipnet.IP.To4(), nil
		}
	}
	return nil, fmt.Errorf("interface %s has no IPv4 address", name)
}

// choosePeerPort returns the first port of the range free for both TCP and
// UDP on the listen address.
func choosePeerPort(host string, begin, end uint16) (int, error) {
	if end < begin {
		end = begin
	}
	for port := int(begin); port <= int(end); port++ {
		address := net.JoinHostPort(host, fmt.Sprint(port))
		tcp, err := net.Listen("tcp4", address)
		if err != nil {
			continue
		}
		udp, err := net.ListenPacket("udp4", address)
		tcp.Close()
		if err != nil {
			continue
		}
		udp.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port in %d-%d", begin, end)
}

// prepareNetwork resolves the listen address and the shared port once at
// start; session restarts reuse them.
func (d *Daemon) prepareNetwork() error {
	n := d.opts.Network
	host := "0.0.0.0"
	if ip, err := resolveInterface(n.ListenInterface); err != nil {
		return err
	} else if ip != nil {
		host = ip.String()
	}
	d.listenHost = host
	if n.PortBegin > 0 {
		port, err := choosePeerPort(host, n.PortBegin, n.PortEnd)
		if err != nil {
			return err
		}
		d.peerPort = port
	}
	if n.OutgoingInterface != "" {
		if _, err := resolveInterface(n.OutgoingInterface); err != nil {
			// Killswitch: the session still starts, but nothing goes out
			// until the interface has an address.
			logf("outgoing interface %s is not available (%v): no traffic until it comes up", n.OutgoingInterface, err)
		}
	}
	return nil
}

// applyNetwork fills the rain configuration.
func (d *Daemon) applyNetwork(cfg *torrent.Config) {
	n := d.opts.Network
	cfg.Host = d.listenHost
	if d.peerPort > 0 {
		cfg.ListenPort = uint16(d.peerPort)
		cfg.DHTPort = uint16(d.peerPort)
	}
	cfg.DHTHost = d.listenHost
	cfg.DHTEnabled = n.DHT
	cfg.PEXEnabled = n.PEX
	cfg.OutgoingInterface = n.OutgoingInterface
	if n.OutgoingInterface != "" {
		// The DHT is UDP: bind it to the VPN address or turn it off.
		if ip, err := resolveInterface(n.OutgoingInterface); err == nil {
			cfg.DHTHost = ip.String()
		} else {
			cfg.DHTEnabled = false
		}
	}
	cfg.UTP = n.UTP && d.peerPort > 0 && n.Proxy == ""
	cfg.UTPOnly = n.UTPOnly
	if n.OutgoingInterface != "" {
		// uTP shares the DHT socket, bound to the VPN address: without it,
		// UDP must not leave by another route.
		if _, err := resolveInterface(n.OutgoingInterface); err != nil {
			cfg.UTP = false
		}
	}
	cfg.Proxy = n.Proxy
	if n.Proxy != "" {
		// UDP cannot go through the proxy and must not leak around it.
		cfg.DHTEnabled = false
	}
	switch n.Encryption {
	case 0:
		cfg.DisableOutgoingEncryption = true
		cfg.ForceOutgoingEncryption = false
		cfg.ForceIncomingEncryption = false
	case 2:
		cfg.DisableOutgoingEncryption = false
		cfg.ForceOutgoingEncryption = true
		cfg.ForceIncomingEncryption = true
	default:
		cfg.DisableOutgoingEncryption = false
		cfg.ForceOutgoingEncryption = false
		cfg.ForceIncomingEncryption = false
	}
	cfg.BlocklistEnabledForTrackers = n.IPFilterTrackers
	cfg.BlocklistEnabledForIncomingConnections = true
	cfg.BlocklistEnabledForOutgoingConnections = true
}

// loadIPFilterLocked (re)loads the IP filter file into the session.
func (d *Daemon) loadIPFilterLocked(path string) (int, error) {
	if path == "" || d.session == nil {
		return 0, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	rules, err := d.session.LoadBlocklist(file)
	if err != nil {
		return 0, err
	}
	d.ipFilterRules = rules
	d.ipFilterPath = path
	return rules, nil
}

func (d *Daemon) loadIPFilter(path string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if path == "" {
		path = d.ipFilterPath
	}
	if path == "" {
		path = d.opts.Network.IPFilter
	}
	rules, err := d.loadIPFilterLocked(path)
	if err == nil {
		logf("IP filter loaded: %d rules from %s", rules, path)
	}
	return rules, err
}
