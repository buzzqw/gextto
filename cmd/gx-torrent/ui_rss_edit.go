package main

// ui_rss_edit.go adds the friendly indexer/feed editors on top of the JSON
// settings of the standalone RSS page: small forms that append to or remove
// from the `indexers` and `feeds` arrays, so the operator does not have to hand
// edit JSON. The raw settings stay the source of truth and remain editable in
// the page's advanced textareas (/ui/rss-config).

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
	if err := d.saveIndexers(list); err != nil {
		d.renderRSS(w, r, "Cannot save the indexers: "+err.Error())
		return
	}
	d.pokeFeeds()
	http.Redirect(w, r, "/ui/rss", http.StatusSeeOther)
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
		b.WriteString(`<td><form method="post" action="/ui/rss-indexer"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="danger">Remove</button></form></td></tr>`)
	}
	return b.String()
}

// uiFeedRowsHTML renders the configured feeds as table rows, each with a remove
// button; the row index is the removal key.
func (d *Daemon) uiFeedRowsHTML() string {
	list := d.feeds()
	if len(list) == 0 {
		return `<tr><td colspan="6" class="muted">None</td></tr>`
	}
	var b strings.Builder
	for index, feed := range list {
		source := strings.TrimSpace(feed.URL)
		if source == "" && strings.TrimSpace(feed.Search) != "" {
			source = "search: " + feed.Search
		}
		state := "active"
		if feed.Paused {
			state = "paused"
		}
		b.WriteString("<tr><td>" + html.EscapeString(orDefault(feed.Name, "—")) + "</td>")
		b.WriteString(`<td class="mono">` + html.EscapeString(source) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(feed.Include, "—")) + "</td>")
		b.WriteString("<td>" + html.EscapeString(orDefault(feed.Exclude, "—")) + "</td>")
		b.WriteString("<td>" + state + "</td>")
		b.WriteString(`<td><form method="post" action="/ui/rss-feed"><input type="hidden" name="op" value="remove"><input type="hidden" name="index" value="` + strconv.Itoa(index) + `"><button type="submit" class="danger">Remove</button></form></td></tr>`)
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
