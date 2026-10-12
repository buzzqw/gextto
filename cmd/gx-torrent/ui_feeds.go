package main

// ui_feeds.go exposes the RSS feeds to the standalone page: the status, the
// items matching the rules, and a small management page to edit feeds, rules
// and indexers. Managed mode has no feeds (Gextto owns acquisition), so every
// endpoint is refused.

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

func (d *Daemon) handleUIFeeds(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, d.feedStatusesSnapshot())
}

func (d *Daemon) handleUIFeedPoll(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	// One feed (the reload button of its row) is polled at once and its new
	// status returned, so the row can show the outcome.
	if name := strings.TrimSpace(r.FormValue("feed")); name != "" {
		if !d.pollFeedsOnly(name) {
			writeError(w, http.StatusConflict, errors.New("a feed check is already running"))
			return
		}
		for _, status := range d.feedStatusesSnapshot() {
			if status.Name == name || status.URL == name {
				writeJSON(w, http.StatusOK, status)
				return
			}
		}
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	res := d.pollFeedsAll()
	if !res.OK {
		writeJSON(w, http.StatusConflict, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleUIRssSettings saves the RSS page's global options: the polling interval
// (minutes) and the archive retention (days, 0 = keep everything).
func (d *Daemon) handleUIRssSettings(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	minutes, err := strconv.Atoi(strings.TrimSpace(r.FormValue("interval")))
	if err != nil || minutes < 1 || minutes > 24*60 {
		d.renderRSS(w, r, "The refresh interval must be between 1 and 1440 minutes.")
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.FormValue("archive_days")))
	if err != nil || days < 0 || days > 3650 {
		d.renderRSS(w, r, "The archive retention must be between 0 and 3650 days.")
		return
	}
	d.opts.Settings.Set(feedIntervalKey, strconv.Itoa(minutes*60))
	d.opts.Settings.Set(archiveDaysKey, strconv.Itoa(days))
	if err := d.opts.Settings.Save(); err != nil {
		d.renderRSS(w, r, "Cannot save settings: "+err.Error())
		return
	}
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
}

// feedItemView is one feed item annotated with the rule decision.
type feedItemView struct {
	Title    string `json:"title"`
	Size     int64  `json:"size"`
	Seeders  int    `json:"seeders"`
	Leechers int    `json:"leechers"`
	Download string `json:"download"`
	Rule     string `json:"rule"`
	Pass     bool   `json:"pass"`
	Seen     bool   `json:"seen"`
}

// handleUIFeedItems fetches one feed now and returns its items with the rule
// that would add each (the "matching articles" view of qBittorrent).
func (d *Daemon) handleUIFeedItems(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("feed"))
	var chosen *feedConfig
	for _, feed := range d.feeds() {
		if feed.Name == name || feed.URL == name {
			copy := feed
			chosen = &copy
			break
		}
	}
	if chosen == nil {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	feedName := chosen.Name
	if feedName == "" {
		feedName = chosen.URL
	}
	ctx, cancel := context.WithTimeout(r.Context(), feedRequestTimeout)
	items, err := rss.Fetch(ctx, &http.Client{Timeout: feedRequestTimeout}, chosen.URL, feedName)
	cancel()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rules := d.rules()
	now := time.Now()
	out := make([]feedItemView, 0, len(items))
	for _, item := range items {
		view := feedItemView{Title: item.Title, Size: item.Size, Seeders: item.Seeders, Leechers: item.Leechers, Download: item.Download(), Seen: d.feedSeen(feedName, item.GUID)}
		if len(rules) > 0 {
			rule, pass, matched := rss.Evaluate(rules, feedName, item, now)
			view.Rule = rule.Name
			view.Pass = matched && pass
		} else {
			view.Pass = chosen.rule().Match(item.Title)
		}
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleUIRss renders the RSS management page.
func (d *Daemon) handleUIRss(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	d.renderRSS(w, r, "")
}

// handleUIRssConfig validates and saves the feeds, rules and indexers edited in
// the RSS page.
func (d *Daemon) handleUIRssConfig(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	feedsRaw := strings.TrimSpace(r.FormValue("feeds"))
	rulesRaw := strings.TrimSpace(r.FormValue("rules"))
	indexersRaw := strings.TrimSpace(r.FormValue("indexers"))

	if feedsRaw != "" {
		var feeds []feedConfig
		if err := json.Unmarshal([]byte(feedsRaw), &feeds); err != nil {
			d.renderRSS(w, r, "Invalid feeds JSON: "+err.Error())
			return
		}
		d.opts.Settings.Set(feedsSettingKey, feedsRaw)
	}
	if rulesRaw != "" {
		var rules []rss.Rule
		if err := json.Unmarshal([]byte(rulesRaw), &rules); err != nil {
			d.renderRSS(w, r, "Invalid rules JSON: "+err.Error())
			return
		}
		d.opts.Settings.Set(rulesSettingKey, rulesRaw)
	}
	if indexersRaw != "" {
		var indexers []torznab.Indexer
		if err := json.Unmarshal([]byte(indexersRaw), &indexers); err != nil {
			d.renderRSS(w, r, "Invalid indexers JSON: "+err.Error())
			return
		}
		d.opts.Settings.Set(indexersSettingKey, indexersRaw)
	}
	if err := d.opts.Settings.Save(); err != nil {
		d.renderRSS(w, r, "Cannot save settings: "+err.Error())
		return
	}
	// A feed saved now must be checked now, not only after a restart or the
	// manual button.
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
}

// renderRSS writes the RSS page, localized like the daemon page.
func (d *Daemon) renderRSS(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.RSSPage()
	page = strings.Replace(page, "{{FEEDS}}", html.EscapeString(orEmptyJSON(d.opts.Settings.Get(feedsSettingKey, ""))), 1)
	page = strings.Replace(page, "{{RULES}}", html.EscapeString(orEmptyJSON(d.opts.Settings.Get(rulesSettingKey, ""))), 1)
	page = strings.Replace(page, "{{INDEXERS}}", html.EscapeString(orEmptyJSON(d.opts.Settings.Get(indexersSettingKey, ""))), 1)
	page = strings.Replace(page, "{{INDEXERROWS}}", d.uiIndexerRowsHTML(), 1)
	page = strings.Replace(page, "{{FEEDROWS}}", d.uiFeedRowsHTML(), 1)
	page = strings.Replace(page, "{{INDEXEROPTIONS}}", uiIndexerOptionsHTML(), 1)
	page = strings.Replace(page, "{{INDEXERCOUNT}}", strconv.Itoa(len(d.indexers())), 1)
	page = strings.Replace(page, "{{FEEDCOUNT}}", strconv.Itoa(len(d.feeds())), 1)
	page = strings.Replace(page, "{{RULEROWS}}", d.uiRuleRowsHTML(), 1)
	page = strings.Replace(page, "{{RULECOUNT}}", strconv.Itoa(len(d.rules())), 1)
	page = strings.Replace(page, "{{FEEDINTERVAL}}", strconv.Itoa(int(d.feedInterval()/time.Minute)), 1)
	page = strings.Replace(page, "{{ARCHIVEDAYS}}", strconv.Itoa(d.archiveRetentionDays()), 1)
	nextPoll := "—"
	if next := d.nextFeedPoll(); !next.IsZero() {
		nextPoll = next.Format("15:04")
	}
	page = strings.Replace(page, "{{NEXTPOLL}}", nextPoll, 1)

	names := make([]string, 0, 8)
	for _, feed := range d.feeds() {
		name := feed.Name
		if name == "" {
			name = feed.URL
		}
		names = append(names, name)
	}
	namesJSON, _ := json.Marshal(names)
	page = strings.Replace(page, "{{FEEDNAMES}}", string(namesJSON), 1)

	errorHTML := ""
	if errKey != "" {
		errorHTML = `<p class="err">` + html.EscapeString(errKey) + `</p>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)

	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	page = uiInjectClientDictionary(page, lang)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func orEmptyJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[]"
	}
	return value
}
