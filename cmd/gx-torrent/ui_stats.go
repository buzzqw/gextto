package main

// ui_stats.go serves the Statistics page: transfer, the totals of this session
// and of every session, the disk and its cache, the network and the queue. The
// page is a static, translated shell; its values are filled live from
// /api/v1/stats, its graph from /api/v1/stats/history and its busiest torrents
// from /api/v1/torrents.

import (
	"html"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

// handleUIStats renders the Statistics page.
func (d *Daemon) handleUIStats(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page := assets.StatsPage()
	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+html.EscapeString(lang)+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	page = uiInjectClientDictionary(page, lang)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
