package ed2k

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const gatewayDescription = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
        <serviceList>
          <service>
            <serviceType>urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1</serviceType>
            <controlURL>/ctl/wancommon</controlURL>
          </service>
        </serviceList>
        <deviceList>
          <device>
            <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
            <serviceList>
              <service>
                <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
                <controlURL>/ctl/wanip</controlURL>
              </service>
            </serviceList>
          </device>
        </deviceList>
      </device>
    </deviceList>
  </device>
</root>`

func TestPickWANService(t *testing.T) {
	base, _ := url.Parse("http://192.0.2.1:5000/rootDesc.xml")
	service, err := pickWANService([]byte(gatewayDescription), base)
	if err != nil {
		t.Fatal(err)
	}
	if service.ControlURL != "http://192.0.2.1:5000/ctl/wanip" {
		t.Fatalf("control URL = %q", service.ControlURL)
	}
	if !strings.Contains(service.ServiceType, "WANIPConnection") {
		t.Fatalf("service = %q", service.ServiceType)
	}
}

func TestPickWANServiceRejectsDescriptionWithoutWAN(t *testing.T) {
	base, _ := url.Parse("http://192.0.2.1:5000/")
	if _, err := pickWANService([]byte(`<root><device></device></root>`), base); err == nil {
		t.Fatal("description without a WAN service accepted")
	}
}

func TestExternalIP(t *testing.T) {
	payload := []byte(`<s:Envelope><s:Body><u:GetExternalIPAddressResponse><NewExternalIPAddress>198.51.100.7</NewExternalIPAddress></u:GetExternalIPAddressResponse></s:Body></s:Envelope>`)
	if got := externalIP(payload); got != "198.51.100.7" {
		t.Fatalf("externalIP = %q", got)
	}
	if got := externalIP([]byte("<x/>")); got != "" {
		t.Fatalf("externalIP on bad payload = %q", got)
	}
}

func TestCallSOAP(t *testing.T) {
	var action string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action = r.Header.Get("SOAPAction")
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "<NewExternalPort>4672</NewExternalPort>") {
			t.Errorf("SOAP body = %s", body)
		}
		_, _ = w.Write([]byte("<ok/>"))
	}))
	defer server.Close()
	body := "<NewExternalPort>4672</NewExternalPort>"
	if _, err := callSOAP(context.Background(), server.URL, "urn:schemas-upnp-org:service:WANIPConnection:1", "AddPortMapping", body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(action, "AddPortMapping") {
		t.Fatalf("SOAPAction = %q", action)
	}
}

func TestReachabilitySensor(t *testing.T) {
	open, _ := reachability(true, UPnPResult{Available: true}, false, true, true)
	if open != "open" {
		t.Fatalf("peer answered = %q", open)
	}
	mappedButSilent, _ := reachability(true, UPnPResult{Available: true}, false, true, false)
	if mappedButSilent != "limited" {
		t.Fatalf("UPnP without Kad traffic = %q", mappedButSilent)
	}
	limited, _ := reachability(true, UPnPResult{}, false, true, false)
	if limited != "limited" {
		t.Fatalf("UPnP failure = %q", limited)
	}
	low, _ := reachability(true, UPnPResult{Available: true}, true, true, true)
	if low != "limited" {
		t.Fatalf("LowID = %q", low)
	}
	unknown, _ := reachability(false, UPnPResult{}, false, false, false)
	if unknown != "unknown" {
		t.Fatalf("no check = %q", unknown)
	}
}
