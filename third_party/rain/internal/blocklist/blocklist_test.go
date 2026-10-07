package blocklist

import (
	"net"
	"strings"
	"testing"
)

func TestFormats(t *testing.T) {
	rules := `# comment
10.0.0.0/8
Bad people:1.2.3.0-1.2.3.255
005.006.007.000 - 005.006.007.010 , 000 , emule
009.009.009.000 - 009.009.009.255 , 200 , allowed
2001:db8::-2001:db8::ff
`
	b := New()
	n, err := b.Reload(strings.NewReader(rules))
	if err != nil || n != 3 {
		t.Fatalf("loaded %d rules: %v", n, err)
	}
	for ip, blocked := range map[string]bool{
		"10.1.2.3": true, "1.2.3.200": true, "5.6.7.9": true, "5.6.7.11": false,
		"9.9.9.9": false, "8.8.8.8": false,
	} {
		if got := b.Blocked(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s blocked=%v want %v", ip, got, blocked)
		}
	}
}
