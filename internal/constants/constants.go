// Package constants holds compile-time defaults shared across gextto.
package constants

import (
	"strings"
)

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

// Version is the full product version (`<series>.<build>`), stamped at build
// time with `-X gextto/internal/constants.Version=...`. AppVersion is the
// authoritative formatter; this variable is kept for compatibility with code
// that reads the compiled-in string.
var Version = "1.2.1000"

// Build is the monotonic build number, overridden at build time with
// `-X gextto/internal/constants.Build=...`. It identifies the exact binary.
var Build = "1000"

// VersionSeries is the manually chosen major.minor of Gextto ("1.2"). It is
// stamped at build time from the `version_series` file and the third number is
// always the build: "1.2.2065". The second number is raised only when the
// project deliberately starts a new series, never by a counter.
var VersionSeries = "1.2"

// AppVersion is the version shown in the Gextto UI and API:
// "<major>.<minor>.<build>".
func AppVersion() string { return VersionSeries + "." + Build }

// GxTorrentBuild is the gx-torrent daemon's own monotonic build number,
// independent from Build: it grows by one every time the daemon is rebuilt
// (`scripts/next-gx-build-number.sh`), and is stamped with
// `-X gextto/internal/constants.GxTorrentBuild=...`. As gx-torrent is left
// untouched across Gextto updates, its number does not follow Gextto's.
var GxTorrentBuild = "0"

// GxVersionSeries is gx-torrent's manually chosen major.minor ("1.1"), stamped
// from `gx_torrent.version_series`. Its build is the third number.
var GxVersionSeries = "1.1"

// GxTorrentVersion is what the gx-torrent daemon's CLI, web UI and
// /api/v1/health report: "<major>.<minor>.<build>".
func GxTorrentVersion() string { return GxVersionSeries + "." + GxTorrentBuild }

// GxTorrentVersionLabel is gx-torrent's version as shown to people. The build
// is already the third number, so no extra label is needed.
func GxTorrentVersionLabel() string {
	return VersionLabelWithBuild(GxTorrentVersion(), GxTorrentBuild)
}

// VersionLabelWithBuild is the version to show to people. The current scheme
// already ends with the build as its third number, so it is returned as it is;
// an old manifest whose version dropped the build still gets it in
// parentheses, so the exact reference is never lost.
func VersionLabelWithBuild(version, build string) string {
	version = strings.TrimSpace(version)
	trimmed := strings.TrimSpace(build)
	if version == "" {
		return ""
	}
	if trimmed == "" || trimmed == "0" || strings.HasSuffix(version, "."+trimmed) {
		return version
	}
	return version + " (build " + trimmed + ")"
}

// VersionLabel is Gextto's version as shown by the UI, the API and --version.
func VersionLabel() string { return VersionLabelWithBuild(AppVersion(), Build) }

// Commit is the git commit the binary was built from, stamped with
// `-X gextto/internal/constants.Commit=...` by scripts/build-daemon.sh. The
// in-app update check compares it with the published release manifest.
var Commit = ""

// GxReleaseChannel is the release channel a gx-torrent binary was published
// on ("continuous" or "stable"), stamped only by scripts/build-release.sh. A
// local build leaves it empty, so its in-app update stays off: a checkout may
// be newer than the published build.
var GxReleaseChannel = ""

// BuiltAt is the build time (RFC 3339, UTC), stamped like Commit. A release is
// offered as an update only when it was built after the running binary.
var BuiltAt = ""

// DefaultIPFilterURL is the IP filter Gextto and a standalone gx-torrent start
// with: the eMule Security list, in P2P format, gzip compressed. It is written
// to the settings once, when the key was never set, so an operator who clears
// it keeps it cleared.
const DefaultIPFilterURL = "https://upd.emule-security.org/ipfilter.gz"
