package gextto

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// validateBackendSetting provides immediate, field-specific feedback from the
// settings page instead of waiting for a restart to expose a typo.
func validateBackendSetting(key, value string) error {
	raw := strings.TrimSpace(value)
	switch key {
	case "torrent_backend":
		switch raw {
		case "", BackendEmbedded, BackendQbittorrent, BackendAnacrolix:
			return nil
		default:
			return fmt.Errorf("torrent_backend must be embedded, qbittorrent or anacrolix")
		}
	case "qbittorrent_url":
		if raw == "" {
			return nil
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("qbittorrent_url must be a complete http(s) URL")
		}
	case "qbittorrent_request_timeout_secs":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 1, 300)
	case "qbittorrent_poll_interval_ms":
		if raw == "" || raw == "0" {
			return nil
		}
		return validateBackendInteger(key, raw, 250, 60000)
	case "qbittorrent_path_mappings", "anacrolix_path_mappings":
		_, err := ParsePathMappings(value)
		return err
	case "anacrolix_listen_port", "anacrolix_proxy_port":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 0, 65535)
	case "anacrolix_max_conns_per_torrent":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 0, 2147483647)
	case "anacrolix_download_limit_kib", "anacrolix_upload_limit_kib":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 0, 9007199254740)
	case "anacrolix_piece_hashers":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 1, 256)
	case "anacrolix_max_unverified_mb":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 1, 8796093022207)
	case "anacrolix_proxy_type":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 0, 5)
	case "anacrolix_tcp", "anacrolix_utp", "anacrolix_dht", "anacrolix_pex", "anacrolix_trackers", "anacrolix_upnp", "anacrolix_apply_ip_filter":
		if raw == "" || raw == "true" || raw == "false" || raw == "yes" || raw == "no" || raw == "1" || raw == "0" {
			return nil
		}
		return fmt.Errorf("%s must be true/false", key)
	}
	return nil
}

func validateBackendInteger(key, raw string, min, max int64) error {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return nil
}
