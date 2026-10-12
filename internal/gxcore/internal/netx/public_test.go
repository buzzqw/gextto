package netx

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestIsPrivateAddress(t *testing.T) {
	private := []string{"10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.119", "127.0.0.1",
		"169.254.10.10", "100.64.0.1", "0.0.0.0", "224.0.0.1", "::1", "fd00::1", "fe80::1", "::ffff:192.168.1.1"}
	public := []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "100.128.0.1", "2001:4860:4860::8888"}
	for _, s := range private {
		if !IsPrivateAddress(net.ParseIP(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range public {
		if IsPrivateAddress(net.ParseIP(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestDialPublicRefusesPrivateDestinations(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	for _, host := range []string{"127.0.0.1", "localhost"} {
		conn, err := DialPublic(context.Background(), net.Dialer{}, "tcp", net.JoinHostPort(host, port))
		if conn != nil {
			conn.Close()
		}
		if !errors.Is(err, ErrPrivateAddress) {
			t.Errorf("DialPublic(%s) err = %v, want ErrPrivateAddress", host, err)
		}
	}
	if err := CheckPublicTCPAddr(net.ParseIP("192.168.1.1"), 80); !errors.Is(err, ErrPrivateAddress) {
		t.Errorf("CheckPublicTCPAddr(private) = %v", err)
	}
	if err := CheckPublicTCPAddr(net.ParseIP("8.8.8.8"), 80); err != nil {
		t.Errorf("CheckPublicTCPAddr(public) = %v", err)
	}
}
