package main

// portmap.go opens the shared peer port on the router, like libtorrent:
// UPnP IGD (WANIPConnection v1/v2, WANPPPConnection) and NAT-PMP. The TCP
// port takes peers, the UDP port is the DHT. Leases are renewed every 20
// minutes and removed on shutdown.

import (
	"context"
	"fmt"
	"net"
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
	pcp    bool
	stop   chan struct{}
	done   chan struct{}

	started    bool
	mu         sync.Mutex
	method     string
	externalIP string
	lastErr    string
	mappedAt   time.Time
	upnpClient upnpClient
	pmpClient  *natpmp.Client
	pcpGateway net.IP
}

func newPortMapper(port int, upnp, pmp bool) *portMapper {
	// PCP is tried whenever NAT-PMP is enabled: both target the default gateway
	// and PCP is its successor, so no separate switch is needed.
	return &portMapper{port: port, upnp: upnp, natpmp: pmp, pcp: pmp, stop: make(chan struct{}), done: make(chan struct{})}
}

func (m *portMapper) start() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	if (!m.upnp && !m.natpmp && !m.pcp) || m.port <= 0 {
		close(m.done)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
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
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
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
			errs = append(errs, oneLine("UPnP: "+err.Error()))
		}
	}
	if m.pcp {
		if err := m.mapPCP(); err == nil {
			return
		} else {
			errs = append(errs, oneLine("PCP: "+err.Error()))
		}
	}
	if m.natpmp {
		if err := m.mapNATPMP(); err == nil {
			return
		} else {
			errs = append(errs, oneLine("NAT-PMP: "+err.Error()))
		}
	}
	message := strings.Join(errs, "; ")
	m.mu.Lock()
	changed := m.lastErr != message
	m.lastErr = message
	m.method = ""
	m.mu.Unlock()
	if changed {
		logf("router port %d not opened automatically (%s): forward it by hand for incoming peers", m.port, message)
	}
}

// oneLine collapses runs of whitespace (including the newlines of a SOAP fault)
// into single spaces, so a router error stays on one log line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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

func upnpMappingPresentForPort(ctx context.Context, client upnpClient, proto string, port int) bool {
	internalPort, internalClient, enabled, _, _, err := client.GetSpecificPortMappingEntryCtx(ctx, "", uint16(port), proto)
	if err != nil || !enabled || int(internalPort) != port {
		return false
	}
	local := client.LocalAddr()
	if local != nil && strings.TrimSpace(internalClient) != "" && internalClient != local.String() {
		return false
	}
	return true
}

// upnpMappingPresent reports whether the router already forwards the peer port
// to this host for the given protocol.
func (m *portMapper) upnpMappingPresent(ctx context.Context, client upnpClient, proto string) bool {
	return upnpMappingPresentForPort(ctx, client, proto, m.port)
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
	// ReflectedPort is the external peer port that remote DHT nodes report for
	// us (BEP 42 "ip" field), 0 while unknown. When it equals Port the NAT keeps
	// the port; a different value means the NAT rewrites it.
	ReflectedPort int `json:"reflected_port,omitempty"`
	// NATMapping is the mapping type inferred from DHT observations: "cone"
	// (endpoint-independent) or "symmetric" (address/port-dependent), empty
	// while unknown.
	NATMapping string `json:"nat_mapping,omitempty"`
}

// natHint explains the NAT mapping from the DHT-reflected external port.
func natHint(check portCheck) string {
	var hint string
	if check.Port > 0 && check.ReflectedPort == check.Port {
		hint = fmt.Sprintf("il NAT conserva la porta (riflessa %d)", check.ReflectedPort)
	} else {
		hint = fmt.Sprintf("porta riflessa %d: il NAT riscrive la porta", check.ReflectedPort)
	}
	switch check.NATMapping {
	case "cone":
		hint += " · mapping endpoint-independent (cone)"
	case "symmetric":
		hint += " · mapping address/port-dependent (simmetrico)"
	}
	return hint
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
	return m.checkPort(m.port)
}

