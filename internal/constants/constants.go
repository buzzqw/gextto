// Package constants holds compile-time defaults shared across gextto.
package constants

// DefaultRefreshSecs is the default interval between automatic series/movie
// searches (6 hours). Comics have their own weekly interval.
const DefaultRefreshSecs uint64 = 21_600

const (
	DefaultListen       = "127.0.0.1:5000"
	DefaultEnginePort   = 8889
	DefaultEngineListen = "127.0.0.1:8889"
	DefaultDBFile       = "gextto_series.db"
	DefaultArchiveFile  = "gextto_archive.db"
	DefaultConfigFile   = "gextto_config.db"
	DefaultStateDir     = "gextto_torrents_state"
)

// AppName is the daemon executable name (also used in release archives).
const AppName = "gexttod"

// Version is the product version. It is overridden at build time with
// `-X gextto/internal/constants.Version=...` from the `VERSION` file.
var Version = "0.1.0"

// Build is the monotonic build number, overridden at build time with
// `-X gextto/internal/constants.Build=...`. It identifies the exact binary.
var Build = "1000"

// AppVersion is the version shown in the UI and by gx-torrent:
// "<major>.<minor>.<build>". Kept here so the gextto UI, the API and the
// gx-torrent daemon all report the same string.
func AppVersion() string { return "1.1." + Build }
