package gextto

// uiweb_v2_feed_stats.go builds the feed activity table and the warning frame
// for the disabled lines. It reads the same per-source history the automatic
// disable rule writes, so the panel cannot disagree with what Gextto actually
// did.

import (
	"fmt"
	"strings"

	"github.com/buzzqw/gextto/internal/utils"
)

type v2FeedStat struct {
	Label    string
	Provider string
	State    string
	// StateClass is the badge class: ok, warn or err.
	StateClass   string
	LastSuccess  string
	LastError    string
	LastResults  int64
	OneDay       string
	SevenDays    string
	ThirtyDays   string
	Disabled     bool
	AutoDisabled bool

	Attempts1  int64
	Successes1 int64
	Failures1  int64
	Accepted1  int64
	Accepted30 int64
}

// v2FeedSummary is the one-line picture shown above the feed table: how many
// feeds are active, how many are off, and what the last 24 hours did.
type v2FeedSummary struct {
	Total       int
	Active      int
	Disabled    int
	Attempts1   int64
	Successes1  int64
	Failures1   int64
	Accepted1   int64
	TopLabel    string
	TopAccepted int64
}

type v2DisabledFeed struct {
	Label        string
	URL          string
	AutoDisabled bool
	DisabledAt   string
	Reason       string
}

// v2FeedStatsFrom reads the configured lines and the stored history. Disabled
// lines stay in the table and are also listed in the frame above it.
func v2FeedStatsFrom(s *AppState) ([]v2FeedStat, []v2DisabledFeed, v2FeedSummary) {
	if s == nil {
		return nil, nil, v2FeedSummary{}
	}
	cfg := latestConfig(s)
	if cfg == nil {
		return nil, nil, v2FeedSummary{}
	}
	lines := feedLinesFromSetting(cfg.Settings["url"])
	if len(lines) == 0 {
		return nil, nil, v2FeedSummary{}
	}
	stats := map[string]v2StatRow{}
	if s.db != nil {
		if rows, err := s.db.SourceStats("feed"); err == nil {
			for _, row := range rows {
				stats[row.Provider] = v2StatRow{
					lastSuccess: row.LastSuccessAt,
					lastResults: row.LastResults,
					oneDay:      fmt.Sprintf("%d/%d/%d/%d", row.Attempts1, row.Successes1, row.Failures1, row.Accepted1),
					sevenDays:   fmt.Sprintf("%d/%d/%d/%d", row.Attempts7, row.Successes7, row.Failures7, row.Accepted7),
					thirtyDays:  fmt.Sprintf("%d/%d/%d/%d", row.Attempts30, row.Successes30, row.Failures30, row.Accepted30),
					attempts1:   row.Attempts1,
					successes1:  row.Successes1,
					failures1:   row.Failures1,
					accepted1:   row.Accepted1,
					accepted30:  row.Accepted30,
					hasData:     row.Attempts30 > 0 || row.Accepted30 > 0,
					disabledAt:  row.DisabledAt,
					reason:      row.DisabledReason,
				}
			}
		}
	}
	statuses := map[string]string{}
	if s.db != nil {
		if rows, err := s.db.ProviderStatuses(); err == nil {
			for _, row := range rows {
				if row.Kind != "feed" {
					continue
				}
				statuses[row.Provider] = row.LastError
			}
		}
	}
	table := make([]v2FeedStat, 0, len(lines))
	disabled := make([]v2DisabledFeed, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		isDisabled := strings.HasPrefix(trimmed, "#")
		rawURL := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if rawURL == "" {
			continue
		}
		source := feedSourceName(rawURL)
		statKey := feedStatKey(rawURL)
		label := feedLabel(rawURL)
		row := stats[statKey]
		if isDisabled && s.db != nil {
			// A manually disabled feed may have no daily row at all, and an
			// auto-disabled one may have only the health marker: read it so the
			// frame can still say when and why.
			if health, err := s.db.SourceHealth("feed", statKey); err == nil {
				if row.disabledAt == "" {
					row.disabledAt = health.DisabledAt
					row.reason = health.DisabledReason
				}
				if row.lastSuccess == "" {
					row.lastSuccess = health.LastSuccessAt
				}
			}
		}
		state, class := "attivo", "ok"
		if isDisabled {
			state, class = "disabilitato", "warn"
			disabled = append(disabled, v2DisabledFeed{
				Label: label, URL: rawURL,
				AutoDisabled: row.disabledAt != "",
				DisabledAt:   v2FeedTime(row.disabledAt),
				Reason:       row.reason,
			})
		} else if statuses[source] != "" {
			state, class = "errori recenti", "warn"
		} else if !row.hasData {
			state, class = "in attesa del primo ciclo", ""
		} else if row.lastSuccess == "" {
			state, class = "mai letto", "warn"
		}
		oneDay, sevenDays, thirtyDays := row.oneDay, row.sevenDays, row.thirtyDays
		if oneDay == "" {
			oneDay, sevenDays, thirtyDays = "0/0/0/0", "0/0/0/0", "0/0/0/0"
		}
		table = append(table, v2FeedStat{
			Label: label, Provider: statKey, State: state, StateClass: class,
			LastSuccess:  v2FeedTime(row.lastSuccess),
			LastError:    statuses[source],
			LastResults:  row.lastResults,
			OneDay:       oneDay,
			SevenDays:    sevenDays,
			ThirtyDays:   thirtyDays,
			Disabled:     isDisabled,
			AutoDisabled: isDisabled && row.disabledAt != "",
			Attempts1:    row.attempts1,
			Successes1:   row.successes1,
			Failures1:    row.failures1,
			Accepted1:    row.accepted1,
			Accepted30:   row.accepted30,
		})
	}
	summary := v2FeedSummary{Total: len(table), Disabled: len(disabled)}
	summary.Active = summary.Total - summary.Disabled
	for _, item := range table {
		summary.Attempts1 += item.Attempts1
		summary.Successes1 += item.Successes1
		summary.Failures1 += item.Failures1
		summary.Accepted1 += item.Accepted1
		if item.Accepted30 > summary.TopAccepted {
			summary.TopLabel = item.Label
			summary.TopAccepted = item.Accepted30
		}
	}
	return table, disabled, summary
}

type v2StatRow struct {
	lastSuccess string
	lastResults int64
	oneDay      string
	sevenDays   string
	thirtyDays  string
	attempts1   int64
	successes1  int64
	failures1   int64
	accepted1   int64
	accepted30  int64
	hasData     bool
	disabledAt  string
	reason      string
}

func v2FeedTime(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if parsed, ok := utils.ParseTimestamp(raw); ok {
		return parsed.Local().Format("2006-01-02 15:04")
	}
	return raw
}
