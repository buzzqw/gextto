package resolver

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestResolveAcceptsIPv6(t *testing.T) {
	ip, port, err := Resolve(context.Background(), "[2001:db8::1]:6881", time.Second, nil)
	if err != nil {
		t.Fatalf("resolve ipv6: %v", err)
	}
	if ip.To4() != nil {
		t.Errorf("ip = %s, want an IPv6 address", ip)
	}
	if !ip.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("ip = %s, want 2001:db8::1", ip)
	}
	if port != 6881 {
		t.Errorf("port = %d, want 6881", port)
	}
}

func TestResolveIPv4LiteralStaysIPv4(t *testing.T) {
	ip, _, err := Resolve(context.Background(), "127.0.0.1:1", time.Second, nil)
	if err != nil {
		t.Fatalf("resolve ipv4: %v", err)
	}
	if ip.To4() == nil {
		t.Errorf("ip = %s, want IPv4", ip)
	}
}
