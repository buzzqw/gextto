package main

// api_qbit_cookies.go implements the qBittorrent cookie API (app/cookies and
// app/setCookies) over the daemon's cookie store (cookies.go), so a client can
// manage the cookies a private tracker needs. Response cookies are not
// persisted, the store is the user-managed set only.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (d *Daemon) handleQbitGetCookies(w http.ResponseWriter, _ *http.Request) {
	out := d.storedCookies()
	if out == nil {
		out = []storedCookie{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitSetCookies accepts a JSON array of cookies (the qBittorrent body)
// or a "cookies" form field.
func (d *Daemon) handleQbitSetCookies(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.FormValue("cookies"))
	if raw == "" {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		raw = strings.TrimSpace(string(body))
	}
	if raw == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var list []storedCookie
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		http.Error(w, "invalid cookies", http.StatusBadRequest)
		return
	}
	if err := d.saveCookies(list); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
