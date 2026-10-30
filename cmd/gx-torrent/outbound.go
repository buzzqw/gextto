package main

// outbound.go: HTTP clients for the daemon's own remote downloads (.torrent
// files, RSS feeds, Torznab indexers). They follow the configured proxy and
// VPN interface, while localhost and LAN services stay direct.

import (
	"net/http"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// outboundClient returns a client for a remote download. A zero timeout keeps
// the caller's context in charge.
func outboundClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: torrent.HTTPTransport()}
}

// outboundClientWithJar is outboundClient with a cookie jar, for servers that
// hand out a session cookie before the .torrent file.
func outboundClientWithJar(timeout time.Duration, jar http.CookieJar) *http.Client {
	return &http.Client{Timeout: timeout, Transport: torrent.HTTPTransport(), Jar: jar}
}
