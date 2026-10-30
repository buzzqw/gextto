package torrent

// session_http.go: outbound HTTP for the daemon's own downloads (IP filter
// lists, for example) must follow the same rules as the engine. A configured
// proxy or VPN interface exists to keep traffic inside the tunnel, so a helper
// fetch must not go out directly.

import (
	"net/http"

	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
)

// ProxyEnabled reports whether an outbound proxy is configured. Callers that
// would hand a URL to another service (FlareSolverr) use it to avoid a fetch
// that bypasses the tunnel.
func ProxyEnabled() bool {
	return netx.Current().Proxy != nil
}

// HTTPTransport returns an HTTP transport that honors the configured outgoing
// interface and proxy. The caller owns it and should close idle connections
// when done with a short-lived client.
func HTTPTransport() *http.Transport {
	return netx.HTTPTransport()
}
