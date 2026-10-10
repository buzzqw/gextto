package main

// ui_feeds.go exposes the RSS feeds' status to the standalone page and lets the
// operator trigger a check now. Managed mode has no feeds (Gextto owns
// acquisition), so both endpoints are refused.

import "net/http"

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
	// Poll in the background: fetching several feeds can take seconds, and the
	// page must stay responsive.
	go d.pollFeeds()
	w.WriteHeader(http.StatusOK)
}
