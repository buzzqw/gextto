package ed2k

// upnp.go implements the small UPnP IGD subset gx-ed2k needs: discover the
// Internet Gateway Device over SSDP, find its WANIPConnection/WANPPPConnection
// control URL, and add a UDP port mapping for the Kad socket. It is deliberately
// self-contained (standard library only) and best-effort: a router without
// UPnP, a disabled IGD or a refused mapping only means the daemon keeps
// searching with whatever reachability it has.

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ssdpAddress = "239.255.255.250:1900"
	ssdpTimeout = 3 * time.Second
	upnpTimeout = 5 * time.Second
)

// UPnPResult is the outcome of the port-mapping attempt.
type UPnPResult struct {
	Available  bool   `json:"available"`
	ExternalIP string `json:"external_ip,omitempty"`
	ControlURL string `json:"control_url,omitempty"`
	Service    string `json:"service,omitempty"`
	TCPMapped  bool   `json:"tcp_mapped"`
	Error      string `json:"error,omitempty"`
}

// serviceDescription is the subset of the device description XML we read.
type serviceDescription struct {
	XMLName xml.Name `xml:"root"`
	Device  struct {
		DeviceList struct {
			Devices []struct {
				DeviceType string `xml:"deviceType"`
				DeviceList struct {
					Devices []struct {
						DeviceType  string `xml:"deviceType"`
						ServiceList struct {
							Services []upnpService `xml:"service"`
						} `xml:"serviceList"`
					} `xml:"device"`
				} `xml:"deviceList"`
				ServiceList struct {
					Services []upnpService `xml:"service"`
				} `xml:"serviceList"`
			} `xml:"device"`
		} `xml:"deviceList"`
		ServiceList struct {
			Services []upnpService `xml:"service"`
		} `xml:"serviceList"`
	} `xml:"device"`
}

type upnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// discoverIGD finds the first UPnP gateway that answers SSDP. It returns the
// device description URL.
func discoverIGD(ctx context.Context) (string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(ssdpTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	target, _ := net.ResolveUDPAddr("udp4", ssdpAddress)
	for _, search := range []string{
		"urn:schemas-upnp-org:device:InternetGatewayDevice:1",
		"urn:schemas-upnp-org:service:WANIPConnection:1",
		"urn:schemas-upnp-org:service:WANPPPConnection:1",
	} {
		request := "M-SEARCH * HTTP/1.1\r\n" +
			"HOST: " + ssdpAddress + "\r\n" +
			"MAN: \"ssdp:discover\"\r\n" +
			"MX: 2\r\n" +
			"ST: " + search + "\r\n\r\n"
		if _, err := conn.WriteToUDP([]byte(request), target); err != nil {
			return "", err
		}
	}
	buffer := make([]byte, 4096)
	for {
		size, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return "", err
		}
		response := string(buffer[:size])
		for _, line := range strings.Split(response, "\r\n") {
			if strings.HasPrefix(strings.ToLower(line), "location:") {
				location := strings.TrimSpace(line[len("location:"):])
				if parsed, err := url.Parse(location); err == nil && parsed.Host != "" {
					return location, nil
				}
			}
		}
	}
}

// pickWANService returns the WANIPConnection (or WANPPPConnection) control URL.
func pickWANService(description []byte, base *url.URL) (upnpService, error) {
	var doc serviceDescription
	if err := xml.Unmarshal(description, &doc); err != nil {
		return upnpService{}, err
	}
	services := append([]upnpService{}, doc.Device.ServiceList.Services...)
	for _, device := range doc.Device.DeviceList.Devices {
		services = append(services, device.ServiceList.Services...)
		for _, inner := range device.DeviceList.Devices {
			services = append(services, inner.ServiceList.Services...)
		}
	}
	for _, service := range services {
		lower := strings.ToLower(service.ServiceType)
		if strings.Contains(lower, "wanipconnection") || strings.Contains(lower, "wanpppconnection") {
			if strings.TrimSpace(service.ControlURL) == "" {
				continue
			}
			control, err := url.Parse(strings.TrimSpace(service.ControlURL))
			if err != nil {
				continue
			}
			if !control.IsAbs() {
				control = base.ResolveReference(control)
			}
			return upnpService{ServiceType: service.ServiceType, ControlURL: control.String()}, nil
		}
	}
	return upnpService{}, errors.New("ed2k: no WANIPConnection/WANPPPConnection service in the gateway description")
}

