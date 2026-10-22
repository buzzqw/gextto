package main

// client_identity.go is the announced client identity: the BEP 20 peer id
// prefix, the BEP 10 client string and the HTTP tracker User-Agent. The
// default is gx-core with its real build version; the presets exist for the
// swarms and trackers that behave differently with an unknown client. They are
// opt-in and only change what is advertised, not the protocol behaviour.

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/constants"
)

const (
	clientIdentityKey        = "client-identity"
	clientIdentityVersionKey = "client-identity-version"
	defaultClientIdentity    = "gx-core"
)

type clientIdentityPreset struct {
	Value   string
	Label   string
	Version string
}

var clientIdentityPresets = []clientIdentityPreset{
	{Value: "gx-core", Label: "gx-core"},
	{Value: "qbittorrent", Label: "qBittorrent", Version: "5.2.3"},
	{Value: "transmission", Label: "Transmission", Version: "4.1.3"},
	{Value: "rain", Label: "Rain", Version: "2.4.4"},
	{Value: "anacrolix", Label: "anacrolix/torrent", Version: "1.61.0"},
}

type clientIdentity struct {
	Name      string
	Version   string
	PeerID    string
	Client    string
	UserAgent string
}

// normalizeClientIdentity keeps only a known preset value.
func normalizeClientIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, preset := range clientIdentityPresets {
		if preset.Value == value {
			return value
		}
	}
	return defaultClientIdentity
}

// clientIdentityVersion keeps a short, safe version; it never contains spaces
// (the peer id is a fixed 20-byte field) and is capped so the prefix fits.
func clientIdentityVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 12 {
		value = value[:12]
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && r != '.' && r != '-' && r != '_' {
			return ""
		}
	}
	return value
}

func resolveClientIdentity(name, version string) clientIdentity {
	name = normalizeClientIdentity(name)
	version = clientIdentityVersion(version)
	presetVersion := ""
	for _, preset := range clientIdentityPresets {
		if preset.Value == name {
			presetVersion = preset.Version
			break
		}
	}
	if version == "" {
		version = presetVersion
	}
	if name == "gx-core" && version == "" {
		version = constants.GxTorrentVersion()
	}
	switch name {
	case "qbittorrent":
		if version == "" {
			version = "5.2.3"
		}
		return clientIdentity{Name: "qBittorrent", Version: version,
			PeerID:    "-qB" + fourDigitVersion(version) + "-",
			Client:    "qBittorrent " + version,
			UserAgent: "qBittorrent/" + version}
	case "transmission":
		if version == "" {
			version = "4.1.3"
		}
		return clientIdentity{Name: "Transmission", Version: version,
			PeerID:    "-TR" + fourDigitVersion(version) + "-",
			Client:    "Transmission " + version,
			UserAgent: "Transmission/" + version}
	case "rain":
		if version == "" {
			version = "2.4.4"
		}
		return clientIdentity{Name: "Rain", Version: version,
			PeerID:    "-RN" + version + "-",
			Client:    "Rain " + version,
			UserAgent: "Rain/" + version}
	case "anacrolix":
		if version == "" {
			version = "1.61.0"
		}
		// anacrolix/torrent's version package keeps -GT0003- for BEP 20 and a
		// module-derived string for BEP 10; the HTTP agent is its package name.
		return clientIdentity{Name: "anacrolix/torrent", Version: version,
			PeerID:    "-GT0003-",
			Client:    "anacrolix/torrent v" + version,
			UserAgent: "anacrolix-torrent/v" + version}
	}
	return clientIdentity{Name: "gx-core", Version: version,
		PeerID:    "-GX" + version + "-",
		Client:    "gx-core " + version,
		UserAgent: "gx-core/" + version}
}

// fourDigitVersion turns 5.2.3 into 5230, the four digits the qBittorrent and
// Transmission peer-id prefixes use. A missing part is zero; a component larger
// than one digit keeps its last digit, as the wire field is only four bytes.
func fourDigitVersion(value string) string {
	parts := strings.Split(value, ".")
	nums := []int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		nums[i] = n
	}
	return fmt.Sprintf("%d%d%d0", nums[0]%10, nums[1]%10, nums[2]%10)
}

// clientIdentityOptions renders the select options for the daemon page.
func clientIdentityOptions(selected string) string {
	selected = normalizeClientIdentity(selected)
	var b strings.Builder
	for _, preset := range clientIdentityPresets {
		mark := ""
		if preset.Value == selected {
			mark = " selected"
		}
		version := preset.Version
		if version == "" && preset.Value == "gx-core" {
			version = constants.GxTorrentVersion()
		}
		b.WriteString(`<option value="` + preset.Value + `"` + mark + `>` + html.EscapeString(preset.Label) +
			` (` + html.EscapeString(version) + `)</option>`)
	}
	return b.String()
}

// validClientIdentityVersion is what the settings form accepts: empty (the
// preset) or a short version; anything else is discarded.
func validClientIdentityVersion(value string) string {
	return clientIdentityVersion(value)
}
