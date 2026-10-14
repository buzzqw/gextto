package main

// ui_archive.go is the standalone Archive page: it searches everything the
// feeds returned (feed_archive.go), adds a release, and deletes entries. The
// rule preview of the RSS page runs on the same data, so it shows at once what
// a rule would take without fetching any feed.

import (
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

const (
	// rulePreviewScan bounds how many recent archive entries a preview tests.
	rulePreviewScan = 5000
	rulePreviewMax  = 100
)

// archiveAvailable gates the archive endpoints (standalone only).
func (d *Daemon) archiveAvailable(w http.ResponseWriter, r *http.Request) bool {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return false
	}
	return d.uiAuthorized(w, r)
}

// handleUIArchive renders the Archive page.
func (d *Daemon) handleUIArchive(w http.ResponseWriter, r *http.Request) {
	if !d.archiveAvailable(w, r) {
		return
	}
	page := assets.ArchivePage()
	stats := d.archive().stats(true)
	var options strings.Builder
	for _, feed := range stats.Feeds {
		// The wanted titles' indexer results are an internal cache, not a feed
		// the operator configured: they stay searchable under "all feeds".
		if strings.HasPrefix(feed, wantedFeedPrefix) {
			continue
		}
		options.WriteString(`<option value="` + html.EscapeString(feed) + `">` + html.EscapeString(feed) + `</option>`)
	}
	page = strings.Replace(page, "{{FEEDOPTIONS}}", options.String(), 1)
	page = strings.Replace(page, "{{ARCHIVE_DAYS}}", strconv.Itoa(d.archiveRetentionDays()), 1)
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

// archiveItemView is one search result with what the page derives from the
// title (resolution, episode) and whether the torrent is in the session.
type archiveItemView struct {
	archiveEntry
	Quality   string `json:"quality,omitempty"`
	Episode   string `json:"episode,omitempty"`
	InSession bool   `json:"in_session,omitempty"`
}

// handleUIArchiveItems returns one page of search results as JSON, with the
// archive size.
func (d *Daemon) handleUIArchiveItems(w http.ResponseWriter, r *http.Request) {
	if !d.archiveAvailable(w, r) {
		return
	}
	query := r.URL.Query()
	q := parseArchiveQuery(query.Get("q"), query.Get("feed"), query.Get("added") == "1")
	pageNum, _ := strconv.Atoi(query.Get("page"))
	result, err := d.archive().search(q, query.Get("sort"), pageNum, archivePageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	inSession := d.sessionInfoHashes()
	items := make([]archiveItemView, 0, len(result.Items))
	for _, entry := range result.Items {
		view := archiveItemView{archiveEntry: entry, Quality: titleQuality(entry.Title)}
		if _, season, episode, ok := rss.FindEpisode(entry.Title); ok {
			view.Episode = "S" + twoDigits(season) + "E" + twoDigits(episode)
		}
		if hash, ok := magnetInfoHash(entry.Download); ok && inSession[hash] {
			view.InSession = true
		}
		items = append(items, view)
	}
	stats := d.archive().stats(false)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"total":      result.Total,
		"page":       result.Page,
		"pages":      result.Pages,
		"entries":    stats.Entries,
		"size_bytes": stats.SizeBytes,
	})
}

// handleUIArchiveAdd adds one archived release to the session and flags it as
// downloaded.
func (d *Daemon) handleUIArchiveAdd(w http.ResponseWriter, r *http.Request) {
	if !d.archiveAvailable(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	entry, ok := d.archive().get(r.FormValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	item := rss.Item{Title: entry.Title}
	if strings.HasPrefix(entry.Download, "magnet:") {
		item.Magnet = entry.Download
	} else {
		item.Link = entry.Download
	}
	if _, err := d.addFeedItem(item, rss.Action{}); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	d.archive().markAdded(entry.Feed, item)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUIArchiveDelete deletes the given entries (id, repeatable) or, with
// all=1, every entry matching the search (q, feed, added); an empty search is
// refused. days=N instead removes the entries older than N days.
func (d *Daemon) handleUIArchiveDelete(w http.ResponseWriter, r *http.Request) {
	if !d.archiveAvailable(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	archive := d.archive()
	var removed int
	var err error
	switch {
	case strings.TrimSpace(r.FormValue("days")) != "":
		days, convErr := strconv.Atoi(strings.TrimSpace(r.FormValue("days")))
		if convErr != nil || days < 1 {
			writeError(w, http.StatusBadRequest, errors.New("days must be a positive number"))
			return
		}
		removed, err = archive.prune(time.Now().Add(-time.Duration(days)*24*time.Hour), archiveMaxEntries)
	case r.FormValue("all") == "1":
		removed, err = archive.deleteMatching(parseArchiveQuery(r.FormValue("q"), r.FormValue("feed"), r.FormValue("added") == "1"))
	default:
		removed, err = archive.deleteIDs(r.Form["id"])
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// handleUIRssRulePreview tests the rule in the form (saved or not) against the
// most recent archived items and returns the ones it matches: the "matching
// articles" view of qBittorrent, without fetching any feed.
func (d *Daemon) handleUIRssRulePreview(w http.ResponseWriter, r *http.Request) {
	if !d.archiveAvailable(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	rule, err := uiRuleFromForm(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// The preview shows what the rule matches even while it is disabled.
	rule.Enabled = nil
	result, err := d.archive().search(archiveQuery{}, "new", 1, rulePreviewScan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now()
	type match struct {
		Title string `json:"title"`
		Feed  string `json:"feed"`
		Size  int64  `json:"size,omitempty"`
		Added bool   `json:"added,omitempty"`
	}
	matches := []match{}
	for _, entry := range result.Items {
		item := rss.Item{Title: entry.Title, Size: entry.Size, Seeders: entry.Seeders, Leechers: entry.Leechers, Magnet: entry.Download}
		if entry.Published > 0 {
			item.Published = time.Unix(entry.Published, 0)
		} else {
			item.Published = time.Unix(entry.FirstSeen, 0)
		}
		if _, _, matched := rss.Evaluate([]rss.Rule{rule}, entry.Feed, item, now); !matched {
			continue
		}
		matches = append(matches, match{Title: entry.Title, Feed: entry.Feed, Size: entry.Size, Added: entry.Added})
		if len(matches) >= rulePreviewMax {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches, "scanned": len(result.Items)})
}

// sessionInfoHashes is the set of v1 info-hashes in the session.
func (d *Daemon) sessionInfoHashes() map[string]bool {
	out := map[string]bool{}
	for _, view := range d.snapshotViews() {
		out[strings.ToLower(view.Hash)] = true
	}
	return out
}

// titleQuality extracts the video resolution of a release title.
func titleQuality(title string) string {
	lower := strings.ToLower(title)
	for _, quality := range []string{"2160p", "1080p", "720p", "576p", "480p"} {
		if strings.Contains(lower, quality) {
			return quality
		}
	}
	if strings.Contains(lower, "4k") || strings.Contains(lower, "uhd") {
		return "2160p"
	}
	return ""
}

func twoDigits(n int) string {
	if n < 10 && n >= 0 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
