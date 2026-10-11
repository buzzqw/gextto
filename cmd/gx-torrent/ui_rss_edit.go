package main

// ui_rss_edit.go adds the friendly indexer/feed editors on top of the JSON
// settings of the standalone RSS page: small forms that append to or remove
// from the `indexers` and `feeds` arrays, so the operator does not have to hand
// edit JSON. The raw settings stay the source of truth and remain editable in
// the page's advanced textareas (/ui/rss-config).

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
// Prowlarr and Jackett are Torznab managers with well-known ports; any other
// Torznab endpoint (a MirCrew indexer, a remote manager) is added as a custom
// name and URL. The wizard template repeats these values by hand: keep them in
// sync with setup.html.
var uiIndexerPresets = []uiIndexerPreset{
	{Name: "Prowlarr", URL: "http://127.0.0.1:9696"},
	{Name: "Jackett", URL: "http://127.0.0.1:9117"},
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
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
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
	default:
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
		}
		if feed.URL == "" && feed.Search == "" {
			d.renderRSS(w, r, "Give the feed an RSS URL or a search query.")
			return
		}
		if feed.URL != "" && !validIndexerURL(feed.URL) {
			d.renderRSS(w, r, "The feed URL must start with http:// or https://.")
			return
		}
		list = append(list, feed)
	}
	if err := d.saveFeeds(list); err != nil {
		d.renderRSS(w, r, "Cannot save the feeds: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
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
		list = append(list, rule)
	}
	if err := d.saveRules(list); err != nil {
		d.renderRSS(w, r, "Cannot save the rules: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
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
	if len([]rune(query)) < 2 || len(d.indexers()) == 0 {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, _ := d.searchFeedSuggest(ctx, query)
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

// handleUIRssFollow creates, in one step, a Torznab search feed for a title
// and a rule scoped to it, so the operator "follows" a series or a movie: the
// rule adds the matches with the chosen destination/category (and, for a
// series, the smart-episode tracking). Once any rule exists it governs every
// feed, which the page warns about.
func (d *Daemon) handleUIRssFollow(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		d.renderRSS(w, r, "Give the title to follow.")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	if kind != "movie" {
		kind = "series"
	}
	include := splitList(r.FormValue("include"))
	exclude := splitList(r.FormValue("exclude"))
	episode := strings.TrimSpace(r.FormValue("episode"))
	folder := strings.TrimSpace(r.FormValue("folder"))
	savePath := strings.TrimSpace(r.FormValue("savepath"))
	category := strings.TrimSpace(r.FormValue("category"))
	if _, err := rss.ParseEpisodeFilter(episode); err != nil {
		d.renderRSS(w, r, err.Error())
		return
	}

	// The search feed: query the configured indexers for the title.
	feeds := d.feeds()
	feed := feedConfig{Name: title, Search: title, Folder: folder}
	replaced := false
	for i := range feeds {
		if strings.EqualFold(feeds[i].Name, feed.Name) {
			feeds[i] = feed
			replaced = true
			break
		}
	}
	if !replaced {
		feeds = append(feeds, feed)
	}
	if err := d.saveFeeds(feeds); err != nil {
		d.renderRSS(w, r, "Cannot save the feed: "+err.Error())
		return
	}

	// The scoped rule: adds the matches with the action, and for a series keeps
	// the smart-episode tracking. The title itself is required (as a tolerant
	// regex), so fuzzy indexer results for common words do not slip in; a
	// followed movie downloads once.
	rule := rss.Rule{
		Name:  title,
		Feeds: []string{feed.Name},
		Match: rss.Criteria{
			Include:       include,
			Exclude:       exclude,
			Regex:         followTitleRegex(title),
			EpisodeFilter: episode,
		},
		Action: rss.Action{
			SavePath: savePath,
			Category: category,
		},
	}
	if kind == "series" {
		rule.Match.RequireEpisode = true
		rule.Match.SmartEpisode = true
	} else {
		rule.Action.Once = true
	}
	rules := d.rules()
	replaced = false
	for i := range rules {
		if strings.EqualFold(rules[i].Name, rule.Name) {
			rules[i] = rule
			replaced = true
			break
		}
	}
	if !replaced {
		rules = append(rules, rule)
	}
	if err := d.saveRules(rules); err != nil {
		d.renderRSS(w, r, "Cannot save the rule: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
}

// followTitleRegex turns a followed title into a case-insensitive regex that
// requires the title's words, in order and separated by any of the usual
// separators (. _ - space). Without it a rule built only from the optional
// "include" words also matches unrelated releases returned by fuzzy indexer
// queries (e.g. any release carrying "1080p").
func followTitleRegex(title string) string {
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

// uiRuleFromForm builds one rule from the add-rule form, validating the regexes
// and the episode filter.
func uiRuleFromForm(r *http.Request) (rss.Rule, error) {
	enabled := formBool(r, "enabled")
	rule := rss.Rule{
		Name:       strings.TrimSpace(r.FormValue("name")),
		Feeds:      splitList(r.FormValue("feeds")),
		Enabled:    &enabled,
		IgnoreDays: int(formInt(r, "ignore_days", 0)),
		Fail:       formBool(r, "fail"),
		Match: rss.Criteria{
			Include:        splitList(r.FormValue("include")),
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

// uiRuleRowsHTML renders the ordered rules as table rows, each with up/down and
// remove buttons (the order is the precedence).
func (d *Daemon) uiRuleRowsHTML() string {
	list := d.rules()
	if len(list) == 0 {
		return `<tr><td colspan="5" class="muted">None</td></tr>`
	}
	var b strings.Builder
	for index, rule := range list {
		kind, kindClass := "Add", "flag on"
		if rule.Fail {
			kind, kindClass = "Skip", "flag"
		}
		scope := "All"
		if len(rule.Feeds) > 0 {
			scope = strings.Join(rule.Feeds, ", ")
		}
		flags := `<span class="` + kindClass + `">` + kind + `</span>`
		if !rss.RuleEnabled(rule) {
			flags += ` <span class="flag">Disabled</span>`
		}
		b.WriteString("<tr><td>" + html.EscapeString(orDefault(rule.Name, "—")) + "</td>")
		b.WriteString("<td>" + flags + ` <span class="muted">` + html.EscapeString(scope) + "</span></td>")
		b.WriteString(`<td class="mono">` + html.EscapeString(uiRuleMatchSummary(rule)) + "</td>")
		b.WriteString(`<td class="mono">` + html.EscapeString(uiRuleActionSummary(rule)) + "</td>")
		b.WriteString(`<td class="actions"><form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="up"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="sm">↑</button></form>`)
		b.WriteString(`<form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="down"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="sm">↓</button></form>`)
		b.WriteString(`<form method="post" action="/ui/rss-rule"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="danger">Remove</button></form></td></tr>`)
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
		return `<tr><td colspan="4" class="muted">None</td></tr>`
	}
	var b strings.Builder
	for index, indexer := range list {
		b.WriteString("<tr><td>" + html.EscapeString(orDefault(indexer.Name, "—")) + "</td>")
		b.WriteString(`<td class="mono">` + html.EscapeString(indexer.URL) + "</td>")
		keyState := "—"
		if strings.TrimSpace(indexer.APIKey) != "" {
			keyState = "set"
		}
		b.WriteString("<td>" + keyState + "</td>")
		b.WriteString(`<td class="actions">`)
		b.WriteString(`<button type="button" class="sm" data-idx-edit data-index="` + strconv.Itoa(index) + `" data-name="` + html.EscapeString(indexer.Name) + `" data-url="` + html.EscapeString(indexer.URL) + `">Edit</button> `)
		b.WriteString(`<button type="button" class="sm" data-idx-test data-index="` + strconv.Itoa(index) + `">Test</button> `)
		b.WriteString(`<span class="muted" data-idx-status="` + strconv.Itoa(index) + `"></span> `)
		b.WriteString(`<form method="post" action="/ui/rss-indexer"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="danger">Remove</button></form></td></tr>`)
	}
	return b.String()
}

// uiFeedRowsHTML renders the configured feeds as table rows, each with a remove
// button; the row index is the removal key. The rows are grouped by folder.
func (d *Daemon) uiFeedRowsHTML() string {
	list := d.feeds()
	if len(list) == 0 {
		return `<tr><td colspan="7" class="muted">None</td></tr>`
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
	for _, row := range rows {
		feed := row.feed
		source := strings.TrimSpace(feed.URL)
		if source == "" && strings.TrimSpace(feed.Search) != "" {
			source = "search: " + feed.Search
		}
		state := "active"
		if feed.Paused {
			state = "paused"
		}
		b.WriteString("<tr><td>" + html.EscapeString(orDefault(feed.Name, "—")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(feed.Folder, "—")) + "</td>")
		b.WriteString(`<td class="mono">` + html.EscapeString(source) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(feed.Include, "—")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(feed.Exclude, "—")) + "</td>")
		b.WriteString("<td>" + state + "</td>")
		b.WriteString(`<td><form method="post" action="/ui/rss-feed"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(row.index) + `"><button type="submit" class="danger">Remove</button></form></td></tr>`)
	}
	return b.String()
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
