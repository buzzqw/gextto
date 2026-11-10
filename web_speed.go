package gextto

// web_speed.go decides the speed limits in force and where they come from: the
// temporary exception set in Downloads and the rule chosen in Configuration.

import "time"

// TempLimitsInput is the body of POST /api/torrents/temp-limits: a limit with its
// expiry, the request that keeps it until removed, or the "auto speed" switch.
type TempLimitsInput struct {
	DownloadKib int64 `json:"download_kib"`
	UploadKib   int64 `json:"upload_kib"`
	Minutes     int64 `json:"minutes"`
	Clear       bool  `json:"clear"`
	// Auto, when set, turns the "auto speed" override on (true) or off (false):
	// the bandwidth is handed to the guided tuning with no cap, like being inside
	// an "Auto" window of the schedule. It is the temporary override's other mode.
	Auto *bool `json:"auto,omitempty"`
}

// speedPolicy picks the global limits in force and where they come from. The
// exception set in Downloads wins over the rule until it expires or is
// removed, whatever the rule is: Auto, a limit with an expiry or one kept until
// removed. Then the rule: the schedule window in force, Auto, the fixed limits.
func speedPolicy(cfg *Config) (download, upload int64, source string) {
	if cfg == nil {
		// No configuration available yet (a daemon still starting, or the state
		// of a page rendered before the first load): no limit and no rule, so
		// the caller draws no limit line and no badge instead of dereferencing
		// a nil config.
		return 0, 0, speedSourceBase
	}
	now := time.Now().Unix()
	tempUntil := parseSettingInt(cfg, "torrent_temp_limit_until")
	tempLive := tempUntil == 0 || tempUntil > now
	if settingTruthy(cfg.Settings["torrent_temp_auto"]) && tempLive {
		return 0, 0, speedSourceTempAuto
	}
	if settingTruthy(cfg.Settings["torrent_temp_limit_enabled"]) && tempLive {
		download, upload = parseSettingInt(cfg, "torrent_temp_dl_limit"), parseSettingInt(cfg, "torrent_temp_ul_limit")
		if tempUntil == 0 {
			return download, upload, speedSourceTempKeep
		}
		return download, upload, speedSourceTemp
	}
	return speedRule(cfg)
}

// speedRule is the rule chosen in Configuration, without the exception: the
// schedule window in force, Auto, or the fixed limits.
func speedRule(cfg *Config) (download, upload int64, source string) {
	if cfg == nil {
		return 0, 0, speedSourceBase
	}
	if settingTruthy(cfg.Settings[settingTorrentSchedEnabled]) {
		if day, ok := speedScheduleWeek(cfg).AtDay(time.Now()); ok {
			if day.Auto() {
				// An "auto" window hands the limit to the guided tuning: it lifts
				// the cap (unlimited) so the line can be measured there, while
				// the user's limits apply in the other windows.
				return 0, 0, speedSourceScheduleAuto
			}
			return day.Download, day.Upload, speedSourceSchedule
		}
	}
	// Auto as the rule, or Auto outside the schedule windows.
	if settingTruthy(cfg.Settings[settingTorrentSpeedAuto]) {
		return 0, 0, speedSourceAuto
	}
	return max(cfg.Transfer.DownloadLimitKib, 0), max(cfg.Transfer.UploadLimitKib, 0), speedSourceBase
}

func currentSpeedLimits(cfg *Config) (int64, int64) {
	download, upload, _ := speedPolicy(cfg)
	return download, upload
}
