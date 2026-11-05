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
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
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
				status.Total = d.archive().feedCounts()[name]
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
		d.renderRSS(w, r, "The check interval must be between 1 and 1440 minutes.")
		return
	}
	days, err := strconv.Atoi(strings.TrimSpace(r.FormValue("archive_days")))
	if err != nil || days < 0 || days > 3650 {
		d.renderRSS(w, r, "The archive retention must be between 0 and 3650 days.")
		return
	}
	d.opts.Settings.Set(feedIntervalKey, strconv.Itoa(minutes*60))
	d.opts.Settings.Set(archiveDaysKey, strconv.Itoa(days))
	// Only set FlareSolverr when the field was submitted, so a form that does not
	// carry it does not clear the configured URL.
	if _, ok := r.Form["flaresolverr"]; ok {
		d.opts.Settings.Set(flareURLKey, strings.TrimSpace(r.FormValue("flaresolverr")))
	}
	if err := d.opts.Settings.Save(); err != nil {
		d.renderRSS(w, r, "Cannot save settings: "+err.Error())
		return
	}
	http.Redirect(w, r, "/ui/rss#options", http.StatusSeeOther)
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
	// Base is the title reduced to what a rule would be named after (the series
	// name, or the release's base name): the page uses it to build a rule from
	// the item (qBittorrent's "add to my rules").
	Base string `json:"base"`
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
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	var chosen *feedConfig
	for _, feed := range d.feeds() {
		if feed.Name == name || feed.URL == name {
			copy := feed
			chosen = &copy
			break
		}
	}
	if chosen == nil {
		// The feed editor tests a URL before saving it: fetch it like a
		// configured feed, without storing anything.
		if rawURL == "" {
			writeError(w, http.StatusNotFound, errNotFound)
			return
		}
		if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") && !strings.HasPrefix(strings.ToLower(rawURL), "dht://") {
			writeError(w, http.StatusBadRequest, errors.New("the URL must start with http://, https:// or dht://"))
			return
		}
		tmp := feedConfig{Name: rawURL, URL: rawURL}
		chosen = &tmp
	}
	feedName := chosen.Name
	if feedName == "" {
		feedName = chosen.URL
	}
	ctx, cancel := context.WithTimeout(r.Context(), feedRequestTimeout)
	items, err := d.fetchFeedItems(ctx, outboundClient(feedRequestTimeout), chosen.URL, feedName)
	cancel()
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	rules := d.rules()
	now := time.Now()
	out := make([]feedItemView, 0, len(items))
	for _, item := range items {
		view := feedItemView{Title: item.Title, Size: item.Size, Seeders: item.Seeders, Leechers: item.Leechers, Download: item.Download(), Seen: d.feedSeen(feedName, item.GUID), Base: previewTitleKey(item.Title)}
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
	// The page imports a whole setup as a file exported by /ui/rss-export,
	// instead of having the operator edit the JSON by hand.
	if file, _, err := r.FormFile("setup"); err == nil {
		raw, readErr := io.ReadAll(io.LimitReader(file, 8<<20))
		file.Close()
		var setup rssSetupFile
		if readErr != nil || json.Unmarshal(raw, &setup) != nil {
			d.renderRSS(w, r, "The file is not a gx-torrent RSS setup.")
			return
		}
		feedsRaw, rulesRaw, indexersRaw = setup.raw(setup.Feeds), setup.raw(setup.Rules), setup.raw(setup.Indexers)
	}

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
	http.Redirect(w, r, "/ui/rss#options", http.StatusSeeOther)
}

// rssSetupFile is the exported RSS setup: feeds, rules and indexers as they
// are stored in settings.json.
type rssSetupFile struct {
	Feeds    json.RawMessage `json:"feeds,omitempty"`
	Rules    json.RawMessage `json:"rules,omitempty"`
	Indexers json.RawMessage `json:"indexers,omitempty"`
}

// raw is one section as stored text; empty (left unchanged) when absent.
func (rssSetupFile) raw(section json.RawMessage) string {
	text := strings.TrimSpace(string(section))
	if text == "null" {
		return ""
	}
	return text
}

// handleUIRssExport downloads the feeds, rules and indexers as one JSON file,
// to copy a setup to another gx-torrent or keep it as a backup.
func (d *Daemon) handleUIRssExport(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	setup := rssSetupFile{
		Feeds:    json.RawMessage(orEmptyJSON(d.opts.Settings.Get(feedsSettingKey, ""))),
		Rules:    json.RawMessage(orEmptyJSON(d.opts.Settings.Get(rulesSettingKey, ""))),
		Indexers: json.RawMessage(orEmptyJSON(d.opts.Settings.Get(indexersSettingKey, ""))),
	}
	raw, err := json.MarshalIndent(setup, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="gx-torrent-rss-setup.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}

// renderRSS writes the RSS page, localized like the daemon page.
func (d *Daemon) renderRSS(w http.ResponseWriter, r *http.Request, errKey string) {
	d.renderRSSPage(w, r, errKey, "")
}

// renderRSSPage writes the RSS page with the error and the neutral notice (the
// outcome of an action, e.g. how many feeds a pasted list added). Both are shown
// at the top of the page; noticeHTML is already-built markup.
func (d *Daemon) renderRSSPage(w http.ResponseWriter, r *http.Request, errKey, noticeHTML string) {
	lang := d.uiLang(r)
	nextPoll := "—"
	if next := d.nextFeedPoll(); !next.IsZero() {
		nextPoll = next.Format("15:04")
	}
	names := make([]string, 0, 8)
	for _, feed := range d.feeds() {
		name := feed.Name
		if name == "" {
			name = feed.URL
		}
		names = append(names, name)
	}
	namesJSON, _ := json.Marshal(names)
	errorHTML := template.HTML("")
	if errKey != "" {
		errorHTML = template.HTML(`<div class="notice err" role="alert">` + html.EscapeString(uiTr(lang, errKey)) + `</div>`)
	}
	d.uiWritePage(w, r, "rss", map[string]any{
		"INDEXERROWS":    template.HTML(d.uiIndexerRowsHTML(lang)),
		"FEEDROWS":       template.HTML(d.uiFeedRowsHTML(lang)),
		"INDEXEROPTIONS": template.HTML(uiIndexerOptionsHTML(lang)),
		"RULEFEEDS":      template.HTML(d.uiRuleFeedsHTML(lang)),
		"RULEROWS":       template.HTML(d.uiRuleRowsHTML(lang)),
		"INDEXERCOUNT":   strconv.Itoa(len(d.indexers())),
		"FEEDCOUNT":      strconv.Itoa(len(d.feeds())),
		"RULECOUNT":      strconv.Itoa(len(d.rules())),
		"FEEDINTERVAL":   strconv.Itoa(int(d.feedInterval() / time.Minute)),
		"ARCHIVEDAYS":    strconv.Itoa(d.archiveRetentionDays()),
		"FLARESOLVERR":   html.EscapeString(strings.TrimSpace(d.opts.Settings.Get(flareURLKey, ""))),
		"NEXTPOLL":       nextPoll,
		"FEEDNAMES":      template.JS(namesJSON),
		"NOTICE":         template.HTML(noticeHTML),
		"ERROR":          errorHTML,
	})
}

func orEmptyJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[]"
	}
	return value
}
