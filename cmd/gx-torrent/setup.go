package main

// setup.go is the standalone first-run wizard: it writes settings.json (language,
// download folder, credentials, LAN bypass, peer port) and marks the setup done,
// so the page is reachable and configured. Managed mode never uses it.

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/auth"
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
	user := strings.TrimSpace(r.FormValue("user"))
	password := r.FormValue("password")
	confirm := r.FormValue("password2")
	bypass := r.FormValue("local-bypass") != ""
	port := strings.TrimSpace(r.FormValue("peer-port"))

	if download != "" && !filepath.IsAbs(download) {
		d.renderSetup(w, r, "The download folder must be an absolute path.")
		return
	}
	if password != "" && password != confirm {
		d.renderSetup(w, r, "The password and the confirmation do not match.")
		return
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
