package main

// ui_rss_edit.go adds the friendly indexer/feed editors on top of the JSON
// settings of the standalone RSS page: small forms that append to or remove
// from the `indexers` and `feeds` arrays, so the operator does not have to hand
// edit JSON. The raw settings stay the source of truth; a whole setup moves
// between daemons as an exported file (/ui/rss-export, imported by
// /ui/rss-config).

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

// uiIndexerPreset is a known Torznab source the wizard and the RSS page offer
// with its usual local address and port, so choosing it fills the URL in.
type uiIndexerPreset struct {
	Name string
	URL  string
}

// uiIndexerPresets are the indexer products gx-torrent speaks to directly.
// Prowlarr, Jackett and indexarr are Torznab managers with well-known ports;
// any other Torznab endpoint (a MirCrew indexer, a remote manager) is added as
// a custom name and URL. The wizard template repeats these values by hand: keep
// them in sync with setup.html.
var uiIndexerPresets = []uiIndexerPreset{
	{Name: "Prowlarr", URL: "http://127.0.0.1:9696"},
	{Name: "Jackett", URL: "http://127.0.0.1:9117"},
	{Name: "indexarr", URL: "http://127.0.0.1:8788"},
	// MirCrew's local indexer serves Torznab on a port of its choosing (often
	// 9118): the URL is typed by hand, the name just fills the field.
	{Name: "MIRCrew", URL: ""},
}

// uiIndexerPresetURL returns the default URL of a preset by name (case
// insensitive), or "".
func uiIndexerPresetURL(name string) string {
	for _, preset := range uiIndexerPresets {
		if strings.EqualFold(strings.TrimSpace(name), preset.Name) {
			return preset.URL
		}
	}
	return ""
}

// validIndexerURL reports whether raw is an absolute http(s) URL.
func validIndexerURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

// validFeedURL reports whether raw is a usable feed source: an absolute http(s)
// URL, or a BEP 46 DHT feed (dht://<pubkey-hex>[?salt=<hex>]).
func validFeedURL(raw string) bool {
	return validIndexerURL(raw) || isDHTFeed(raw)
}

// saveIndexers/feeds persist one settings array, reporting a user-facing error.
func (d *Daemon) saveIndexers(list []torznab.Indexer) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(indexersSettingKey, string(raw))
	return d.opts.Settings.Save()
}

func (d *Daemon) saveFeeds(list []feedConfig) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(feedsSettingKey, string(raw))
	return d.opts.Settings.Save()
}

func (d *Daemon) saveRules(list []rss.Rule) error {
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(rulesSettingKey, string(raw))
	return d.opts.Settings.Save()
}

// handleUIRssIndexer adds or removes one Torznab indexer.
func (d *Daemon) handleUIRssIndexer(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.indexers()
	switch strings.TrimSpace(r.FormValue("op")) {
	case "remove":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The indexer to remove does not exist anymore.")
			return
		}
		list = append(list[:index], list[index+1:]...)
	default:
		preset := strings.TrimSpace(r.FormValue("type"))
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = preset
		}
		if name == "" {
			name = "indexer"
		}
		indexerURL := strings.TrimSpace(r.FormValue("url"))
		if indexerURL == "" {
			// Prefer the chosen type's usual URL, then the name's (a custom
			// name leaves the URL to type).
			indexerURL = uiIndexerPresetURL(preset)
			if indexerURL == "" {
				indexerURL = uiIndexerPresetURL(name)
			}
		}
		if !validIndexerURL(indexerURL) {
			d.renderRSS(w, r, "The indexer URL must start with http:// or https://.")
			return
		}
		indexer := torznab.Indexer{
			Name:   name,
			URL:    strings.TrimRight(indexerURL, "/"),
			APIKey: strings.TrimSpace(r.FormValue("apikey")),
		}
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 {
			// Editing an existing entry: replace it in place, so a rename works.
			if index >= len(list) {
				d.renderRSS(w, r, "The indexer to edit does not exist anymore.")
				return
			}
			// A blank API key keeps the stored one (the page never shows it).
			if strings.TrimSpace(r.FormValue("apikey")) == "" {
				indexer.APIKey = list[index].APIKey
			}
			list[index] = indexer
		} else {
			replaced := false
			for i := range list {
				if strings.EqualFold(list[i].Name, indexer.Name) {
					list[i] = indexer
					replaced = true
					break
				}
			}
			if !replaced {
				list = append(list, indexer)
			}
		}
	}
	if err := d.saveIndexers(list); err != nil {
		d.renderRSS(w, r, "Cannot save the indexers: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss#indexers", http.StatusSeeOther)
}

// handleUIRssIndexerTest checks one configured indexer with a Torznab `t=caps`
// request and returns the outcome, so the page can offer a Test button.
func (d *Daemon) handleUIRssIndexerTest(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.indexers()
	index, err := parseFormIndex(r.FormValue("index"))
	if err != nil || index < 0 || index >= len(list) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "indexer not found"})
		return
	}
	idx := list[index]
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	start := time.Now()
	err = idx.Ping(ctx)
	out := map[string]any{"ok": err == nil, "name": idx.Name, "ms": time.Since(start).Milliseconds()}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUIRssFeed adds or removes one RSS feed (or a Torznab search feed).
