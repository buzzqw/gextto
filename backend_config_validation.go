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
		case "", BackendQbittorrent, BackendGxTorrent:
			return nil
		default:
			return fmt.Errorf("torrent_backend must be qbittorrent or gx-torrent")
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
	case latencyGuardSourceKey:
		switch strings.ToLower(raw) {
		case "", latencySourcePing, latencySourceBoth, latencySourceRouter:
			return nil
		}
		return fmt.Errorf("%s must be %q, %q or %q", latencyGuardSourceKey, latencySourcePing, latencySourceBoth, latencySourceRouter)
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

// validateGxProxyURL accepts the proxy forms the engine supports:
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
	"trash_path":          true,
	"archive_root":        true,
	"torrent_dir":         true,
	"torrent_temp_dir":    true,
	"torrent_copy_dir":    true,
	"torrent_ramdisk_dir": true,
}

// pathSettingError is a rejected path value. Error() is the English text the
// JSON API returns; UIText is the Italian source string the settings page
// shows, translated through the interface catalogs like every other label.
type pathSettingError struct {
	Key    string
	UIText string
	reason string
}

func (e *pathSettingError) Error() string { return e.Key + " " + e.reason }

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
		return &pathSettingError{Key: key, UIText: "deve essere un percorso assoluto", reason: "must be an absolute path"}
	}
	if cleaned == string(filepath.Separator) {
		return &pathSettingError{Key: key, UIText: "non può essere la radice del filesystem", reason: "cannot be the filesystem root"}
	}
	if cfg == nil {
		return nil
	}
	for _, critical := range []string{cfg.DataDir, cfg.StateDir, cfg.DownloadDir} {
		if strings.TrimSpace(critical) == "" {
			continue
		}
		if pathWithin(critical, cleaned) {
			return &pathSettingError{Key: key, UIText: "non può contenere la cartella dati o download di Gextto", reason: "cannot contain the Gextto data/download directory"}
		}
	}
	return nil
}

// validateSettingValue is the value check every write of a setting goes
// through — the JSON API, the settings page and the setup wizard — so a value
// one entry point rejects cannot slip in through another.
func validateSettingValue(key, value string, cfg *Config) error {
	if err := validateBackendSetting(key, value); err != nil {
		return err
	}
	if err := validateNumericSetting(key, value); err != nil {
		return err
	}
	if key == seedRulesKey {
		if err := validateSeedRulesSetting(value); err != nil {
			return err
		}
	}
	if key == latencyGuardTargetKey {
		if err := validateLatencyTarget(value); err != nil {
			return err
		}
	}
	return validatePathSetting(key, value, cfg)
}

// validateLatencyTarget accepts an empty value (the built-in targets), a host
// or a host:port with a numeric port; a URL or spaces are refused.
func validateLatencyTarget(value string) error {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	host := raw
	if h, port, err := net.SplitHostPort(raw); err == nil {
		if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%s: invalid port %q", latencyGuardTargetKey, port)
		}
		host = h
	}
	if host == "" || strings.ContainsAny(host, " /\\?#@") {
		return fmt.Errorf("%s must be a host or host:port, not %q", latencyGuardTargetKey, raw)
	}
	return nil
}

// validateNumericSetting rejects a non-numeric value for the settings whose
// reader silently falls back to the default. Without it a typo
// (`score_res_1080p=abc`, a port, the seed ratio) is accepted, does nothing,
// and disables the rule the operator meant to tune — with no error anywhere.
// Empty is always allowed: it clears the setting and the default applies.
func validateNumericSetting(key, value string) error {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	switch {
	case strings.HasPrefix(key, "score_"):
		if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
			return fmt.Errorf("%s must be an integer score", key)
		}
	case key == "upgrade_min_score_diff", key == "cleanup_min_score_diff",
		key == "upgrade_until_score", key == "stall_alternative_after_min",
		key == "stall_alternative_score_drop", key == "torrent_seed_idle_minutes":
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			return fmt.Errorf("%s must be zero or a positive integer", key)
		}
	case key == latencyGuardLineMaxDownKey, key == latencyGuardLineMaxUpKey:
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 || parsed > 10_000_000 {
			return fmt.Errorf("%s must be between 0 and 10000000 KiB/s", key)
		}
	case key == "min_free_space_gb", key == "torrent_seed_ratio":
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 {
			return fmt.Errorf("%s must be zero or a positive number", key)
		}
	case key == "torrent_port_min", key == "torrent_port_max":
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || parsed > 65535 {
			return fmt.Errorf("%s must be a port between 1 and 65535", key)
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
