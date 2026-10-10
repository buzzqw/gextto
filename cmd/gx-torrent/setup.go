package main

// setup.go is the standalone first-run wizard: it writes settings.json (language,
// download folder, credentials, LAN bypass, peer port) and marks the setup done,
// so the page is reachable and configured. Managed mode never uses it.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/auth"
	"github.com/buzzqw/gextto/internal/torznab"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

const (
	setupCompleteKey = "setup-complete"
	setupPath        = "/ui/setup"
)

// setupComplete reports whether the standalone wizard has run. With no settings
// store (managed, or a test daemon) it is considered done.
func (d *Daemon) setupComplete() bool {
	if d.opts.Settings == nil {
		return true
	}
	return d.opts.Settings.Get(setupCompleteKey, "") == "true"
}

func validStandaloneLang(lang string) bool {
	if lang == "en" {
		return true
	}
	_, ok := uiLangIndex[lang]
	return ok
}

// handleUISetup renders the wizard (GET) and saves its result (POST).
func (d *Daemon) handleUISetup(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		d.renderSetup(w, r, "")
		return
	}

	lang := strings.TrimSpace(r.FormValue("lang"))
	download := strings.TrimSpace(r.FormValue("download-dir"))
	temp := strings.TrimSpace(r.FormValue("temp-dir"))
	user := strings.TrimSpace(r.FormValue("user"))
	password := r.FormValue("password")
	confirm := r.FormValue("password2")
	bypass := r.FormValue("local-bypass") != ""
	port := strings.TrimSpace(r.FormValue("peer-port"))
	indexerName := strings.TrimSpace(r.FormValue("indexer-name"))
	indexerURL := strings.TrimSpace(r.FormValue("indexer-url"))
	indexerKey := strings.TrimSpace(r.FormValue("indexer-key"))

	if download != "" && !filepath.IsAbs(download) {
		d.renderSetup(w, r, "The download folder must be an absolute path.")
		return
	}
	if temp != "" && !filepath.IsAbs(temp) {
		d.renderSetup(w, r, "The temporary folder must be an absolute path.")
		return
	}
	if indexerURL != "" && !strings.HasPrefix(strings.ToLower(indexerURL), "http://") && !strings.HasPrefix(strings.ToLower(indexerURL), "https://") {
		d.renderSetup(w, r, "The indexer URL must start with http:// or https://.")
		return
	}
	if password != "" && password != confirm {
		d.renderSetup(w, r, "The password and the confirmation do not match.")
		return
	}
	limits := map[string]json.RawMessage{}
	for field, key := range map[string]string{
		"speed-download": "speed_limit_download",
		"speed-upload":   "speed_limit_upload",
	} {
		raw := strings.TrimSpace(r.FormValue(field))
		if raw == "" {
			continue
		}
		kib, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || kib < 0 {
			d.renderSetup(w, r, "The bandwidth limits must be positive numbers (0 = unlimited).")
			return
		}
		value, _ := json.Marshal(kib)
		limits[key] = value
	}
	if !validStandaloneLang(lang) {
		lang = "en"
	}
	if user == "" {
		user = defaultStandaloneUser
	}

	store := d.opts.Settings
	store.Set("lang", lang)
	if download != "" {
		store.Set("download-dir", filepath.Clean(download))
	}
	if temp != "" {
		store.Set("temp-dir", filepath.Clean(temp))
	}
	if indexerURL != "" {
		if indexerName == "" {
			indexerName = "indexer"
		}
		raw, err := json.Marshal([]torznab.Indexer{{Name: indexerName, URL: indexerURL, APIKey: indexerKey}})
		if err != nil {
			d.renderSetup(w, r, "Setup failed.")
			return
		}
		store.Set(indexersSettingKey, string(raw))
	}
	store.Set(standaloneUserKey, user)
	if password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			logf("setup: cannot hash the password: %v", err)
			d.renderSetup(w, r, "Setup failed.")
			return
		}
		store.Set(standalonePasswordKey, hash)
	}
	store.Set(standaloneBypassKey, fmt.Sprintf("%t", bypass))
	if port != "" {
		store.Set("peer-ports", port)
	}
	// The bandwidth limits are a runtime setting (state.json), like the ones the
	// web page changes: apply them through the same path so they take effect at
	// once and are persisted. They are applied before the wizard is marked done,
	// so a failure is shown to the operator instead of being silently logged.
	if len(limits) > 0 {
		if _, err := d.setConfig(limits); err != nil {
			logf("setup: cannot apply the bandwidth limits: %v", err)
			d.renderSetup(w, r, "The bandwidth limits could not be applied; check that the data folder is writable.")
			return
		}
	}
	store.Set(setupCompleteKey, "true")
	if err := store.Save(); err != nil {
		logf("setup: cannot save settings: %v", err)
		d.renderSetup(w, r, "Setup failed.")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// renderSetup writes the wizard page, localized like the daemon page.
func (d *Daemon) renderSetup(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.SetupPage()
	errorHTML := ""
	if errKey != "" {
		errorHTML = `<p class="m err">` + errKey + `</p>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)
	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
