package main

// pprof.go is an opt-in profiling endpoint for performance work on the daemon.
// It is off unless GX_TORRENT_PPROF names a loopback address (for example
// "127.0.0.1:6060"); the Go profiles are then served under /debug/pprof/
// (profile, heap, goroutine, allocs, block, mutex, trace, cmdline, symbol).
// Only loopback is accepted, and the endpoint is deliberately not part of the
// API: no token, no TLS, never bound to the network. Gextto does not set the
// variable, so a managed daemon stays without a profiling port.

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
)

// pprofEnv names the variable that enables the profiling endpoint.
const pprofEnv = "GX_TORRENT_PPROF"

// pprofNamedProfiles are the profiles net/http/pprof exposes by name under
// /debug/pprof/. They are registered explicitly because the daemon binds its own
// mux instead of the package-level one.
var pprofNamedProfiles = []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"}

// pprofAddress validates the raw value and returns the loopback address to bind.
// ok is false for an empty or malformed value and for any non-loopback host, so
// a misconfiguration cannot expose the profiles on the network.
func pprofAddress(raw string) (address string, ok bool) {
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

// startPProf starts the debug server when the environment asks for it. A
// missing value is silent; a non-loopback one is reported and ignored.
func startPProf() {
	raw := strings.TrimSpace(os.Getenv(pprofEnv))
	if raw == "" {
		return
	}
	address, ok := pprofAddress(raw)
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
	for _, name := range pprofNamedProfiles {
		mux.Handle("/debug/pprof/"+name, pprof.Handler(name))
	}
	go func() {
		logf("pprof profiling endpoint on http://%s/debug/pprof/", address)
		if err := http.ListenAndServe(address, mux); err != nil {
			logf("pprof server stopped: %v", err)
		}
	}()
}
