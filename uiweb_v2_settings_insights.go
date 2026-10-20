package gextto

import (
	"fmt"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

// uiweb_v2_settings_insights.go adds the read-only insights the settings page
// was missing: the stuck-download timeline, live score examples, and the
// sources entry point. Nothing here moves a setting or adds a control: these
// are views built from the values already configured.

// --- Stuck-download timeline -------------------------------------------------

// stallSummaryGroupTitles are the two panels that split the stuck-download
// policy. Both get the same one-line timeline, so the whole sequence stays
// visible wherever the user lands (the rows themselves are not moved).
var stallSummaryGroupTitles = map[string]bool{
	"Torrent bloccati": true,
}

// stallPolicySummary renders the six stuck-download windows in the order they
// fire, from the values currently configured.
func stallPolicySummary(s *AppState, cfg *Config) string {
	tr := func(italian string) string { return uiText(s, italian) }
	window := func(minutes int64) string {
		switch {
		case minutes <= 0:
			return tr("mai")
		case minutes%1440 == 0:
			return fmt.Sprintf("%d %s", minutes/1440, tr("giorni"))
		default:
			return fmt.Sprintf("%d %s", minutes, tr("minuti"))
		}
	}
	template := tr("Sequenza: bloccato dopo %s; riprovo ogni %s; provo un'alternativa dopo %s (fino a %d punti in meno); senza seeder rimuovo dopo %s; comunque rimuovo dopo %s.")
	return fmt.Sprintf(template,
		window(numberSettingInt(cfg.Settings, "torrent_stall_after_min", 60)),
		window(numberSettingInt(cfg.Settings, "torrent_stall_retry_min", 60)),
		window(cfg.StallAlternativeAfterMin),
		cfg.StallAlternativeScoreDrop,
		window(numberSettingInt(cfg.Settings, "torrent_dead_swarm_giveup_min", 4320)),
		window(numberSettingInt(cfg.Settings, "torrent_stall_giveup_min", 20160)),
	)
}

// --- Live score examples -----------------------------------------------------

// v2ScoreExample is one reference release with its current score.
type v2ScoreExample struct {
	// Text is a technical token (resolution/source/codec): the same in every
	// language, so it is not translated.
	Text  string
	Value int64
}

// scoreExampleSamples are the reference releases shown above the weights.
var scoreExampleSamples = []v2ScoreExample{
	{Text: "1080p WEB-DL H.264", Value: 0},
	{Text: "1080p WEB-DL H.265 · DD+", Value: 0},
	{Text: "2160p · Dolby Vision", Value: 0},
}

func scoreExampleQuality(index int) models.Quality {
	switch index {
	case 0:
		return models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h264"}
	case 1:
		return models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h265", Audio: "ddp"}
	default:
		return models.Quality{Resolution: "2160p", IsDV: true, HDR: "DV"}
	}
}

// scoreExamples scores the reference releases with the current weights. It is
// pure so a test can assert that moving a weight moves the example.
func scoreExamples(cfg *Config) []v2ScoreExample {
	out := make([]v2ScoreExample, 0, len(scoreExampleSamples))
	for index, sample := range scoreExampleSamples {
		release := models.Release{Quality: scoreExampleQuality(index)}
		out = append(out, v2ScoreExample{Text: sample.Text, Value: cfg.ReleaseScore(&release)})
	}
	return out
}

// scoreExampleLines is what the Punteggi tab renders: the examples plus the
// current stop threshold, translated.
func scoreExampleLines(s *AppState, cfg *Config) []string {
	tr := func(italian string) string { return uiText(s, italian) }
	out := make([]string, 0, len(scoreExampleSamples)+1)
	for _, example := range scoreExamples(cfg) {
		out = append(out, fmt.Sprintf("%s ≈ %d", example.Text, example.Value))
	}
	if cfg.UpgradeUntilScore > 0 {
		out = append(out, fmt.Sprintf(tr("Soglia di stop: %d"), cfg.UpgradeUntilScore))
	} else {
		out = append(out, tr("Nessuna soglia di stop."))
	}
	return out
}

// --- Sources entry point -----------------------------------------------------

// v2SourcesLink is one deep link of the Sorgenti entry point.
type v2SourcesLink struct {
	Label  string
	Href   string
	Hint   string
	Status string
	// Alert marks a status that needs attention (failed last cycle).
	Alert bool
}

type v2SourcesIndex struct {
	Title string
	Hint  string
	Links []v2SourcesLink
}

// lastCycleIndexerFailures returns the indexer failures of the most recent
// cycle and whether any indexer actually ran, so the entry point can show the
// real state instead of a guess.
func lastCycleIndexerFailures(s *AppState) (int, bool) {
	if s == nil || s.db == nil {
		return 0, false
	}
	cycles, err := s.db.RecentCycleStats(1)
	if err != nil || len(cycles) == 0 {
		return 0, false
	}
	failures, seen := 0, false
	for _, source := range cycles[0].Sources {
		if source.Kind != "indexer" {
			continue
		}
		seen = true
		failures += source.Fail
	}
	return failures, seen
}

// v2SourcesIndexFrom builds the Sorgenti entry point: three deep links to the
// places the real controls live, each with a one-line status.
func v2SourcesIndexFrom(s *AppState, cfg *Config) *v2SourcesIndex {
	tr := func(italian string) string { return uiText(s, italian) }

	active, total := 0, len(cfg.Indexers)
	for _, indexer := range cfg.Indexers {
		if indexer.Enabled {
			active++
		}
	}
	indexerStatus := tr("Nessun indexer configurato.")
	alert := false
	if total > 0 {
		indexerStatus = fmt.Sprintf(tr("%d attivi su %d configurati"), active, total)
	}
	if failures, seen := lastCycleIndexerFailures(s); seen {
		if failures > 0 {
			indexerStatus = fmt.Sprintf(tr("ultimo ciclo: %d errori"), failures)
			alert = true
		} else {
			indexerStatus = tr("ultimo ciclo: ok")
		}
	}

	engines := 0
	for _, engine := range cfg.WebsearchEngines {
		if strings.TrimSpace(engine) != "" {
			engines++
		}
	}
	ed2kLabel := tr("spenta")
	ed2kAlert := false
	if ed2kEnabled(cfg) {
		entry, known := ed2kHealthCached(cfg)
		switch {
		case !known:
			ed2kLabel = tr("stato sconosciuto")
		case !entry.healthy:
			ed2kLabel = tr("demone non raggiungibile")
			ed2kAlert = true
		case entry.reachability == "open":
			ed2kLabel = tr("aperta")
			if entry.mapping != "" {
				ed2kLabel += " · " + strings.ToUpper(entry.mapping)
			}
		case entry.reachability == "limited":
			ed2kLabel = tr("limitata (firewall/NAT)")
			ed2kAlert = true
		default:
			ed2kLabel = tr("stato sconosciuto")
		}
	}
	return &v2SourcesIndex{
		Title: tr("Le tue fonti"),
		Hint:  tr("Qui si aprono le aree delle fonti; i controlli restano nelle loro pagine."),
		Links: []v2SourcesLink{
			{
				Label:  tr("Indexer e manager"),
				Href:   "/?view=integrations",
				Hint:   tr("Jackett, Prowlarr e FlareSolverr."),
				Status: indexerStatus,
				Alert:  alert,
			},
			{
				Label:  tr("Feed RSS"),
				Href:   "#v2-sources-feeds",
				Hint:   tr("Gli indirizzi dei feed, uno per riga."),
				Status: fmt.Sprintf(tr("%d feed configurati"), len(cfg.FeedURLs)),
			},
			{
				Label:  tr("Motori web"),
				Href:   "#v2-checkbox-panel-websearch_engines",
				Hint:   tr("I motori di ricerca usati per gli episodi mancanti."),
				Status: fmt.Sprintf(tr("%d motori attivi"), engines),
			},
			{
				Label:  tr("eD2k e Kad"),
				Href:   "#v2-setting-ed2k_enabled",
				Hint:   tr("Ricerca Kad/eD2k e stato della connessione."),
				Status: ed2kLabel,
				Alert:  ed2kAlert,
			},
		},
	}
}
