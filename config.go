package gextto

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/rules"
	"github.com/buzzqw/gextto/internal/utils"
)

// CONFIG_GENERATION is a monotonic counter bumped on every persisted
// configuration change. It lets `Config` loading be cached until something
// changes, avoiding re-reading files and SQLite on every HTTP request.
var configGeneration atomic.Uint64

func init() {
	configGeneration.Store(1)
}

// ConfigGeneration returns the current monotonic configuration generation.
func ConfigGeneration() uint64 {
	return configGeneration.Load()
}

// TouchConfigGeneration bumps the configuration generation.
func TouchConfigGeneration() {
	configGeneration.Add(1)
}

// SeriesConfig is one monitored series.
type SeriesConfig struct {
	Name             string   `json:"name"`
	Seasons          string   `json:"seasons"`
	Quality          string   `json:"quality"`
	Language         string   `json:"language"`
	ArchivePath      string   `json:"archive_path"`
	Timeframe        int64    `json:"timeframe"`
	Aliases          []string `json:"aliases"`
	TmdbID           string   `json:"tmdb_id"`
	TvdbID           string   `json:"tvdb_id"`
	Subtitle         string   `json:"subtitle"`
	Exclude          string   `json:"exclude"`
	Enabled          bool     `json:"enabled"`
	IgnoredSeasons   []int64  `json:"ignored_seasons"`
	SeasonSubfolders bool     `json:"season_subfolders"`
	// Anime series number releases by absolute episode ("Title - 1071"):
	// those numbers are mapped onto TMDB seasons.
	Anime bool `json:"anime"`
	// When true, a better release never replaces an archived file for this
	// series. Default false (= upgrades allowed).
	DisableUpgrades bool `json:"disable_upgrades"`
}

// UpgradesAllowed reports whether a better release may replace an archived
// file (default true).
func (s *SeriesConfig) UpgradesAllowed() bool { return !s.DisableUpgrades }

// MovieConfig is one monitored movie.
type MovieConfig struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Year string `json:"year"`
	// Identificativi e dati editoriali: non intervengono nei filtri o nello
	// stato del download e possono essere aggiornati dalla scelta TMDB/TVDB.
	TmdbID               string `json:"tmdb_id"`
	TvdbID               string `json:"tvdb_id"`
	OriginalTitle        string `json:"original_title"`
	Overview             string `json:"overview"`
	PosterPath           string `json:"poster_path"`
	Quality              string `json:"quality"`
	Language             string `json:"language"`
	Enabled              bool   `json:"enabled"`
	Subtitle             string `json:"subtitle"`
	Exclude              string `json:"exclude"`
	LanguageRequirements string `json:"language_requirements"`
	SubtitleRequirements string `json:"subtitle_requirements"`
	// When true, a better release never replaces an archived file for this
	// movie. Default false (= upgrades allowed).
	DisableUpgrades bool `json:"disable_upgrades"`
}

// UpgradesAllowed reports whether a better release may replace an archived
// file (default true).
func (m *MovieConfig) UpgradesAllowed() bool { return !m.DisableUpgrades }

// IndexerConfig describes one configured indexer.
//
// `Manager` optionally declares that the indexer is a Jackett/Prowlarr manager
// entry ("jackett" or "prowlarr"), so the endpoint is chosen explicitly instead
// of being inferred from the URL/port/name.
type IndexerConfig struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
	Manager string `json:"manager,omitempty"`
}

// UnmarshalJSON applies the JSON default: `enabled` defaults to true when the
// key is absent. The plain Go zero value keeps it false, matching
// `IndexerConfig::default()`.
func (i *IndexerConfig) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		APIKey  string `json:"api_key"`
		Enabled *bool  `json:"enabled"`
		Manager string `json:"manager"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.Name = raw.Name
	i.URL = raw.URL
	i.APIKey = raw.APIKey
	i.Enabled = true
	if raw.Enabled != nil {
		i.Enabled = *raw.Enabled
	}
	i.Manager = normalizeManagerKind(raw.Manager)
	return nil
}

// SourceFilter is a per-source content filter: when a release's `source`
// contains `source` (case-insensitive), any of `keywords` found in the title
// blocks the release. Lets the user silence one feed/indexer/engine without
// disabling it entirely.
type SourceFilter struct {
	Source   string   `json:"source"`
	Keywords []string `json:"keywords"`
	Enabled  bool     `json:"enabled"`
}

// UnmarshalJSON applies the JSON default: `enabled` defaults to true when the
// key is absent. The plain Go zero value keeps it false, matching
// `SourceFilter::default()`.
func (f *SourceFilter) UnmarshalJSON(data []byte) error {
	var raw struct {
		Source   string   `json:"source"`
		Keywords []string `json:"keywords"`
		Enabled  *bool    `json:"enabled"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	f.Source = raw.Source
	f.Keywords = raw.Keywords
	f.Enabled = true
	if raw.Enabled != nil {
		f.Enabled = *raw.Enabled
	}
	return nil
}

// ParseSourceFilters parses the `source_filters` setting (a JSON array); bad
// input yields none.
func ParseSourceFilters(raw string) []SourceFilter {
	var filters []SourceFilter
	if err := json.Unmarshal([]byte(raw), &filters); err != nil {
		return nil
	}
	return filters
}

// LibtorrentSettings mirrors the libtorrent session configuration.
type LibtorrentSettings struct {
	PortMin          uint16  `json:"port_min"`
	PortMax          uint16  `json:"port_max"`
	DownloadLimitKib int64   `json:"download_limit_kib"`
	UploadLimitKib   int64   `json:"upload_limit_kib"`
	SeedRatio        float64 `json:"seed_ratio"`
	SeedTimeMinutes  int64   `json:"seed_time_minutes"`
	SeedTimeDays     int64   `json:"seed_time_days"`
	ActiveDownloads  int64   `json:"active_downloads"`
	ActiveSeeds      int64   `json:"active_seeds"`
	ActiveLimit      int64   `json:"active_limit"`
	Dht              bool    `json:"dht"`
	Pex              bool    `json:"pex"`
	Lsd              bool    `json:"lsd"`
	Upnp             bool    `json:"upnp"`
	Natpmp           bool    `json:"natpmp"`
	DynamicQueue     bool    `json:"dynamic_queue"`
	DynamicQueueMin  int64   `json:"dynamic_queue_min"`
	DynamicQueueMax  int64   `json:"dynamic_queue_max"`
	// Quando è true, i torrent che non trasferiscono payload non occupano uno
	// slot attivo: evita che gli swarm senza peer blocchino i download sani.
	DontCountSlowTorrents bool `json:"dont_count_slow_torrents"`
	AutoRemoveCompleted   bool `json:"auto_remove_completed"`
	// Extended session settings.
	ConnectionsLimit              int64  `json:"connections_limit"`
	UploadSlotsLimit              int64  `json:"upload_slots_limit"`
	HalfOpenLimit                 int64  `json:"half_open_limit"`
	AlertQueueSize                int64  `json:"alert_queue_size"`
	MaxConnectionsPerTorrent      int64  `json:"max_connections_per_torrent"`
	MaxUploadsPerTorrent          int64  `json:"max_uploads_per_torrent"`
	AioThreads                    int64  `json:"aio_threads"`
	CacheSize                     int64  `json:"cache_size"`
	CacheExpiry                   int64  `json:"cache_expiry"`
	AnnounceInterval              int64  `json:"announce_interval"`
	TorrentConnectBoost           int64  `json:"torrent_connect_boost"`
	Utp                           bool   `json:"utp"`
	PreferRc4                     bool   `json:"prefer_rc4"`
	AnnounceToAllTrackers         bool   `json:"announce_to_all_trackers"`
	AnnounceToAllTiers            bool   `json:"announce_to_all_tiers"`
	AllowMultipleConnectionsPerIp bool   `json:"allow_multiple_connections_per_ip"`
	ApplyIpFilter                 bool   `json:"apply_ip_filter"`
	Encryption                    int64  `json:"encryption"`
	IpFilterPath                  string `json:"ip_filter_path"`
	ListenInterfaces              string `json:"listen_interfaces"`
	// Interfaccia forzata per il traffico in uscita (killswitch VPN). Quando è
	// impostata, libtorrent annuncia e si connette solo da questa scheda.
	OutgoingInterface string `json:"outgoing_interface"`
	DhtBootstrapNodes string `json:"dht_bootstrap_nodes"`
}

// DefaultLibtorrentSettings implements `impl Default for LibtorrentSettings`.
func DefaultLibtorrentSettings() LibtorrentSettings {
	return LibtorrentSettings{
		PortMin:                       6881,
		PortMax:                       6891,
		DownloadLimitKib:              0,
		UploadLimitKib:                0,
		SeedRatio:                     0.0,
		SeedTimeMinutes:               0,
		SeedTimeDays:                  0,
		ActiveDownloads:               3,
		ActiveSeeds:                   3,
		ActiveLimit:                   5,
		Dht:                           true,
		Pex:                           true,
		Lsd:                           true,
		Upnp:                          true,
		Natpmp:                        true,
		DynamicQueue:                  false,
		DynamicQueueMin:               1,
		DynamicQueueMax:               10,
		DontCountSlowTorrents:         true,
		AutoRemoveCompleted:           false,
		ConnectionsLimit:              200,
		UploadSlotsLimit:              -1,
		HalfOpenLimit:                 -1,
		AlertQueueSize:                1000,
		MaxConnectionsPerTorrent:      -1,
		MaxUploadsPerTorrent:          -1,
		AioThreads:                    -1,
		CacheSize:                     -1,
		CacheExpiry:                   300,
		AnnounceInterval:              1800,
		TorrentConnectBoost:           50,
		Utp:                           true,
		PreferRc4:                     false,
		AnnounceToAllTrackers:         false,
		AnnounceToAllTiers:            false,
		AllowMultipleConnectionsPerIp: true,
		ApplyIpFilter:                 true,
		Encryption:                    1,
		IpFilterPath:                  "",
		ListenInterfaces:              "",
		OutgoingInterface:             "",
		DhtBootstrapNodes:             "",
	}
}

// UnmarshalJSON overlays the present JSON keys on top of the defaults, so
// an omitted field keeps its documented default instead of the Go zero value.
func (s *LibtorrentSettings) UnmarshalJSON(data []byte) error {
	*s = DefaultLibtorrentSettings()
	type alias LibtorrentSettings
	return json.Unmarshal(data, (*alias)(s))
}

