package ipfilter

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndBlock(t *testing.T) {
	const list = `
# comment
// another comment
001.002.003.000 - 001.002.003.255 , 000 , eMule range
010.000.000.000 - 010.000.000.255 , 100 , another range
020.000.000.000 - 020.000.000.255 , 128 , allow rule
030.000.000.000 - 030.000.000.127 , 000 , overlapping
030.000.000.100 - 030.000.000.255 , 000 , overlapping
2001:db8::/32
`
	filter, err := Parse(strings.NewReader(list))
	if err != nil {
		t.Fatal(err)
	}
	if filter.Len() != 3 {
		t.Fatalf("ranges = %d, want 3 (allow and IPv6 ignored, overlap merged)", filter.Len())
	}
	blocked := []string{"1.2.3.0", "1.2.3.255", "10.0.0.1", "30.0.0.0", "30.0.0.200"}
	for _, text := range blocked {
		if !filter.Blocked(net.ParseIP(text)) {
			t.Errorf("%s should be blocked", text)
		}
	}
	allowed := []string{"1.2.2.255", "1.2.4.0", "11.0.0.1", "20.0.0.1", "31.0.0.1", "2001:db8::1"}
	for _, text := range allowed {
		if filter.Blocked(net.ParseIP(text)) {
			t.Errorf("%s must not be blocked", text)
		}
	}
}

func TestParseFormats(t *testing.T) {
	cases := map[string]string{
		"plain":     "1.2.3.0-1.2.3.255",
		"spaces":    "1.2.3.0 - 1.2.3.255",
		"padded":    "001.002.003.000 - 001.002.003.255 , 000 , desc",
		"peerguard": "Bad range:1.2.3.0-1.2.3.255",
		"cidr":      "1.2.3.0/24",
		"reversed":  "1.2.3.255 - 1.2.3.0",
	}
	for name, line := range cases {
		filter, err := Parse(strings.NewReader(line))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if filter.Len() != 1 || !filter.Blocked(net.ParseIP("1.2.3.100")) {
			t.Errorf("%s: range not parsed", name)
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not an ip\nstill not an ip")); err == nil {
		t.Fatal("invalid list accepted")
	}
	filter, err := Parse(strings.NewReader("# only comments\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filter.Len() != 0 || filter.Blocked(net.ParseIP("1.2.3.4")) {
		t.Fatal("empty list must allow everything")
	}
}

func TestNilFilterAllowsAll(t *testing.T) {
	var filter *Filter
	if filter.Blocked(net.ParseIP("1.2.3.4")) {
		t.Fatal("nil filter blocked an address")
	}
	if filter.Len() != 0 {
		t.Fatal("nil filter has ranges")
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ipfilter.dat")
	if err := os.WriteFile(path, []byte("9.9.9.0 - 9.9.9.255 , 000 , test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	filter, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Blocked(net.ParseIP("9.9.9.9")) {
		t.Fatal("loaded range not active")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.dat")); err == nil {
		t.Fatal("missing file accepted")
	}
}