func (d *Daemon) handleUIRssFeed(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.feeds()
	switch strings.TrimSpace(r.FormValue("op")) {
	case "remove":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The feed to remove does not exist anymore.")
			return
		}
		list = append(list[:index], list[index+1:]...)
	case "toggle":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The feed does not exist anymore.")
			return
		}
		list[index].Disabled = !list[index].Disabled
	default:
		// A pasted list of addresses adds one feed per line, sharing the
		// options of the form (see ui_rss_bulk.go).
		if raw := strings.TrimSpace(r.FormValue("urls")); raw != "" {
			d.uiRssAddFeedsBulk(w, r, list, raw)
			return
		}
		feed := feedConfig{
			Name:     strings.TrimSpace(r.FormValue("name")),
			URL:      strings.TrimSpace(r.FormValue("url")),
			Search:   strings.TrimSpace(r.FormValue("search")),
			Folder:   strings.TrimSpace(r.FormValue("folder")),
			Include:  strings.TrimSpace(r.FormValue("include")),
			Exclude:  strings.TrimSpace(r.FormValue("exclude")),
			Category: strings.TrimSpace(r.FormValue("category")),
			SavePath: strings.TrimSpace(r.FormValue("savepath")),
			Paused:   r.FormValue("paused") != "",
			Disabled: r.FormValue("disabled") != "",
			All:      r.FormValue("all") != "",
		}
		if feed.URL == "" && feed.Search == "" {
			d.renderRSS(w, r, "Give the feed an RSS URL.")
			return
		}
		if feed.URL != "" && !validFeedURL(feed.URL) {
			d.renderRSS(w, r, "The feed URL must start with http://, https:// or dht://.")
			return
		}
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 {
			// Editing an existing feed: replace it in place, so a rename works.
			if index >= len(list) {
				d.renderRSS(w, r, "The feed does not exist anymore.")
				return
			}
			old := list[index]
			list[index] = feed
			// A changed address keeps the feed's uid, like qBittorrent.
			d.carryFeedUID(feedIdentity(old), feedIdentity(feed))
		} else {
			list = append(list, feed)
		}
	}
	if err := d.saveFeeds(list); err != nil {
		d.renderRSS(w, r, "Cannot save the feeds: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss#feeds", http.StatusSeeOther)
}

// handleUIRssRule adds, removes or reorders one ordered RSS rule. At least one
// rule switches the feed loop from the simple per-feed include/exclude to the
// ordered rule set: the first rule that matches decides (PASS adds with its
// action, FAIL skips), and an item matched by no rule is not added.
func (d *Daemon) handleUIRssRule(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	list := d.rules()
	switch strings.TrimSpace(r.FormValue("op")) {
	case "remove":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The rule to remove does not exist anymore.")
			return
		}
		list = append(list[:index], list[index+1:]...)
	case "toggle":
		index, err := parseFormIndex(r.FormValue("index"))
		if err != nil || index < 0 || index >= len(list) {
			d.renderRSS(w, r, "The rule does not exist anymore.")
			return
		}
		enabled := !rss.RuleEnabled(list[index])
		if enabled {
			list[index].Enabled = nil
		} else {
			list[index].Enabled = &enabled
		}
	case "up":
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index > 0 && index < len(list) {
			list[index-1], list[index] = list[index], list[index-1]
		}
	case "down":
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 && index < len(list)-1 {
			list[index+1], list[index] = list[index], list[index+1]
		}
	default:
		rule, err := uiRuleFromForm(r)
		if err != nil {
			d.renderRSS(w, r, err.Error())
			return
		}
		if index, err := parseFormIndex(r.FormValue("index")); err == nil && index >= 0 {
			// Editing an existing rule keeps its place in the order.
			if index >= len(list) {
				d.renderRSS(w, r, "The rule does not exist anymore.")
				return
			}
			list[index] = rule
		} else {
			list = append(list, rule)
		}
	}
	if err := d.saveRules(list); err != nil {
		d.renderRSS(w, r, "Cannot save the rules: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss#rules", http.StatusSeeOther)
}

// handleUIRssSuggest returns series-name suggestions for the follow box, derived
// from the configured indexers: it runs the typed prefix as a query and reduces
// the release titles to unique series names, so the operator does not have to
// type the name exactly.
func (d *Daemon) handleUIRssSuggest(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) < 2 {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	// The typeahead reads the archive (fast): the feeds and the periodic indexer
	// polls keep it full, so typing does not hit the indexers.
	items := d.archiveWantedItems(query, wantedArchiveScan)
	writeJSON(w, http.StatusOK, seriesSuggestions(items, 12))
}

