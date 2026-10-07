package main

// ui.go serves a minimal, read-only web page on the daemon's own HTTP port
// (http://127.0.0.1:8890/ by default). Gextto drives the daemon through the
// REST API; this page is for a human who opens that address in a browser, like
// qBittorrent's Web UI, to see what gx-torrent is doing. It never mutates
// anything: add/pause/remove stay in Gextto.
//
// It is served outside the API token middleware so a browser can reach it: when
// a token is configured the page itself asks for it (and remembers it in a
// cookie), instead of requiring the X-Gx-Token header that a browser cannot
// send for a plain navigation.

import (
	"crypto/subtle"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"
)

type uiPageData struct {
	Version     string
	Uptime      string
	Now         string
	TokenNeeded bool

	Torrents int
	Down     int
	Seeding  int
	Stalled  int
	Paused   int
	Moving   int

	DownloadRate string
	UploadRate   string

	PeerPort    int
	Listen      string
	Router      string
	DHT         bool
	Encryption  string
	Proxy       bool
	IPFilter    int
	CacheReadMB int64
	CacheWB     int64

	Rows []uiTorrentRow
}

type uiTorrentRow struct {
	Name      string
	State     string
	Progress  float64
	ProgressS string
	DoneSize  string
	Down      string
	Up        string
	Peers     int
	Seeds     int
	Ratio     string
	ETA       string
	SavePath  string
}

var uiTemplate = template.Must(template.New("ui").Funcs(template.FuncMap{
	"percent": func(p float64) string { return fmt.Sprintf("%.1f", p) },
}).Parse(`<!doctype html>
<html lang="it">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="5">
<title>gx-torrent</title>
<style>
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font:14px/1.45 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#0f1420;color:#e7ecf3}
header{padding:14px 18px;border-bottom:1px solid #223;display:flex;gap:12px;align-items:baseline;flex-wrap:wrap}
header h1{font-size:16px;margin:0;font-weight:600}
header .muted{color:#93a1b5;font-size:12px}
main{padding:18px;max-width:1200px;margin:0 auto}
.cards{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:16px}
.card{background:#161d2c;border:1px solid #243049;border-radius:10px;padding:10px 14px;min-width:120px}
.card b{display:block;font-size:20px}
.card span{color:#93a1b5;font-size:12px}
table{width:100%;border-collapse:collapse;background:#161d2c;border:1px solid #243049;border-radius:10px;overflow:hidden}
th,td{padding:8px 10px;text-align:left;border-bottom:1px solid #1f2839;font-size:13px;vertical-align:top}
th{color:#93a1b5;font-weight:500;font-size:12px;text-transform:uppercase;letter-spacing:.03em}
tr:last-child td{border-bottom:0}
td.num{text-align:right;white-space:nowrap}
.name{max-width:380px;word-break:break-word}
.state{font-size:12px;padding:1px 8px;border-radius:999px;background:#243049;white-space:nowrap}
.s-seeding{background:#14532d;color:#86efac}
.s-downloading,.s-checking_files,.s-downloading_metadata{background:#172f4f;color:#93c5fd}
.s-stalled,.s-paused{background:#4a3410;color:#fcd34d}
.s-error{background:#4a1520;color:#fca5a5}
.s-moving{background:#3b2a4a;color:#d8b4fe}
.bar{height:7px;background:#243049;border-radius:99px;overflow:hidden;margin-top:3px;min-width:80px}
.bar i{display:block;height:100%;background:#3b82f6}
.muted{color:#93a1b5}
form.token{max-width:360px;margin:80px auto;background:#161d2c;border:1px solid #243049;border-radius:12px;padding:22px}
input[type=password]{width:100%;padding:8px;margin:8px 0;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit}
button{padding:8px 14px;border-radius:8px;border:0;background:#2563eb;color:#fff;font-weight:600;cursor:pointer}
</style>
</head>
<body>
<header>
  <h1>gx-torrent</h1>
  <span class="muted">v{{.Version}} · attivo da {{.Uptime}} · {{.Now}}</span>
  <span class="muted">sola consultazione — le azioni si fanno da Gextto</span>
</header>
<main>
  <div class="cards">
    <div class="card"><b>{{.Torrents}}</b><span>torrent</span></div>
    <div class="card"><b>{{.Down}}</b><span>in download</span></div>
    <div class="card"><b>{{.Seeding}}</b><span>in seed</span></div>
    <div class="card"><b>{{.Stalled}}</b><span>bloccati</span></div>
    <div class="card"><b>{{.Paused}}</b><span>in pausa</span></div>
    <div class="card"><b>{{.DownloadRate}}</b><span>↓ velocità</span></div>
    <div class="card"><b>{{.UploadRate}}</b><span>↑ velocità</span></div>
    <div class="card"><b>{{.PeerPort}}</b><span>porta peer</span></div>
    <div class="card"><b>{{.Router}}</b><span>router</span></div>
    <div class="card"><b>DHT {{if .DHT}}on{{else}}off{{end}}</b><span>cifratura {{.Encryption}} · proxy {{if .Proxy}}on{{else}}off{{end}} · filtro IP {{.IPFilter}}</span></div>
    <div class="card"><b>{{.CacheReadMB}}/{{.CacheWB}} MB</b><span>cache lettura/scrittura</span></div>
  </div>

  {{if .Rows}}
  <table>
    <thead><tr>
      <th>Nome</th><th>Stato</th><th>Progresso</th>
      <th class="num">Fatto / Dimensione</th>
      <th class="num">↓</th><th class="num">↑</th>
      <th class="num">Peer</th><th class="num">Seed</th><th class="num">Ratio</th><th class="num">ETA</th>
      <th>Cartella</th>
    </tr></thead>
    <tbody>
    {{range .Rows}}
      <tr>
        <td class="name">{{.Name}}</td>
        <td><span class="state s-{{.State}}">{{.State}}</span></td>
        <td><div>{{.ProgressS}}</div><div class="bar"><i style="width:{{percent .Progress}}%"></i></div></td>
        <td class="num">{{.DoneSize}}</td>
        <td class="num">{{.Down}}</td>
        <td class="num">{{.Up}}</td>
        <td class="num">{{.Peers}}</td>
        <td class="num">{{.Seeds}}</td>
        <td class="num">{{.Ratio}}</td>
        <td class="num">{{.ETA}}</td>
        <td class="muted">{{.SavePath}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p class="muted">Nessun torrent nella sessione.</p>
  {{end}}
</main>
</body>
</html>`))

