package main

// portmap.go opens the shared peer port on the router, like libtorrent:
// UPnP IGD (WANIPConnection v1/v2, WANPPPConnection) and NAT-PMP. The TCP
// port takes peers, the UDP port is the DHT. Leases are renewed every 20
// minutes and removed on shutdown.

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
	natpmp "github.com/jackpal/go-nat-pmp"
)

const (
	portLease   = time.Hour
	portRefresh = 20 * time.Minute
)

// upnpClient is the subset shared by the IGD service clients.
type upnpClient interface {
	AddPortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, lease uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) error
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	LocalAddr() net.IP
}

// portMapper keeps the router mappings alive.
type portMapper struct {
	port   int
	upnp   bool
	natpmp bool
	stop   chan struct{}
	done   chan struct{}

	mu         sync.Mutex
	method     string
	externalIP string
	lastErr    string
	mappedAt   time.Time
	upnpClient upnpClient
	pmpClient  *natpmp.Client
}

func newPortMapper(port int, upnp, pmp bool) *portMapper {
	return &portMapper{port: port, upnp: upnp, natpmp: pmp, stop: make(chan struct{}), done: make(chan struct{})}
}

func (m *portMapper) start() {
	if m == nil || (!m.upnp && !m.natpmp) || m.port <= 0 {
		close(m.done)
		return
	}
	go func() {
		defer close(m.done)
		for {
			m.refresh()
			select {
			case <-m.stop:
				m.unmap()
				return
			case <-time.After(portRefresh):
			}
		}
	}()
}

func (m *portMapper) close() {
	if m == nil {
		return
	}
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	<-m.done
}

type portMapStatus struct {
	Method     string `json:"method,omitempty"`
	ExternalIP string `json:"external_ip,omitempty"`
	MappedAt   int64  `json:"mapped_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (m *portMapper) status() portMapStatus {
	if m == nil {
		return portMapStatus{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := portMapStatus{Method: m.method, ExternalIP: m.externalIP, Error: m.lastErr}
	if !m.mappedAt.IsZero() {
		out.MappedAt = m.mappedAt.Unix()
	}
	return out
}

func (m *portMapper) refresh() {
	var errs []string
	if m.upnp {
		if err := m.mapUPnP(); err == nil {
			return
		} else {
			errs = append(errs, "UPnP: "+err.Error())
		}
	}
	if m.natpmp {
		if err := m.mapNATPMP(); err == nil {
			return
		} else {
			errs = append(errs, "NAT-PMP: "+err.Error())
		}
	}
	m.mu.Lock()
	changed := m.lastErr != strings.Join(errs, "; ")
	m.lastErr = strings.Join(errs, "; ")
	m.method = ""
	m.mu.Unlock()
	if changed {
		logf("router port %d not opened automatically (%s): forward it by hand for incoming peers", m.port, strings.Join(errs, "; "))
	}
}

func (m *portMapper) mapUPnP() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	m.mu.Lock()
	client := m.upnpClient
	m.mu.Unlock()
	if client == nil {
		var err error
		if client, err = discoverIGD(ctx); err != nil {
			return err
		}
	}
	local := client.LocalAddr()
	if local == nil {
		return fmt.Errorf("cannot determine the local address towards the router")
	}
	for _, proto := range []string{"TCP", "UDP"} {
		if err := client.AddPortMappingCtx(ctx, "", uint16(m.port), proto, uint16(m.port), local.String(), true,
			"gx-torrent", uint32(portLease.Seconds())); err != nil {
			m.mu.Lock()
			m.upnpClient = nil
			m.mu.Unlock()
			return err
		}
	}
	external, _ := client.GetExternalIPAddressCtx(ctx)
	m.recordSuccess("upnp", external, client, nil)
	return nil
}

func discoverIGD(ctx context.Context) (upnpClient, error) {
	if clients, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	return nil, fmt.Errorf("no UPnP internet gateway found")
}

func (m *portMapper) mapNATPMP() error {
	m.mu.Lock()
	client := m.pmpClient
	m.mu.Unlock()
	if client == nil {
		gateway, err := defaultGateway()
		if err != nil {
			return err
		}
		client = natpmp.NewClientWithTimeout(gateway, 5*time.Second)
	}
	for _, proto := range []string{"tcp", "udp"} {
		if _, err := client.AddPortMapping(proto, m.port, m.port, int(portLease.Seconds())); err != nil {
			return err
		}
	}
	external := ""
	if result, err := client.GetExternalAddress(); err == nil {
		external = net.IP(result.ExternalIPAddress[:]).String()
	}
	m.recordSuccess("natpmp", external, nil, client)
	return nil
}

func (m *portMapper) recordSuccess(method, external string, upnp upnpClient, pmp *natpmp.Client) {
	m.mu.Lock()
	first := m.method != method || m.externalIP != external
	m.method = method
	m.externalIP = external
	m.lastErr = ""
	m.mappedAt = time.Now()
	m.upnpClient = upnp
	m.pmpClient = pmp
	m.mu.Unlock()
	if first {
		logf("router port %d opened with %s (external address %s)", m.port, strings.ToUpper(method), external)
	}
}

func (m *portMapper) unmap() {
	m.mu.Lock()
	upnp, pmp := m.upnpClient, m.pmpClient
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if upnp != nil {
		for _, proto := range []string{"TCP", "UDP"} {
			_ = upnp.DeletePortMappingCtx(ctx, "", uint16(m.port), proto)
		}
	}
	if pmp != nil {
		for _, proto := range []string{"tcp", "udp"} {
			_, _ = pmp.AddPortMapping(proto, m.port, 0, 0)
		}
	}
}

// defaultGateway reads the IPv4 default route from /proc/net/route.
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
		// The kernel prints the address little-endian.
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]), nil
	}
	return nil, fmt.Errorf("no default gateway")
}