// localIPv4 returns the address the OS would use to reach the internet, which
// is the address the router must map the port to.
func localIPv4() (string, error) {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
		return addr.IP.String(), nil
	}
	return "", errors.New("ed2k: cannot determine the local IPv4 address")
}

type soapRequest struct {
	Action string
	Body   string
}

func soapEnvelope(serviceType, action, body string) string {
	return `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + serviceType + `">` + body + `</u:` + action + `></s:Body></s:Envelope>`
}

func callSOAP(ctx context.Context, controlURL, serviceType, action, body string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, bytes.NewBufferString(soapEnvelope(serviceType, action, body)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	request.Header.Set("SOAPAction", `"`+serviceType+"#"+action+`"`)
	client := &http.Client{Timeout: upnpTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return payload, fmt.Errorf("ed2k: UPnP %s: HTTP %d", action, response.StatusCode)
	}
	return payload, nil
}

// externalIP reads NewExternalIPAddress from a GetExternalIPAddress response.
func externalIP(payload []byte) string {
	text := string(payload)
	const open = "<NewExternalIPAddress>"
	const close = "</NewExternalIPAddress>"
	start := strings.Index(text, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[start : start+end])
}

// mapKadPort discovers the gateway and asks it to forward udpPort. It returns a
// descriptive result for /api/v1/status and the log; a failure is not fatal.
func mapKadPort(ctx context.Context, udpPort, tcpPort int, description string) UPnPResult {
	result := UPnPResult{}
	if udpPort <= 0 || udpPort > 65535 {
		result.Error = "invalid UDP port"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, ssdpTimeout+2*upnpTimeout)
	defer cancel()
	location, err := discoverIGD(ctx)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	base, err := url.Parse(location)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	response, err := (&http.Client{Timeout: upnpTimeout}).Do(request)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	descriptionXML, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	service, err := pickWANService(descriptionXML, base)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Available = true
	result.ControlURL = service.ControlURL
	result.Service = service.ServiceType

	if payload, err := callSOAP(ctx, service.ControlURL, service.ServiceType, "GetExternalIPAddress", ""); err == nil {
		result.ExternalIP = externalIP(payload)
	}

	local, err := localIPv4()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	add := func(protocol string, port int) error {
		body := "<NewRemoteHost></NewRemoteHost>" +
			"<NewExternalPort>" + strconv.Itoa(port) + "</NewExternalPort>" +
			"<NewProtocol>" + protocol + "</NewProtocol>" +
			"<NewInternalPort>" + strconv.Itoa(port) + "</NewInternalPort>" +
			"<NewInternalClient>" + local + "</NewInternalClient>" +
			"<NewEnabled>1</NewEnabled>" +
			"<NewPortMappingDescription>" + description + "</NewPortMappingDescription>" +
			"<NewLeaseDuration>0</NewLeaseDuration>"
		_, err := callSOAP(ctx, service.ControlURL, service.ServiceType, "AddPortMapping", body)
		return err
	}
	if err := add("UDP", udpPort); err != nil {
		result.Error = err.Error()
		return result
	}
	if tcpPort > 0 && tcpPort <= 65535 {
		if err := add("TCP", tcpPort); err != nil {
			// The Kad mapping succeeded; a TCP-only failure leaves the daemon
			// searchable but LowID on eD2k. Report it without discarding the
			// result we have.
			result.Error = "TCP: " + err.Error()
		} else {
			result.TCPMapped = true
		}
	}
	return result
}