const uiTokenPage = `<!doctype html><html lang="it"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>gx-torrent</title>
<style>body{margin:0;font:14px system-ui,sans-serif;background:#0f1420;color:#e7ecf3}
form{max-width:360px;margin:80px auto;background:#161d2c;border:1px solid #243049;border-radius:12px;padding:22px}
input,button{width:100%%;padding:8px;margin:8px 0;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit}
button{background:#2563eb;color:#fff;font-weight:600;border:0;cursor:pointer}.m{color:#93a1b5}</style></head>
<body><form method="get" action="/">
<h2 style="margin:0 0 6px">gx-torrent</h2>
<p class="m">Questa istanza richiede il token configurato in Gextto.</p>
<input type="password" name="token" placeholder="token" autofocus autocomplete="off">
<button type="submit">Entra</button>
</form></body></html>`

// handleUI renders the read-only page. It authenticates itself so the browser
// navigation works with ?token= (and remembers it in a cookie).
func (d *Daemon) handleUI(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page, err := d.uiPageData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := uiTemplate.Execute(w, page); err != nil {
		return
	}
}

func (d *Daemon) uiAuthorized(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(d.opts.Token)
	if token == "" {
		return true
	}
	given := r.Header.Get("X-Gx-Token")
	if given == "" {
		given = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if given == "" {
		given = r.URL.Query().Get("token")
	}
	if given == "" {
		if cookie, err := r.Cookie("gx_token"); err == nil {
			given = cookie.Value
		}
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(token)) == 1 {
		if r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{
				Name: "gx_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
		}
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, uiTokenPage)
	return false
}

func (d *Daemon) uiPageData() (uiPageData, error) {
	stats := d.stats()
	views := d.list()
	page := uiPageData{
		Version:      stats.Version,
		Uptime:       uiDuration(stats.UptimeSeconds),
		Now:          time.Now().Format("15:04:05"),
		Torrents:     stats.Torrents,
		Down:         stats.Downloading,
		Seeding:      stats.Seeding,
		Stalled:      stats.Stalled,
		Paused:       stats.Paused,
		Moving:       stats.Moving,
		DownloadRate: uiRate(stats.DownloadRate),
		UploadRate:   uiRate(stats.UploadRate),
		PeerPort:     stats.PeerPort,
		Listen:       stats.ListenAddress,
		DHT:          stats.DHT,
		Encryption:   uiEncryption(stats.Encryption),
		Proxy:        stats.Proxy,
		IPFilter:     stats.IPFilterRules,
		CacheReadMB:  stats.CacheReadMB,
		CacheWB:      stats.CacheWriteMB,
	}
	switch {
	case stats.PortMapping.Method != "":
		page.Router = strings.ToUpper(stats.PortMapping.Method)
		if stats.PortMapping.ExternalIP != "" {
			page.Router += " " + stats.PortMapping.ExternalIP
		}
	case stats.PortMapping.Error != "":
		page.Router = "non aperto"
	default:
		page.Router = "—"
	}
	for _, view := range views {
		row := uiTorrentRow{
			Name:      view.Name,
			State:     view.State,
			Progress:  view.Progress,
			ProgressS: fmt.Sprintf("%.1f%%", view.Progress),
			DoneSize:  uiBytes(view.TotalDone) + " / " + uiBytes(view.TotalSize),
			Down:      uiRate(view.DownloadRate),
			Up:        uiRate(view.UploadRate),
			Peers:     view.NumPeers,
			Seeds:     view.NumSeeds,
			ETA:       "—",
			SavePath:  view.SavePath,
		}
		if view.Progress >= 99.99 && view.TotalSize > 0 {
			if view.SeedRatio > 0 {
				row.Ratio = fmt.Sprintf("%.2f", view.SeedRatio)
			} else {
				row.Ratio = "∞"
			}
		} else if view.ETASeconds >= 0 {
			row.ETA = uiDuration(view.ETASeconds)
		}
		page.Rows = append(page.Rows, row)
	}
	sort.Slice(page.Rows, func(i, j int) bool {
		if page.Rows[i].State != page.Rows[j].State {
			return page.Rows[i].State < page.Rows[j].State
		}
		return page.Rows[i].Name < page.Rows[j].Name
	})
	return page, nil
}

func uiBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

func uiRate(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "—"
	}
	return uiBytes(bytesPerSecond) + "/s"
}

func uiDuration(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dg %02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func uiEncryption(level int) string {
	switch level {
	case 0:
		return "off"
	case 2:
		return "forzata"
	default:
		return "attiva"
	}
}
