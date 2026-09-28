package gextto

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// validateAnacrolixConfig is shared by the settings API, backend preflight and
// the optional implementation. Unset values remain valid because they inherit
// the libtorrent defaults.
func validateAnacrolixConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("anacrolix configuration is unavailable")
	}
	if _, err := ParsePathMappings(cfg.Settings["anacrolix_path_mappings"]); err != nil {
		return err
	}
	integer := func(key string, min, max int64) error {
		raw := strings.TrimSpace(cfg.Settings[key])
		if raw == "" {
			return nil
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < min || value > max {
			return fmt.Errorf("%s must be between %d and %d", key, min, max)
		}
		return nil
	}
	for _, item := range []struct {
		key      string
		min, max int64
	}{
		{"anacrolix_listen_port", 0, 65535},
		{"anacrolix_proxy_port", 0, 65535},
		{"anacrolix_max_conns_per_torrent", 0, math.MaxInt32},
		{"anacrolix_download_limit_kib", 0, math.MaxInt64 / 1024},
		{"anacrolix_upload_limit_kib", 0, math.MaxInt64 / 1024},
		{"anacrolix_piece_hashers", 1, 256},
		{"anacrolix_max_unverified_mb", 1, math.MaxInt64 / (1 << 20)},
		{"anacrolix_proxy_type", 0, 5},
	} {
		if err := integer(item.key, item.min, item.max); err != nil {
			return err
		}
	}
	return nil
}
