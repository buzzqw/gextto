// Package gextto's decision module implements the core module: a
// read-only explanation of the decisions taken on a release.
//
// It does not replace the approval path used by the automatic cycle yet. It
// rebuilds the checks without writing to the database, so the UI can explain a
// choice without creating placeholders, backups or other side effects.

package gextto

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// DecisionStep is one labelled check of the decision trace.
type DecisionStep struct {
	Rule   string `json:"rule"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// DecisionTrace is the structured, side-effect-free explanation of a decision.
type DecisionTrace struct {
	Decision        string           `json:"decision"`
	Reason          string           `json:"reason"`
	Candidate       string           `json:"candidate"`
	Target          string           `json:"target"`
	Score           int64            `json:"score"`
	ScoreComponents []map[string]any `json:"score_components"`
	Steps           []DecisionStep   `json:"steps"`
	Comparison      map[string]any   `json:"comparison"`
}

// decisionStep builds one step of the trace.
func decisionStep(rule string, result string, detail string) DecisionStep {
	return DecisionStep{Rule: rule, Result: result, Detail: detail}
}

// currentQuality parses the technical quality of a title.
func currentQuality(title string) models.Quality {
	return ParseQuality(title)
}

// int64ValueOrZero mirrors the `Option::unwrap_or_default` for `i64`.
func int64ValueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// ArchiveQuality scans the configured series archive. This is deliberately
// exposed to the HTTP layer so it can run in a blocking task; walking a NAS must
// not occupy a worker.
func ArchiveQuality(cfg *Config, release *models.Release) *models.ArchiveQuality {
	if release.Series == nil {
		return nil
	}
	series := cfg.FindSeriesMatch(*release.Series, release.Season)
	if series == nil {
		return nil
	}
	archivePath := cfg.ResolveArchivePath(series)
	if archivePath == nil {
		return nil
	}
	index := IndexArchive(series.Name, *archivePath, cfg.Settings)
	if release.Season == nil || release.Episode == nil {
		return nil
	}
	if best, ok := index.BestFor(*release.Season, *release.Episode); ok {
		return &best
	}
	return nil
}

// targetFor renders the human-readable target of a release.
func targetFor(release *models.Release) string {
	if release.Kind == "series" {
		series := release.Title
		if release.Series != nil {
			series = *release.Series
		}
		if release.IsPack {
			return fmt.Sprintf("%s S%02d pack", series, int64ValueOrZero(release.Season))
		}
		return fmt.Sprintf(
			"%s S%02dE%02d",
			series,
			int64ValueOrZero(release.Season),
			int64ValueOrZero(release.Episode),
		)
	}
	if release.Year != nil {
		return fmt.Sprintf("%s (%d)", release.Title, *release.Year)
	}
	return release.Title
}

// decisionReleaseMatch resolves the monitored series/movie a release belongs to.
func decisionReleaseMatch(cfg *Config, release *models.Release) (*SeriesConfig, *MovieConfig) {
	if release.Kind == "series" {
		name := release.Title
		if release.Series != nil {
			name = *release.Series
		}
		return cfg.FindSeriesMatch(name, release.Season), nil
	} else if release.Kind == "movie" {
		return nil, cfg.FindMovieMatchForRelease(release, true)
	}
	return nil, nil
}

// activeReason reports an in-progress download for the same hash or target.
func activeReason(db *Database, release *models.Release, hash *string) (*string, error) {
	if hash != nil {
		var active bool
		if err := db.db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM torrent_meta WHERE lower(hash)=lower(?1) AND status NOT IN ('completed','error','removed'))",
			*hash,
		).Scan(&active); err != nil {
			return nil, err
		}
		if active {
			reason := "lo stesso hash è già attivo"
			return &reason, nil
		}
	}
	if release.Kind == "series" {
		if release.Series != nil && release.Season != nil && release.Episode != nil {
			var active bool
			if err := db.db.QueryRow(
				"SELECT EXISTS(SELECT 1 FROM torrent_meta WHERE lower(series_name)=lower(?1) AND season=?2 AND (episode=?3 OR episode IS NULL) AND status NOT IN ('completed','error','removed'))",
				*release.Series, *release.Season, *release.Episode,
			).Scan(&active); err != nil {
				return nil, err
			}
			if active {
				reason := "l'episodio ha già un download attivo"
				return &reason, nil
			}
		}
	}
	return nil, nil
}

// archiveComparison builds the comparison against the archived/database copy.
func archiveComparison(
	db *Database,
	cfg *Config,
	release *models.Release,
	score int64,
	forbidUpgrade bool,
	disk *models.ArchiveQuality,
) (map[string]any, error) {
	if release.Kind == "movie" {
		var title string
		var oldScore int64
		var downloadedAt sql.NullString
		var metadataJSON string
		var mediaInfoJSON string
		query := "SELECT COALESCE(m.title,''), m.quality_score, m.downloaded_at, COALESCE(t.metadata_json,''), COALESCE(m.media_info_json,'') FROM movies m LEFT JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash) WHERE m.removed_at IS NULL AND m.name=?1 AND (m.year IS ?2 OR (?2 IS NOT NULL AND m.year IS NOT NULL AND abs(m.year - ?2) <= 1)) ORDER BY (m.year IS ?2) DESC, m.id DESC LIMIT 1"
		err := db.db.QueryRow(query, release.Title, release.Year).Scan(&title, &oldScore, &downloadedAt, &metadataJSON, &mediaInfoJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		oldQuality := currentQuality(title)
		var metadata models.TorrentMeta
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
			oldQuality = metadata.Release.Quality
		}
		enrichQualityWithMediaInfo(mediaInfoJSON, &oldQuality)
		var upgradeReason any
		if value := release.Quality.UpgradeReason(&oldQuality, score, oldScore, cfg.UpgradeMinScoreDiff); value != "" {
			upgradeReason = value
		}
		return map[string]any{
			"kind":            "movie",
			"title":           title,
			"quality":         oldQuality,
			"score":           oldScore,
			"downloaded":      downloadedAt.Valid,
			"upgrade_allowed": !forbidUpgrade,
			"upgrade_reason":  upgradeReason,
		}, nil
	}

	if release.Series == nil || release.Season == nil || release.Episode == nil {
		return nil, nil
	}
	var title string
	var oldScore int64
	var archivePath string
	var downloadedAt sql.NullString
	err := db.db.QueryRow(
		"SELECT COALESCE(e.title,''), e.quality_score, COALESCE(e.archive_path,''), e.downloaded_at FROM episodes e JOIN series s ON s.id=e.series_id WHERE lower(s.name)=lower(?1) AND e.season=?2 AND e.episode=?3",
		*release.Series, *release.Season, *release.Episode,
	).Scan(&title, &oldScore, &archivePath, &downloadedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if disk == nil {
			return nil, nil
		}
		var upgradeReason any
		if value := release.Quality.UpgradeReason(&disk.Quality, score, disk.Score, cfg.UpgradeMinScoreDiff); value != "" {
			upgradeReason = value
		}
		return map[string]any{
			"kind":            "episode",
			"title":           "file presente su disco",
			"quality":         disk.Quality,
			"score":           disk.Score,
			"archive_path":    "",
			"downloaded":      true,
			"upgrade_allowed": !forbidUpgrade,
			"upgrade_reason":  upgradeReason,
			"source":          "disk",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	diskPresent := disk != nil
	var oldQuality models.Quality
	if disk != nil {
		oldQuality = MergeQuality(disk.Quality, currentQuality(title))
		oldScore = maxInt64(disk.Score, oldScore)
	} else {
		oldQuality = currentQuality(title)
	}
	var upgradeReason any
	if value := release.Quality.UpgradeReason(&oldQuality, score, oldScore, cfg.UpgradeMinScoreDiff); value != "" {
		upgradeReason = value
	}
	source := "database"
	if diskPresent {
		source = "disk+database"
	}
	return map[string]any{
		"kind":            "episode",
		"title":           title,
		"quality":         oldQuality,
		"score":           oldScore,
		"archive_path":    archivePath,
		"downloaded":      downloadedAt.Valid,
		"upgrade_allowed": !forbidUpgrade,
		"upgrade_reason":  upgradeReason,
		"source":          source,
	}, nil
}

// Explain produces a detailed, side-effect-free explanation of the automatic
// decision.
func Explain(cfg *Config, db *Database, release *models.Release) (*DecisionTrace, error) {
	disk := ArchiveQuality(cfg, release)
	return ExplainWithArchive(cfg, db, release, disk)
}

// ExplainWithArchive is the same explanation using a previously computed
// archive index. The web layer uses this variant after moving the filesystem
// scan to a blocking task.
func ExplainWithArchive(
	cfg *Config,
	db *Database,
	release *models.Release,
	disk *models.ArchiveQuality,
) (*DecisionTrace, error) {
	steps := []DecisionStep{}
	var firstFailure *string
	setFailure := func(detail string) {
		if firstFailure == nil {
			firstFailure = &detail
		}
	}
	series, movie := decisionReleaseMatch(cfg, release)
	target := targetFor(release)
	forbidUpgrade := (series != nil && series.DisableUpgrades) ||
		(movie != nil && movie.DisableUpgrades)
	// The orchestrator canonicalises the title before computing the score and
	// comparing it with the DB. The explanation must do the same, otherwise a
	// manual search with the original title would not see the movie already
	// archived under the configured name.
	evaluated := *release
	if series != nil {
		name := series.Name
		evaluated.Series = &name
	}
	if movie != nil {
		evaluated.Title = movie.Name
		if parsed, err := strconv.ParseInt(movie.Year, 10, 64); err == nil {
			evaluated.Year = &parsed
		}
	}
	score := cfg.ReleaseScore(&evaluated)
	scoreComponents := []map[string]any{{
		"label": "qualità",
		"value": cfg.QualityScore(&evaluated.Quality),
	}}
	if movie != nil {
		if bonus := MovieSubtitleBonus(movie, &evaluated.Quality); bonus != 0 {
			scoreComponents = append(scoreComponents, map[string]any{
				"label": "preferenza sottotitoli",
				"value": bonus,
			})
		}
	} else if series != nil {
		if bonus := SeriesSubtitleBonus(series, &evaluated.Quality); bonus != 0 {
			scoreComponents = append(scoreComponents, map[string]any{
				"label": "preferenza sottotitoli",
				"value": bonus,
			})
		}
	}
	if sizeBonus := rules.SizeScoreBonus(&evaluated); sizeBonus != 0 {
		scoreComponents = append(scoreComponents, map[string]any{
			"label": "dimensione",
			"value": sizeBonus,
		})
	}

	if series != nil || movie != nil {
		steps = append(steps, decisionStep(
			"titolo monitorato",
			"pass",
			"La release corrisponde a un elemento monitorato.",
		))
	} else {
		detail := "Nessun film o serie attiva corrisponde al titolo, stagione e anno."
		steps = append(steps, decisionStep("titolo monitorato", "fail", detail))
		firstFailure = &detail
	}

	var hash *string
	if value, ok := utils.MagnetHash(release.Magnet); ok {
		hash = &value
	}
	if hash != nil {
		blocked, err := db.IsBlocklisted(*hash)
		if err != nil {
			return nil, err
		}
		if blocked {
			detail := "L'hash è presente nella blocklist."
			steps = append(steps, decisionStep("blocklist", "fail", detail))
			setFailure(detail)
		} else {
			steps = append(steps, decisionStep(
				"blocklist",
				"pass",
				"Hash non presente nella blocklist.",
			))
		}
	} else {
		detail := "La release non contiene un infohash verificabile."
		steps = append(steps, decisionStep("infohash", "fail", detail))
		setFailure(detail)
	}

	if reason := cfg.ReleaseDeniedReason(release); reason != "" {
		steps = append(steps, decisionStep("filtri globali", "fail", reason))
		setFailure(reason)
	} else {
		steps = append(steps, decisionStep(
			"filtri globali",
			"pass",
			"Blacklist, filtri contenuto ed età massima superati.",
		))
	}
	if reason := cfg.SourceFilterDeniedReason(release); reason != "" {
		steps = append(steps, decisionStep("filtro sorgente", "fail", reason))
		setFailure(reason)
	} else {
		steps = append(steps, decisionStep(
			"filtro sorgente",
			"pass",
			"Nessun filtro per sorgente rifiuta questa release.",
		))
	}
	if reason := rules.DeniedReason(release); reason != "" {
		steps = append(steps, decisionStep("sanità della release", "fail", reason))
		setFailure(reason)
	} else {
		steps = append(steps, decisionStep(
			"sanità della release",
			"pass",
			"Dimensione minima e sottotitoli hardcoded superati.",
		))
	}

	policyAllowed := false
	if series != nil {
		policyAllowed = cfg.SeriesReleaseAllowed(series, &release.Quality, release.Title)
	} else if movie != nil {
		policyAllowed = cfg.MovieReleaseAllowedForTitle(movie, &release.Quality, release.Title)
	}
	if policyAllowed {
		steps = append(steps, decisionStep(
			"qualità, lingua e sottotitoli",
			"pass",
			"La release rispetta i requisiti del titolo.",
		))
	} else {
		var detail string
		if series != nil {
			detail = fmt.Sprintf(
				"Configurazione — qualità: %s · lingua: %s · sottotitoli: %s · esclusioni: %s",
				series.Quality, series.Language, series.Subtitle, series.Exclude,
			)
		} else if movie != nil {
			detail = fmt.Sprintf(
				"Configurazione — qualità: %s · lingua: %s · requisiti lingua: %s · sottotitoli: %s · requisiti sottotitoli: %s",
				movie.Quality,
				movie.Language,
				movie.LanguageRequirements,
				movie.Subtitle,
				movie.SubtitleRequirements,
			)
		} else {
			detail = "Nessun profilo di titolo disponibile."
		}
		steps = append(steps, decisionStep("qualità, lingua e sottotitoli", "fail", detail))
		setFailure(detail)
	}

	if series != nil {
		minSize, err := db.SeriesArchivedMinSize(series.Name)
		if err != nil {
			return nil, err
		}
		if reason := rules.SaneSizeDeniedReason(release, minSize); reason != "" {
			steps = append(steps, decisionStep(
				"dimensione rispetto all'archivio",
				"fail",
				reason,
			))
			setFailure(reason)
		} else {
			steps = append(steps, decisionStep(
				"dimensione rispetto all'archivio",
				"pass",
				"La dimensione è compatibile con la storia dell'archivio.",
			))
		}
	} else if movie != nil {
		minSize, err := db.MovieArchivedSize(movie.Name, release.Year)
		if err != nil {
			return nil, err
		}
		if reason := rules.SaneSizeDeniedReason(release, minSize); reason != "" {
			steps = append(steps, decisionStep(
				"dimensione rispetto all'archivio",
				"fail",
				reason,
			))
			setFailure(reason)
		} else {
			steps = append(steps, decisionStep(
				"dimensione rispetto all'archivio",
				"pass",
				"La dimensione è compatibile con l'archivio.",
			))
		}
	}

	if reason, err := activeReason(db, &evaluated, hash); err != nil {
		return nil, err
	} else if reason != nil {
		steps = append(steps, decisionStep("download attivo", "fail", *reason))
		setFailure(*reason)
	} else {
		steps = append(steps, decisionStep(
			"download attivo",
			"pass",
			"Non risultano download concorrenti per questo target.",
		))
	}

	// A later episode already archived makes the cycle skip this older one
	// unless it fills a gap or is a valid upgrade. Surface it explicitly instead
	// of leaving it only in the generic note below.
	if series != nil && !release.IsPack && release.Season != nil && release.Episode != nil {
		if rank, rankErr := db.LaterArchivedMaxResolutionRank(series.Name, *release.Season, *release.Episode); rankErr == nil && rank != nil {
			steps = append(steps, decisionStep(
				"smart episode",
				"info",
				"Un episodio successivo è già archiviato: verrà scaricato solo se è una lacuna o un upgrade valido.",
			))
		}
	}
	// These checks are deliberately informative: the endpoint receives one
	// release and not the complete cycle context (gap set, other candidates,
	// free space and pending-delay state). They must not be presented as a
	// definitive approval/rejection.
	steps = append(steps, decisionStep(
		"selezione del ciclo",
		"info",
		"La scelta finale può inoltre dipendere da gap filling, smart episode, ritardi, spazio libero e confronto con gli altri candidati del ciclo.",
	))

	comparison, err := archiveComparison(db, cfg, &evaluated, score, forbidUpgrade, disk)
	if err != nil {
		return nil, err
	}
	if comparison == nil {
		if release.IsPack {
			steps = append(steps, decisionStep(
				"confronto archivio",
				"info",
				"Season pack: la scelta viene valutata episodio per episodio rispetto all'archivio.",
			))
		} else {
			steps = append(steps, decisionStep(
				"confronto archivio",
				"pass",
				"Nessun file esistente da sostituire: è un primo download.",
			))
		}
	} else {
		upgradeReason, hasUpgradeReason := comparison["upgrade_reason"].(string)
		downloaded, _ := comparison["downloaded"].(bool)
		if forbidUpgrade && downloaded {
			detail := "Gli upgrade sono disabilitati per questo titolo."
			steps = append(steps, decisionStep("confronto archivio", "fail", detail))
			setFailure(detail)
		} else if hasUpgradeReason {
			steps = append(steps, decisionStep(
				"confronto archivio",
				"pass",
				fmt.Sprintf("Upgrade riconosciuto: %s.", upgradeReason),
			))
		} else {
			detail := "Il file presente è uguale o migliore, oppure il miglioramento non supera la soglia configurata."
			steps = append(steps, decisionStep("confronto archivio", "fail", detail))
			setFailure(detail)
		}
	}

	// The verdict is not rebuilt here: it comes from the same approval engine
	// the automatic cycle uses, run in dry-run mode (no placeholder, torrent or
	// upgrade is written). The steps above are only the human-readable detail.
	decision := "eligible"
	reason := "La release supera i controlli read-only."
	if (series != nil || movie != nil) && hash != nil {
		approvalContext := &models.ApprovalContext{
			Archive:       indexFromDisk(disk, release),
			ForbidUpgrade: forbidUpgrade,
			DryRun:        true,
		}
		var approved bool
		var realReason string
		if series != nil {
			approved, realReason, err = db.CheckSeriesScored(&evaluated, score, cfg.UpgradeMinScoreDiff, approvalContext)
		} else {
			approved, realReason, err = db.checkMovieScoredWith(&evaluated, score, cfg.UpgradeMinScoreDiff, forbidUpgrade, true)
		}
		if err != nil {
			// An incomplete release (for example a pack without a season) must
			// not make the explanation fail: fall back to the read-only steps.
			if firstFailure != nil {
				decision = "rejected"
				reason = *firstFailure
			}
		} else {
			if !approved {
				decision = "rejected"
			}
			reason = decisionReasonText(realReason)
		}
	} else if firstFailure != nil {
		decision = "rejected"
		reason = *firstFailure
	}
	return &DecisionTrace{
		Decision:        decision,
		Reason:          reason,
		Candidate:       release.Title,
		Target:          target,
		Score:           score,
		ScoreComponents: scoreComponents,
		Steps:           steps,
		Comparison:      comparison,
	}, nil
}

// indexFromDisk builds a single-entry archive index from the best file found on
// disk, so the real approval engine receives the same disk context the
// explanation already computed.
func indexFromDisk(disk *models.ArchiveQuality, release *models.Release) *models.ArchiveQualityIndex {
	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	if disk != nil && release.Season != nil && release.Episode != nil {
		index.Best[[2]int64{*release.Season, *release.Episode}] = *disk
	}
	return index
}

// decisionReasonText turns the approval reason code into a readable sentence.
func decisionReasonText(reason string) string {
	switch reason {
	case "approved", "gap_filled", "gap_fill":
		return "La release supera i controlli di approvazione."
	case "upgrade":
		return "Upgrade riconosciuto rispetto al file esistente."
	case "restored":
		return "La release ripristina un download rimosso."
	case "duplicate":
		return "Esiste già un file uguale o migliore: nessun download."
	case "upgrades_disabled":
		return "Gli upgrade sono disabilitati per questo titolo."
	case "active_episode", "active_pack":
		return "Un download per questo titolo è già attivo."
	case "blocklisted":
		return "L'hash è presente nella blocklist."
	case "smart_episode":
		return "Esiste un episodio successivo archiviato e questa non è una lacuna."
	default:
		if strings.TrimSpace(reason) == "" {
			return "La release è stata rifiutata."
		}
		return reason
	}
}
