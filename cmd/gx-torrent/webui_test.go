package main

import (
	"strings"
	"testing"
)

// TestWebUIURLsConcreteHost: a pinned listen address yields exactly one URL,
// with the host kept as typed.
func TestWebUIURLsConcreteHost(t *testing.T) {
	got := webUIURLs("192.168.1.5", "8890")
	if len(got) != 1 || got[0] != "http://192.168.1.5:8890/" {
		t.Fatalf("webUIURLs(192.168.1.5) = %v", got)
	}
	got = webUIURLs("::1", "9000")
	if len(got) != 1 || got[0] != "http://[::1]:9000/" {
		t.Fatalf("webUIURLs(::1) = %v", got)
	}
}

// TestWebUIURLsWildcardIncludesLoopback: a wildcard listen address (the default)
// always offers a loopback URL, never duplicates it, and never prints the
// unusable wildcard itself.
func TestWebUIURLsWildcardIncludesLoopback(t *testing.T) {
	for _, listen := range []string{"", "0.0.0.0", "::"} {
		got := webUIURLs(listen, "8890")
		if len(got) == 0 || got[len(got)-1] != "http://127.0.0.1:8890/" {
			t.Fatalf("webUIURLs(%q) = %v, want a loopback URL", listen, got)
		}
		seen := map[string]bool{}
		for _, url := range got {
			if seen[url] {
				t.Fatalf("webUIURLs(%q) repeats %q: %v", listen, url, got)
			}
			seen[url] = true
			if strings.Contains(url, "0.0.0.0") || strings.Contains(url, "[::]") {
				t.Fatalf("webUIURLs(%q) printed the wildcard: %v", listen, got)
			}
		}
	}
}

// TestPrimaryLocalIPIsUsable: when the host has a LAN address it must be a
// non-loopback IPv4 (an isolated host may return nil, which is fine).
func TestPrimaryLocalIPIsUsable(t *testing.T) {
	ip := primaryLocalIP()
	if ip == nil {
		t.Skip("no LAN address in this environment")
	}
	if ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		t.Fatalf("primaryLocalIP = %v, want a usable IPv4", ip)
	}
}

// TestFirstInterfaceIPv4SkipsLoopback pins that the fallback never returns the
// loopback address.
func TestFirstInterfaceIPv4SkipsLoopback(t *testing.T) {
	if ip := firstInterfaceIPv4(); ip != nil && ip.IsLoopback() {
		t.Fatalf("firstInterfaceIPv4 = %v, must not be loopback", ip)
	}
}

// TestOneLineCollapsesSOAPFault: the multiline UPnP fault collapses to one line.
func TestOneLineCollapsesSOAPFault(t *testing.T) {
	fault := "SOAP fault. Detail: \n<UPnPError>\n  <errorCode>606</errorCode>\n</UPnPError>\n"
	if got := oneLine(fault); strings.ContainsAny(got, "\n\r\t") || got != "SOAP fault. Detail: <UPnPError> <errorCode>606</errorCode> </UPnPError>" {
		t.Fatalf("oneLine(%q) = %q", fault, got)
	}
}
