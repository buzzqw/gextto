package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"sync"
)

//go:embed page.tmpl
var pageFS embed.FS

var (
	pageOnce sync.Once
	pageTmpl *template.Template
	pageErr  error
)

func parsedPage() (*template.Template, error) {
	pageOnce.Do(func() {
		pageTmpl, pageErr = template.ParseFS(pageFS, "page.tmpl")
	})
	return pageTmpl, pageErr
}

type pageData struct {
	Lang       string
	T          map[string]string
	Version    string
	Mode       string
	Nodes      int
	UDPPort    int
	Reach      string
	ReachNote  string
	ClientI18n template.JS
}

func (d *Daemon) handleUI(w http.ResponseWriter, r *http.Request) {
	lang := normalizeLang(r.URL.Query().Get("lang"))
	if lang == "" {
		if cookie, err := r.Cookie("ed2k_lang"); err == nil {
			lang = normalizeLang(cookie.Value)
		}
	}
	if lang == "" {
		lang = normalizeLang(d.opts.Lang)
	}
	if lang == "" {
		lang = "en"
	}

	if strings.TrimSpace(d.opts.Token) != "" {
		// A browser that opens the page with ?token=... gets a session cookie,
		// so the page's fetch calls authenticate like the browser does.
		if supplied := strings.TrimSpace(r.URL.Query().Get("token")); supplied != "" && d.authorized(r) {
			http.SetCookie(w, &http.Cookie{
				Name: "gx_token", Value: supplied, Path: "/", HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		if !d.authorized(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(tokenPageHTML(uiStrings(lang))))
			return
		}
	}

	tmpl, err := parsedPage()
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	clientI18n := map[string]string{}
	dictionary := uiStrings(lang)
	for _, key := range clientKeys {
		if value, ok := dictionary[key]; ok {
			clientI18n[key] = value
		}
	}
	encoded, _ := json.Marshal(clientI18n)
	stats := d.client.Stats()
	reachKey := "reach_unknown"
	switch stats.Reachability {
	case "open":
		reachKey = "reach_open"
	case "limited":
		reachKey = "reach_limited"
	}
	data := pageData{
		Lang:       lang,
		T:          dictionary,
		Version:    runtimeVersion(),
		Mode:       string(d.opts.Mode),
		Nodes:      stats.Nodes,
		UDPPort:    d.client.UDPPort(),
		Reach:      dictionary[reachKey],
		ReachNote:  stats.ReachDetail,
		ClientI18n: template.JS(encoded),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// normalizeLang maps a requested language to one of the six UI languages.
// An empty result means "not recognized".
func normalizeLang(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.IndexAny(value, "_-"); i == 2 {
		value = value[:2]
	}
	if validLang(value) {
		return value
	}
	if value == "en" {
		return "en"
	}
	return ""
}

func tokenPageHTML(t map[string]string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>` + template.HTMLEscapeString(t["token_title"]) + `</title>` +
		`<link rel="icon" type="image/svg+xml" href="/favicon.ico"><link rel="stylesheet" href="/assets/theme.css"></head><body>` +
		`<main style="max-width:420px;margin:10vh auto;padding:0 16px">` +
		`<h1>` + template.HTMLEscapeString(t["token_title"]) + `</h1>` +
		`<form method="get" action="/"><label>` + template.HTMLEscapeString(t["token_label"]) +
		`<input type="password" name="token" autocomplete="current-password" required title="` + template.HTMLEscapeString(t["token_tooltip"]) + `"></label>` +
		`<p><button type="submit">` + template.HTMLEscapeString(t["token_send"]) + `</button></p></form></main></body></html>`
}
