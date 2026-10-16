package gextto

import (
	"strconv"
	"strings"
)

// autotune_peaks.go records the highest transfer rates the line actually reached,
// so the guided tuning can propose a bandwidth limit from a measurement instead
// of a guess. The peaks live in the settings database (hidden keys, never shown
// on the page) and only ever grow: a later slower sample is not a new lower
// ceiling.

const (
	// autotunePeakDownKey/UpKey hold the highest observed rate in KiB/s.
	autotunePeakDownKey = "autotune_peak_down_kib"
	autotunePeakUpKey   = "autotune_peak_up_kib"
)

// speedPeakKib reads a recorded peak in KiB/s (0 when none was observed yet).
func speedPeakKib(cfg *Config, key string) int64 {
	if cfg == nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(cfg.Settings[key]), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// observeSpeedPeak records the highest download/upload rate seen (bytes per
// second), kept in KiB/s. It only raises the peak and does nothing when there is
// no data dir (a hand-built config in a test must not write).
func observeSpeedPeak(cfg *Config, downloadBytesPerSec, uploadBytesPerSec int64) {
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return
	}
	observe := func(key string, bytesPerSec int64) {
		kib := bytesPerSec / 1024
		if kib <= speedPeakKib(cfg, key) {
			return
		}
		value := strconv.FormatInt(kib, 10)
		if err := saveConfigSetting(cfg.DataDir, key, value); err == nil {
			cfg.Settings[key] = value
		}
	}
	observe(autotunePeakDownKey, downloadBytesPerSec)
	observe(autotunePeakUpKey, uploadBytesPerSec)
}