// seriesSuggestions reduces release titles to unique series names, the most
// frequent first, title-cased for display. Only titles carrying an episode
// marker are considered.
func seriesSuggestions(items []rss.Item, limit int) []string {
	counts := map[string]int{}
	var names []string
	for _, item := range items {
		series, _, _, ok := rss.FindEpisode(item.Title)
		if !ok || len([]rune(series)) < 2 {
			continue
		}
		if _, seen := counts[series]; !seen {
			names = append(names, series)
		}
		counts[series]++
	}
	sort.SliceStable(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = titleCase(name)
	}
	return out
}

// titleCase upper-cases the first letter of every word.
func titleCase(value string) string {
	words := strings.Fields(value)
	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		words[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// titleMatchRegex turns a title into a case-insensitive regex that requires the
// title's words, in order and separated by any of the usual separators
// (. _ - space). Without it a match built only from the optional
// language/quality words also matches unrelated releases returned by fuzzy
// indexer queries (e.g. any release carrying "1080p").
func titleMatchRegex(title string) string {
	if pattern, ok := titleRegexPattern(title); ok {
		return "(?i)" + pattern
	}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		if word != "" {
			quoted = append(quoted, regexp.QuoteMeta(word))
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	return "(?i)" + strings.Join(quoted, `[\s._\-]+`)
}

// titleRegexPattern recognizes a title written as a regular expression:
// between slashes, like /ted.?lasso/. It returns the pattern inside.
func titleRegexPattern(title string) (string, bool) {
	title = strings.TrimSpace(title)
	if len(title) < 3 || !strings.HasPrefix(title, "/") || !strings.HasSuffix(title, "/") {
		return "", false
	}
	return title[1 : len(title)-1], true
}

// titleSearchText is the plain text an indexer or the archive is searched with
// for a title: the title itself, or for a /regex/ title its literal words. A
// regex with alternatives (|) has no single text: it is matched only against
// what the feeds and the archive scan bring.
func titleSearchText(title string) string {
	pattern, ok := titleRegexPattern(title)
	if !ok {
		return strings.TrimSpace(title)
	}
	if strings.Contains(pattern, "|") {
		return ""
	}
	// Drop escapes and classes (\d, \s, [..]) before taking the words.
	cleaned := regexp.MustCompile(`\\.|\[[^\]]*\]|\{[^}]*\}`).ReplaceAllString(pattern, " ")
	words := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, " ")
}

// uiRuleFromForm builds one rule from the add-rule form, validating the regexes
// and the episode filter.
func uiRuleFromForm(r *http.Request) (rss.Rule, error) {
	enabled := formBool(r, "enabled")
	rule := rss.Rule{
		Name:       strings.TrimSpace(r.FormValue("name")),
		Feeds:      formList(r, "feeds"),
		Enabled:    &enabled,
		IgnoreDays: int(formInt(r, "ignore_days", 0)),
		Fail:       formBool(r, "fail"),
		Match: rss.Criteria{
			Include:        splitList(r.FormValue("include")),
			Require:        splitList(r.FormValue("require")),
			Exclude:        splitList(r.FormValue("exclude")),
			Regex:          strings.TrimSpace(r.FormValue("regex")),
			NotRegex:       strings.TrimSpace(r.FormValue("not_regex")),
			MinSize:        mibToBytes(r.FormValue("min_size")),
			MaxSize:        mibToBytes(r.FormValue("max_size")),
			MinSeeders:     int(formInt(r, "min_seeders", 0)),
			MaxSeeders:     int(formInt(r, "max_seeders", 0)),
			MinPeers:       int(formInt(r, "min_peers", 0)),
			MaxAgeDays:     int(formInt(r, "max_age_days", 0)),
			RequireEpisode: formBool(r, "require_episode"),
			SmartEpisode:   formBool(r, "smart_episode"),
			EpisodeFilter:  strings.TrimSpace(r.FormValue("episode_filter")),
		},
		Action: rss.Action{
			SavePath:   strings.TrimSpace(r.FormValue("savepath")),
			Category:   strings.TrimSpace(r.FormValue("category")),
			Tags:       splitList(r.FormValue("tags")),
			Paused:     formBool(r, "paused"),
			Sequential: formBool(r, "sequential"),
			FirstLast:  formBool(r, "first_last"),
			Top:        formBool(r, "top"),
			Once:       formBool(r, "once"),
		},
	}
	for field, pattern := range map[string]string{"regex": rule.Match.Regex, "not_regex": rule.Match.NotRegex} {
		if strings.TrimSpace(pattern) == "" {
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return rule, fmt.Errorf("%s is not a valid regular expression: %v", field, err)
		}
	}
	if _, err := rss.ParseEpisodeFilter(rule.Match.EpisodeFilter); err != nil {
		return rule, err
	}
	return rule, nil
}

// mibToBytes parses a size in MiB (accepting a decimal) into bytes; empty or
// non-positive means "not enforced".
func mibToBytes(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int64(value * 1024 * 1024)
}

// uiRuleFeedsHTML is the "apply this rule to these feeds" picker: one checkbox
// per configured feed (qBittorrent's RSS Downloader), nothing checked meaning
// every feed. The feed names are data, not strings to translate.
func (d *Daemon) uiRuleFeedsHTML() string {
	feeds := d.feeds()
	if len(feeds) == 0 {
		return `<span class="muted">No feeds configured yet: add one in the Feeds tab and it can be picked here.</span>`
	}
	names := make([]string, 0, len(feeds))
	for _, feed := range feeds {
		name := feed.Name
		if name == "" {
			name = feed.URL
		}
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(`<label class="chk"><input type="checkbox" name="feeds" value="` + html.EscapeString(name) + `">` + html.EscapeString(name) + `</label>`)
	}
	return b.String()
}

// formList collects every value of a repeated form field, each split on commas
// too: the "apply to these feeds" picker posts one value per checked box, and
// the older hand-written "a, b" list keeps working.
func formList(r *http.Request, key string) []string {
	var out []string
	for _, value := range r.Form[key] {
		for _, term := range splitList(value) {
			if !slices.Contains(out, term) {
				out = append(out, term)
			}
		}
	}
	return out
}

// uiRuleRowsHTML renders the ordered rules as table rows, each with up/down and
// remove buttons (the order is the precedence).
func (d *Daemon) uiRuleRowsHTML() string {
	list := d.rules()
	if len(list) == 0 {
		return `<li class="empty"><p>No rules yet: every feed only grows the archive, or takes what its own Include/Exclude allow.</p></li>`
	}
	var b strings.Builder
	for index, rule := range list {
		idx := strconv.Itoa(index)
		enabled := rss.RuleEnabled(rule)
		kind := `<span class="badge b-seeding">Add</span>`
		if rule.Fail {
			kind = `<span class="badge b-error">Skip</span>`
		}
		if !enabled {
			kind += ` <span class="badge b-paused">Disabled</span>`
		}
		scope := `<span>All feeds</span>`
		if len(rule.Feeds) > 0 {
			scope = html.EscapeString(strings.Join(rule.Feeds, ", "))
		}
		cls := "item"
		if !enabled {
			cls += " off"
		}
		b.WriteString(`<li class="` + cls + `"><div class="item-order" aria-hidden="true">` + strconv.Itoa(index+1) + `</div><div class="item-main">`)
		b.WriteString(`<div class="item-title"><span class="name">` + html.EscapeString(orDefault(rule.Name, "—")) + `</span>` + kind + `</div>`)
		b.WriteString(`<div class="item-sub"><svg class="i"><use href="/assets/icons.svg#i-rss"/></svg>` + scope + `</div>`)
		if chips := uiRuleChipsHTML(rule); chips != "" {
			b.WriteString(`<div class="chips">` + chips + `</div>`)
		}
		// Cooldown visibility: with ignore_days the rule stops matching for a
		// while after each match, which looks like a bug when nothing arrives.
		// Show when it may match again.
		if rule.IgnoreDays > 0 {
			if last := d.ruleLastMatch(rule.Name); !last.IsZero() {
				if next := last.Add(time.Duration(rule.IgnoreDays) * 24 * time.Hour); time.Now().Before(next) {
					b.WriteString(`<div class="item-sub">` + uiChip("", "Next match", next.Format("2006-01-02 15:04")) + `</div>`)
				}
			}
		}
		b.WriteString(`</div><div class="item-actions">`)
		ruleJSON, _ := json.Marshal(rule)
		toggle := "Disable"
		if !enabled {
			toggle = "Enable"
		}
		b.WriteString(`<form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="up"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="ghost icon sm" title="Move up: rules are evaluated in order" aria-label="Move up"` + disabledIf(index == 0) + `><svg class="i"><use href="/assets/icons.svg#i-up"/></svg></button></form>`)
		b.WriteString(`<form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="down"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="ghost icon sm" title="Move down: rules are evaluated in order" aria-label="Move down"` + disabledIf(index == len(list)-1) + `><svg class="i"><use href="/assets/icons.svg#i-down"/></svg></button></form>`)
		b.WriteString(`<button type="button" class="sm" data-rule-edit data-index="` + idx + `" data-rule="` + html.EscapeString(string(ruleJSON)) + `" title="Open this rule to change it">Edit</button>`)
		b.WriteString(`<form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="toggle"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="sm ghost" title="Turn this rule on or off without deleting it">` + toggle + `</button></form>`)
		b.WriteString(`<form method="post" action="/ui/rss-rule" data-confirm="Delete this rule?"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="ghost icon sm danger" title="Delete this rule" aria-label="Delete this rule"><svg class="i"><use href="/assets/icons.svg#i-trash"/></svg></button></form>`)
		b.WriteString(`</div></li>`)
	}
	return b.String()
}

// disabledIf renders the disabled attribute when cond holds.
func disabledIf(cond bool) string {
	if cond {
		return " disabled"
	}
	return ""
}

// uiChip renders one labelled chip; the label is its own element so the page
// translator localizes it, the value stays as typed.
func uiChip(class, label, value string) string {
	return `<span class="chip ` + class + `"><span>` + label + `</span>&nbsp;` + html.EscapeString(value) + `</span>`
}

// uiRuleChipsHTML shows a rule as readable chips: what it matches, then what
// it does with what it matched.
func uiRuleChipsHTML(rule rss.Rule) string {
	m, a := rule.Match, rule.Action
	var b strings.Builder
	add := func(class, label, value string) { b.WriteString(uiChip(class, label, value)) }
	flag := func(label string) { b.WriteString(`<span class="chip"><span>` + label + `</span></span>`) }
	if len(m.Require) > 0 {
		add("in", "All of", strings.Join(m.Require, ", "))
	}
	if len(m.Include) > 0 {
		add("in", "One of", strings.Join(m.Include, ", "))
	}
	if len(m.Exclude) > 0 {
		add("out", "None of", strings.Join(m.Exclude, ", "))
	}
	if m.Regex != "" {
		add("in", "Regex", m.Regex)
	}
	if m.NotRegex != "" {
		add("out", "Not regex", m.NotRegex)
	}
	if m.EpisodeFilter != "" {
		add("", "Episodes", m.EpisodeFilter)
	}
	if m.MinSize > 0 {
		add("", "Min size", uiBytes(m.MinSize))
	}
	if m.MaxSize > 0 {
		add("", "Max size", uiBytes(m.MaxSize))
	}
	if m.MinSeeders != 0 {
		add("", "Min seeders", strconv.Itoa(m.MinSeeders))
	}
	if m.MaxSeeders != 0 {
		add("", "Max seeders", strconv.Itoa(m.MaxSeeders))
	}
	if m.MinPeers != 0 {
		add("", "Min peers", strconv.Itoa(m.MinPeers))
	}
	if m.MaxAgeDays != 0 {
		add("", "Max age (days)", strconv.Itoa(m.MaxAgeDays))
	}
	if rule.IgnoreDays > 0 {
		add("", "Cooldown (days)", strconv.Itoa(rule.IgnoreDays))
	}
	if m.RequireEpisode {
		flag("Episodes only")
	}
	if m.SmartEpisode {
		flag("Each episode once")
	}
	if a.SavePath != "" {
		add("act", "Save to", a.SavePath)
	}
	if a.Category != "" {
		add("act", "Category", a.Category)
	}
	if len(a.Tags) > 0 {
		add("act", "Tags", strings.Join(a.Tags, ", "))
	}
	if a.Paused {
		flag("Paused")
	}
	if a.Sequential {
		flag("sequential")
	}
	if a.FirstLast {
		flag("first/last")
	}
	if a.Top {
		flag("top")
	}
	if a.Once {
		flag("Download once")
	}
	return b.String()
}

// uiRuleMatchSummary is a compact "field=value" view of a rule's criteria.
func uiRuleMatchSummary(rule rss.Rule) string {
	m := rule.Match
	var parts []string
	if len(m.Include) > 0 {
		parts = append(parts, "include="+strings.Join(m.Include, ","))
	}
	if len(m.Exclude) > 0 {
		parts = append(parts, "exclude="+strings.Join(m.Exclude, ","))
	}
	if m.Regex != "" {
		parts = append(parts, "regex="+m.Regex)
	}
	if m.NotRegex != "" {
		parts = append(parts, "not_regex="+m.NotRegex)
	}
	if m.MinSize > 0 {
		parts = append(parts, "min_size="+uiBytes(m.MinSize))
	}
	if m.MaxSize > 0 {
		parts = append(parts, "max_size="+uiBytes(m.MaxSize))
	}
	if m.MinSeeders != 0 {
		parts = append(parts, fmt.Sprintf("min_seeders=%d", m.MinSeeders))
	}
	if m.MaxSeeders != 0 {
		parts = append(parts, fmt.Sprintf("max_seeders=%d", m.MaxSeeders))
	}
	if m.MinPeers != 0 {
		parts = append(parts, fmt.Sprintf("min_peers=%d", m.MinPeers))
	}
	if m.MaxAgeDays != 0 {
		parts = append(parts, fmt.Sprintf("max_age_days=%d", m.MaxAgeDays))
	}
	if m.RequireEpisode {
		parts = append(parts, "require_episode")
	}
	if m.EpisodeFilter != "" {
		parts = append(parts, "episode="+m.EpisodeFilter)
	}
	if m.SmartEpisode {
		parts = append(parts, "smart_episode")
	}
	if rule.IgnoreDays > 0 {
		parts = append(parts, fmt.Sprintf("ignore_days=%d", rule.IgnoreDays))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

// uiRuleActionSummary is a compact view of a rule's action.
func uiRuleActionSummary(rule rss.Rule) string {
	a := rule.Action
	var parts []string
	if a.SavePath != "" {
		parts = append(parts, "savepath="+a.SavePath)
	}
	if a.Category != "" {
		parts = append(parts, "category="+a.Category)
	}
	if len(a.Tags) > 0 {
		parts = append(parts, "tags="+strings.Join(a.Tags, ","))
	}
	if a.Paused {
		parts = append(parts, "paused")
	}
	if a.Sequential {
		parts = append(parts, "sequential")
	}
	if a.FirstLast {
		parts = append(parts, "first_last")
	}
	if a.Top {
		parts = append(parts, "top")
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

// uiIndexerOptionsHTML renders the "type" options of the add-indexer form: the
// known presets carry their usual URL in data-url (the page fills the URL field
// with it), plus a generic custom choice.
func uiIndexerOptionsHTML() string {
	var b strings.Builder
	for _, preset := range uiIndexerPresets {
		b.WriteString(`<option value="` + html.EscapeString(preset.Name) + `" data-url="` + html.EscapeString(preset.URL) + `">` + html.EscapeString(preset.Name) + `</option>`)
	}
	b.WriteString(`<option value="" data-url="">Other</option>`)
	return b.String()
}

// uiIndexerRowsHTML renders the configured indexers as table rows, each with a
// remove button; the row index is the removal key.
func (d *Daemon) uiIndexerRowsHTML() string {
	list := d.indexers()
	if len(list) == 0 {
		return `<li class="empty"><p>No indexers: add Prowlarr or Jackett to search releases and to let wanted titles find what the feeds miss.</p></li>`
	}
	var b strings.Builder
	for index, indexer := range list {
		idx := strconv.Itoa(index)
		name := html.EscapeString(orDefault(indexer.Name, "—"))
		if page := indexerPageURL(indexer.URL); page != "" {
			name = `<a href="` + html.EscapeString(page) + `" target="_blank" rel="noopener noreferrer" title="Open the indexer's web page in a new tab">` + name + ` <span aria-hidden="true">↗</span></a>`
		}
		key := `<span class="badge b-paused">No API key</span>`
		if strings.TrimSpace(indexer.APIKey) != "" {
			key = `<span class="badge b-seeding">API key set</span>`
		}
		b.WriteString(`<li class="item"><div class="item-main">`)
		b.WriteString(`<div class="item-title"><span class="name">` + name + `</span>` + key + `</div>`)
		b.WriteString(`<div class="item-sub mono">` + html.EscapeString(indexer.URL) + `</div>`)
		b.WriteString(`<div class="item-status" data-idx-status="` + idx + `" aria-live="polite"></div>`)
		b.WriteString(`</div><div class="item-actions">`)
		b.WriteString(`<button type="button" class="sm" data-idx-test data-index="` + idx + `" title="Check that the indexer answers (Torznab caps)">Test</button>`)
		b.WriteString(`<button type="button" class="sm" data-idx-edit data-index="` + idx + `" data-name="` + html.EscapeString(indexer.Name) + `" data-url="` + html.EscapeString(indexer.URL) + `" title="Change this indexer">Edit</button>`)
		b.WriteString(`<form method="post" action="/ui/rss-indexer" data-confirm="Delete this indexer?"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + idx + `"><button type="submit" class="ghost icon sm danger" title="Delete this indexer" aria-label="Delete this indexer"><svg class="i"><use href="/assets/icons.svg#i-trash"/></svg></button></form>`)
		b.WriteString(`</div></li>`)
	}
	return b.String()
}

// uiFeedRowsHTML renders the configured feeds as table rows with their last
// outcome and the reload, edit, enable/disable and remove buttons; the row
// index is the key of the edit/remove operations.
func (d *Daemon) uiFeedRowsHTML() string {
	list := d.feeds()
	if len(list) == 0 {
		return `<li class="empty"><p>No feeds yet. Add an RSS URL, or the page of an ext.to or TorrentGalaxy listing: its items fill the archive and the rules decide what to download.</p></li>`
	}
	statuses := map[string]feedStatus{}
	for _, status := range d.feedStatusesSnapshot() {
		statuses[status.Name] = status
	}
	rows := make([]struct {
		index int
		feed  feedConfig
	}, len(list))
	for i, feed := range list {
		rows[i].index = i
		rows[i].feed = feed
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !strings.EqualFold(rows[i].feed.Folder, rows[j].feed.Folder) {
			return strings.ToLower(rows[i].feed.Folder) < strings.ToLower(rows[j].feed.Folder)
		}
		return strings.ToLower(rows[i].feed.Name) < strings.ToLower(rows[j].feed.Name)
	})
	var b strings.Builder
	rules := d.rules()
	totals := d.archive().feedCounts()
	wantedOn := false
	for _, w := range d.wantedList() {
		if !w.Paused {
			wantedOn = true
			break
		}
	}
	// The feeds are grouped by folder: each group opens with a header the page
	// can fold away (34 imported feeds are unmanageable as one flat list).
	folderCounts := map[string]int{}
	var order []string
	for _, row := range rows {
		folder := strings.TrimSpace(row.feed.Folder)
		if _, seen := folderCounts[folder]; !seen {
			order = append(order, folder)
		}
		folderCounts[folder]++
	}
	for _, row := range rows {
		feed := row.feed
		index := strconv.Itoa(row.index)
		folder := strings.TrimSpace(feed.Folder)
		if folderCounts[folder] > 0 {
			b.WriteString(uiFeedFolderHeaderHTML(folder, folderCounts[folder], len(order) > 1))
			folderCounts[folder] = 0 // once per group
		}
		source := strings.TrimSpace(feed.URL)
		if source == "" && strings.TrimSpace(feed.Search) != "" {
			source = "search: " + feed.Search
		}
		key := feed.Name
		if key == "" {
			key = source
		}
		badges := ""
		if feed.Disabled {
			badges += `<span class="badge b-paused">disabled</span>`
		}
		if feed.Paused {
			badges += `<span class="badge plain" title="Torrents from this feed are added paused">adds paused</span>`
		}
		if feedTakesEverything(rules, feed, key) {
			badges += `<span class="badge b-stalled" title="No rule and no Include/Exclude: this feed only grows the archive, nothing is downloaded">archive only</span>`
		}
		cls := "item"
		if feed.Disabled {
			cls += " off"
		}
		b.WriteString(`<li class="` + cls + `" data-feed-row="` + html.EscapeString(key) + `" data-folder="` + html.EscapeString(folder) + `"><div class="item-main">`)
		b.WriteString(`<div class="item-title"><span class="name">` + html.EscapeString(orDefault(feed.Name, "—")) + `</span>` + badges + `<span class="item-status" data-feed-status>` + uiFeedStatusHTML(statuses[key], totals[key]) + `</span></div>`)
		b.WriteString(`<div class="item-sub mono" title="` + html.EscapeString(source) + `">` + html.EscapeString(source) + `</div>`)
		covering := feedCoveringRules(rules, key)
		b.WriteString(uiFeedGovernanceHTML(rules, feed, key, covering, wantedOn))
		if chips := uiFeedChipsHTML(feed); chips != "" {
			b.WriteString(`<div class="chips">` + chips + `</div>`)
		}
		feedJSON, _ := json.Marshal(feed)
		toggle := "Disable"
		if feed.Disabled {
			toggle = "Enable"
		}
		b.WriteString(`</div><div class="item-actions">`)
		b.WriteString(`<button type="button" class="sm" data-feed-reload="` + html.EscapeString(key) + `" title="Fetch this feed now and apply the rules" aria-label="Reload this feed now"><svg class="i"><use href="/assets/icons.svg#i-reload"/></svg>Reload</button>`)
		b.WriteString(`<button type="button" class="sm" data-feed-items="` + html.EscapeString(key) + `" title="Show the items of this feed and the rule that takes each one">Items</button>`)
		b.WriteString(`<button type="button" class="sm" data-feed-edit data-index="` + index + `" data-feed="` + html.EscapeString(string(feedJSON)) + `" data-covered="` + html.EscapeString(strings.Join(covering, ", ")) + `" title="Open this feed to change it">Edit</button>`)
		b.WriteString(`<form method="post" action="/ui/rss-feed"><input type="hidden" name="op" value="toggle"><input type="hidden" name="index" value="` + index + `"><button type="submit" class="sm ghost" title="Stop or resume the automatic checks of this feed without deleting it">` + toggle + `</button></form>`)
		b.WriteString(`<form method="post" action="/ui/rss-feed" data-confirm="Delete this feed?"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + index + `"><button type="submit" class="ghost icon sm danger" title="Delete this feed" aria-label="Delete this feed"><svg class="i"><use href="/assets/icons.svg#i-trash"/></svg></button></form>`)
		b.WriteString(`</div></li>`)
	}
	return b.String()
}

// uiFeedFolderHeaderHTML opens a feed folder group: the page folds the group
// away with the header (the state is remembered per folder in the browser). The
// count says how many feeds the group holds. A single group (all the feeds
// unfiled, say) gets no header: there would be nothing to fold.
func uiFeedFolderHeaderHTML(folder string, feeds int, grouped bool) string {
	if !grouped {
		return ""
	}
	name := folder
	if name == "" {
		name = "No folder"
	}
	return `<li class="feed-folder" data-folder-head="` + html.EscapeString(folder) + `"><button type="button" class="folder-toggle" data-folder-toggle="` + html.EscapeString(folder) + `" aria-expanded="true" title="Show or hide this folder"><svg class="i"><use href="/assets/icons.svg#i-down"/></svg><span class="fname">` + html.EscapeString(name) + `</span><span class="cnt">` + strconv.Itoa(feeds) + `</span></button></li>`
}

// uiFeedChipsHTML shows the options of a feed that change what it adds.
// feedCoveringRules are the names of the rules that watch a feed; with at
// least one, the feed's own filter is not used.
func feedCoveringRules(rules []rss.Rule, feedName string) []string {
	var out []string
	for _, rule := range rules {
		if rss.RuleAppliesToFeed(rule, feedName) {
			out = append(out, orDefault(rule.Name, "—"))
		}
	}
	return out
}

// uiFeedGovernanceHTML says, on the feed row, what decides what the feed
// downloads: the rules that watch it, its own filter, everything, or nothing
// (archive only), and whether the wanted titles also take from it. Watching
// rules are buttons: one click opens the rule editor for that rule.
func uiFeedGovernanceHTML(rules []rss.Rule, feed feedConfig, key string, covering []string, wantedOn bool) string {
	var b strings.Builder
	b.WriteString(`<div class="item-sub governs"><svg class="i"><use href="/assets/icons.svg#i-filter"/></svg>`)
	switch {
	case len(covering) > 0:
		b.WriteString(`<span>Downloads what these rules take:</span> `)
		indexByName := map[string]int{}
		for index, rule := range rules {
			if _, ok := indexByName[orDefault(rule.Name, "—")]; !ok {
				indexByName[orDefault(rule.Name, "—")] = index
			}
		}
		for i, name := range covering {
			if i > 0 {
				b.WriteString(`<span>, </span>`)
			}
			if index, ok := indexByName[name]; ok {
				b.WriteString(`<button type="button" class="linklike" data-open-rule="` + strconv.Itoa(index) + `" title="Open this rule to change it"><b>` + html.EscapeString(name) + `</b></button>`)
			} else {
				b.WriteString(`<b>` + html.EscapeString(name) + `</b>`)
			}
		}
	case feed.All:
		b.WriteString(`<span>Downloads every item (Take everything)</span>`)
	case feedTakesEverything(rules, feed, key):
		b.WriteString(`<span>Archive only: no rule watches it and it has no filter, so nothing is downloaded</span>`)
	default:
		b.WriteString(`<span>Downloads what its own filter takes</span>`)
	}
	if wantedOn {
		b.WriteString(` · <span>the wanted titles also take from it</span>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func uiFeedChipsHTML(feed feedConfig) string {
	var b strings.Builder
	if strings.TrimSpace(feed.Include) != "" {
		b.WriteString(uiChip("in", "One of", feed.Include))
	}
	if strings.TrimSpace(feed.Exclude) != "" {
		b.WriteString(uiChip("out", "None of", feed.Exclude))
	}
	if strings.TrimSpace(feed.SavePath) != "" {
		b.WriteString(uiChip("act", "Save to", feed.SavePath))
	}
	if strings.TrimSpace(feed.Category) != "" {
		b.WriteString(uiChip("act", "Category", feed.Category))
	}
	if feed.All {
		b.WriteString(`<span class="chip act"><span>Take everything</span></span>`)
	}
	return b.String()
}

// uiFeedStatusHTML renders a feed's last outcome: when it was checked, how
// many items it returned (and how many were new or added), or its error.
// uiFeedStatusHTML renders a feed's row status: the total of torrents it has
// brought into the archive, then the outcome of the last check.
func uiFeedStatusHTML(status feedStatus, total int) string {
	totalHTML := `<span title="Torrents this feed has brought into the archive">` + strconv.Itoa(total) + `</span> <span class="muted">torrents</span>`
	if status.LastCheck == 0 {
		if total > 0 {
			return totalHTML
		}
		return `<span class="muted">not checked yet</span>`
	}
	when := time.Unix(status.LastCheck, 0).Format("02/01 15:04")
	out := `<span class="muted" title="Last check">` + when + `</span> `
	if status.LastError != "" {
		errHTML := `<span class="err" role="status">` + html.EscapeString(status.LastError) + `</span>`
		if hint := feedErrorHint(status.LastError); hint != "" {
			errHTML += ` <span class="muted">` + hint + `</span>`
		}
		return out + errHTML
	}
	out += totalHTML
	if status.New > 0 {
		out += ` · <span class="ok">` + strconv.Itoa(status.New) + `</span> <span class="muted">new</span>`
	}
	if status.Added > 0 {
		out += ` · <span class="ok">` + strconv.Itoa(status.Added) + `</span> <span class="muted">added</span>`
	}
	return out
}

// feedErrorHint maps a feed check error to an actionable hint (a catalog key):
// a wrong API key, a Cloudflare challenge (FlareSolverr), or a page without
// torrent links mistaken for a feed. Empty when the error says it all.
func feedErrorHint(errText string) string {
	lower := strings.ToLower(errText)
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") || strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "apikey"):
		return "Check the indexer's API key"
	case strings.Contains(lower, "429") || strings.Contains(lower, "too many requests"):
		return "Too many requests: the feeds of this site are spaced and the rest is left for the next check"
	case strings.Contains(lower, "cloudflare") || strings.Contains(lower, "challenge") ||
		strings.Contains(lower, "403") || strings.Contains(lower, "just a moment"):
		return "Cloudflare challenge: set FlareSolverr in Options"
	case strings.Contains(lower, "no torrent link") || strings.Contains(lower, "without torrent links"):
		return "The page returned no items: it may be an empty listing, or an address that is not a listing"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "The server took too long: retry, or check its address"
	}
	return ""
}

// indexerPageURL is the web page of an indexer: the scheme and host of its
// Torznab URL, where Prowlarr, Jackett and the other managers serve their UI
// (the API path below it is not a page). Empty for an invalid URL.
func indexerPageURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

// orDefault returns value when it is not blank, otherwise fallback.
func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// parseFormIndex reads a non-negative integer from a form value.
func parseFormIndex(raw string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(raw))
}
