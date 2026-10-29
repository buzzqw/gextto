package main

// setup.go is the standalone first-run wizard: it writes settings.json (language,
// download folder, credentials, LAN bypass, peer port) and marks the setup done,
// so the page is reachable and configured. Managed mode never uses it.

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
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

	// setupTempFolderName is the name the wizard proposes for the temporary
	// (incomplete) downloads folder. It is the same in every language so the
	// path stays predictable when the interface language changes later.
	setupTempFolderName = "download-temp"
)

// setupDirToken and setupTempToken are placeholders in assets/setup.html that
// renderSetup fills with the proposed storage folders.
const (
	setupDirToken  = "{{DOWNLOAD_DIR}}"
	setupTempToken = "{{TEMP_DIR}}"
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
	// The wizard is a first-run step only. Once the setup is complete it must
	// not be reachable again: otherwise any client that reaches the port could
	// POST new credentials and take over the daemon (the handler is not behind
	// the login, unlike every other /ui/* page). Later reconfiguration goes
	// through the authenticated pages.
	if d.setupComplete() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
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
	// "other" is the dropdown's custom choice: the name is left to the default
	// and the URL is typed by the operator.
	if strings.EqualFold(indexerName, "other") {
		indexerName = ""
	}
	// A known indexer chosen from the dropdown carries its usual URL: fill it
	// in when the URL field was left empty (e.g. with JavaScript disabled).
	if indexerURL == "" {
		indexerURL = uiIndexerPresetURL(indexerName)
	}

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

	// Apply the bandwidth limits before touching the settings store: they go
	// through setConfig (state.json) and can fail if the data folder is not
	// writable, and a failure must not leave the in-memory password changed.
	// They are applied before the wizard is marked done, so the operator sees
	// the error instead of it being silently logged.
	if len(limits) > 0 {
		if _, err := d.setConfig(limits); err != nil {
			logfError("setup: cannot apply the bandwidth limits: %v", err)
			d.renderSetup(w, r, "The bandwidth limits could not be applied; check that the data folder is writable.")
			return
		}
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
			logfError("setup: cannot hash the password: %v", err)
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
		logfError("setup: cannot save settings: %v", err)
		d.renderSetup(w, r, "Setup failed.")
		return
	}
	if d.mapper != nil {
		d.mapper.start()
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setupDefaultDirs returns the folders the wizard proposes for the first run:
// two subfolders of the directory that holds the daemon binary, so a standalone
// install keeps its downloads next to itself and the operator does not have to
// type a path. The main folder name follows the interface language (download,
// scarico, …); the temporary one is always "download-temp". Empty strings mean
// "no proposal" and leave the field blank.
func setupDefaultDirs(lang string) (download, temp string) {
	base := daemonDir()
	if base == "" {
		return "", ""
	}
	return filepath.Join(base, setupDownloadFolderName(lang)), filepath.Join(base, setupTempFolderName)
}

// daemonDir is the directory that holds the running daemon binary, with symlinks
// resolved so a packaged install points at its real folder. It is empty when the
// path cannot be determined.
func daemonDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// setupDownloadFolderName is the localized name of the main download folder:
// plain "download" for English and the languages where the word is used as-is,
// a local word otherwise.
func setupDownloadFolderName(lang string) string {
	switch lang {
	case "it":
		return "scarico"
	case "fr":
		return "telechargements"
	case "es":
		return "descargas"
	case "pl":
		return "pobrane"
	default:
		return "download"
	}
}

// renderSetup writes the wizard page, localized like the daemon page.
func (d *Daemon) renderSetup(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.SetupPage()
	errorHTML := ""
	if errKey != "" {
		errorHTML = `<p class="m err">` + html.EscapeString(errKey) + `</p>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)
	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	// Propose the storage folders (subfolders of the daemon's own directory):
	// pre-filled only, so the operator can accept, change or clear them. Fill the
	// tokens after translation, escaping every value as an HTML attribute.
	download, temp := setupDefaultDirs(lang)
	page = strings.Replace(page, setupDirToken, html.EscapeString(download), 1)
	page = strings.Replace(page, setupTempToken, html.EscapeString(temp), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
