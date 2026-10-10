package main

// network.go: listening port, interfaces, proxy, encryption and IP filter.

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// NetworkOptions mirror Gextto's libtorrent network settings.
type NetworkOptions struct {
	// PortBegin..PortEnd: the first free port is the single peer port (TCP
	// for peers, UDP for the DHT). Zero keeps the engine's port per torrent.
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
	// DHTBootstrap are the routers used to bootstrap the DHT (comma-separated
	// from Gextto's libtorrent_dht_bootstrap_nodes); empty keeps the engine's defaults.
	DHTBootstrap []string
	PEX          bool
	// UTP adds uTP (UDP) next to TCP for peers; the UDP port is shared with
	// the DHT. UTPOnly dials uTP alone (tests).
	UTP     bool
	UTPOnly bool
	// Holepunch enables BEP 55 holepunching over uTP, so peers behind a NAT
	// can still be reached through a relaying peer.
	Holepunch bool
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
// UDP on the listen address. An unspecified host is probed as dual-stack, the
// same binding the engine will use (F4 IPv6).
func choosePeerPort(host string, begin, end uint16) (int, error) {
	if end < begin {
		end = begin
	}
	probeHost := host
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		probeHost = ""
	}
	for port := int(begin); port <= int(end); port++ {
		address := net.JoinHostPort(probeHost, fmt.Sprint(port))
		tcp, err := net.Listen("tcp", address)
		if err != nil {
			continue
		}
		udp, err := net.ListenPacket("udp", address)
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

// applyNetwork fills the engine configuration.
func (d *Daemon) applyNetwork(cfg *torrent.Config) {
	n := d.opts.Network
	cfg.Host = d.listenHost
	if d.peerPort > 0 {
		cfg.ListenPort = uint16(d.peerPort)
		cfg.DHTPort = uint16(d.peerPort)
	}
	cfg.DHTHost = d.listenHost
	cfg.DHTEnabled = n.DHT
	if len(n.DHTBootstrap) > 0 {
		cfg.DHTBootstrapNodes = n.DHTBootstrap
	}
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
	// Holepunching is uTP-only (it punches UDP mappings), so it follows uTP.
	cfg.Holepunch = n.Holepunch && cfg.UTP
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

// ipFilterBytes returns the filter file content, reusing the cached copy while
// path, size and mtime are unchanged. The same bytes are then handed to the engine,
// which still parses them (its blocklist is session-scoped), but the multi-
// megabyte disk read is skipped on every session reopen.
func (d *Daemon) ipFilterBytes(path string) ([]byte, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	stamp := fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())
	if d.ipFilterData != nil && d.ipFilterStamp == stamp {
		return d.ipFilterData, stamp, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, stamp, nil
}

// loadIPFilterLocked (re)loads the IP filter file into the session.
func (d *Daemon) loadIPFilterLocked(path string) (int, error) {
	if path == "" || d.session == nil {
		return 0, nil
	}
	data, stamp, err := d.ipFilterBytes(path)
	if err != nil {
		return 0, err
	}
	rules, err := d.session.LoadBlocklist(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	d.ipFilterData = data
	d.ipFilterStamp = stamp
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
	// A manual reload must bypass the cache: the operator may have edited the
	// file without changing its path.
	d.ipFilterStamp = ""
	rules, err := d.loadIPFilterLocked(path)
	if err == nil {
		logf("IP filter loaded: %d rules from %s", rules, path)
	}
	return rules, err
}

// ipFilterFetchTimeout bounds the download of an IP filter list.
const ipFilterFetchTimeout = 60 * time.Second

// fetchIPFilterURL downloads an IP filter list (plain or gzip), stores it in the
// daemon data directory and returns the local path. It lets the page load a
// filter straight from a URL, like Gextto does.
func (d *Daemon) fetchIPFilterURL(rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ipFilterFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gx-torrent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", err
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, gzErr := gzip.NewReader(bytes.NewReader(data))
		if gzErr != nil {
			return "", gzErr
		}
		defer reader.Close()
		if data, err = io.ReadAll(io.LimitReader(reader, 256<<20)); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(d.opts.DataDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(d.opts.DataDir, "ipfilter.dat")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
