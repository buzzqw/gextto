package gextto

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

const sourceTrustSchema = `CREATE TABLE IF NOT EXISTS source_trust_daily (
	kind TEXT NOT NULL,
	source_key TEXT NOT NULL,
	source_label TEXT NOT NULL,
	day TEXT NOT NULL,
	attempts INTEGER NOT NULL DEFAULT 0,
	successes INTEGER NOT NULL DEFAULT 0,
	failures INTEGER NOT NULL DEFAULT 0,
	skipped INTEGER NOT NULL DEFAULT 0,
	results INTEGER NOT NULL DEFAULT 0,
	accepted INTEGER NOT NULL DEFAULT 0,
	completed INTEGER NOT NULL DEFAULT 0,
	hnr_warnings INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(kind, source_key, day)
);
CREATE INDEX IF NOT EXISTS idx_source_trust_day ON source_trust_daily(day);
CREATE TABLE IF NOT EXISTS torrent_transfer_finals (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	hash TEXT NOT NULL,
	finalized_at TEXT NOT NULL,
	source_key TEXT NOT NULL DEFAULT '',
	source_label TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	uploaded_bytes INTEGER NOT NULL DEFAULT 0,
	downloaded_bytes INTEGER NOT NULL DEFAULT 0,
	total_size INTEGER NOT NULL DEFAULT 0,
	seeding_seconds INTEGER NOT NULL DEFAULT 0,
	end_reason TEXT NOT NULL DEFAULT '',
	end_actor TEXT NOT NULL DEFAULT '',
	stats_known INTEGER NOT NULL DEFAULT 0,
	private INTEGER NOT NULL DEFAULT -1,
	freeleech INTEGER NOT NULL DEFAULT 0,
	freeleech_known INTEGER NOT NULL DEFAULT 0,
	share_ratio REAL,
	target_ratio REAL,
	target_seconds INTEGER,
	hnr_state TEXT NOT NULL DEFAULT 'unknown',
	hnr_detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_transfer_finals_hash ON torrent_transfer_finals(hash, finalized_at);
CREATE INDEX IF NOT EXISTS idx_transfer_finals_hnr ON torrent_transfer_finals(hnr_state, finalized_at);
CREATE INDEX IF NOT EXISTS idx_transfer_finals_source ON torrent_transfer_finals(source_key, finalized_at);
CREATE TABLE IF NOT EXISTS source_penalties (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source TEXT NOT NULL,
	reason TEXT NOT NULL,
	points INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL DEFAULT ''
);`

type SourceTrustStat struct {
	Kind        string `json:"kind"`
	Key         string `json:"key"`
	Label       string `json:"label"`
	Attempts    int64  `json:"attempts"`
	Successes   int64  `json:"successes"`
	Failures    int64  `json:"failures"`
	Skipped     int64  `json:"skipped"`
	Results     int64  `json:"results"`
	Accepted    int64  `json:"accepted"`
	Completed   int64  `json:"completed"`
	HNRWarnings int64  `json:"hnr_warnings"`
	LastError   string `json:"last_error,omitempty"`
	LastDay     string `json:"last_day,omitempty"`
}

type TransferFinal struct {
	ID              int64   `json:"id"`
	Hash            string  `json:"hash"`
	FinalizedAt     string  `json:"finalized_at"`
	SourceKey       string  `json:"source_key"`
	SourceLabel     string  `json:"source_label"`
	Title           string  `json:"title"`
	UploadedBytes   int64   `json:"uploaded_bytes"`
	DownloadedBytes int64   `json:"downloaded_bytes"`
	TotalSize       int64   `json:"total_size"`
	SeedingSeconds  int64   `json:"seeding_seconds"`
	EndReason       string  `json:"end_reason"`
	EndActor        string  `json:"end_actor"`
	StatsKnown      bool    `json:"stats_known"`
	Private         bool    `json:"private"`
	Freeleech       bool    `json:"freeleech"`
	FreeleechKnown  bool    `json:"freeleech_known"`
	ShareRatio      float64 `json:"share_ratio"`
	TargetRatio     float64 `json:"target_ratio"`
	TargetSeconds   int64   `json:"target_seconds"`
	HNRState        string  `json:"hnr_state"`
	HNRDetail       string  `json:"hnr_detail,omitempty"`
}

type SourceTrustReport struct {
	Days    int               `json:"days"`
	Sources []SourceTrustStat `json:"sources"`
	Finals  []TransferFinal   `json:"finals"`
}

