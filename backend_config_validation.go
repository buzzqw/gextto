package gextto

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
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
		case "", BackendEmbedded, BackendQbittorrent, BackendGxTorrent:
			return nil
		default:
			return fmt.Errorf("torrent_backend must be embedded, qbittorrent or gx-torrent")
		}
	case "qbittorrent_url", "gxtorrent_url":
		if raw == "" {
			return nil
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%s must be a complete http(s) URL", key)
		}
	case "gxtorrent_listen":
		if raw == "" {
			return nil
		}
		if _, port, err := net.SplitHostPort(raw); err != nil || strings.TrimSpace(port) == "" {
			return fmt.Errorf("gxtorrent_listen must be host:port (e.g. 0.0.0.0:8890)")
		}
	case "gxtorrent_proxy":
		return validateGxProxyURL(raw)
	case "qbittorrent_request_timeout_secs", "gxtorrent_request_timeout_secs":
		if raw == "" {
			return nil
		}
		return validateBackendInteger(key, raw, 1, 300)
	case "qbittorrent_poll_interval_ms", "gxtorrent_poll_interval_ms":
		if raw == "" || raw == "0" {
			return nil
		}
		return validateBackendInteger(key, raw, 250, 60000)
	case "qbittorrent_path_mappings", "jellyfin_path_mappings", "plex_path_mappings":
		_, err := ParsePathMappings(value)
		return err
	case "upgrade_until_score":
		if raw == "" {
			return nil
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return fmt.Errorf("upgrade_until_score must be zero or a positive score")
		}
		return nil
	case "trash_retention_days":
		if raw == "" {
			return nil
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return fmt.Errorf("trash_retention_days must be zero or a positive number of days")
		}
		return nil
	}
	return nil
}

// validateGxProxyURL accepts the proxy forms rain supports:
// socks5://[user:pass@]host:port and http://host:port. Empty clears it.
func validateGxProxyURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
		return fmt.Errorf("gxtorrent_proxy must be socks5://host:port or http://host:port")
	}
	switch parsed.Scheme {
	case "socks5", "socks5h", "http", "https":
		return nil
	default:
		return fmt.Errorf("gxtorrent_proxy scheme must be socks5, socks5h, http or https")
	}
}

func validateBackendInteger(key, raw string, min, max int64) error {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return nil
}

// pathSettings are the configuration keys that hold filesystem paths.
var pathSettings = map[string]bool{
	"trash_path":                  true,
	"archive_root":                true,
	"libtorrent_dir":              true,
	"libtorrent_temp_dir":         true,
	"libtorrent_torrent_copy_dir": true,
	"libtorrent_ramdisk_dir":      true,
}

// validatePathSetting rejects path values that are unsafe for the role they
// play. Empty values are allowed (they clear/disable the setting). A path that
// is the filesystem root or that contains the daemon's data/state directory is
// refused: the trash cleanup deletes the contents recursively, so a mistyped
// `trash_path=/` would otherwise erase everything it can reach.
func validatePathSetting(key, value string, cfg *Config) error {
	if !pathSettings[key] {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	cleaned := filepath.Clean(trimmed)
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("%s must be an absolute path", key)
	}
	if cleaned == string(filepath.Separator) {
		return fmt.Errorf("%s cannot be the filesystem root", key)
	}
	if cfg == nil {
		return nil
	}
	for _, critical := range []string{cfg.DataDir, cfg.StateDir, cfg.LibtorrentDir} {
		if strings.TrimSpace(critical) == "" {
			continue
		}
		if pathWithin(critical, cleaned) {
			return fmt.Errorf("%s cannot contain the Gextto data/download directory", key)
		}
	}
	return nil
}

// safeTrashRoot also protects configurations already on disk (not just new
// saves): emptying a trash folder that is the filesystem root or an ancestor of
// the data directory would destroy Gextto's own data.
func safeTrashRoot(cfg *Config, root string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(root))
	if cleaned == "" || cleaned == string(filepath.Separator) || cleaned == "." {
		return false
	}
	if cfg == nil {
		return true
	}
	for _, critical := range []string{cfg.DataDir, cfg.StateDir} {
		if strings.TrimSpace(critical) == "" {
			continue
		}
		if pathWithin(critical, cleaned) {
			return false
		}
	}
	return true
}
