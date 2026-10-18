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
	// autotuneMinPeakKib is the lowest rate that can be a measurement of the
	// line. A slower sample is a dead or tiny swarm, not the connection: using
	// it as a peak made the tuning propose 80 % of a stall (30 KiB/s became a
	// 24 KiB/s limit) and anchored the latency guard to the same low value.
	autotuneMinPeakKib = 128
)

// speedPeakKib reads a recorded peak in KiB/s (0 when none was observed yet, or
// when the stored value is too low to be a measurement of the line).
func speedPeakKib(cfg *Config, key string) int64 {
	if cfg == nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(cfg.Settings[key]), 10, 64)
	if err != nil || value < autotuneMinPeakKib {
		return 0
	}
	return value
}

// observeSpeedPeak records the highest download/upload rate seen (bytes per
// second), kept in KiB/s. It does nothing when there is no data dir (a
// hand-built config in a test must not write).
//
// A direction that currently has a speed limit is NOT a measurement of the line:
// the achieved rate is bounded by the cap, so recording it would let the tuning
// propose 80% of its own cap and ratchet the limit down every cycle. When a
// direction is limited, any peak recorded earlier is forgotten, so a peak always
// means "the rate the line reached with no cap".
func observeSpeedPeak(cfg *Config, downloadBytesPerSec, uploadBytesPerSec int64, downloadLimited, uploadLimited bool) {
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return
	}
	observe := func(key string, bytesPerSec int64, limited bool) {
		if limited {
			if cfg.Settings[key] != "" {
				if _, err := DeleteSetting(cfg.DataDir, key); err == nil {
					delete(cfg.Settings, key)
				}
			}
			return
		}
		kib := bytesPerSec / 1024
		if kib < autotuneMinPeakKib || kib <= speedPeakKib(cfg, key) {
			return
		}
		value := strconv.FormatInt(kib, 10)
		if err := saveConfigSetting(cfg.DataDir, key, value); err == nil {
			cfg.Settings[key] = value
		}
	}
	observe(autotunePeakDownKey, downloadBytesPerSec, downloadLimited)
	observe(autotunePeakUpKey, uploadBytesPerSec, uploadLimited)
}
