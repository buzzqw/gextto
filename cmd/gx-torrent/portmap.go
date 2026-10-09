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
	GetSpecificPortMappingEntryCtx(ctx context.Context, remoteHost string, externalPort uint16, protocol string) (internalPort uint16, internalClient string, enabled bool, description string, lease uint32, err error)
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
		// Verify before (re)adding: a mapping that is already there does not
		// need to be reopened every refresh.
		if m.upnpMappingPresent(ctx, client, proto) {
			continue
		}
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

// upnpMappingPresent reports whether the router already forwards the peer port
// to this host for the given protocol.
func (m *portMapper) upnpMappingPresent(ctx context.Context, client upnpClient, proto string) bool {
	internalPort, internalClient, enabled, _, _, err := client.GetSpecificPortMappingEntryCtx(ctx, "", uint16(m.port), proto)
	if err != nil || !enabled || int(internalPort) != m.port {
		return false
	}
	local := client.LocalAddr()
	if local != nil && strings.TrimSpace(internalClient) != "" && internalClient != local.String() {
		return false
	}
	return true
}

// portCheck is the result of the "Test porte" action: whether the daemon is
// listening locally and whether the router forwards the port.
type portCheck struct {
	Port       int    `json:"port"`
	Listening  bool   `json:"listening"`
	Mapped     bool   `json:"mapped"`
	Method     string `json:"method,omitempty"`
	ExternalIP string `json:"external_ip,omitempty"`
	Open       bool   `json:"open"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

// checkLocalListener dials the daemon's own peer port: a successful TCP
// connection proves the listener is bound.
func checkLocalListener(port int) bool {
	if port <= 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// check reports whether the peer port is open, verifying the UPnP mapping with
// the router when possible instead of trusting only the last map result.
func (m *portMapper) check() portCheck {
	if m == nil {
		return portCheck{}
	}
	out := portCheck{Port: m.port, Listening: checkLocalListener(m.port)}
	m.mu.Lock()
	method, external, lastErr := m.method, m.externalIP, m.lastErr
	upnp := m.upnpClient
	mappedRecently := !m.mappedAt.IsZero() && time.Since(m.mappedAt) < 2*portRefresh
	m.mu.Unlock()
	out.Method, out.ExternalIP, out.Error = method, external, lastErr

	switch {
	case method == "upnp" && upnp != nil:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tcp := m.upnpMappingPresent(ctx, upnp, "TCP")
		udp := m.upnpMappingPresent(ctx, upnp, "UDP")
		out.Mapped = tcp && udp
		if out.Mapped {
			out.Detail = "mapping UPnP presente (TCP+UDP)"
		} else {
			out.Detail = "mapping UPnP mancante o incompleto"
		}
	case mappedRecently:
		out.Mapped = true
		out.Detail = "mapping " + strings.ToUpper(method) + " attivo"
	default:
		out.Mapped = false
		out.Detail = "nessun mapping automatico: verifica l'inoltro manuale sul router"
	}
	out.Open = out.Listening && out.Mapped
	return out
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
