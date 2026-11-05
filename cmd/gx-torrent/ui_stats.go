package main

// ui_stats.go serves the Statistics page: transfer, the totals of this session
// and of every session, the disk and its cache, the network and the queue. The
// page is a static, translated shell; its values are filled live from
// /api/v1/stats, its graph from /api/v1/stats/history and its busiest torrents
// from /api/v1/torrents.

import (
	"net/http"
)

// handleUIStats renders the Statistics page.
func (d *Daemon) handleUIStats(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	d.uiWritePage(w, r, "stats", nil)
}
