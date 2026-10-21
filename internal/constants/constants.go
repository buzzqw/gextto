// Package constants holds compile-time defaults shared across gextto.
package constants

import (
	"strconv"
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

// Version is the product version. It is overridden at build time with
// `-X gextto/internal/constants.Version=...` from the `VERSION` file.
var Version = "0.1.0"

// Build is the monotonic build number, overridden at build time with
// `-X gextto/internal/constants.Build=...`. It identifies the exact binary.
var Build = "1000"

// First build at which each counter rolls over from the historical 1.1 series:
// from then on the third number runs 0..999 and the middle one grows by one
// every thousand builds (1.2.999 -> 1.3.0). Builds published before the
// rollover keep the old "1.1.<build>" form, so the installed version is not
// renumbered: Gextto's 1.1 series ends at build 1999, gx-torrent's at 999.
const (
	appFirstRollover = 2000
	gxFirstRollover  = 1000
)

// AppVersion is the version shown in the Gextto UI and API:
// "1.1.<build>" up to build 1999, then "<major>.<minor>.<patch>" with the
// patch in 0..999 (1.2.0, 1.2.999, 1.3.0, …).
func AppVersion() string { return versionFromBuild(Build, appFirstRollover) }

// GxTorrentBuild is the gx-torrent daemon's own monotonic build number,
// independent from Build: it grows by one every time the daemon is rebuilt
// (`scripts/next-gx-build-number.sh`), and is stamped with
// `-X gextto/internal/constants.GxTorrentBuild=...`. As gx-torrent is left
// untouched across Gextto updates, its number does not follow Gextto's.
var GxTorrentBuild = "0"

// GxTorrentVersion is what the gx-torrent daemon's CLI, web UI and
// /api/v1/health report: "1.1.<build>" up to build 999, then 1.2.0, 1.2.999,
// 1.3.0, … (gx-torrent's counter is independent and started at zero).
func GxTorrentVersion() string { return versionFromBuild(GxTorrentBuild, gxFirstRollover) }

// GxTorrentVersionLabel is gx-torrent's version with its build number when the
// rollover makes them differ, like VersionLabel for Gextto.
func GxTorrentVersionLabel() string {
	return VersionLabelWithBuild(GxTorrentVersion(), GxTorrentBuild)
}

// versionFromBuild formats a monotonic build number as a three-number version.
// Below firstRollover it keeps the historical "1.1.<build>" form; from then on
// the third number restarts at 0 every thousand builds and the middle number
// grows, starting at 2: 1000 -> 1.2.0, 1999 -> 1.2.999, 2000 -> 1.3.0.
func versionFromBuild(build string, firstRollover int) string {
	trimmed := strings.TrimSpace(build)
	value, err := strconv.Atoi(trimmed)
	if err != nil || value < 0 {
		return "1.1." + trimmed
	}
	if value < firstRollover {
		return "1.1." + strconv.Itoa(value)
	}
	minor := 2 + (value-firstRollover)/1000
	return "1." + strconv.Itoa(minor) + "." + strconv.Itoa(value%1000)
}

// VersionLabelWithBuild is the version to show to people: VERSION plus the
// build in parentheses, unless VERSION already ends with that build (the
// historical "1.1.<build>" form). So the build number is never lost from
// sight once the third number restarts every thousand: "1.1.1925" becomes
// "1.2.925 (build 2925)".
func VersionLabelWithBuild(version, build string) string {
	version = strings.TrimSpace(version)
	trimmed := strings.TrimSpace(build)
	if version == "" {
		return ""
	}
	if trimmed == "" || trimmed == "0" || version == "1.1."+trimmed {
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
