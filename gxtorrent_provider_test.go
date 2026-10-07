package gextto

import (
	"net/http"
	"testing"
)

func TestGxTorrentProviderURLUsesClientHost(t *testing.T) {
	cfg := &Config{Settings: map[string]string{
		"gxtorrent_url":    "http://127.0.0.1:8890",
		"gxtorrent_listen": "0.0.0.0:8890",
	}}
	// The link must use the host the client browsed to, never the loopback URL.
	if got := gh5_gxTorrentURL("http://media.local:5000", nil, cfg); got != "http://media.local:8890/" {
		t.Fatalf("client URL = %q, want http://media.local:8890/", got)
	}
	// A custom listen port is used for the link.
	cfg.Settings["gxtorrent_listen"] = "0.0.0.0:9999"
	if got := gh5_gxTorrentURL("https://media.local", nil, cfg); got != "https://media.local:9999/" {
		t.Fatalf("custom port URL = %q, want https://media.local:9999/", got)
	}
	// With no public base, it falls back to the request Host.
	req, _ := http.NewRequest(http.MethodGet, "http://gextto.local:5000/", nil)
	req.Host = "gextto.local:5000"
	cfg.Settings["gxtorrent_listen"] = ""
	if got := gh5_gxTorrentURL("", req, cfg); got != "http://gextto.local:8890/" {
		t.Fatalf("request URL = %q, want http://gextto.local:8890/", got)
	}
	// A loopback public host is still honoured: the operator chose to expose it.
	if got := gh5_gxTorrentURL("http://127.0.0.1:5000", nil, cfg); got != "http://127.0.0.1:8890/" {
		t.Fatalf("loopback public URL = %q", got)
	}
}

func TestGxTorrentClientPortDefault(t *testing.T) {
	if got := gh5_gxTorrentClientPort(&Config{Settings: map[string]string{}}); got != "8890" {
		t.Fatalf("default port = %q, want 8890", got)
	}
	if got := gh5_gxTorrentClientPort(&Config{Settings: map[string]string{"gxtorrent_url": "http://127.0.0.1:1234"}}); got != "1234" {
		t.Fatalf("url port = %q, want 1234", got)
	}
}
