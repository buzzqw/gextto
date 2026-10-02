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
		case "", BackendEmbedded, BackendQbittorrent:
			return nil
		default:
			return fmt.Errorf("torrent_backend must be embedded or qbittorrent")
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
	case "qbittorrent_path_mappings":
		_, err := ParsePathMappings(value)
		return err
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
