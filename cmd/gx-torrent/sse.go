package main

// sse.go streams the engine events as Server-Sent Events. The page
// (/ui/events) and the token API (/api/v1/events) share it: the events are the
// same and so is the rule that matters, that a stream belongs to one session.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// sseKeepAlive is the comment cadence that keeps an idle stream open through a
// proxy. It is well under the usual 60 s idle timeout.
const sseKeepAlive = 30 * time.Second

// streamSessionEvents writes the engine events as Server-Sent Events until the
// client disconnects or the engine session is replaced. opening, when not
// empty, is sent as a first comment (a client can use it to tell a live stream
// from an empty one).
//
// The session and its "done" channel are taken together under d.mu: a stream
// subscribes to one session, and when gx-torrent reopens it for a setting only
// the engine reads (restartSessionLocked) that session will never emit again.
// Ending the response makes the client reconnect and subscribe to the new one,
// instead of leaving a stream that looks alive and delivers nothing.
func (d *Daemon) streamSessionEvents(w http.ResponseWriter, r *http.Request, opening string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	d.mu.Lock()
	session, done := d.session, d.sessionDone
	d.mu.Unlock()
	if session == nil {
		http.Error(w, "session not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// A reverse proxy must pass the stream through as it comes, not buffer it.
	w.Header().Set("X-Accel-Buffering", "no")
	// Subscribe before the headers: an event that happens while the response is
	// being set up is queued, not lost.
	ch, cancel := session.Subscribe()
	defer cancel()
	w.WriteHeader(http.StatusOK)
	if opening != "" {
		_, _ = fmt.Fprintf(w, ": %s\n\n", opening)
	}
	flusher.Flush()

	keepAlive := time.NewTicker(sseKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			return // the session was replaced: the client reconnects
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
