// Package debugpprof is the opt-in profiling endpoint shared by gexttod and the
// gx-torrent daemon. It is off unless an environment variable names a loopback
// address (for example "127.0.0.1:6060"); the Go profiles are then served under
// /debug/pprof/ (profile, heap, goroutine, allocs, block, mutex, trace, cmdline,
// symbol). Only loopback is accepted, and the endpoint is deliberately not part
// of either API: no token, no TLS, never bound to the network.
package debugpprof

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
)

// namedProfiles are the profiles net/http/pprof exposes by name under
// /debug/pprof/. They are registered explicitly because the endpoint binds its
// own mux instead of the package-level one.
var namedProfiles = []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"}

// Address validates the raw value and returns the loopback address to bind.
// ok is false for an empty or malformed value and for any non-loopback host, so
// a misconfiguration cannot expose the profiles on the network.
func Address(raw string) (address string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil || port == "" {
		return "", false
	}
	switch host {
	case "":
		return net.JoinHostPort("127.0.0.1", port), true
	case "127.0.0.1", "::1", "localhost":
		return net.JoinHostPort(host, port), true
	default:
		return "", false
	}
}

// StartFromEnv starts the debug server when the variable env asks for it. A
// missing value is silent; a non-loopback one is reported through logf and
// ignored.
func StartFromEnv(env string, logf func(format string, args ...any)) {
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return
	}
	address, ok := Address(raw)
	if !ok {
		logf("pprof skipped: %q is not a loopback host:port", raw)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	for _, name := range namedProfiles {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	go func() {
		logf("pprof profiling endpoint on http://%s/debug/pprof/", address)
		if err := http.ListenAndServe(address, mux); err != nil {
			logf("pprof server stopped: %v", err)
		}
	}()
}
