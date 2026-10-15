package main

// ui_prefs.go keeps a copy of the page's display preferences on the server
// (standalone only), so columns, filters and theme follow the operator across
// browsers and devices. The browser's localStorage stays the fast path: on
// load the server copy fills in only what the browser does not have yet, and
// every local change is pushed back (debounced, best effort). In managed mode
// there is no settings store: the page keeps working on localStorage alone.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	uiPrefsColsKey   = "ui-cols"
	uiPrefsFilterKey = "ui-filter"
	uiPrefsThemeKey  = "ui-theme"
)

// uiPrefs is the JSON body of GET/POST /ui/uiprefs. Raw JSON per field keeps
// unknown future keys untouched.
type uiPrefs struct {
	Cols   json.RawMessage `json:"cols,omitempty"`
	Filter json.RawMessage `json:"filter,omitempty"`
	Theme  string          `json:"theme,omitempty"`
}

func (d *Daemon) handleUIGetPrefs(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	out := map[string]any{}
	if d.opts.Settings != nil {
		if raw := strings.TrimSpace(d.opts.Settings.Get(uiPrefsColsKey, "")); raw != "" {
			out["cols"] = json.RawMessage(raw)
		}
		if raw := strings.TrimSpace(d.opts.Settings.Get(uiPrefsFilterKey, "")); raw != "" {
			out["filter"] = json.RawMessage(raw)
		}
		if theme := strings.TrimSpace(d.opts.Settings.Get(uiPrefsThemeKey, "")); theme != "" {
			out["theme"] = theme
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (d *Daemon) handleUIPostPrefs(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	if d.opts.Settings == nil {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var prefs uiPrefs
	if err := json.Unmarshal(raw, &prefs); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("expecting a JSON object"))
		return
	}
	if len(prefs.Cols) > 0 {
		var probe map[string]any
		if err := json.Unmarshal(prefs.Cols, &probe); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("cols must be a JSON object"))
			return
		}
		d.opts.Settings.Set(uiPrefsColsKey, string(prefs.Cols))
	}
	if len(prefs.Filter) > 0 {
		var probe map[string]any
		if err := json.Unmarshal(prefs.Filter, &probe); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("filter must be a JSON object"))
			return
		}
		d.opts.Settings.Set(uiPrefsFilterKey, string(prefs.Filter))
	}
	switch prefs.Theme {
	case "auto", "light", "dark":
		d.opts.Settings.Set(uiPrefsThemeKey, prefs.Theme)
	case "":
	default:
		writeError(w, http.StatusBadRequest, errors.New("theme must be auto, light or dark"))
		return
	}
	if err := d.opts.Settings.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
