package main

// ui_rss_bulk.go adds several feeds at once from a pasted list: the operator
// copies the addresses of many feeds (a pile of RSS URLs, the pages of ext.to or
// TorrentGalaxy listings of several uploaders) into one textarea instead of
// filling the feed form once per feed. The lines that are not usable feed
// addresses, and the ones already configured, are skipped and reported.

import (
	"html"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// bulkFeedLines splits pasted text into the lines worth reading: empty lines and
// "#" comments are dropped, and the surrounding space of each one is trimmed.
func bulkFeedLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// bulkFeedNameURL reads one line: a name and a URL when the line carries both,
// otherwise the whole line is the URL. The two are separated by "|" only when
// what follows it is a URL again, because a listing address can itself contain
// "|" (the tag filters of an RSS-Bridge URL, for example).
func bulkFeedNameURL(line string) (name, rawURL string) {
	if before, after, ok := strings.Cut(line, "|"); ok {
		after = strings.TrimSpace(after)
		if validFeedURL(after) {
			return strings.TrimSpace(before), after
		}
	}
	return "", strings.TrimSpace(line)
}

// feedNameFromURL derives a readable name for a pasted URL, so a list can be
// added without typing 30 names: the site, followed by what tells its listings
// apart (the user or category of a browse page, a query, the last path
// segment). It is a starting point the operator can change with Edit.
func feedNameFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	detail := feedURLDetail(parsed)
	switch {
	case detail == "":
		return orDefault(host, raw)
	case host == "":
		return detail
	default:
		return uiTruncate(host+" "+detail, 70)
	}
}

// feedURLDetail is the part of an address that makes one listing different from
// the next: the values of the browse filters (filter=u=<user>, filter=c=<category>,
// filter=tg=<language>), a search query, or the last meaningful path segment.
func feedURLDetail(parsed *url.URL) string {
	query := parsed.Query()
	var parts []string
	for _, filter := range query["filter"] {
		if _, value, ok := strings.Cut(filter, "="); ok {
			filter = value
		}
		if strings.TrimSpace(filter) != "" {
			parts = append(parts, strings.TrimSpace(filter))
		}
	}
	if len(parts) == 0 && strings.TrimSpace(query.Get("q")) != "" {
		parts = append(parts, strings.TrimSpace(query.Get("q")))
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.TrimSpace(segments[i]) == "" {
			continue
		}
		// Only the last named segment is the listing; an earlier one ("ita", a
		// date folder) is not more telling than the site itself.
		return feedNameFromPathSegment(segments[i])
	}
	return ""
}

// feedNameFromPathSegment turns one path segment into a name: "user:MIRCrewRS"
// becomes "MIRCrewRS", "feed.rss" becomes "feed", and the tag filters of an
// RSS-Bridge listing become readable words. The generic segments of a listing
// page give nothing, so the site name alone is used.
func feedNameFromPathSegment(segment string) string {
	name := strings.TrimSpace(segment)
	if _, after, ok := strings.Cut(name, ":"); ok {
		name = after
	}
	if ext := path.Ext(name); ext != "" && len(ext) <= 5 {
		name = strings.TrimSuffix(name, ext)
	}
	name = strings.TrimSpace(strings.NewReplacer("|", " ", "+", " ", "_", " ").Replace(name))
	switch strings.ToLower(name) {
	case "", "browse", "index", "feed", "rss", "api", "search":
		return ""
	}
	return name
}

// uniqueFeedName keeps the derived names distinct: the name is what the rules
// use to address a feed, and two uploaders of different sites can share one
// ("metalhit" of ext.to and of TorrentGalaxy).
func uniqueFeedName(name string, taken map[string]bool) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "feed"
	}
	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = name + " " + strconv.Itoa(n)
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

// uiTruncate cuts a string to at most limit runes, adding an ellipsis.
func uiTruncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// uiRssAddFeedsBulk adds every address of the pasted list as a feed, sharing the
// options of the feed form (folder, filter, category, paused, disabled, take
// everything). A line already configured (same address) and a line that is not a
// usable feed address are skipped; the page then says how many feeds were added
// and which lines were left out.
func (d *Daemon) uiRssAddFeedsBulk(w http.ResponseWriter, r *http.Request, list []feedConfig, raw string) {
	template := feedConfig{
		Folder:   strings.TrimSpace(r.FormValue("folder")),
		Include:  strings.TrimSpace(r.FormValue("include")),
		Exclude:  strings.TrimSpace(r.FormValue("exclude")),
		Category: strings.TrimSpace(r.FormValue("category")),
		SavePath: strings.TrimSpace(r.FormValue("savepath")),
		Paused:   r.FormValue("paused") != "",
		Disabled: r.FormValue("disabled") != "",
		All:      r.FormValue("all") != "",
	}

	addresses := map[string]bool{}
	names := map[string]bool{}
	for _, feed := range list {
		if address := strings.ToLower(strings.TrimSpace(feed.URL)); address != "" {
			addresses[address] = true
		}
		if feed.Name != "" {
			names[strings.ToLower(feed.Name)] = true
		}
	}

	added, skipped := 0, 0
	var unusable []string
	for _, line := range bulkFeedLines(raw) {
		name, address := bulkFeedNameURL(line)
		if !validFeedURL(address) {
			unusable = append(unusable, line)
			skipped++
			continue
		}
		key := strings.ToLower(address)
		if addresses[key] {
			skipped++
			continue
		}
		addresses[key] = true
		feed := template
		feed.URL = address
		if name == "" {
			name = feedNameFromURL(address)
		}
		feed.Name = uniqueFeedName(name, names)
		list = append(list, feed)
		added++
	}

	if added == 0 && skipped == 0 {
		d.renderRSS(w, r, "No feed address was found in the pasted text.")
		return
	}
	if added > 0 {
		if err := d.saveFeeds(list); err != nil {
			d.renderRSS(w, r, "Cannot save the feeds: "+err.Error())
			return
		}
		// The new feeds must be checked now, not only after a restart.
		d.pokeFeeds()
	}
	if skipped == 0 {
		http.Redirect(w, r, "/ui/rss#feeds", http.StatusSeeOther)
		return
	}
	d.renderRSSPage(w, r, "", feedBulkNoticeHTML(added, skipped, unusable))
}

// feedBulkNoticeHTML is the outcome of a pasted list: how many feeds were added,
// how many lines were skipped, and the lines that are not usable addresses. The
// words are catalog keys, so the page translates them like the rest.
func feedBulkNoticeHTML(added, skipped int, unusable []string) string {
	var b strings.Builder
	b.WriteString(`<div class="notice" role="status">`)
	if added > 0 {
		b.WriteString(`<b>` + strconv.Itoa(added) + `</b> <span>feeds added</span>`)
	} else {
		b.WriteString(`<span>No feed added</span>`)
	}
	if skipped > 0 {
		b.WriteString(` · <b>` + strconv.Itoa(skipped) + `</b> <span>skipped</span>`)
	}
	b.WriteString(`</div>`)
	if len(unusable) > 0 {
		b.WriteString(`<div class="notice err" role="alert"><span>These lines are not feed addresses:</span> <code>` + html.EscapeString(strings.Join(unusable, " · ")) + `</code></div>`)
	}
	return b.String()
}
