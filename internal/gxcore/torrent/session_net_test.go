package torrent

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func ipv6LoopbackAvailable() bool {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func TestListenTCPOnUnspecifiedIsDualStack(t *testing.T) {
	l, err := listenTCPOn("0.0.0.0", 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	if c, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second); err != nil {
		t.Errorf("IPv4 loopback dial failed: %v", err)
	} else {
		c.Close()
	}
	if !ipv6LoopbackAvailable() {
		t.Log("no IPv6 loopback: skipping IPv6 leg")
		return
	}
	if c, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)), 2*time.Second); err != nil {
		t.Errorf("IPv6 loopback dial failed: %v", err)
	} else {
		c.Close()
	}
}

func TestListenTCPOnSpecificAddressIsSingleStack(t *testing.T) {
	l, err := listenTCPOn("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	if ip := l.Addr().(*net.TCPAddr).IP; ip.To4() == nil {
		t.Errorf("specific IPv4 host bound %q, want IPv4", ip)
	}
}

func TestListenUDPOnUnspecifiedIsDualStack(t *testing.T) {
	pc, err := listenUDPOn("0.0.0.0", 0)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	recv := func(network, host string) {
		t.Helper()
		c, err := net.Dial(network, net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			t.Errorf("%s dial: %v", network, err)
			return
		}
		defer c.Close()
		if _, err := c.Write([]byte("ping")); err != nil {
			t.Errorf("%s write: %v", network, err)
			return
		}
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 16)
		if _, _, err := pc.ReadFrom(buf); err != nil {
			t.Errorf("%s not received on the shared socket: %v", network, err)
		}
	}

	recv("udp4", "127.0.0.1")
	if ipv6LoopbackAvailable() {
		recv("udp6", "::1")
	} else {
		t.Log("no IPv6 loopback: skipping IPv6 leg")
	}
}
