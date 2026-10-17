package main

// pprof.go enables the opt-in profiling endpoint (internal/debugpprof) when
// GX_TORRENT_PPROF names a loopback address. Gextto does not set the variable,
// so a managed daemon stays without a profiling port.

import "github.com/buzzqw/gextto/internal/debugpprof"

// pprofEnv names the variable that enables the profiling endpoint.
const pprofEnv = "GX_TORRENT_PPROF"

func startPProf() { debugpprof.StartFromEnv(pprofEnv, logf) }
