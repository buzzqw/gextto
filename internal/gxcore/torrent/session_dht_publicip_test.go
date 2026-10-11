package torrent

import "testing"

func TestPublicIPForConfig(t *testing.T) {
	cases := []struct{ host, want string }{
		{"124.31.75.21", "124.31.75.21"},
		{"2001:4860:4860::8888", "2001:4860:4860::8888"},
		{"0.0.0.0", ""},
		{"::", ""},
		{"10.0.0.5", ""},
		{"192.168.1.2", ""},
		{"127.0.0.1", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := publicIPForConfig(Config{DHTHost: c.host})
		if c.want == "" {
			if got != nil {
				t.Errorf("host %q: got %v, want nil", c.host, got)
			}
			continue
		}
		if got == nil || got.String() != c.want {
			t.Errorf("host %q: got %v, want %s", c.host, got, c.want)
		}
	}
}
