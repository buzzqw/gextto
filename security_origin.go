package gextto

import (
	"fmt"
	"net/http"

	"github.com/buzzqw/gextto/internal/logging"
)

// crossOriginGuard refuses cross-site state-changing requests, so a page served
// from another origin cannot reconfigure Gextto (settings, hooks, torrent
// actions) through the browser of a user who is on the local network. Gextto is
// meant for a trusted LAN and, by default, does not require a login from local
// addresses, so without this guard a malicious page could drive the API with
// the visitor's browser.
//
// It is built on net/http.CrossOriginProtection: the safe methods (GET, HEAD,
// OPTIONS) always pass, same-origin requests pass, and requests without an
// Origin/Sec-Fetch-Site header (curl, the TUI, the internal mux, other
// non-browser clients) pass too. Only browser requests that declare themselves
// cross-site are refused.
func crossOriginGuard(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logging.Warn(fmt.Sprintf("🛡️ Refused a request to %s %s coming from another site (%s)",
			r.Method, r.URL.Path, r.Header.Get("Origin")))
		if requestWantsJSON(r) {
			jsonError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
	}))
	return protection.Handler(next)
}
