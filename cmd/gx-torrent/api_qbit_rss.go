package main

// api_qbit_rss.go implements the qBittorrent RSS API (/api/v2/rss/*) on top of
// the daemon's own feeds and rules. The tree is the qBittorrent one: a folder
// is a JSON object of its children, a feed is an object with uid/url (and, with
// withData, title/lastBuildDate/isLoading/hasError/articles). Feeds are the ones
// in settings.json; folders are the feeds' "folder" field plus an explicit list
// (rss-folders) so an empty folder created via the API survives.
//
// Semantic note: qBittorrent's rule "mustContain" requires ALL the words, while
// gx-core's include is "at least one". setRule maps mustContain to the include
// list (OR), so a multi-word mustContain is more permissive here; the gx rule
// editor is the place for precise AND filtering. Registered only in standalone.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

const (
	qbitRssPathSep         = `\`
	rssFoldersSettingKey   = "rss-folders"
	qbitRssArticlesPerFeed = 20
)

// rssFolders returns the explicit folder names (settings "rss-folders").
func (d *Daemon) rssFolders() []string {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(rssFoldersSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

func (d *Daemon) saveRssFolders(list []string) {
	if d.opts.Settings == nil {
		return
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	d.opts.Settings.Set(rssFoldersSettingKey, string(raw))
	_ = d.opts.Settings.Save()
}

// qbitRssJoin builds the qBittorrent path of a feed ("folder\name").
func qbitRssJoin(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + qbitRssPathSep + name
}

// qbitRssSplit splits a qBittorrent path into folder and name.
func qbitRssSplit(path string) (folder, name string) {
	if idx := strings.LastIndex(path, qbitRssPathSep); idx >= 0 {
		return path[:idx], path[idx+1:]
	}
	return "", path
}

// qbitRssUID is a stable UUID (v5-like) derived from the feed path, since
// gx-core has no per-feed uid.
func qbitRssUID(path string) string {
	sum := sha1.Sum([]byte(path))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func (d *Daemon) handleQbitRssItems(w http.ResponseWriter, r *http.Request) {
	withData := formBool(r, "withData")
	statuses := map[string]feedStatus{}
	for _, s := range d.feedStatusesSnapshot() {
		statuses[s.Name] = s
	}
	root := map[string]any{}
	folderNode := func(name string) map[string]any {
		if existing, ok := root[name].(map[string]any); ok {
			return existing
		}
		node := map[string]any{}
		root[name] = node
		return node
	}
	for _, folder := range d.rssFolders() {
		if folder != "" {
			folderNode(folder)
		}
	}
	for _, feed := range d.feeds() {
		node := d.qbitRssFeedNode(feed, withData, statuses[feed.Name])
		if feed.Folder == "" {
			root[feed.Name] = node
			continue
		}
		folderNode(feed.Folder)[feed.Name] = node
	}
	writeJSON(w, http.StatusOK, root)
}

// qbitRssFeedNode serializes one feed the way qBittorrent does: uid/url always,
// the rest only with withData.
func (d *Daemon) qbitRssFeedNode(feed feedConfig, withData bool, status feedStatus) map[string]any {
	node := map[string]any{
		"uid": qbitRssUID(qbitRssJoin(feed.Folder, feed.Name)),
		"url": feed.URL,
	}
	if withData {
		node["title"] = feed.Name
		node["isLoading"] = false
		node["hasError"] = status.LastError != ""
		if status.LastCheck > 0 {
			node["lastBuildDate"] = time.Unix(status.LastCheck, 0).UTC().Format(time.RFC1123Z)
		} else {
			node["lastBuildDate"] = ""
		}
		if status.LastError != "" {
			node["error"] = status.LastError
		}
		node["articles"] = d.qbitRssArticles(feed.Name)
	}
	return node
}

// qbitRssArticles maps the feed's archived items to qBittorrent articles.
func (d *Daemon) qbitRssArticles(feed string) []map[string]any {
	out := []map[string]any{}
	archive := d.archive()
	if archive == nil || feed == "" {
		return out
	}
	page, err := archive.search(archiveQuery{feed: feed}, "new", 1, qbitRssArticlesPerFeed)
	if err != nil {
		return out
	}
	for _, entry := range page.Items {
		article := map[string]any{
			"id":          entry.ID,
			"title":       entry.Title,
			"torrentURL":  entry.Download,
			"link":        entry.Download,
			"description": "",
			"isRead":      false,
		}
		switch {
		case entry.Published > 0:
			article["date"] = time.Unix(entry.Published, 0).UTC().Format(time.RFC1123Z)
		case entry.FirstSeen > 0:
			article["date"] = time.Unix(entry.FirstSeen, 0).UTC().Format(time.RFC1123Z)
		}
		out = append(out, article)
	}
	return out
}

func (d *Daemon) handleQbitRssAddFolder(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	d.addRssFolder(path)
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) addRssFolder(path string) {
	if path == "" {
		return
	}
	for _, folder := range d.rssFolders() {
		if folder == path {
			return
		}
	}
	d.saveRssFolders(append(d.rssFolders(), path))
}

func (d *Daemon) handleQbitRssAddFeed(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.FormValue("url"))
	path := strings.TrimSpace(r.FormValue("path"))
	if url == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if path == "" {
		path = url
	}
	folder, name := qbitRssSplit(path)
	feeds := d.feeds()
	for _, feed := range feeds {
		if feed.URL == url {
			w.WriteHeader(http.StatusOK) // already present
			return
		}
	}
	if err := d.saveFeeds(append(feeds, feedConfig{Name: name, URL: url, Folder: folder})); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if folder != "" {
		d.addRssFolder(folder)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssSetFeedURL changes the URL of one feed (path -> url).
func (d *Daemon) handleQbitRssSetFeedURL(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	url := strings.TrimSpace(r.FormValue("url"))
	if path == "" || url == "" {
		http.Error(w, "path and url are required", http.StatusBadRequest)
		return
	}
	feeds := d.feeds()
	changed := false
	for i := range feeds {
		if qbitRssJoin(feeds[i].Folder, feeds[i].Name) == path {
			feeds[i].URL = url
			changed = true
		}
	}
	if changed {
		if err := d.saveFeeds(feeds); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssSetFeedInterval accepts the per-feed refresh interval: the daemon
// uses one global interval (feed-interval-secs), so the value is accepted and
// ignored.
func (d *Daemon) handleQbitRssSetFeedInterval(w http.ResponseWriter, r *http.Request) {
	logf("qbit rss/setFeedRefreshInterval: the daemon uses the global feed-interval-secs")
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRemoveItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	kept := make([]feedConfig, 0, len(d.feeds()))
	for _, feed := range d.feeds() {
		if qbitRssCovered(qbitRssJoin(feed.Folder, feed.Name), feed.Folder, path) {
			continue
		}
		kept = append(kept, feed)
	}
	if err := d.saveFeeds(kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.saveRssFolders(qbitRssDropFolders(d.rssFolders(), path))
	w.WriteHeader(http.StatusOK)
}

// qbitRssCovered reports whether a feed (by full path and folder) is the item
// at "path" or lives under the folder "path".
func qbitRssCovered(full, folder, path string) bool {
	if full == path {
		return true
	}
	return folder != "" && (folder == path || strings.HasPrefix(folder, path+qbitRssPathSep))
}

func qbitRssDropFolders(folders []string, path string) []string {
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder == path || strings.HasPrefix(folder, path+qbitRssPathSep) {
			continue
		}
		out = append(out, folder)
	}
	return out
}

func (d *Daemon) handleQbitRssMoveItem(w http.ResponseWriter, r *http.Request) {
	itemPath := strings.TrimSpace(r.FormValue("itemPath"))
	destPath := strings.TrimSpace(r.FormValue("destPath"))
	if itemPath == "" {
		http.Error(w, "itemPath is required", http.StatusBadRequest)
		return
	}
	feeds := d.feeds()
	for i := range feeds {
		full := qbitRssJoin(feeds[i].Folder, feeds[i].Name)
		switch {
		case full == itemPath:
			feeds[i].Folder = destPath
		case feeds[i].Folder != "" && strings.HasPrefix(feeds[i].Folder, itemPath+qbitRssPathSep):
			rest := strings.TrimPrefix(feeds[i].Folder, itemPath+qbitRssPathSep)
			feeds[i].Folder = qbitRssJoin(destPath, rest)
		}
	}
	if err := d.saveFeeds(feeds); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	folders := d.rssFolders()
	moved := make([]string, 0, len(folders))
	for _, folder := range folders {
		switch {
		case folder == itemPath:
			if destPath != "" {
				moved = append(moved, destPath)
			}
		case strings.HasPrefix(folder, itemPath+qbitRssPathSep):
			moved = append(moved, qbitRssJoin(destPath, strings.TrimPrefix(folder, itemPath+qbitRssPathSep)))
		default:
			moved = append(moved, folder)
		}
	}
	d.saveRssFolders(moved)
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssRefreshItem reloads the feed (or the feeds of a folder) now.
func (d *Daemon) handleQbitRssRefreshItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("itemPath"))
	if path == "" {
		path = strings.TrimSpace(r.URL.Query().Get("itemPath"))
	}
	for _, feed := range d.feeds() {
		if qbitRssCovered(qbitRssJoin(feed.Folder, feed.Name), feed.Folder, path) {
			name := feed.Name
			go d.pollFeedsOnlyDetailed(name)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssMarkAsRead is accepted: the daemon has no per-article read flag
// in the API (the archive records what was added).
func (d *Daemon) handleQbitRssMarkAsRead(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRules(w http.ResponseWriter, _ *http.Request) {
	urlByFeed := map[string]string{}
	for _, feed := range d.feeds() {
		urlByFeed[feed.Name] = feed.URL
	}
	out := map[string]any{}
	for _, rule := range d.rules() {
		out[rule.Name] = gxRuleToQbit(rule, urlByFeed)
	}
	writeJSON(w, http.StatusOK, out)
}

// gxRuleToQbit renders a gx rule in the qBittorrent rule JSON shape.
func gxRuleToQbit(rule rss.Rule, urlByFeed map[string]string) map[string]any {
	enabled := true
	if rule.Enabled != nil {
		enabled = *rule.Enabled
	}
	mustContain := strings.Join(rule.Match.Include, " ")
	mustNotContain := strings.Join(rule.Match.Exclude, " ")
	useRegex := false
	if rule.Match.Regex != "" {
		mustContain = rule.Match.Regex
		useRegex = true
	}
	if rule.Match.NotRegex != "" {
		mustNotContain = rule.Match.NotRegex
		useRegex = true
	}
	affected := []string{}
	if len(rule.Feeds) == 0 {
		for _, url := range urlByFeed {
			affected = append(affected, url)
		}
	} else {
		for _, name := range rule.Feeds {
			if url, ok := urlByFeed[name]; ok {
				affected = append(affected, url)
			}
		}
	}
	sort.Strings(affected)
	return map[string]any{
		"enabled":                   enabled,
		"mustContain":               mustContain,
		"mustNotContain":            mustNotContain,
		"useRegex":                  useRegex,
		"episodeFilter":             rule.Match.EpisodeFilter,
		"smartFilter":               rule.Match.SmartEpisode,
		"previouslyMatchedEpisodes": []string{},
		"affectedFeeds":             affected,
		"ignoreDays":                rule.IgnoreDays,
		"lastMatch":                 "",
		"addPaused":                 rule.Action.Paused,
		"assignedCategory":          rule.Action.Category,
		"savePath":                  rule.Action.SavePath,
	}
}

func (d *Daemon) handleQbitRssMatchingArticles(w http.ResponseWriter, r *http.Request) {
	ruleName := strings.TrimSpace(r.URL.Query().Get("ruleName"))
	if ruleName == "" {
		ruleName = strings.TrimSpace(r.FormValue("ruleName"))
	}
	out := map[string][]string{}
	var rule *rss.Rule
	for _, candidate := range d.rules() {
		if candidate.Name == ruleName {
			c := candidate
			rule = &c
			break
		}
	}
	archive := d.archive()
	if rule == nil || archive == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	page, err := archive.search(archiveQuery{}, "new", 1, rulePreviewScan)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	now := time.Now()
	for _, entry := range page.Items {
		item := rss.Item{Title: entry.Title, Size: entry.Size, Seeders: entry.Seeders, Leechers: entry.Leechers, Magnet: entry.Download}
		if entry.Published > 0 {
			item.Published = time.Unix(entry.Published, 0)
		} else {
			item.Published = time.Unix(entry.FirstSeen, 0)
		}
		if _, _, matched := rss.Evaluate([]rss.Rule{*rule}, entry.Feed, item, now); matched {
			out[entry.Feed] = append(out[entry.Feed], entry.Title)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitRuleDef is the qBittorrent auto-downloading rule definition.
type qbitRuleDef struct {
	Enabled          *bool    `json:"enabled"`
	MustContain      string   `json:"mustContain"`
	MustNotContain   string   `json:"mustNotContain"`
	UseRegex         bool     `json:"useRegex"`
	EpisodeFilter    string   `json:"episodeFilter"`
	SmartFilter      bool     `json:"smartFilter"`
	AffectedFeeds    []string `json:"affectedFeeds"`
	IgnoreDays       int      `json:"ignoreDays"`
	AddPaused        bool     `json:"addPaused"`
	AssignedCategory string   `json:"assignedCategory"`
	SavePath         string   `json:"savePath"`
}

func (d *Daemon) handleQbitRssSetRule(w http.ResponseWriter, r *http.Request) {
	ruleName := strings.TrimSpace(r.FormValue("ruleName"))
	if ruleName == "" {
		http.Error(w, "ruleName is required", http.StatusBadRequest)
		return
	}
	var def qbitRuleDef
	if err := json.Unmarshal([]byte(r.FormValue("ruleDef")), &def); err != nil {
		http.Error(w, "invalid ruleDef", http.StatusBadRequest)
		return
	}
	nameByFeedURL := map[string]string{}
	for _, feed := range d.feeds() {
		nameByFeedURL[feed.URL] = feed.Name
	}
	rule := rss.Rule{Name: ruleName, IgnoreDays: def.IgnoreDays}
	if def.Enabled != nil && !*def.Enabled {
		disabled := false
		rule.Enabled = &disabled
	}
	rule.Match.EpisodeFilter = strings.TrimSpace(def.EpisodeFilter)
	rule.Match.SmartEpisode = def.SmartFilter
	if def.UseRegex {
		rule.Match.Regex = strings.TrimSpace(def.MustContain)
		rule.Match.NotRegex = strings.TrimSpace(def.MustNotContain)
	} else {
		rule.Match.Include = qbitTerms(def.MustContain)
		rule.Match.Exclude = qbitTerms(def.MustNotContain)
	}
	for _, url := range def.AffectedFeeds {
		if name, ok := nameByFeedURL[url]; ok {
			rule.Feeds = append(rule.Feeds, name)
		}
	}
	rule.Action = rss.Action{SavePath: strings.TrimSpace(def.SavePath), Category: strings.TrimSpace(def.AssignedCategory), Paused: def.AddPaused}

	rules := d.rules()
	replaced := false
	for i := range rules {
		if rules[i].Name == ruleName {
			rules[i] = rule
			replaced = true
			break
		}
	}
	if !replaced {
		rules = append(rules, rule)
	}
	if err := d.saveRules(rules); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// qbitTerms splits a qBittorrent mustContain/mustNotContain value into terms.
func qbitTerms(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func (d *Daemon) handleQbitRssRenameRule(w http.ResponseWriter, r *http.Request) {
	oldName := strings.TrimSpace(r.FormValue("ruleName"))
	newName := strings.TrimSpace(r.FormValue("newRuleName"))
	if oldName == "" || newName == "" {
		http.Error(w, "ruleName and newRuleName are required", http.StatusBadRequest)
		return
	}
	rules := d.rules()
	for i := range rules {
		if rules[i].Name == oldName {
			rules[i].Name = newName
		}
	}
	if err := d.saveRules(rules); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRemoveRule(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("ruleName"))
	if name == "" {
		http.Error(w, "ruleName is required", http.StatusBadRequest)
		return
	}
	kept := make([]rss.Rule, 0, len(d.rules()))
	for _, rule := range d.rules() {
		if rule.Name == name {
			continue
		}
		kept = append(kept, rule)
	}
	if err := d.saveRules(kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
