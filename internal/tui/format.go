package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// HumanBytes renders a byte count with binary units, matching the web UI.
func HumanBytes(value float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	if value < 0 || value != value { // negative or NaN
		value = 0
	}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

// HumanRate renders a transfer rate.
func HumanRate(value float64) string {
	return HumanBytes(value) + "/s"
}

// HumanDuration renders a coarse duration (days/hours/minutes).
func HumanDuration(seconds float64) string {
	if seconds < 0 || seconds != seconds {
		seconds = 0
	}
	total := int64(seconds)
	if total <= 0 {
		return "0m"
	}
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// toFloat extracts a float from the values encoding/json produces.
func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// OptionalNumber formats a nullable numeric JSON value. `null` and unparsable
// values render as "-", like the classic interface.
func OptionalNumber(value any, suffix string, decimals int) string {
	number, ok := toFloat(value)
	if !ok {
		return "-"
	}
	if decimals == 0 {
		return fmt.Sprintf("%.0f%s", number, suffix)
	}
	return fmt.Sprintf("%.*f%s", decimals, number, suffix)
}
