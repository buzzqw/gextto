package gextto

import (
	"fmt"
	"sort"

	"github.com/buzzqw/gextto/internal/models"
)

// upgrade_impact.go answers «what would the next cycles replace?» before it
// happens: the releases already seen in the feeds (the archive) are run through
// the same series decision the cycle uses, in dry-run, and the ones that would
// replace an archived episode are reported. Nothing is written.

// upgradeImpactPerSeries bounds the releases read per series: the archive
// search returns the newest first, which are the ones a cycle could still meet.
const upgradeImpactPerSeries = 200

// upgradeImpactExamples is how many replacements the report lists.
const upgradeImpactExamples = 15

type upgradeImpactItem struct {
	Target    string
	Candidate string
	Score     int64
}

type upgradeImpactReport struct {
	Series   int
	Releases int
	// Episodes is how many archived episodes at least one release would
	// replace; Items lists some of them, the best candidate each.
	Episodes int
	Items    []upgradeImpactItem
}

// UpgradeImpact replays the archived feed releases of every monitored series
// through the dry-run decision and reports the episodes they would upgrade.
func UpgradeImpact(cfg *Config, db *Database, archive *Archive) (upgradeImpactReport, error) {
	report := upgradeImpactReport{}
	if cfg == nil || db == nil || archive == nil {
		return report, nil
	}
	best := map[string]upgradeImpactItem{}
	for index := range cfg.Series {
		series := &cfg.Series[index]
		if !series.Enabled || series.DisableUpgrades {
			continue
		}
		report.Series++
		results, err := archive.Search(series.Name)
		if err != nil {
			return report, err
		}
		if len(results) > upgradeImpactPerSeries {
			results = results[:upgradeImpactPerSeries]
		}
		// The cycle compares against the files actually on disk as well as the
		// database rows: without the index this report would count upgrades the
		// real decision refuses. Build it once per series, like the cycle.
		archiveIndex := &models.ArchiveQualityIndex{}
		if resolved := cfg.ResolveArchivePath(series); resolved != nil {
			indexed := IndexArchive(series.Name, *resolved, cfg)
			archiveIndex = &indexed
		}
		for _, result := range results {
			release := ParseRelease(result[0], result[1], result[2])
			if release == nil || release.Kind != "series" || release.IsPack || release.Series == nil || release.Season == nil || release.Episode == nil {
				continue
			}
			if cfg.FindSeriesMatch(*release.Series, release.Season) != series {
				continue
			}
			if cfg.AllReleaseDeniedReason(release) != "" || !cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title) {
				continue
			}
			name := series.Name
			release.Series = &name
			report.Releases++
			score := cfg.ReleaseScore(release)
			context := &models.ApprovalContext{
				Archive:           archiveIndex,
				Live:              &models.LiveDownloads{Hashes: map[string]struct{}{}, Episodes: map[models.LiveEpisodeKey]struct{}{}},
				UpgradeUntilScore: cfg.UpgradeUntilScore,
				ScoreSettings:     cfg.Settings,
				DryRun:            true,
			}
			approved, reason, err := db.CheckSeriesScored(release, score, cfg.UpgradeMinScoreDiff, context)
			if err != nil || !approved || reason != "upgrade" {
				continue
			}
			target := fmt.Sprintf("%s S%02dE%02d", series.Name, *release.Season, *release.Episode)
			if current, ok := best[target]; !ok || score > current.Score {
				best[target] = upgradeImpactItem{Target: target, Candidate: release.Title, Score: score}
			}
		}
	}
	report.Episodes = len(best)
	for _, item := range best {
		report.Items = append(report.Items, item)
	}
	sort.Slice(report.Items, func(i, j int) bool { return report.Items[i].Target < report.Items[j].Target })
	if len(report.Items) > upgradeImpactExamples {
		report.Items = report.Items[:upgradeImpactExamples]
	}
	return report, nil
}