// checkPort reports whether the specified port is open and bindable locally,
// and whether the router forwards the port.
func (m *portMapper) checkPort(port int) portCheck {
	if port <= 0 {
		if m != nil && m.port > 0 {
			port = m.port
		} else {
			return portCheck{}
		}
	}
	out := portCheck{Port: port}
	if m != nil && port == m.port {
		out.Listening = checkLocalListener(port)
	} else {
		if checkLocalListener(port) {
			out.Listening = true
		} else {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				out.Listening = true
				_ = ln.Close()
			}
		}
	}

	if m == nil {
		out.Open = out.Listening
		return out
	}

	m.mu.Lock()
	method, external, lastErr := m.method, m.externalIP, m.lastErr
	upnp := m.upnpClient
	mappedRecently := !m.mappedAt.IsZero() && time.Since(m.mappedAt) < 2*portRefresh
	m.mu.Unlock()
	out.Method, out.ExternalIP, out.Error = method, external, lastErr

	if port == m.port {
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
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client := upnp
		if client == nil && m.upnp {
			client, _ = discoverIGD(ctx)
		}
		if client != nil {
			if ext, err := client.GetExternalIPAddressCtx(ctx); err == nil && ext != "" {
				out.ExternalIP = ext
			}
			tcp := upnpMappingPresentForPort(ctx, client, "TCP", port)
			udp := upnpMappingPresentForPort(ctx, client, "UDP", port)
			if tcp && udp {
				out.Mapped = true
				out.Method = "upnp"
				out.Detail = "mapping UPnP presente (TCP+UDP)"
			} else {
				local := client.LocalAddr()
				if local != nil {
					errTCP := client.AddPortMappingCtx(ctx, "", uint16(port), "TCP", uint16(port), local.String(), true, "gx-torrent-test", uint32(portLease.Seconds()))
					errUDP := client.AddPortMappingCtx(ctx, "", uint16(port), "UDP", uint16(port), local.String(), true, "gx-torrent-test", uint32(portLease.Seconds()))
					if errTCP == nil && errUDP == nil {
						out.Mapped = true
						out.Method = "upnp"
						out.Detail = "mapping UPnP aperto con successo"
					} else {
						out.Mapped = false
						out.Detail = "richiesta mapping UPnP non riuscita"
					}
				} else {
					out.Mapped = false
					out.Detail = "mapping UPnP mancante o incompleto"
				}
			}
		} else {
			out.Mapped = false
			out.Detail = "nessun router UPnP rilevato: verifica inoltro manuale"
		}
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

// mapPCP opens TCP and UDP on the gateway with PCP (RFC 6887).
func (m *portMapper) mapPCP() error {
	gateway, err := defaultGateway()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	external := ""
	for _, proto := range []byte{pcpProtoTCP, pcpProtoUDP} {
		_, extIP, err := pcpMap(ctx, gateway, pcpPort, proto, uint16(m.port), uint32(portLease.Seconds()))
		if err != nil {
			return err
		}
		if external == "" && extIP != nil && !extIP.IsUnspecified() {
			external = extIP.String()
		}
	}
	m.mu.Lock()
	m.pcpGateway = gateway
	m.mu.Unlock()
	m.recordSuccess("pcp", external, nil, nil)
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
	upnp, pmp, pcpGateway := m.upnpClient, m.pmpClient, m.pcpGateway
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if upnp != nil {
		for _, proto := range []string{"TCP", "UDP"} {
			_ = upnp.DeletePortMappingCtx(ctx, "", uint16(m.port), proto)
		}
	}
	if pcpGateway != nil {
		for _, proto := range []byte{pcpProtoTCP, pcpProtoUDP} {
			_, _, _ = pcpMap(ctx, pcpGateway, pcpPort, proto, uint16(m.port), 0)
		}
	}
	if pmp != nil {
		for _, proto := range []string{"tcp", "udp"} {
			_, _ = pmp.AddPortMapping(proto, m.port, 0, 0)
		}
	}
}

// defaultGateway is defined per platform (gateway_linux.go / gateway_windows.go
// / gateway_other.go).
