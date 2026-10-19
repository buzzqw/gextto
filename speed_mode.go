package gextto

import (
	"fmt"
	"time"
)

// The rule that decides the speed, chosen in Configuration with a single
// choice: Auto (the line is measured, no cap), the fixed limits, or the hourly
// schedule (each window with limits or Auto; outside the windows
// torrent_speed_auto says Auto or the fixed limits). The choice is not stored
// on its own: it is read from, and written to, torrent_speed_auto and
// torrent_sched_enabled, so older settings need no migration. The exception
// set in Downloads (torrent_temp_*) wins over any rule.
const (
	speedModeSetting  = "torrent_speed_mode"
	speedModeAuto     = "auto"
	speedModeFixed    = "fixed"
	speedModeSchedule = "schedule"
)

// speedModeOf is the rule chosen in Configuration.
func speedModeOf(cfg *Config) string {
	if cfg == nil {
		return speedModeFixed
	}
	switch {
	case settingTruthy(cfg.Settings["torrent_sched_enabled"]):
		return speedModeSchedule
	case settingTruthy(cfg.Settings["torrent_speed_auto"]):
		return speedModeAuto
	}
	return speedModeFixed
}

// speedModeWrites are the stored settings a choice of the rule writes. The
// schedule keeps torrent_speed_auto as it is: it becomes what applies outside
// the windows (Auto stays Auto, fixed limits stay fixed).
func speedModeWrites(mode string) ([][2]string, error) {
	switch mode {
	case speedModeAuto:
		return [][2]string{{"torrent_speed_auto", "true"}, {"torrent_sched_enabled", "false"}}, nil
	case speedModeFixed:
		return [][2]string{{"torrent_speed_auto", "false"}, {"torrent_sched_enabled", "false"}}, nil
	case speedModeSchedule:
		return [][2]string{{"torrent_sched_enabled", "true"}}, nil
	}
	return nil, fmt.Errorf("modalità velocità non valida: %q (auto, fixed o schedule)", mode)
}

// speedExceptionDurations are the durations of the exception offered in
// Downloads, in minutes; 0 keeps it until it is removed.
var speedExceptionDurations = []int64{60, 180, 360, 720, 0}

// uiSpeedState is what Downloads shows about the speed: the exception, if one
// is on, and the rule from Configuration with what it means right now.
type uiSpeedState struct {
	// ExceptionAuto / ExceptionLimit: the exception in force, Auto or a limit.
	ExceptionAuto  bool
	ExceptionLimit bool
	// ExceptionDL/UL are the stored exception limits in KiB/s (0 = none),
	// also the prefill of the custom values.
	ExceptionDL, ExceptionUL int64
	// ExceptionMinutes is the time left (0 = until removed); ExceptionEnds
	// the clock time it ends at.
	ExceptionMinutes int64
	ExceptionEnds    string
	// Mode is the rule (speedModeAuto/Fixed/Schedule); RuleSource, RuleDL and
	// RuleUL what it applies now; Window the schedule window in force.
	Mode           string
	RuleSource     string
	RuleDL, RuleUL int64
	Window         string
	// Guard is the latency guard of Auto, shown while it slows the line down.
	Guard latencyGuardStatus
}

// Exception reports an exception in force.
func (s uiSpeedState) Exception() bool { return s.ExceptionAuto || s.ExceptionLimit }

// RuleAuto reports that the rule leaves the line to Auto right now.
func (s uiSpeedState) RuleAuto() bool {
	return s.RuleSource == speedSourceAuto || s.RuleSource == speedSourceScheduleAuto
}

// InWindow reports that a schedule window is in force now.
func (s uiSpeedState) InWindow() bool {
	return s.RuleSource == speedSourceSchedule || s.RuleSource == speedSourceScheduleAuto
}

// Durations are the duration choices of the exception.
func (s uiSpeedState) Durations() []int64 { return speedExceptionDurations }

func uiSpeedStateFor(cfg *Config, guard latencyGuardStatus) uiSpeedState {
	state := uiSpeedState{Mode: speedModeOf(cfg), Guard: guard}
	state.RuleDL, state.RuleUL, state.RuleSource = speedRule(cfg)
	if state.InWindow() {
		if day, ok := speedScheduleWeek(cfg).AtDay(time.Now()); ok {
			state.Window = fmt.Sprintf("%s–%s", clockOfMinutes(day.Start), clockOfMinutes(day.End))
		}
	}
	state.ExceptionDL = uiSettingInt(cfg, "torrent_temp_dl_limit")
	state.ExceptionUL = uiSettingInt(cfg, "torrent_temp_ul_limit")
	_, _, source := speedPolicy(cfg)
	switch source {
	case speedSourceTempAuto:
		state.ExceptionAuto = true
	case speedSourceTemp, speedSourceTempKeep:
		state.ExceptionLimit = true
	}
	if until := uiSettingInt(cfg, "torrent_temp_limit_until"); state.Exception() && until > 0 {
		if remaining := until - time.Now().Unix(); remaining > 0 {
			state.ExceptionMinutes = (remaining + 59) / 60
			state.ExceptionEnds = time.Unix(until, 0).Format("15:04")
		}
	}
	return state
}

// clockOfMinutes writes minutes since midnight as hh:mm (1440 is 24:00).
func clockOfMinutes(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
