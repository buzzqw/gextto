package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNATHint(t *testing.T) {
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 6881}); !strings.Contains(got, "conserva") {
		t.Errorf("port-preserving hint = %q, want conserva", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 40000}); !strings.Contains(got, "riscrive") {
		t.Errorf("port-rewriting hint = %q, want riscrive", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 6881, NATMapping: "cone"}); !strings.Contains(got, "endpoint-independent") {
		t.Errorf("cone hint = %q", got)
	}
	if got := natHint(portCheck{Port: 6881, ReflectedPort: 40000, NATMapping: "symmetric"}); !strings.Contains(got, "address/port-dependent") {
		t.Errorf("symmetric hint = %q", got)
	}
}

func TestPortCheckLocalListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if !checkLocalListener(port) {
		t.Fatal("an open listener was not detected")
	}
	if checkLocalListener(0) {
		t.Fatal("port 0 must not be considered open")
	}
	_ = listener.Close()
	if checkLocalListener(port) {
		t.Fatal("a closed listener was reported open")
	}
}

func TestPortCheckNilMapper(t *testing.T) {
	var mapper *portMapper
	got := mapper.check()
	if got.Open || got.Listening || got.Mapped {
		t.Fatalf("nil mapper = %+v, want zero", got)
	}
}

func TestPortMapperCheckRecentMapping(t *testing.T) {
	mapper := newPortMapper(6881, false, true)
	mapper.mu.Lock()
	mapper.method = "natpmp"
	mapper.externalIP = "1.2.3.4"
	mapper.mappedAt = time.Now()
	mapper.mu.Unlock()
	got := mapper.check()
	if !got.Mapped {
		t.Fatalf("a recent NAT-PMP mapping was not detected: %+v", got)
	}
	if got.Method != "natpmp" || got.ExternalIP != "1.2.3.4" {
		t.Fatalf("status not copied: %+v", got)
	}
}

// fakeIGD is one router's WANIPConnection service: it reports a single mapping
// and records the Add calls mapUPnP makes.
type fakeIGD struct {
	local     net.IP
	present   bool
	lease     uint32
	added     []string
	addFailed bool
}

func (f *fakeIGD) LocalAddr() net.IP { return f.local }

func (f *fakeIGD) GetExternalIPAddressCtx(context.Context) (string, error) { return "1.2.3.4", nil }

func (f *fakeIGD) GetSpecificPortMappingEntryCtx(_ context.Context, _ string, externalPort uint16, protocol string) (uint16, string, bool, string, uint32, error) {
	if !f.present {
		return 0, "", false, "", 0, errNoMapping
	}
	return externalPort, f.local.String(), true, "gx-torrent", f.lease, nil
}

func (f *fakeIGD) AddPortMappingCtx(_ context.Context, _ string, _ uint16, protocol string, _ uint16, _ string, _ bool, _ string, _ uint32) error {
	if f.addFailed {
		return errNoMapping
	}
	f.added = append(f.added, protocol)
	return nil
}

func (f *fakeIGD) DeletePortMappingCtx(context.Context, string, uint16, string) error { return nil }

var errNoMapping = errors.New("no such entry")

// A mapping that still has enough lease is left alone; one that is about to
// expire is renewed with the same Add that created it, so the router never
// drops the port and announces the removal and the addition every hour.
func TestPortMapperRenewsTheLeaseBeforeItExpires(t *testing.T) {
	port := 6881
	cases := []struct {
		name    string
		present bool
		lease   uint32
		want    []string
	}{
		{"fresh lease: no renewal", true, uint32(portLease.Seconds()), nil},
		{"lease about to expire: renew both protocols", true, portRenewBefore - 1, []string{"TCP", "UDP"}},
		{"permanent mapping: no renewal", true, 0, nil},
		{"missing mapping: add both protocols", false, 0, []string{"TCP", "UDP"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := &fakeIGD{local: net.ParseIP("192.168.1.161"), present: tc.present, lease: tc.lease}
			mapper := newPortMapper(port, true, false)
			mapper.upnpClient = router
			if err := mapper.mapUPnP(); err != nil {
				t.Fatalf("mapUPnP: %v", err)
			}
			if strings.Join(router.added, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("added = %v, want %v", router.added, tc.want)
			}
		})
	}
}

func TestPortMapperLifecycle(t *testing.T) {
	// Case 1: Close without start must return immediately without blocking.
	m1 := newPortMapper(6881, true, true)
	m1.close()

	// Case 2: Start with disabled mapping or port 0 must close cleanly.
	m2 := newPortMapper(0, false, false)
	m2.start()
	m2.start() // idempotent
	m2.close()
}