// Config is the runtime configuration of the daemon.
type Config struct {
	DataDir             string             `json:"data_dir"`
	Listen              string             `json:"listen"`
	EngineListen        string             `json:"engine_listen"`
	RefreshSecs         uint64             `json:"refresh_secs"`
	Active              bool               `json:"active"`
	DryRun              bool               `json:"dry_run"`
	FeedURLs            []string           `json:"feed_urls"`
	Blacklist           []string           `json:"blacklist"`
	ContentFilters      []string           `json:"content_filters"`
	SourceFilters       []SourceFilter     `json:"source_filters"`
	MaxReleaseAgeDays   int64              `json:"max_release_age_days"`
	Series              []SeriesConfig     `json:"series"`
	Movies              []MovieConfig      `json:"movies"`
	Indexers            []IndexerConfig    `json:"indexers"`
	WebsearchEngines    []string           `json:"websearch_engines"`
	FlaresolverrURL     *string            `json:"flaresolverr_url"`
	Settings            map[string]string  `json:"settings"`
	TmdbAPIKey          *string            `json:"tmdb_api_key"`
	RenameEpisodes      bool               `json:"rename_episodes"`
	RenameFormat        string             `json:"rename_format"`
	RenameTemplate      string             `json:"rename_template"`
	ArchiveRoot         *string            `json:"archive_root"`
	TrashPath           *string            `json:"trash_path"`
	NotifyTelegram      bool               `json:"notify_telegram"`
	TelegramBotToken    *string            `json:"telegram_bot_token"`
	TelegramChatID      *string            `json:"telegram_chat_id"`
	NotifyWebhookURL    *string            `json:"notify_webhook_url"`
	NotifyWebhookSecret *string            `json:"notify_webhook_secret"`
	NotifyWebhookFormat string             `json:"notify_webhook_format"`
	NotifyWebhookToken  *string            `json:"notify_webhook_token"`
	NotifyWebhookUser   *string            `json:"notify_webhook_user"`
	NotifyEmail         bool               `json:"notify_email"`
	EmailSMTP           string             `json:"email_smtp"`
	EmailFrom           *string            `json:"email_from"`
	EmailTo             *string            `json:"email_to"`
	EmailPassword       *string            `json:"email_password"`
	CleanupUpgrades     bool               `json:"cleanup_upgrades"`
	CleanupMinScoreDiff int64              `json:"cleanup_min_score_diff"`
	UpgradeMinScoreDiff int64              `json:"upgrade_min_score_diff"`
	UpgradeUntilScore   int64              `json:"upgrade_until_score"`
	CleanupAction       string             `json:"cleanup_action"`
	LibtorrentEnabled   bool               `json:"libtorrent_enabled"`
	Libtorrent          LibtorrentSettings `json:"libtorrent"`
	LibtorrentDir       string             `json:"libtorrent_dir"`
	LibtorrentTempDir   *string            `json:"libtorrent_temp_dir"`
	StateDir            string             `json:"state_dir"`
}

const defaultRenameFormatValue = "base"
const defaultRenameTemplateValue = "{Serie} - {Stagione}{Episodio} - {Titolo} [{Risoluzione}][{Lingue}]"

// defaultBlacklist implements `default_blacklist()`.
func defaultBlacklist() []string {
	return []string{
		"cam",
		"camrip",
		"ts",
		"telesync",
		"telecine",
		"scr",
		"screener",
		"workprint",
		"sample",
	}
}

// parseConfigList parses a JSON array of strings, falling back to a
// comma-separated list.
func parseConfigList(value string) []string {
	if strings.HasPrefix(strings.TrimSpace(value), "[") {
		var items []string
		if err := json.Unmarshal([]byte(value), &items); err == nil {
			return items
		}
	}
	result := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// contentFilterRanges maps a named Unicode-script filter to its code point
// ranges.
func contentFilterRanges(filter string) ([][2]int64, bool) {
	switch filter {
	case "[cjk]":
		return [][2]int64{
			{0x4E00, 0x9FFF},
			{0x3400, 0x4DBF},
			{0xF900, 0xFAFF},
			{0x3040, 0x30FF},
			{0xAC00, 0xD7AF},
			{0x3000, 0x303F},
		}, true
	case "[cirillico]":
		return [][2]int64{{0x0400, 0x04FF}, {0x0500, 0x052F}}, true
	case "[arabo]":
		return [][2]int64{
			{0x0600, 0x06FF},
			{0x0750, 0x077F},
			{0xFB50, 0xFDFF},
			{0xFE70, 0xFEFF},
		}, true
	case "[ebraico]":
		return [][2]int64{{0x0590, 0x05FF}, {0xFB00, 0xFB4F}}, true
	case "[thai]":
		return [][2]int64{{0x0E00, 0x0E7F}}, true
	default:
		return nil, false
	}
}

// adultFilterKeywords are the words recognised by the `[porno]`/`[adulto]`
// content filters.
var adultFilterKeywords = []string{
	"porn",
	"porno",
	"xxx",
	"milf",
	"anal",
	"creampie",
	"hentai",
	"onlyfans",
	"brazzers",
	"bangbros",
	"tushy",
	"vixen",
	"legalporno",
	"japorn",
	"fakings",
	"erotic",
	"erotico",
	"erotica",
	"hardcore",
	"blowjob",
	"cumshot",
	"deepthroat",
	"squirt",
	"stepmom",
	"stepsister",
	"sextape",
	"nsfw",
	"xhamster",
	"xvideos",
	"pornhub",
	"redtube",
	"youporn",
	"chaturbate",
	"camgirl",
}

var adultFilterRe = utils.MustCachedRegex(`\b(` + strings.Join(adultFilterKeywords, "|") + `)\b`)

// legacy blacklist semantics: exclude when a pattern matches the title as a word.
func titleIsBlacklisted(title string, patterns []string) bool {
	if title == "" || len(patterns) == 0 {
		return false
	}
	normalized := strings.ToLower(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(title))
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if re, err := utils.CachedRegex(`\b` + regexp.QuoteMeta(pattern) + `\b`); err == nil && re.MatchString(normalized) {
			return true
		}
	}
	return false
}

// legacy content-filter semantics: exclude when the title contains a filtered
// token (word) or a filtered Unicode script (e.g. `[non-latino]`).
func titleIsContentFiltered(title string, filters []string) bool {
	if title == "" || len(filters) == 0 {
		return false
	}
	normalized := strings.ToLower(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(title))
	for _, filter := range filters {
		filter = strings.ToLower(strings.TrimSpace(filter))
		if filter == "" {
			continue
		}
		if filter == "[non-latino]" {
			for _, character := range title {
				if unicode.IsLetter(character) && int64(character) > 0x024F {
					return true
				}
			}
			continue
		}
		if filter == "[porno]" || filter == "[adulto]" {
			if adultFilterRe.MatchString(normalized) {
				return true
			}
			continue
		}
		if ranges, ok := contentFilterRanges(filter); ok {
			for _, character := range title {
				code := int64(character)
				for _, bounds := range ranges {
					if code >= bounds[0] && code <= bounds[1] {
						return true
					}
				}
			}
			continue
		}
		if re, err := utils.CachedRegex(`\b` + regexp.QuoteMeta(filter) + `\b`); err == nil && re.MatchString(normalized) {
			return true
		}
	}
	return false
}

// normalizeLanguageCode canonicalises a language code.
func normalizeLanguageCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "it", "ita", "italian":
		return "ita"
	case "en", "eng", "english":
		return "eng"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// parseLanguageRequirements parses a language requirement, supporting both the