func sourceTrustIdentity(kind, raw string) (string, string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "unknown"
	}
	label := strings.TrimSpace(raw)
	if label == "" {
		label = kind
	}
	keySource := strings.ToLower(label)
	switch kind {
	case "feed":
		keySource = strings.ToLower(label)
	case "indexer":
		keySource = strings.ReplaceAll(keySource, " · ", ":")
	case "archive":
		keySource = strings.TrimPrefix(keySource, "archive:")
	}
	return kind + ":" + keySource, label
}

func sourceTrustKind(cfg *Config, release *models.Release) string {
	if release == nil {
		return "unknown"
	}
	if _, ok := feedProviderForSource(cfg, release.Source); ok {
		return "feed"
	}
	source := strings.ToLower(strings.TrimSpace(release.Source))
	switch {
	case strings.HasPrefix(source, "archive:"):
		return "archive"
	case strings.HasPrefix(source, "prowlarr:"), strings.HasPrefix(source, "jackett:"), strings.HasPrefix(source, "indexarr:"):
		return "indexer"
	case strings.HasPrefix(source, "web:"):
		return "web"
	case release.Network == "ed2k":
		return "ed2k"
	default:
		return "unknown"
	}
}

var (
	sourceTrustUserInfoRE = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`)
	sourceTrustSecretRE   = regexp.MustCompile(`(?i)([?&](?:secret|passkey|token|password|api_?key)=)[^&\s]+`)
)

func redactSourceTrustError(value string) string {
	value = utils.RedactURLSecrets(strings.TrimSpace(value))
	value = sourceTrustUserInfoRE.ReplaceAllString(value, "${1}[redacted]@")
	value = sourceTrustSecretRE.ReplaceAllString(value, "${1}[redacted]")
	if len(value) > 300 {
		value = value[:300] + "…"
	}
	return value
}

func (d *Database) recordSourceTrustDaily(kind, provider string, attempts, successes, failures, skipped, results, accepted, completed, warnings int64, lastError string) error {
	key, label := sourceTrustIdentity(kind, provider)
	day := time.Now().UTC().Format("2006-01-02")
	_, err := d.db.Exec(`INSERT INTO source_trust_daily(
		kind,source_key,source_label,day,attempts,successes,failures,skipped,results,accepted,completed,hnr_warnings,last_error)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13)
		ON CONFLICT(kind,source_key,day) DO UPDATE SET
			attempts=source_trust_daily.attempts+excluded.attempts,
			successes=source_trust_daily.successes+excluded.successes,
			failures=source_trust_daily.failures+excluded.failures,
			skipped=source_trust_daily.skipped+excluded.skipped,
			results=source_trust_daily.results+excluded.results,
			accepted=source_trust_daily.accepted+excluded.accepted,
			completed=source_trust_daily.completed+excluded.completed,
			hnr_warnings=source_trust_daily.hnr_warnings+excluded.hnr_warnings,
			last_error=CASE WHEN excluded.last_error<>'' THEN excluded.last_error ELSE source_trust_daily.last_error END`,
		kind, key, label, day, attempts, successes, failures, skipped, results, accepted, completed, warnings, redactSourceTrustError(lastError))
	return err
}

func feedTrustProviderForSource(cfg *Config, source string) (string, bool) {
	key, ok := feedProviderForSource(cfg, source)
	if !ok {
		return "", false
	}
	for _, raw := range cfg.FeedURLs {
		if feedStatKey(raw) == key {
			return feedSourceName(raw), true
		}
	}
	return "", false
}

// RecordSourceTrustAccepted counts one automatic acquisition. Calling it from
// the shared acceptance hook keeps feeds, indexers and archive releases together.
func (d *Database) RecordSourceTrustAccepted(cfg *Config, release *models.Release) error {
	if d == nil || release == nil {
		return nil
	}
	kind := sourceTrustKind(cfg, release)
	provider := release.Source
	if kind == "feed" {
		if feed, ok := feedTrustProviderForSource(cfg, release.Source); ok {
			provider = feed
		}
	}
	if kind == "unknown" && provider == "" {
		return nil
	}
	return d.recordSourceTrustDaily(kind, provider, 0, 0, 0, 0, 0, 1, 0, 0, "")
}

// RecordSourceTrustCycle folds the cycle's per-source counters into one daily
// row per source. This avoids a database write for every title/indexer query.
func (d *Database) RecordSourceTrustCycle(sources []models.CycleSourceStat) error {
	if d == nil || len(sources) == 0 {
		return nil
	}
	type aggregate struct {
		kind, label       string
		ok, fail, results int64
		lastError         string
	}
	merged := map[string]*aggregate{}
	for _, source := range sources {
		kind := strings.TrimSpace(source.Kind)
		label := strings.TrimSpace(source.Name)
		if kind == "" || label == "" {
			continue
		}
		key, _ := sourceTrustIdentity(kind, label)
		item := merged[key]
		if item == nil {
			item = &aggregate{kind: kind, label: label}
			merged[key] = item
		}
		if source.OK > 0 {
			item.ok += int64(source.OK)
		}
		if source.Fail > 0 {
			item.fail += int64(source.Fail)
		}
		if source.Results > 0 {
			item.results += int64(source.Results)
		}
		if source.LastError != "" {
			item.lastError = source.LastError
		}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := merged[key]
		if err := d.recordSourceTrustDaily(item.kind, item.label, item.ok+item.fail, item.ok, item.fail, 0, item.results, 0, 0, 0, item.lastError); err != nil {
			return err
		}
	}
	return nil
}

func evaluateTransferHNR(cfg *Config, view *models.TorrentView, limits seedLimits, endActor string, freeleech, freeleechKnown bool) (string, string, float64, float64, int64) {
	state := "unknown"
	detail := ""
	ratio := 0.0
	targetRatio := 0.0
	var targetSeconds int64
	if view == nil {
		return state, "transfer statistics are unavailable", ratio, targetRatio, targetSeconds
	}
	if limits.Ratio != nil {
		targetRatio = *limits.Ratio
	}
	if limits.Seconds != nil {
		targetSeconds = *limits.Seconds
	}
	if limits.none() {
		return "not_applicable", "no finite seed policy applies", ratio, targetRatio, targetSeconds
	}
	if !view.Private {
		return "not_applicable", "the torrent is not private", ratio, targetRatio, targetSeconds
	}
	if view.TotalSize <= 0 {
		return state, "transfer statistics are unavailable", ratio, targetRatio, targetSeconds
	}
	if download := tevEffectiveDownloadForRatio(view); download > 0 {
		ratio = float64(view.AllTimeUpload) / download
	}
	switch endActor {
	case "policy":
		state = "met"
		detail = "the configured seed policy reached its limit"
	case "operator", "cleanup":
		ratioOK := limits.Ratio == nil || freeleechKnown || ratio >= targetRatio
		timeOK := limits.Seconds == nil || view.SeedingSeconds >= targetSeconds
		if ratioOK && timeOK {
			state = "met"
			detail = "the configured seed targets were reached"
		} else {
			state = "possible_shortfall"
			detail = fmt.Sprintf("seed stopped before every target: ratio %.2f/%.2f, time %ds/%ds", ratio, targetRatio, view.SeedingSeconds, targetSeconds)
		}
	default:
		detail = "the transfer ended outside a confirmed seed policy"
	}
	return state, detail, ratio, targetRatio, targetSeconds
}

// captureTorrentTransferFinal stores the last known counters after a successful
// removal. It is deliberately best-effort: reporting must never block the user
// from removing a torrent.
func captureTorrentTransferFinal(cfg *Config, db *Database, view *models.TorrentView, endReason, endActor string) {
	if db == nil || view == nil {
		return
	}
	meta, _ := db.TorrentMeta(view.Hash)
	source, title := "", view.Name
	private := view.Private
	freeleech, freeleechKnown := false, false
	if meta != nil {
		release := meta.Release
		if release.Source != "" {
			source = release.Source
		}
		if release.Title != "" {
			title = release.Title
		}
		freeleech, freeleechKnown = release.Freeleech, release.FreeleechKnown
	}
	key, label := sourceTrustIdentity(sourceTrustKind(cfg, &models.Release{Source: source, Network: view.Network}), source)
	limits := newSeedPolicy(cfg, db).limitsFor(view)
	state, detail, ratio, targetRatio, targetSeconds := evaluateTransferHNR(cfg, view, limits, endActor, freeleech, freeleechKnown)
	privateValue := -1
	if view.TotalSize > 0 {
		if private {
			privateValue = 1
		} else {
			privateValue = 0
		}
	}
	_, err := db.db.Exec(`INSERT INTO torrent_transfer_finals(
		hash,finalized_at,source_key,source_label,title,uploaded_bytes,downloaded_bytes,total_size,seeding_seconds,
		end_reason,end_actor,stats_known,private,freeleech,freeleech_known,share_ratio,target_ratio,target_seconds,hnr_state,hnr_detail)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16,?17,?18,?19,?20)`,
		strings.ToLower(view.Hash), sqliteTimestamp(time.Now()), key, label, title,
		view.AllTimeUpload, view.AllTimeDownload, view.TotalSize, view.SeedingSeconds,
		strings.TrimSpace(endReason), strings.TrimSpace(endActor), boolInt(view.TotalSize > 0), privateValue,
		boolInt(freeleech), boolInt(freeleechKnown), ratio, targetRatio, targetSeconds, state, detail)
	if err != nil {
		logging.Warn("could not record final torrent transfer statistics", "hash", view.Hash, "error", err.Error())
		return
	}
	warnings := int64(0)
	if state == "possible_shortfall" {
		warnings = 1
	}
	kind := "unknown"
	if strings.HasPrefix(key, ":") {
		kind = "unknown"
	} else if colon := strings.IndexByte(key, ':'); colon > 0 {
		kind = key[:colon]
	}
	if err := db.recordSourceTrustDaily(kind, label, 0, 0, 0, 0, 0, 0, 1, warnings, ""); err != nil {
		logging.Warn("could not update source trust totals", "hash", view.Hash, "error", err.Error())
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (d *Database) SourceTrustReport(days int) (SourceTrustReport, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := d.db.Query(`SELECT kind,source_key,source_label,day,attempts,successes,failures,skipped,results,accepted,completed,hnr_warnings,last_error
		FROM source_trust_daily WHERE day>=?1 ORDER BY day DESC, kind, source_key`, cutoff)
	if err != nil {
		return SourceTrustReport{}, err
	}
	report := SourceTrustReport{Days: days, Sources: []SourceTrustStat{}, Finals: []TransferFinal{}}
	index := map[string]int{}
	for rows.Next() {
		var item SourceTrustStat
		var day string
		if err := rows.Scan(&item.Kind, &item.Key, &item.Label, &day, &item.Attempts, &item.Successes, &item.Failures, &item.Skipped, &item.Results, &item.Accepted, &item.Completed, &item.HNRWarnings, &item.LastError); err != nil {
			rows.Close()
			return report, err
		}
		key := item.Kind + "\x00" + item.Key
		if at, ok := index[key]; ok {
			current := &report.Sources[at]
			current.Attempts += item.Attempts
			current.Successes += item.Successes
			current.Failures += item.Failures
			current.Skipped += item.Skipped
			current.Results += item.Results
			current.Accepted += item.Accepted
			current.Completed += item.Completed
			current.HNRWarnings += item.HNRWarnings
			continue
		}
		item.LastDay = day
		index[key] = len(report.Sources)
		report.Sources = append(report.Sources, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	rows.Close()
	sort.Slice(report.Sources, func(i, j int) bool {
		if report.Sources[i].Kind != report.Sources[j].Kind {
			return report.Sources[i].Kind < report.Sources[j].Kind
		}
		return report.Sources[i].Label < report.Sources[j].Label
	})
	finalRows, err := d.db.Query(`SELECT id,hash,finalized_at,source_key,source_label,title,uploaded_bytes,downloaded_bytes,total_size,seeding_seconds,
		end_reason,end_actor,stats_known,private,freeleech,freeleech_known,COALESCE(share_ratio,0),COALESCE(target_ratio,0),COALESCE(target_seconds,0),hnr_state,hnr_detail
		FROM torrent_transfer_finals WHERE finalized_at>=?1 ORDER BY finalized_at DESC, id DESC LIMIT 100`, cutoff)
	if err != nil {
		return report, err
	}
	defer finalRows.Close()
	for finalRows.Next() {
		var item TransferFinal
		var statsKnown, private, freeleech, freeleechKnown int
		if err := finalRows.Scan(&item.ID, &item.Hash, &item.FinalizedAt, &item.SourceKey, &item.SourceLabel, &item.Title,
			&item.UploadedBytes, &item.DownloadedBytes, &item.TotalSize, &item.SeedingSeconds, &item.EndReason, &item.EndActor,
			&statsKnown, &private, &freeleech, &freeleechKnown, &item.ShareRatio, &item.TargetRatio, &item.TargetSeconds, &item.HNRState, &item.HNRDetail); err != nil {
			return report, err
		}
		item.StatsKnown = statsKnown != 0
		item.Private = private == 1
		item.Freeleech = freeleech != 0
		item.FreeleechKnown = freeleechKnown != 0
		report.Finals = append(report.Finals, item)
	}
	return report, finalRows.Err()
}