// legacy legacy JSON form (`[{"language": "ita", "required": true}]`) and a
// plain comma-separated list (`ita,eng`). Only entries with `required != false`
// are returned.
func parseLanguageRequirements(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &items); err == nil {
			result := []string{}
			for _, item := range items {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil {
					continue
				}
				required := true
				if rawRequired, ok := object["required"]; ok {
					var parsed bool
					if err := json.Unmarshal(rawRequired, &parsed); err == nil {
						required = parsed
					}
				}
				if !required {
					continue
				}
				rawLanguage, ok := object["language"]
				if !ok {
					continue
				}
				var language string
				if err := json.Unmarshal(rawLanguage, &language); err != nil {
					continue
				}
				normalized := normalizeLanguageCode(language)
				if normalized != "" {
					result = append(result, normalized)
				}
			}
			return result
		}
	}
	result := []string{}
	for _, value := range strings.FieldsFunc(trimmed, func(r rune) bool { return r == ',' || r == '+' }) {
		normalized := normalizeLanguageCode(value)
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

// SanitizeLanguageRequirements cleans a stored `language_requirements`: drops
// empty/`-` entries and returns the canonical JSON form (or an empty string).
func SanitizeLanguageRequirements(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "-" {
		return ""
	}
	type entry struct {
		Language string `json:"language"`
		Required bool   `json:"required"`
	}
	entries := []entry{}
	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &items); err == nil {
			for _, item := range items {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil {
					continue
				}
				rawLanguage, ok := object["language"]
				if !ok {
					continue
				}
				var language string
				if err := json.Unmarshal(rawLanguage, &language); err != nil {
					continue
				}
				language = strings.TrimSpace(language)
				if language == "" || language == "-" {
					continue
				}
				required := false
				if rawRequired, ok := object["required"]; ok {
					var parsed bool
					if err := json.Unmarshal(rawRequired, &parsed); err == nil {
						required = parsed
					}
				}
				normalized := normalizeLanguageCode(language)
				if normalized == "" {
					continue
				}
				entries = append(entries, entry{Language: normalized, Required: required})
			}
		}
	} else {
		for _, value := range strings.FieldsFunc(trimmed, func(r rune) bool { return r == ',' || r == '+' }) {
			value = strings.TrimSpace(value)
			if value == "" || value == "-" {
				continue
			}
			normalized := normalizeLanguageCode(value)
			if normalized == "" {
				continue
			}
			entries = append(entries, entry{Language: normalized, Required: true})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// SanitizeSubtitleRequirements cleans a stored `subtitle_requirements` into a
// plain comma list.
func SanitizeSubtitleRequirements(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "-" {
		return ""
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
			return ""
		}
		languages := []string{}
		for _, item := range items {
			var language string
			if err := json.Unmarshal(item, &language); err != nil {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil {
					continue
				}
				rawLanguage, ok := object["language"]
				if !ok {
					continue
				}
				if err := json.Unmarshal(rawLanguage, &language); err != nil {
					continue
				}
			}
			language = strings.TrimSpace(language)
			if language == "" || language == "-" {
				continue
			}
			normalized := normalizeLanguageCode(language)
			if normalized != "" {
				languages = append(languages, normalized)
			}
		}
		return strings.Join(languages, ",")
	}
	return trimmed
}

// OpenConfigDB opens the shared `gextto_config.db` (used by the daemon, i18n
// and libtorrent) with the pragmas required for concurrent access: WAL journal
// mode and a busy timeout, so a concurrent writer waits instead of failing with
// `SQLITE_BUSY` and silently losing the update.
func OpenConfigDB(path string) (*sql.DB, error) {
	return OpenSQLite(path)
}

// CleanupMovieRequirements is a one-time cleanup of legacy/imported dirty movie
// requirement rows.
func CleanupMovieRequirements(dataDir string) error {
	path := filepath.Join(dataDir, "gextto_config.db")
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil
	}
	conn, err := OpenConfigDB(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	rows, err := conn.Query("SELECT id,COALESCE(language_requirements,''),COALESCE(subtitle_requirements,'') FROM movies_config")
	if err != nil {
		return err
	}
	type requirementRow struct {
		id       int64
		language string
		subtitle string
	}
	var entries []requirementRow
	for rows.Next() {
		var row requirementRow
		if err := rows.Scan(&row.id, &row.language, &row.subtitle); err != nil {
			continue
		}
		entries = append(entries, row)
	}
	rows.Close()
	for _, row := range entries {
		cleanLanguage := SanitizeLanguageRequirements(row.language)
		cleanSubtitle := SanitizeSubtitleRequirements(row.subtitle)
		if cleanLanguage != row.language || cleanSubtitle != row.subtitle {
			if _, err := conn.Exec(
				"UPDATE movies_config SET language_requirements=?1, subtitle_requirements=?2 WHERE id=?3",
				cleanLanguage, cleanSubtitle, row.id,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

// DefaultConfig implements `impl Default for Config`.
func DefaultConfig() Config {
	dataDir, ok := os.LookupEnv("GEXTTO_DATA_DIR")
	if !ok {
		dataDir = "data"
	}
	listen, ok := os.LookupEnv("GEXTTO_LISTEN")
	if !ok {
		listen = constants.DefaultListen
	}
	engineListen, ok := os.LookupEnv("GEXTTO_ENGINE_LISTEN")
	if !ok {
		engineListen = constants.DefaultEngineListen
	}
	libtorrentTempDir := filepath.Join(dataDir, "incomplete")
	trashPath := filepath.Join(dataDir, "trash")
	return Config{
		StateDir:            filepath.Join(dataDir, constants.DefaultStateDir),
		LibtorrentDir:       filepath.Join(dataDir, "downloads"),
		LibtorrentTempDir:   &libtorrentTempDir,
		ArchiveRoot:         nil,
		TrashPath:           &trashPath,
		NotifyTelegram:      false,
		TelegramBotToken:    nil,
		TelegramChatID:      nil,
		NotifyWebhookURL:    nil,
		NotifyWebhookSecret: nil,
		NotifyWebhookFormat: "gextto",
		NotifyWebhookToken:  nil,
		NotifyWebhookUser:   nil,
		NotifyEmail:         false,
		EmailSMTP:           "smtp.gmail.com:587",
		EmailFrom:           nil,
		EmailTo:             nil,
		EmailPassword:       nil,
		CleanupUpgrades:     false,
		CleanupMinScoreDiff: 0,
		UpgradeMinScoreDiff: 200,
		CleanupAction:       "move",
		Listen:              listen,
		EngineListen:        engineListen,
		Active:              false,
		DryRun:              true,
		DataDir:             dataDir,
		RefreshSecs:         constants.DefaultRefreshSecs,
		FeedURLs:            []string{},
		Blacklist:           defaultBlacklist(),
		ContentFilters:      []string{},
		SourceFilters:       []SourceFilter{},
		MaxReleaseAgeDays:   0,
		Series:              []SeriesConfig{},
		Movies:              []MovieConfig{},
		Settings:            map[string]string{},
		Indexers:            []IndexerConfig{},
		WebsearchEngines:    []string{},
		FlaresolverrURL:     nil,
		TmdbAPIKey:          nil,
		RenameEpisodes:      false,
		LibtorrentEnabled:   true,
		Libtorrent:          DefaultLibtorrentSettings(),
		RenameFormat:        defaultRenameFormatValue,
		RenameTemplate:      defaultRenameTemplateValue,
	}
}

// UnmarshalJSON reproduces the JSON JSON default attributes of
// `Config`: `blacklist`, `rename_format` and `rename_template`
// fall back to their defaults when the key is absent.
func (c *Config) UnmarshalJSON(data []byte) error {
	type configAlias Config
	// Start from the current value (the caller pre-fills DefaultConfig) so a
	// partial JSON file only overrides the keys it actually contains instead of
	// zeroing data_dir/listen/refresh_secs and the other defaults.
	decoded := configAlias(*c)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Config(decoded)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["blacklist"]; !ok {
		c.Blacklist = defaultBlacklist()
	}
	if _, ok := raw["rename_format"]; !ok {
		c.RenameFormat = defaultRenameFormatValue
	}
	if _, ok := raw["rename_template"]; !ok {
		c.RenameTemplate = defaultRenameTemplateValue
	}
	if _, ok := raw["libtorrent"]; !ok {
		c.Libtorrent = DefaultLibtorrentSettings()
	}
	return nil
}

// ScoreQuality applies the configured quality weights without requiring a full
// `Config`. Archive indexing uses this same primitive while it is handed a
// settings map rather than the complete runtime configuration.
func ScoreQuality(quality *models.Quality, settings map[string]string) int64 {
	return quality.ScoreWithSettings(settings)
}

// ScoreWeightsFingerprint is a stable hash of every score-* setting. The daemon
// compares it across restarts to detect a scoring change: stored quality scores
// are then recomputed so a weight edit cannot masquerade as an upgrade and
// trigger pointless re-downloads.
func (c *Config) ScoreWeightsFingerprint() string {
	keys := make([]string, 0, len(c.Settings))
	for key := range c.Settings {
		if strings.HasPrefix(strings.ToLower(key), "score_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		hash.Write([]byte(key))
		hash.Write([]byte("="))
		hash.Write([]byte(c.Settings[key]))
		hash.Write([]byte("\n"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// TmdbLanguage returns the BCP-47 language used for TMDB API calls (e.g.
// `it-IT`).
func (c *Config) TmdbLanguage() string {
	if value, ok := c.Settings["tmdb_language"]; ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "it-IT"
}

// DelayMinutes is the delay profile: minutes a series (`series`) or movie
// (`movie`) release is held before grabbing. `0` disables the delay.
func (c *Config) DelayMinutes(kind string) int64 {
	key := "delay_torrent_minutes"
	if kind == "movie" {
		key = "delay_movies_minutes"
	}
	minutes := int64(0)
	if value, ok := c.Settings[key]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			minutes = parsed
		}
	}
	if minutes < 0 {
		return 0
	}
	return minutes
}

// DelayBypassScore is the score at or above which a release is grabbed
// immediately, bypassing the delay profile. `0` disables the bypass.
func (c *Config) DelayBypassScore() int64 {
	if value, ok := c.Settings["delay_bypass_score"]; ok {
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

// DefaultLanguage is the default language for acquisitions and rename fallback
// (e.g. `ita`).
func (c *Config) DefaultLanguage() string {
	if value, ok := c.Settings["default_language"]; ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "ita"
}

// FeedMaxPages is the number of listing pages to fetch per feed (legacy
// `MAX_PAGES`, default 3). This used to be read from
// `stop_on_old_page_threshold`, which in legacy is a 0..1 ratio — a value like
// `0.8` could not parse as a page count and silently fell back to 3.
func (c *Config) FeedMaxPages() int {
	// The UI field "Feed pages to read" persists to
	// `stop_on_old_page_threshold` for backward compatibility, but that key
	// also carries the legacy 0..1 "old page ratio". Accept an integer page
	// count there and prefer the explicit `feed_max_pages` key when present.
	pages := numberSettingUint64(c.Settings, "feed_max_pages", 0)
	if pages == 0 {
		if raw, ok := c.Settings["stop_on_old_page_threshold"]; ok {
			if parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil && parsed >= 1 {
				pages = parsed
			}
		}
	}
	if pages == 0 {
		pages = 3
	}
	if pages > 10 {
		return 10
	}
	return int(pages)
}

// StopOnOldPageRatio is the legacy `stop_on_old_page_threshold`: ratio (0..=1)
// of old releases on a page above which the listing walk stops early. Only
// meaningful together with `max_release_age_days`.
func (c *Config) StopOnOldPageRatio() float64 {
	if value, ok := c.Settings["stop_on_old_page_threshold"]; ok {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0.0 && parsed <= 1.0 {
			return parsed
		}
	}
	return 0.8
}

// ShouldSearchTitlesInCycle reports whether the daemon's scheduled cycle
// should run the online title search fan-out across Torznab indexers.
//
// By default ("auto" or empty), when feeds are configured they provide the
// continuous stream of releases that populate the local SQLite archive; the
// cycle then queries the archive in milliseconds, skipping the multi-minute
// online sweep across dozens of series and movies. When no feeds are
// configured, title search runs so installations without RSS/HTML feeds can
// still discover releases on their Torznab indexers.
//
// The operator can override this with the `cycle_title_search` setting:
// - "never" / "no" / "false": never search titles online during cycles.
// - "always" / "yes" / "true": always search titles online during cycles.
// - "auto" (default): search titles online only if FeedURLs is empty.
func (c *Config) ShouldSearchTitlesInCycle() bool {
	mode := strings.ToLower(strings.TrimSpace(c.Settings["cycle_title_search"]))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(c.Settings["title_search_in_cycle"]))
	}
	switch mode {
	case "always", "yes", "true", "1":
		return true
	case "never", "no", "false", "0":
		return false
	default:
		return len(c.FeedURLs) == 0
	}
}

// DebugEnabled reports whether to log debug-level diagnostics (legacy
// `debug_*`).
func (c *Config) DebugEnabled() bool {
	return configBoolSetting(mapValue(c.Settings, "debug_enabled")) ||
		configBoolSetting(mapValue(c.Settings, "debug_mode"))
}

// TvdbAPIKey returns TheTVDB API key, when configured.
func (c *Config) TvdbAPIKey() *string {
	if value, ok := c.Settings["tvdb_api_key"]; ok {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return &trimmed
		}
	}
	return nil
}

// TvdbLanguage is the preferred TVDB language (falls back to the default
// acquisition language).
func (c *Config) TvdbLanguage() string {
	if value, ok := c.Settings["tvdb_language"]; ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return c.DefaultLanguage()
}

// RamdiskDir returns the configured RAM disk directory, when non-empty.
func (c *Config) RamdiskDir() *string {
	return configPathSetting(mapValue(c.Settings, "libtorrent_ramdisk_dir"))
}

// RamdiskEnabled is true when the RAM disk download tier is active. A
// configured directory enables it unless `libtorrent_ramdisk_enabled` is
// explicitly falsy.
func (c *Config) RamdiskEnabled() bool {
	explicitlyOff := false
	if value, ok := c.Settings["libtorrent_ramdisk_enabled"]; ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "no", "false", "0":
			explicitlyOff = true
		}
	}
	return !explicitlyOff && c.RamdiskDir() != nil
}

// LibtorrentPreallocate reports whether new downloads reserve their full size
// on disk up front (`storage_mode_allocate`) instead of growing sparsely.
// Default true: it avoids fragmentation on NAS/HDD and surfaces "no space"
// immediately. It can be overridden per add.
func (c *Config) LibtorrentPreallocate() bool {
	value, ok := c.Settings["libtorrent_preallocate"]
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "no", "false", "0", "off":
		return false
	}
	return true
}

// LibtorrentTorrentCopyDir is the optional folder where the `.torrent` file of
// every started torrent is copied (empty disables the copy).
func (c *Config) LibtorrentTorrentCopyDir() *string {
	return configPathSetting(mapValue(c.Settings, "libtorrent_torrent_copy_dir"))
}

// RamdiskThresholdBytes is the maximum size (bytes) for a single torrent
// admitted to the RAM disk.
func (c *Config) RamdiskThresholdBytes() uint64 {
	return gibSetting(c.Settings, "libtorrent_ramdisk_threshold_gb", 3.5)
}

// RamdiskMarginBytes is the free space (bytes) to keep on the RAM disk after a
// download finishes.
func (c *Config) RamdiskMarginBytes() uint64 {
	return gibSetting(c.Settings, "libtorrent_ramdisk_margin_gb", 0.5)
}

// RamdiskMinFreeBytes is the explicit free-space floor in bytes; `0` derives it
// from the margin.
func (c *Config) RamdiskMinFreeBytes() uint64 {
	if value, ok := c.Settings["libtorrent_ramdisk_min_free_bytes"]; ok {
		if parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

// configBoolSetting implements `bool_setting`.
func configBoolSetting(value *string) bool {
	if value == nil {
		return false
	}
	return settingTruthy(*value)
}

// settingTruthy is the single reading of a yes/no setting: "yes", "true", "1"
// or "on", ignoring case and surrounding spaces. Before, each setting had its
// own variant (some case-sensitive, some accepting "on"), so the same stored
// value could be on for one feature and off for another.
func settingTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}

// boolSettingOr implements `bool_setting_or`.
func boolSettingOr(settings map[string]string, key string, defaultValue bool) bool {
	if value, ok := settings[key]; ok {
		return configBoolSetting(&value)
	}
	return defaultValue
}

// configPathSetting implements `path_setting`.
func configPathSetting(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	result := *value
	return &result
}

// mapValue returns a pointer to the value stored under key, or nil.
func mapValue(settings map[string]string, key string) *string {
	if value, ok := settings[key]; ok {
		return &value
	}
	return nil
}

// numberSettingInt parses an integer setting, falling back to default.
func numberSettingInt(settings map[string]string, key string, defaultValue int64) int64 {
	if value, ok := settings[key]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// numberSettingUint64 parses an unsigned setting, falling back to default.
func numberSettingUint64(settings map[string]string, key string, defaultValue uint64) uint64 {
	if value, ok := settings[key]; ok {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// numberSettingUint16 parses a port, falling back to default.
func numberSettingUint16(settings map[string]string, key string, defaultValue uint16) uint16 {
	if value, ok := settings[key]; ok {
		if parsed, err := strconv.ParseUint(value, 10, 16); err == nil {
			return uint16(parsed)
		}
	}
	return defaultValue
}

// numberSettingFloat parses a float setting, falling back to default.
func numberSettingFloat(settings map[string]string, key string, defaultValue float64) float64 {
	if value, ok := settings[key]; ok {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// gibSetting reads a GiB-sized setting (e.g. `3.5`) and returns it as bytes.
func gibSetting(settings map[string]string, key string, defaultValue float64) uint64 {
	gib := defaultValue
	if value, ok := settings[key]; ok {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			gib = parsed
		}
	}
	if gib <= 0.0 {
		return 0
	}
	const gibBytes = 1024.0 * 1024.0 * 1024.0
	// Saturate instead of overflowing the float->uint64 conversion for absurd
	// inputs (the conversion is otherwise implementation-defined).
	if gib >= float64(^uint64(0))/gibBytes {
		return ^uint64(0)
	}
	return uint64(gib * gibBytes)
}

// FindSeriesMatch finds the monitored series a release belongs to, honouring
// enabled state, ignored seasons and the configured season ranges.
func (c *Config) FindSeriesMatch(name string, season *int64) *SeriesConfig {
	normalized := NormalizeSeriesName(name)
	eligible := func(series *SeriesConfig) bool {
		if !series.Enabled {
			return false
		}
		if season != nil {
			if containsInt64(series.IgnoredSeasons, *season) || !seasonAllowed(series.Seasons, *season) {
				return false
			}
		}
		return true
	}
	candidates := func(series *SeriesConfig) []string {
		values := make([]string, 0, len(series.Aliases)+1)
		values = append(values, series.Name)
		values = append(values, series.Aliases...)
		return values
	}
	// Prefer an exact normalized name over a tolerant suffix match. This keeps
	// similarly named monitored series distinct (for example `Scrubs` and
	// `Scrubs 2026`) while still accepting release years and technical suffixes
	// when no exact configuration exists.
	for index := range c.Series {
		series := &c.Series[index]
		if !eligible(series) {
			continue
		}
		for _, candidate := range candidates(series) {
			if NormalizeSeriesName(candidate) == normalized {
				return series
			}
		}
	}
	for index := range c.Series {
		series := &c.Series[index]
		if !eligible(series) {
			continue
		}
		for _, candidate := range candidates(series) {
			if SeriesNamesMatch(candidate, normalized) {
				return series
			}
		}
	}
	return nil
}

// FindSeriesByName finds a configured series by name/alias ignoring the
// monitored season and enabled filters. Used to rename/operate on
// already-archived files, which must not be skipped just because their season
// is not monitored.
func (c *Config) FindSeriesByName(name string) *SeriesConfig {
	normalized := NormalizeSeriesName(name)
	for index := range c.Series {
		series := &c.Series[index]
		if SeriesNamesMatch(series.Name, normalized) {
			return series
		}
		for _, alias := range series.Aliases {
			if SeriesNamesMatch(alias, normalized) {
				return series
			}
		}
	}
	return nil
}

// ResolveArchivePath is the effective archive folder of a series: `archive_path`
// when configured, otherwise a subfolder of `archive_root` whose name matches
// the series (or an alias). Port of the legacy auto-detect: it serves both the
// download destination and the on-disk file scan.
func (c *Config) ResolveArchivePath(series *SeriesConfig) *string {
	configured := strings.TrimSpace(series.ArchivePath)
	if configured != "" {
		return &configured
	}
	if c.ArchiveRoot == nil {
		return nil
	}
	root := *c.ArchiveRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var candidates []string
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		name := entry.Name()
		matches := SeriesNamesMatch(series.Name, name)
		if !matches {
			for _, alias := range series.Aliases {
				if SeriesNamesMatch(alias, name) {
					matches = true
					break
				}
			}
		}
		if matches {
			candidates = append(candidates, path)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return nil
	}
	return &candidates[0]
}

// ResolveTrashPath returns the configured trash directory or a safe default
// under DataDir ("trash") when no path was explicitly configured.
func (c *Config) ResolveTrashPath() string {
	if c.TrashPath != nil && strings.TrimSpace(*c.TrashPath) != "" {
		return *c.TrashPath
	}
	return filepath.Join(c.DataDir, "trash")
}

// seasonAllowed parses a season specification like `1,3-4,7+` or `*`.
func seasonAllowed(specification string, season int64) bool {
	specification = strings.TrimSpace(specification)
	if specification == "" || specification == "*" {
		return true
	}
	for _, part := range strings.Split(specification, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasSuffix(part, "+") {
			if minimum, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(part, "+")), 10, 64); err == nil {
				if season >= minimum {
					return true
				}
				continue
			}
		}
		if startText, endText, found := strings.Cut(part, "-"); found {
			start, startErr := strconv.ParseInt(strings.TrimSpace(startText), 10, 64)
			end, endErr := strconv.ParseInt(strings.TrimSpace(endText), 10, 64)
			if startErr == nil && endErr == nil {
				if season >= start && season <= end {
					return true
				}
				continue
			}
		}
		if value, err := strconv.ParseInt(part, 10, 64); err == nil && value == season {
			return true
		}
	}
	return false
}

// SeasonAllowedForScan is the static season-range check for archive scans.
func SeasonAllowedForScan(specification string, season int64) bool {
	return seasonAllowed(specification, season)
}

// containsInt64 reports whether values contains needle.
func containsInt64(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// titleTokens returns the lowercase alphanumeric tokens of a title:
// "Spider-Man" -> ["spider","man"], "Minions & Monsters" ->
// ["minions","monsters"]. Punctuation must not prevent the match.
func titleTokens(value string) []string {
	lowered := strings.ToLower(value)
	tokens := []string{}
	for _, token := range strings.FieldsFunc(lowered, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		tokens = append(tokens, token)
	}
	return tokens
}

// movieTitleMatches is true when the title contains every word of the monitored
// name (or original title), ignoring punctuation. Handles single-word names,
// hyphens, "&", "vs." and double spaces.
func movieTitleMatches(movie *MovieConfig, title string) bool {
	tokens := titleTokens(title)
	if len(tokens) == 0 {
		return false
	}
	matchesName := func(value string) bool {
		words := titleTokens(value)
		if len(words) == 0 {
			return false
		}
		for _, word := range words {
			found := false
			for _, token := range tokens {
				if token == word {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if matchesName(movie.Name) {
		return true
	}
	return strings.TrimSpace(movie.OriginalTitle) != "" && matchesName(movie.OriginalTitle)
}

// movieExcluded reports whether any exclusion word appears in the title.
func movieExcluded(movie *MovieConfig, title string) bool {
	if movie.Exclude == "" {
		return false
	}
	lowered := strings.ToLower(title)
	for _, word := range strings.Split(movie.Exclude, ",") {
		word = strings.TrimSpace(word)
		if word != "" && strings.Contains(lowered, strings.ToLower(word)) {
			return true
		}
	}
	return false
}

// FindMovieMatch finds the monitored movie matching title and year.
func (c *Config) FindMovieMatch(title string, year *int64) *MovieConfig {
	// legacy rejects obvious non-movies before matching (episodes, sport,
	// wrestling, magazines, videogames, console ROMs, some music).
	if !PassesMovieFilter(title) {
		return nil
	}
	for index := range c.Movies {
		movie := &c.Movies[index]
		if !movie.Enabled || !movieTitleMatches(movie, title) || movieExcluded(movie, title) {
			continue
		}
		configuredYear, err := strconv.ParseInt(movie.Year, 10, 64)
		if err != nil {
			continue
		}
		if year != nil && absInt64(*year-configuredYear) <= 1 {
			return movie
		}
	}
	return nil
}

// FindMovieMatchManual is like `FindMovieMatch`, but for manual queueing
// (archive, search, feed results): the title must match, while the year is
// optional. If both years are present they must stay within ±1; if the movie or
// release year is missing the match is accepted, so a monitored movie is not
// rejected with "film non monitorato".
func (c *Config) FindMovieMatchManual(title string, year *int64) *MovieConfig {
	if !PassesMovieFilter(title) {
		return nil
	}
	for index := range c.Movies {
		movie := &c.Movies[index]
		if !movie.Enabled || !movieTitleMatches(movie, title) || movieExcluded(movie, title) {
			continue
		}
		configuredYear, err := strconv.ParseInt(movie.Year, 10, 64)
		if err != nil {
			return movie
		}
		if year == nil {
			return movie
		}
		if absInt64(*year-configuredYear) <= 1 {
			return movie
		}
	}
	return nil
}

// FindMovieMatchForRelease resolves a monitored movie from a collected
// release. Prowlarr supplies TmdbID in its native JSON response; use that
// authoritative identifier before the title/year fallback, because release
// titles frequently omit the theatrical year.
func (c *Config) FindMovieMatchForRelease(release *models.Release, manual bool) *MovieConfig {
	if release == nil {
		return nil
	}
	if tmdbID := strings.TrimSpace(release.TmdbID); tmdbID != "" {
		for index := range c.Movies {
			movie := &c.Movies[index]
			if movie.Enabled && strings.TrimSpace(movie.TmdbID) == tmdbID {
				return movie
			}
		}
	}
	if manual {
		return c.FindMovieMatchManual(release.Title, release.Year)
	}
	return c.FindMovieMatch(release.Title, release.Year)
}

// absInt64 returns the absolute value of value.
func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// resolutionPixels parses a numeric resolution label (e.g. "1080p").
func resolutionPixels(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(strings.TrimRight(value, "p"), 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// resolutionAllowed implements the legacy resolution-range semantics.
func resolutionAllowed(quality *models.Quality, requirement string) bool {
	requirement = strings.ToLower(requirement)
	resolution := func(value string) *int64 {
		for _, candidate := range []string{"2160p", "1080p", "720p", "576p", "480p", "360p"} {
			if strings.Contains(value, candidate) {
				if parsed, ok := resolutionPixels(candidate); ok {
					return &parsed
				}
				return nil
			}
		}
		return nil
	}
	actual, actualOK := resolutionPixels(quality.Resolution)
	var actualPtr *int64
	if actualOK {
		actualPtr = &actual
	}
	var minimum, maximum *int64
	if strings.HasPrefix(strings.TrimLeft(requirement, " \t\r\n\v\f"), "<") {
		maximum = resolution(requirement)
	} else if startText, endText, found := strings.Cut(requirement, "-"); found {
		minimum = resolution(startText)
		maximum = resolution(endText)
	} else {
		minimum = resolution(requirement)
	}
	if minimum != nil && (actualPtr == nil || *actualPtr < *minimum) {
		return false
	}
	if maximum != nil && (actualPtr == nil || *actualPtr > *maximum) {
		return false
	}
	return true
}

// languageSubtitleAllowed checks the requested language list and the optional
// subtitle flag.
func languageSubtitleAllowed(quality *models.Quality, language string, subtitle string) bool {
	requested := []string{}
	for _, value := range strings.Split(language, ",") {
		normalized := normalizeLanguageCode(value)
		// These are UI/configuration sentinels, not literal audio language
		// codes.  Treating `any` as a required language made the v2
		// "Qualsiasi" option reject every release.
		if normalized == "any" || normalized == "*" || normalized == "none" || normalized == "custom" {
			continue
		}
		if normalized != "" {
			requested = append(requested, normalized)
		}
	}
	if len(requested) > 0 {
		detected := []string{}
		if len(quality.Languages) == 0 {
			detected = append(detected, normalizeLanguageCode(quality.Language))
		} else {
			for _, value := range quality.Languages {
				detected = append(detected, normalizeLanguageCode(value))
			}
		}
		if len(requested) == 1 && requested[0] == "multi" {
			return len(detected) > 1
		}
		found := false
		for _, value := range detected {
			for _, wanted := range requested {
				if value == wanted {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	switch strings.ToLower(subtitle) {
	case "yes", "true", "1":
		if !quality.HasSubtitle {
			return false
		}
	}
	return true
}

// QualityAllowed implements the legacy resolution/language/subtitle checks,
// kept static for compatibility with tests and callers that have no `Config`.
func QualityAllowed(quality *models.Quality, requirement string, language string, subtitle string) bool {
	return resolutionAllowed(quality, requirement) &&
		languageSubtitleAllowed(quality, language, subtitle)
}

// MovieReleaseAllowed checks a movie release against the title requirements.
func (c *Config) MovieReleaseAllowed(movie *MovieConfig, quality *models.Quality) bool {
	return c.movieQualityAllowed(movie, quality)
}

// MovieReleaseAllowedForTitle applies the movie's technical requirements and
// its title-specific exclusion list to the raw release title.
func (c *Config) MovieReleaseAllowedForTitle(movie *MovieConfig, quality *models.Quality, title string) bool {
	return c.movieQualityAllowed(movie, quality) && !titleExcluded(movie.Exclude, title)
}

func (c *Config) movieQualityAllowed(movie *MovieConfig, quality *models.Quality) bool {
	requiredLanguages := parseLanguageRequirements(movie.LanguageRequirements)
	language := ""
	if len(requiredLanguages) == 0 {
		language = strings.Join(parseLanguageRequirements(movie.Language), ",")
	} else {
		language = strings.Join(requiredLanguages, ",")
	}
	if !QualityAllowed(quality, movie.Quality, language, "") {
		return false
	}
	// "Requisiti sottotitoli" è obbligatorio: una release senza quei
	// sottotitoli viene scartata. Il campo "Sottotitoli" è invece facoltativo
	// (solo una preferenza) e non blocca nulla qui.
	requirements := strings.TrimSpace(movie.SubtitleRequirements)
	if requirements == "" {
		return true
	}
	required := parseLanguageRequirements(requirements)
	if len(required) == 0 {
		flag := isSubtitleFlag(requirements)
		return !flag || quality.HasSubtitle
	}
	for _, value := range required {
		if (isSubtitleFlag(value) || value == "any" || value == "multi") && quality.HasSubtitle {
			return true
		}
		for _, item := range quality.SubtitleLanguages {
			if normalizeLanguageCode(item) == value {
				return true
			}
		}
	}
	return false
}

// titleExcluded reports whether a release title contains one of the
// per-title exclusion tokens. An empty title is intentionally never rejected;
// callers can use this helper before canonicalising a matched movie/series.
func titleExcluded(exclude, title string) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	lowered := strings.ToLower(title)
	for _, word := range strings.Split(exclude, ",") {
		word = strings.TrimSpace(word)
		if word != "" && strings.Contains(lowered, strings.ToLower(word)) {
			return true
		}
	}
	return false
}

// isSubtitleFlag reports whether a requirement value is a generic subtitle
// flag rather than a language code.
func isSubtitleFlag(value string) bool {
	switch strings.ToLower(value) {
	case "yes", "true", "1", "sub", "subs":
		return true
	default:
		return false
	}
}

// MovieSubtitleBonus is the optional bonus when the release contains the movie's
// preferred subtitles ("Sottotitoli" field). It never blocks the download: it
// only helps prefer, at equal quality, releases that contain them.
func MovieSubtitleBonus(movie *MovieConfig, quality *models.Quality) int64 {
	return subtitlePreferenceBonus(movie.Subtitle, quality)
}

// SeriesSubtitleBonus is the optional score bonus for a monitored series'
// preferred subtitle languages. The series subtitle field is a preference, not
// a hard requirement; generic values such as yes/true/1 still mean that the
// release must carry subtitles, as in the legacy rule.
func SeriesSubtitleBonus(series *SeriesConfig, quality *models.Quality) int64 {
	return subtitlePreferenceBonus(series.Subtitle, quality)
}

func subtitlePreferenceBonus(raw string, quality *models.Quality) int64 {
	preferred := strings.TrimSpace(raw)
	if preferred == "" {
		return 0
	}
	wanted := parseLanguageRequirements(preferred)
	hasPreferred := false
	if len(wanted) == 0 {
		hasPreferred = isSubtitleFlag(preferred) && quality.HasSubtitle
	} else {
		for _, value := range wanted {
			if (isSubtitleFlag(value) || value == "any" || value == "multi") && quality.HasSubtitle {
				hasPreferred = true
				break
			}
			for _, item := range quality.SubtitleLanguages {
				if normalizeLanguageCode(item) == value {
					hasPreferred = true
					break
				}
			}
			if hasPreferred {
				break
			}
		}
	}
	if hasPreferred {
		return 50
	}
	return 0
}

// SeriesReleaseAllowed checks a series release against the title requirements.
func (c *Config) SeriesReleaseAllowed(series *SeriesConfig, quality *models.Quality, title string) bool {
	if !QualityAllowed(quality, series.Quality, series.Language, series.Subtitle) {
		return false
	}
	return !titleExcluded(series.Exclude, title)
}

// ReleaseAllowed reports whether a release passes every global filter.
func (c *Config) ReleaseAllowed(release *models.Release) bool {
	return c.AllReleaseDeniedReason(release) == ""
}

// ReleaseIsMonitored reports whether a release belongs to a monitored title.
// Global filters run before acquisition matching, so unrelated indexer noise
// must not produce rejection logs (not even at DEBUG).
func (c *Config) ReleaseIsMonitored(release *models.Release) bool {
	switch release.Kind {
	case "series":
		if release.Series == nil {
			return false
		}
		return c.FindSeriesMatch(*release.Series, release.Season) != nil
	case "movie":
		return c.FindMovieMatchForRelease(release, false) != nil
	default:
		return false
	}
}

// AllReleaseDeniedReason combines every global rejection reason (static
// filters, per-source filters and the built-in sanity rules). Used for logging
// and the manual search `allowed` flag so all layers speak with one voice.
func (c *Config) AllReleaseDeniedReason(release *models.Release) string {
	if reason := c.ReleaseDeniedReason(release); reason != "" {
		return reason
	}
	if reason := c.SourceFilterDeniedReason(release); reason != "" {
		return reason
	}
	return rules.DeniedReason(release)
}

// QualityScore is the score used for every release decision's quality
// component.
func (c *Config) QualityScore(quality *models.Quality) int64 {
	return ScoreQuality(quality, c.Settings)
}

// ReleaseScore is the score used for acquisition, upgrade decisions and archive
// comparisons.
func (c *Config) ReleaseScore(release *models.Release) int64 {
	var movie *MovieConfig
	if release.Kind == "movie" {
		movie = c.FindMovieMatchForRelease(release, false)
	}
	score := c.releaseScoreWithMovie(release, movie)
	if movie == nil && release.Series != nil {
		if series := c.FindSeriesMatch(*release.Series, release.Season); series != nil {
			score += SeriesSubtitleBonus(series, &release.Quality)
		}
	}
	return score
}

// ReleaseScoreForMovie is the variant used by a movie-specific search, where
// the monitored movie is already known even if the raw release title is not a
// perfect match.
func (c *Config) ReleaseScoreForMovie(release *models.Release, movie *MovieConfig) int64 {
	return c.releaseScoreWithMovie(release, movie)
}

// releaseScoreWithMovie computes the combined release score.
func (c *Config) releaseScoreWithMovie(release *models.Release, movie *MovieConfig) int64 {
	score := c.QualityScore(&release.Quality)
	if movie != nil {
		score += MovieSubtitleBonus(movie, &release.Quality)
	}
	return score + rules.SizeScoreBonus(release)
}

// FileScore scores an already archived file with the same release scorer used at
// acquisition time. `title` is the monitored identity for movies (and is only
// informational for series); an empty title falls back to the file name. This
// keeps archive scans and post-processing from silently dropping the
// size/subtitle parts of the score.
func (c *Config) FileScore(path string, kind string, title string) int64 {
	fileName := filepath.Base(path)
	if path == "" {
		fileName = ""
	}
	if strings.TrimSpace(title) == "" {
		title = fileName
	}
	sizeBytes := int64(0)
	if info, err := os.Stat(path); err == nil {
		size := info.Size()
		if size > int64(^uint64(0)>>1) {
			size = int64(^uint64(0) >> 1)
		}
		sizeBytes = size
	}
	release := models.Release{
		Title:        title,
		Magnet:       "",
		Source:       "archive",
		Quality:      ParseQuality(fileName),
		Kind:         kind,
		EpisodeRange: []int64{},
		SizeBytes:    sizeBytes,
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	var movie *MovieConfig
	if kind == "movie" {
		movie = c.FindMovieMatchManual(title, nil)
	}
	return c.releaseScoreWithMovie(&release, movie)
}

// SourceFilterDeniedReason is the reason a release is refused by a per-source
// filter, if any. Unlike the global filters this needs the dynamic
// source/keyword names, so it returns an owned string.
func (c *Config) SourceFilterDeniedReason(release *models.Release) string {
	if len(c.SourceFilters) == 0 {
		return ""
	}
	source := strings.ToLower(release.Source)
	title := strings.ToLower(release.Title)
	for _, filter := range c.SourceFilters {
		if !filter.Enabled {
			continue
		}
		needle := strings.ToLower(strings.TrimSpace(filter.Source))
		if needle == "" || !strings.Contains(source, needle) {
			continue
		}
		for _, keyword := range filter.Keywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword != "" && strings.Contains(title, keyword) {
				return fmt.Sprintf(
					"blocked by source filter '%s' (keyword '%s')",
					strings.TrimSpace(filter.Source), keyword,
				)
			}
		}
	}
	return ""
}

// ReleaseDeniedReason is the human-readable reason a release is refused by the
// global filters, or `""` when it is allowed. Used to make every filter
// decision visible in the log instead of silently dropping releases.
func (c *Config) ReleaseDeniedReason(release *models.Release) string {
	if titleIsBlacklisted(release.Title, c.Blacklist) {
		return "title matches the blacklist"
	}
	if titleIsContentFiltered(release.Title, c.ContentFilters) {
		return "title matches a content filter"
	}
	if c.MaxReleaseAgeDays > 0 {
		maxAge := time.Duration(c.MaxReleaseAgeDays) * 24 * time.Hour
		if time.Now().UTC().Sub(release.DiscoveredAt) > maxAge {
			return "older than max_release_age_days"
		}
	}
	return ""
}

// loadConfigDB overlays the settings stored in `gextto_config.db`, the legacy
// series database and the movies table on top of this configuration.
func (c *Config) loadConfigDB() error {
	path := filepath.Join(c.DataDir, "gextto_config.db")
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil
	}
	if c.Settings == nil {
		c.Settings = map[string]string{}
	}
	conn, err := OpenConfigDB(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	rows, err := conn.Query("SELECT key,value FROM settings")
	if err != nil {
		// Missing settings table: nothing to overlay.
		return nil
	}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		c.Settings[key] = value
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, ok := c.Settings["api_token"]; ok {
		delete(c.Settings, "api_token")
		if _, err := conn.Exec("DELETE FROM settings WHERE key = ?1", "api_token"); err != nil {
			return err
		}
	}
	if value, ok := c.Settings["url"]; ok {
		var feeds []string
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			if err := json.Unmarshal([]byte(value), &feeds); err == nil {
				c.FeedURLs = feeds
			} else {
				c.FeedURLs = []string{value}
			}
		} else {
			c.FeedURLs = []string{value}
		}
	}
	if value, ok := c.Settings["blacklist"]; ok {
		c.Blacklist = parseConfigList(value)
	} else {
		c.Blacklist = defaultBlacklist()
	}
	if value, ok := c.Settings["content_filters"]; ok {
		c.ContentFilters = parseConfigList(value)
	} else if value, ok := c.Settings["content_filter"]; ok {
		c.ContentFilters = parseConfigList(value)
	} else {
		c.ContentFilters = []string{}
	}
	if value, ok := c.Settings["source_filters"]; ok {
		c.SourceFilters = ParseSourceFilters(value)
	} else {
		c.SourceFilters = []SourceFilter{}
	}
	c.MaxReleaseAgeDays = 0
	if value, ok := c.Settings["max_release_age_days"]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.MaxReleaseAgeDays = parsed
		}
	} else if value, ok := c.Settings["max_age_days"]; ok {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			c.MaxReleaseAgeDays = parsed
		}
	}
	if value, ok := c.Settings["_migrated_series"]; ok {
		var series []SeriesConfig
		if err := json.Unmarshal([]byte(value), &series); err == nil {
			c.Series = series
		} else {
			c.Series = []SeriesConfig{}
		}
	}
	if len(c.Series) == 0 {
		seriesPath := filepath.Join(c.DataDir, "gextto_series.db")
		if info, err := os.Stat(seriesPath); err == nil && !info.IsDir() {
			seriesConn, err := OpenSQLite(seriesPath)
			if err != nil {
				return err
			}
			loaded, err := loadLegacySeries(seriesConn)
			seriesConn.Close()
			if err == nil {
				c.Series = loaded
			}
		}
	}
	columns, err := tableColumns(conn, "movies_config")
	if err != nil {
		logging.Warn("loadConfigDB: cannot read movies_config schema", "error", err)
		columns = map[string]bool{}
	}
	hasMovieMetadata := columns["tmdb_id"] && columns["tvdb_id"] && columns["original_title"] && columns["overview"] && columns["poster_path"]
	hasDisableUpgrades := columns["disable_upgrades"]
	hasRequirements := columns["language_requirements"] && columns["subtitle_requirements"]
	hasExclude := columns["exclude"]
	movieQuery := ""
	switch {
	case hasMovieMetadata && hasDisableUpgrades:
		movieQuery = "SELECT id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,tmdb_id,tvdb_id,original_title,overview,poster_path,disable_upgrades FROM movies_config"
	case hasMovieMetadata:
		// Databases created before the per-title upgrade switch do not have
		// `disable_upgrades`; keep loading their movies and use the default
		// false value instead of silently returning an empty list.
		movieQuery = "SELECT id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,tmdb_id,tvdb_id,original_title,overview,poster_path,0 FROM movies_config"
	case hasRequirements:
		movieQuery = "SELECT id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,'','','','','',0 FROM movies_config"
	case hasExclude:
		movieQuery = "SELECT id,name,year,quality,language,enabled,subtitle,exclude,'','','','','','','',0 FROM movies_config"
	default:
		movieQuery = "SELECT id,name,year,quality,language,enabled,subtitle,'','','','','','','','',0 FROM movies_config"
	}
	// Keep the library presentation stable when a newly added movie is saved:
	// SQLite otherwise returns rows in insertion/rowid order.
	movieQuery += " ORDER BY name COLLATE NOCASE, year COLLATE NOCASE, id"
	rows, err = conn.Query(movieQuery)
	if err != nil {
		// A missing movies_config table is benign (no movies configured yet);
		// any other error must not silently drop the monitored movie list.
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			logging.Warn("loadConfigDB: cannot load movies", "error", err)
		}
	} else {
		movies := []MovieConfig{}
		for rows.Next() {
			var (
				id                                          int64
				name, year, quality, language               string
				enabled                                     int64
				subtitle, exclude, languageReq, subtitleReq string
				tmdbID, tvdbID, originalTitle, overview     string
				posterPath                                  string
				disable                                     int64
			)
			if err := rows.Scan(
				&id, &name, &year, &quality, &language, &enabled, &subtitle, &exclude,
				&languageReq, &subtitleReq, &tmdbID, &tvdbID, &originalTitle, &overview,
				&posterPath, &disable,
			); err != nil {
				continue
			}
			movies = append(movies, MovieConfig{
				ID:                   id,
				Name:                 name,
				Year:                 year,
				Quality:              quality,
				Language:             language,
				Enabled:              enabled != 0,
				Subtitle:             subtitle,
				Exclude:              exclude,
				LanguageRequirements: SanitizeLanguageRequirements(languageReq),
				SubtitleRequirements: SanitizeSubtitleRequirements(subtitleReq),
				TmdbID:               tmdbID,
				TvdbID:               tvdbID,
				OriginalTitle:        originalTitle,
				Overview:             overview,
				PosterPath:           posterPath,
				DisableUpgrades:      disable != 0,
			})
		}
		rows.Close()
		c.Movies = movies
	}
	if value, ok := c.Settings["indexers"]; ok {
		var indexers []IndexerConfig
		if err := json.Unmarshal([]byte(value), &indexers); err == nil {
			c.Indexers = indexers
		} else {
			c.Indexers = []IndexerConfig{}
		}
	}
	if value, ok := c.Settings["websearch_engines"]; ok {
		var engines []string
		if err := json.Unmarshal([]byte(value), &engines); err == nil {
			c.WebsearchEngines = engines
		} else {
			result := []string{}
			for _, part := range strings.Split(value, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					result = append(result, strings.ToLower(part))
				}
			}
			c.WebsearchEngines = result
		}
	}
	if value, ok := c.Settings["flaresolverr_url"]; ok && strings.TrimSpace(value) != "" {
		c.FlaresolverrURL = &value
	} else {
		c.FlaresolverrURL = nil
	}
	legacyIndexers := [][3]string{
		{"jackett", "jackett_url", "jackett_api"},
		{"prowlarr", "prowlarr_url", "prowlarr_api"},
	}
	for _, legacy := range legacyIndexers {
		name, urlKey, keyKey := legacy[0], legacy[1], legacy[2]
		exists := false
		for _, indexer := range c.Indexers {
			if strings.EqualFold(indexer.Name, name) {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if url, ok := c.Settings[urlKey]; ok && strings.TrimSpace(url) != "" {
			apiKey := c.Settings[keyKey]
			c.Indexers = append(c.Indexers, IndexerConfig{
				Name:    name,
				URL:     url,
				APIKey:  apiKey,
				Enabled: true,
			})
		}
	}
	if value, ok := c.Settings["refresh_interval"]; ok {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			c.RefreshSecs = parsed
		}
	}
	if value, ok := c.Settings["active"]; ok {
		c.Active = configBoolSetting(&value)
	}
	if value, ok := c.Settings["dry_run"]; ok {
		c.DryRun = configBoolSetting(&value)
	}
	if value, ok := c.Settings["tmdb_api_key"]; ok && value != "" {
		c.TmdbAPIKey = &value
	} else {
		c.TmdbAPIKey = nil
	}
	c.RenameEpisodes = configBoolSetting(mapValue(c.Settings, "rename_episodes"))
	if value, ok := c.Settings["rename_format"]; ok {
		switch value {
		case "base", "standard", "full", "completo", "custom":
			c.RenameFormat = value
		default:
			c.RenameFormat = defaultRenameFormatValue
		}
	} else {
		c.RenameFormat = defaultRenameFormatValue
	}
	if value, ok := c.Settings["rename_template"]; ok {
		c.RenameTemplate = value
	}
	c.ArchiveRoot = configPathSetting(mapValue(c.Settings, "archive_root"))
	if _, present := c.Settings["trash_path"]; present {
		// Honour an explicit value, including clearing it: an empty setting
		// disables the trash instead of silently keeping the previous path.
		c.TrashPath = configPathSetting(mapValue(c.Settings, "trash_path"))
	}
	// A present-but-empty trash path would otherwise reach os.MkdirAll("") and
	// abort startup. Treat it as "not configured".
	if c.TrashPath != nil && strings.TrimSpace(*c.TrashPath) == "" {
		c.TrashPath = nil
	}
	c.NotifyTelegram = configBoolSetting(mapValue(c.Settings, "notify_telegram"))
	if value, ok := c.Settings["telegram_bot_token"]; ok && value != "" {
		c.TelegramBotToken = &value
	} else {
		c.TelegramBotToken = nil
	}
	if value, ok := c.Settings["telegram_chat_id"]; ok && value != "" {
		c.TelegramChatID = &value
	} else {
		c.TelegramChatID = nil
	}
	if value, ok := c.Settings["notify_webhook_url"]; ok && value != "" {
		c.NotifyWebhookURL = &value
	} else {
		c.NotifyWebhookURL = nil
	}
	if value, ok := c.Settings["notify_webhook_secret"]; ok && value != "" {
		c.NotifyWebhookSecret = &value
	} else {
		c.NotifyWebhookSecret = nil
	}
	if value, ok := c.Settings["notify_webhook_format"]; ok && strings.TrimSpace(value) != "" {
		c.NotifyWebhookFormat = strings.TrimSpace(value)
	} else {
		c.NotifyWebhookFormat = "gextto"
	}
	if value, ok := c.Settings["notify_webhook_token"]; ok && value != "" {
		c.NotifyWebhookToken = &value
	} else {
		c.NotifyWebhookToken = nil
	}
	if value, ok := c.Settings["notify_webhook_user"]; ok && value != "" {
		c.NotifyWebhookUser = &value
	} else {
		c.NotifyWebhookUser = nil
	}
	c.NotifyEmail = configBoolSetting(mapValue(c.Settings, "notify_email"))
	if value, ok := c.Settings["email_smtp"]; ok {
		c.EmailSMTP = value
	} else {
		c.EmailSMTP = "smtp.gmail.com:587"
	}
	if value, ok := c.Settings["email_from"]; ok && value != "" {
		c.EmailFrom = &value
	} else {
		c.EmailFrom = nil
	}
	if value, ok := c.Settings["email_to"]; ok && value != "" {
		c.EmailTo = &value
	} else {
		c.EmailTo = nil
	}
	if value, ok := c.Settings["email_password"]; ok && value != "" {
		c.EmailPassword = &value
	} else {
		c.EmailPassword = nil
	}
	c.CleanupUpgrades = configBoolSetting(mapValue(c.Settings, "cleanup_upgrades"))
	c.CleanupMinScoreDiff = numberSettingInt(c.Settings, "cleanup_min_score_diff", 0)
	c.UpgradeMinScoreDiff = numberSettingInt(c.Settings, "upgrade_min_score_diff", 200)
	if c.UpgradeMinScoreDiff < 0 {
		c.UpgradeMinScoreDiff = 0
	}
	c.UpgradeUntilScore = numberSettingInt(c.Settings, "upgrade_until_score", 0)
	if c.UpgradeUntilScore < 0 {
		c.UpgradeUntilScore = 0
	}
	if value, ok := c.Settings["cleanup_action"]; ok {
		switch value {
		case "move", "delete":
			c.CleanupAction = value
		default:
			c.CleanupAction = "move"
		}
	} else {
		c.CleanupAction = "move"
	}
	c.LibtorrentEnabled = boolSettingOr(c.Settings, "libtorrent_enabled", true)
	c.Libtorrent = LibtorrentSettings{
		PortMin:                       numberSettingUint16(c.Settings, "libtorrent_port_min", 6881),
		PortMax:                       numberSettingUint16(c.Settings, "libtorrent_port_max", 6891),
		DownloadLimitKib:              numberSettingInt(c.Settings, "libtorrent_dl_limit", 0),
		UploadLimitKib:                numberSettingInt(c.Settings, "libtorrent_ul_limit", 0),
		SeedRatio:                     numberSettingFloat(c.Settings, "libtorrent_seed_ratio", 0.0),
		SeedTimeMinutes:               numberSettingInt(c.Settings, "libtorrent_seed_time", 0),
		SeedTimeDays:                  numberSettingInt(c.Settings, "libtorrent_seed_time_days", 0),
		ActiveDownloads:               numberSettingInt(c.Settings, "libtorrent_active_downloads", 3),
		ActiveSeeds:                   numberSettingInt(c.Settings, "libtorrent_active_seeds", 3),
		ActiveLimit:                   numberSettingInt(c.Settings, "libtorrent_active_limit", 5),
		Dht:                           boolSettingOr(c.Settings, "libtorrent_dht", true),
		Pex:                           boolSettingOr(c.Settings, "libtorrent_pex", true),
		Lsd:                           boolSettingOr(c.Settings, "libtorrent_lsd", true),
		Upnp:                          boolSettingOr(c.Settings, "libtorrent_upnp", true),
		Natpmp:                        boolSettingOr(c.Settings, "libtorrent_natpmp", true),
		DynamicQueue:                  boolSettingOr(c.Settings, "libtorrent_dynamic_queue", false),
		DynamicQueueMin:               numberSettingInt(c.Settings, "libtorrent_dynamic_queue_min", 1),
		DynamicQueueMax:               numberSettingInt(c.Settings, "libtorrent_dynamic_queue_max", 10),
		DontCountSlowTorrents:         boolSettingOr(c.Settings, "libtorrent_dont_count_slow_torrents", true),
		AutoRemoveCompleted:           configBoolSetting(mapValue(c.Settings, "auto_remove_completed")),
		ConnectionsLimit:              numberSettingInt(c.Settings, "libtorrent_connections_limit", 200),
		UploadSlotsLimit:              numberSettingInt(c.Settings, "libtorrent_upload_slots_limit", -1),
		HalfOpenLimit:                 numberSettingInt(c.Settings, "libtorrent_half_open_limit", -1),
		AlertQueueSize:                numberSettingInt(c.Settings, "libtorrent_alert_queue_size", 1000),
		MaxConnectionsPerTorrent:      numberSettingInt(c.Settings, "libtorrent_max_connections_per_torrent", -1),
		MaxUploadsPerTorrent:          numberSettingInt(c.Settings, "libtorrent_max_uploads_per_torrent", -1),
		AioThreads:                    numberSettingInt(c.Settings, "libtorrent_aio_threads", -1),
		CacheSize:                     numberSettingInt(c.Settings, "libtorrent_cache_size", -1),
		CacheExpiry:                   numberSettingInt(c.Settings, "libtorrent_cache_expiry", 300),
		AnnounceInterval:              numberSettingInt(c.Settings, "libtorrent_announce_interval", 1800),
		TorrentConnectBoost:           numberSettingInt(c.Settings, "libtorrent_torrent_connect_boost", 50),
		Utp:                           boolSettingOr(c.Settings, "libtorrent_utp", true),
		PreferRc4:                     configBoolSetting(mapValue(c.Settings, "libtorrent_prefer_rc4")),
		AnnounceToAllTrackers:         configBoolSetting(mapValue(c.Settings, "libtorrent_announce_to_all_trackers")),
		AnnounceToAllTiers:            configBoolSetting(mapValue(c.Settings, "libtorrent_announce_to_all_tiers")),
		AllowMultipleConnectionsPerIp: boolSettingOr(c.Settings, "libtorrent_allow_multiple_connections_per_ip", true),
		ApplyIpFilter:                 boolSettingOr(c.Settings, "libtorrent_apply_ip_filter", true),
		Encryption:                    numberSettingInt(c.Settings, "libtorrent_encryption", 1),
		IpFilterPath:                  settingOrDefault(c.Settings, "libtorrent_ipfilter_url", ""),
		ListenInterfaces:              settingOrDefault(c.Settings, "libtorrent_listen_interfaces", ""),
		OutgoingInterface:             settingOrDefault(c.Settings, "libtorrent_outgoing_interface", ""),
		DhtBootstrapNodes:             settingOrDefault(c.Settings, "libtorrent_dht_bootstrap_nodes", ""),
	}
	if value := configPathSetting(mapValue(c.Settings, "libtorrent_dir")); value != nil {
		c.LibtorrentDir = *value
	}
	c.LibtorrentTempDir = configPathSetting(mapValue(c.Settings, "libtorrent_temp_dir"))
	return nil
}

// loadLegacySeries reads the legacy `series` table, tolerating missing columns.
func loadLegacySeries(conn *sql.DB) ([]SeriesConfig, error) {
	rows, err := conn.Query("SELECT name,seasons,quality,language,archive_path,COALESCE(timeframe,0),aliases,tmdb_id,COALESCE(subtitle,''),enabled,COALESCE(ignored_seasons,'[]'),COALESCE(exclude,'') FROM series")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	series := []SeriesConfig{}
	for rows.Next() {
		var (
			name, seasons, quality, language, archivePath string
			timeframe                                     int64
			aliases, tmdbID, subtitle                     string
			enabled                                       int64
			ignoredSeasons, exclude                       string
		)
		if err := rows.Scan(
			&name, &seasons, &quality, &language, &archivePath, &timeframe,
			&aliases, &tmdbID, &subtitle, &enabled, &ignoredSeasons, &exclude,
		); err != nil {
			continue
		}
		var aliasList []string
		if err := json.Unmarshal([]byte(aliases), &aliasList); err != nil {
			for _, value := range strings.Split(aliases, ",") {
				value = strings.TrimSpace(value)
				if value != "" {
					aliasList = append(aliasList, value)
				}
			}
		}
		var ignored []int64
		_ = json.Unmarshal([]byte(ignoredSeasons), &ignored)
		series = append(series, SeriesConfig{
			Name:             name,
			Seasons:          seasons,
			Quality:          quality,
			Language:         language,
			ArchivePath:      archivePath,
			Timeframe:        timeframe,
			Aliases:          aliasList,
			TmdbID:           tmdbID,
			TvdbID:           "",
			Subtitle:         subtitle,
			Enabled:          enabled != 0,
			IgnoredSeasons:   ignored,
			Exclude:          exclude,
			SeasonSubfolders: false,
			DisableUpgrades:  false,
		})
	}
	return series, rows.Err()
}

// settingOrDefault returns a settings value or a default string.
func settingOrDefault(settings map[string]string, key string, defaultValue string) string {
	if value, ok := settings[key]; ok {
		return value
	}
	return defaultValue
}

// FromEnv applies the environment overrides (implementation of `from_env`).
func (c *Config) FromEnv() {
	if value, ok := os.LookupEnv("GEXTTO_REFRESH_SECS"); ok {
		if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
			c.RefreshSecs = seconds
		}
	}
	if value, ok := os.LookupEnv("GEXTTO_ACTIVE"); ok {
		c.Active = value == "1" || strings.EqualFold(value, "true")
	}
	if value, ok := os.LookupEnv("GEXTTO_DRY_RUN"); ok {
		c.DryRun = value != "0"
	}
	if value, ok := os.LookupEnv("GEXTTO_LIBTORRENT"); ok {
		c.LibtorrentEnabled = value == "1"
	}
}

// LoadConfig reads the JSON configuration file, overlays the settings stored in
// `gextto_config.db` and finally applies the environment overrides.
func LoadConfig(path string) (Config, error) {
	// Start from the defaults so a partial gextto.json (or a missing one) never
	// leaves data_dir/listen/refresh_secs empty: the JSON only overrides the
	// keys it actually contains.
	cfg := DefaultConfig()
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return Config{}, fmt.Errorf("read %s: is a directory", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read %s: %w", path, err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if err := cfg.loadConfigDB(); err != nil {
		return Config{}, err
	}
	cfg.FromEnv()
	ConfigureCloudflareState(cfg.DataDir)
	return cfg, nil
}

// SaveSetting persists one setting in `gextto_config.db`.
func SaveSetting(dataDir string, key string, value string) error {
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		return err
	}
	if err := sqlExec(conn, "INSERT INTO settings(key,value) VALUES (?1,?2) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
		return err
	}
	TouchConfigGeneration()
	return nil
}

// DeleteSetting removes one setting from `gextto_config.db`, reporting whether
// a row was actually deleted.
func DeleteSetting(dataDir string, key string) (bool, error) {
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		return false, err
	}
	defer conn.Close()
	result, err := conn.Exec("DELETE FROM settings WHERE key=?1", key)
	if err != nil {
		return false, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if removed > 0 {
		TouchConfigGeneration()
	}
	return removed > 0, nil
}

// SaveLibrary persists the monitored series and movies into `gextto_config.db`.
func SaveLibrary(dataDir string, series []SeriesConfig, movies []MovieConfig) error {
	if series == nil {
		series = []SeriesConfig{}
	}
	if movies == nil {
		movies = []MovieConfig{}
	}
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS movies_config (id INTEGER PRIMARY KEY, name TEXT NOT NULL, year TEXT DEFAULT '', quality TEXT DEFAULT '', language TEXT DEFAULT '', enabled INTEGER DEFAULT 1, subtitle TEXT DEFAULT '', exclude TEXT DEFAULT '', language_requirements TEXT DEFAULT '', subtitle_requirements TEXT DEFAULT '', tmdb_id TEXT DEFAULT '', tvdb_id TEXT DEFAULT '', original_title TEXT DEFAULT '', overview TEXT DEFAULT '', poster_path TEXT DEFAULT '', disable_upgrades INTEGER NOT NULL DEFAULT 0);"); err != nil {
		return err
	}
	_ = sqlExec(conn, "ALTER TABLE movies_config ADD COLUMN exclude TEXT NOT NULL DEFAULT ''")
	_ = sqlExec(conn, "ALTER TABLE movies_config ADD COLUMN language_requirements TEXT NOT NULL DEFAULT ''")
	_ = sqlExec(conn, "ALTER TABLE movies_config ADD COLUMN subtitle_requirements TEXT NOT NULL DEFAULT ''")
	for _, column := range []string{
		"tmdb_id TEXT NOT NULL DEFAULT ''",
		"tvdb_id TEXT NOT NULL DEFAULT ''",
		"original_title TEXT NOT NULL DEFAULT ''",
		"overview TEXT NOT NULL DEFAULT ''",
		"poster_path TEXT NOT NULL DEFAULT ''",
		"disable_upgrades INTEGER NOT NULL DEFAULT 0",
	} {
		_ = sqlExec(conn, fmt.Sprintf("ALTER TABLE movies_config ADD COLUMN %s", column))
	}
	// Conserva gli id esistenti anche quando il client invia `id: 0` (film
	// aggiunti di recente), abbinando per nome+anno. Evita di riassegnare un id
	// nuovo ad ogni salvataggio e i conseguenti dettagli film vuoti quando si
	// clicca una voce appena aggiunta.
	existingIDs := map[[2]string]int64{}
	if rows, err := conn.Query("SELECT id,name,year FROM movies_config"); err == nil {
		for rows.Next() {
			var id int64
			var name, year string
			if err := rows.Scan(&id, &name, &year); err != nil {
				continue
			}
			existingIDs[[2]string{strings.ToLower(name), year}] = id
		}
		rows.Close()
	}
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	encodedSeries, err := json.Marshal(series)
	if err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES ('_migrated_series',?1) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(encodedSeries)); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("DELETE FROM movies_config"); err != nil {
		tx.Rollback()
		return err
	}
	for _, movie := range movies {
		id := movie.ID
		if id <= 0 {
			id = existingIDs[[2]string{strings.ToLower(movie.Name), movie.Year}]
		}
		if id > 0 {
			if _, err := tx.Exec(
				"INSERT INTO movies_config(id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,tmdb_id,tvdb_id,original_title,overview,poster_path,disable_upgrades) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15,?16)",
				id, movie.Name, movie.Year, movie.Quality, movie.Language, boolToInt(movie.Enabled), movie.Subtitle, movie.Exclude,
				movie.LanguageRequirements, movie.SubtitleRequirements, movie.TmdbID, movie.TvdbID, movie.OriginalTitle,
				movie.Overview, movie.PosterPath, boolToInt(movie.DisableUpgrades),
			); err != nil {
				tx.Rollback()
				return err
			}
		} else {
			if _, err := tx.Exec(
				"INSERT INTO movies_config(name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements,tmdb_id,tvdb_id,original_title,overview,poster_path,disable_upgrades) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14,?15)",
				movie.Name, movie.Year, movie.Quality, movie.Language, boolToInt(movie.Enabled), movie.Subtitle, movie.Exclude,
				movie.LanguageRequirements, movie.SubtitleRequirements, movie.TmdbID, movie.TvdbID, movie.OriginalTitle,
				movie.Overview, movie.PosterPath, boolToInt(movie.DisableUpgrades),
			); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	TouchConfigGeneration()
	return nil
}

// boolToInt converts a bool to the 0/1 integer SQLite stores.
func boolToInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// PrepareDirs creates the runtime directories.
func (c *Config) PrepareDirs() error {
	for _, path := range []string{c.DataDir, c.StateDir, c.LibtorrentDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	if c.LibtorrentTempDir != nil {
		if err := os.MkdirAll(*c.LibtorrentTempDir, 0o755); err != nil {
			return err
		}
	}
	if c.TrashPath != nil {
		if trimmed := strings.TrimSpace(*c.TrashPath); trimmed != "" {
			if err := os.MkdirAll(trimmed, 0o755); err != nil {
				return err
			}
		}
	}
	return nil
}
