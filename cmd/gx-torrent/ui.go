package main

// ui.go serves a small web page on the daemon's own HTTP port
// (http://127.0.0.1:8890/ by default). Gextto drives the daemon through the
// REST API; this page is for a human who opens that address in a browser, like
// qBittorrent's Web UI: a session summary, the torrent table and the common
// actions (pause/resume, recheck, reannounce, queue top, add a magnet, remove).
// It is served outside the API token middleware so a browser can reach it: when
// a token is configured the page asks for it (and remembers it in a cookie).

import (
	"crypto/subtle"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type uiPageData struct {
	Version string
	Uptime  string
	Now     string
	Notice  string
	Error   bool

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
	Hash      string
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
main{padding:18px;max-width:1280px;margin:0 auto}
.muted{color:#93a1b5;font-size:12px}
.cards{display:flex;gap:10px;flex-wrap:wrap;margin-bottom:16px}
.card{background:#161d2c;border:1px solid #243049;border-radius:10px;padding:10px 14px;min-width:120px}
.card b{display:block;font-size:20px}
.card span{color:#93a1b5;font-size:12px}
.toolbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;background:#161d2c;border:1px solid #243049;border-radius:10px;padding:10px;margin-bottom:14px}
.toolbar form{display:flex;gap:6px;align-items:center;margin:0}
.toolbar input[type=text]{min-width:260px}
table{width:100%;border-collapse:collapse;background:#161d2c;border:1px solid #243049;border-radius:10px;overflow:hidden}
th,td{padding:8px 10px;text-align:left;border-bottom:1px solid #1f2839;font-size:13px;vertical-align:middle}
th{color:#93a1b5;font-weight:500;font-size:12px;text-transform:uppercase;letter-spacing:.03em}
tr:last-child td{border-bottom:0}
td.num{text-align:right;white-space:nowrap}
.name{max-width:340px;word-break:break-word}
.state{font-size:12px;padding:1px 8px;border-radius:999px;background:#243049;white-space:nowrap}
.s-seeding{background:#14532d;color:#86efac}
.s-downloading,.s-checking_files,.s-downloading_metadata{background:#172f4f;color:#93c5fd}
.s-stalled,.s-paused{background:#4a3410;color:#fcd34d}
.s-error{background:#4a1520;color:#fca5a5}
.s-moving{background:#3b2a4a;color:#d8b4fe}
.bar{height:7px;background:#243049;border-radius:99px;overflow:hidden;margin-top:3px;min-width:80px}
.bar i{display:block;height:100%;background:#3b82f6}
.actions{white-space:nowrap}
.actions form{display:inline-block;margin:0 2px 2px 0}
input[type=text],input[type=password]{padding:7px;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit}
button{padding:5px 10px;border-radius:8px;border:1px solid #2b3a55;background:#1b2536;color:#dbe4f0;font-size:12px;cursor:pointer}
button:hover{background:#243049}
button.primary{background:#2563eb;border-color:#2563eb;color:#fff;font-weight:600}
button.danger{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
.notice{padding:9px 12px;border-radius:9px;margin-bottom:12px;background:#14351f;border:1px solid #1f6b3a}
.notice.err{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
form.token{max-width:360px;margin:80px auto;background:#161d2c;border:1px solid #243049;border-radius:12px;padding:22px}
form.token input,form.token button{width:100%;padding:8px;margin:8px 0}
</style>
</head>
<body>
<header>
  <h1>gx-torrent</h1>
  <span class="muted">v{{.Version}} · attivo da {{.Uptime}} · {{.Now}}</span>
</header>
<main>
  {{if .Notice}}<div class="notice{{if .Error}} err{{end}}">{{.Notice}}</div>{{end}}

  <div class="toolbar">
    <form method="post" action="/ui/add">
      <input type="text" name="magnet" placeholder="magnet:?xt=urn:btih:…" autocomplete="off">
      <button class="primary" type="submit">Aggiungi</button>
    </form>
    <form method="post" action="/ui/action"><input type="hidden" name="op" value="resume-all"><button type="submit">Riprendi tutti</button></form>
    <form method="post" action="/ui/action"><input type="hidden" name="op" value="pause-all"><button type="submit">Pausa tutti</button></form>
    <form method="post" action="/ui/action"><input type="hidden" name="op" value="verify-all"><button type="submit">Verifica tutti</button></form>
  </div>

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
      <th>Azioni</th>
    </tr></thead>
    <tbody>
    {{range .Rows}}
      <tr>
        <td class="name">{{.Name}}<div class="muted">{{.SavePath}}</div></td>
        <td><span class="state s-{{.State}}">{{.State}}</span></td>
        <td><div>{{.ProgressS}}</div><div class="bar"><i style="width:{{percent .Progress}}%"></i></div></td>
        <td class="num">{{.DoneSize}}</td>
        <td class="num">{{.Down}}</td>
        <td class="num">{{.Up}}</td>
        <td class="num">{{.Peers}}</td>
        <td class="num">{{.Seeds}}</td>
        <td class="num">{{.Ratio}}</td>
        <td class="num">{{.ETA}}</td>
        <td class="actions">
          {{if eq .State "paused"}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="resume"><button title="Riprendi">▶</button></form>
          {{else}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="pause"><button title="Metti in pausa">⏸</button></form>
          {{end}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="verify"><button title="Riverifica i dati su disco">✓</button></form>
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="reannounce"><button title="Ri-annuncia ai tracker">↻</button></form>
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="top"><button title="Porta in cima alla coda">⤒</button></form>
          <form method="post" action="/ui/remove" onsubmit="return confirm('Rimuovere il torrent? I file restano su disco.');"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="files" value="0"><button title="Rimuovi dalla sessione (i file restano)">✕</button></form>
          <form method="post" action="/ui/remove" onsubmit="return confirm('Rimuovere il torrent E CANCELLARE i file? irreversibile.');"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="files" value="1"><button class="danger" title="Rimuovi e cancella i file">✕ file</button></form>
        </td>
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

// handleUI renders the page.
func (d *Daemon) handleUI(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page, err := d.uiPageData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msg := strings.TrimSpace(r.URL.Query().Get("msg")); msg != "" {
		page.Notice = msg
		page.Error = r.URL.Query().Get("ok") == "0"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := uiTemplate.Execute(w, page); err != nil {
		return
	}
}

// handleUIAction runs one of the per-torrent or bulk actions. It is the same
// set Gextto uses over the API, so the page never grows its own behaviour.
func (d *Daemon) handleUIAction(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	hash := strings.TrimSpace(r.FormValue("hash"))
	var err error
	switch op {
	case "pause":
		err = d.pause(hash)
	case "resume":
		err = d.resume(hash)
	case "verify":
		err = d.verify(hash)
	case "reannounce":
		err = d.reannounce(hash)
	case "top":
		err = d.moveToTop(hash)
	case "pause-all":
		err = d.uiEach(func(h string) error { return d.pause(h) })
	case "resume-all":
		err = d.uiEach(func(h string) error { return d.resume(h) })
	case "verify-all":
		err = d.uiEach(func(h string) error { return d.verify(h) })
	default:
		err = fmt.Errorf("azione sconosciuta: %s", op)
	}
	d.uiDone(w, r, uiActionMessage(op), err)
}

func (d *Daemon) handleUIRemove(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	err := d.remove(hash, formBool(r, "files"))
	d.uiDone(w, r, "Torrent rimosso", err)
}

func (d *Daemon) handleUIAdd(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	if magnet == "" {
		d.uiDone(w, r, "", fmt.Errorf("incolla un link magnet"))
		return
	}
	_, _, err := d.add(addRequest{Magnet: magnet, Destination: d.opts.DownloadDir, SeedRatio: -1, SeedDays: -1})
	d.uiDone(w, r, "Torrent aggiunto", err)
}

func (d *Daemon) uiEach(fn func(hash string) error) error {
	var firstErr error
	for _, view := range d.list() {
		if err := fn(view.Hash); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func uiActionMessage(op string) string {
	switch op {
	case "pause", "pause-all":
		return "In pausa"
	case "resume", "resume-all":
		return "Ripresi"
	case "verify", "verify-all":
		return "Verifica avviata"
	case "reannounce":
		return "Ri-annuncio inviato"
	case "top":
		return "Portato in cima alla coda"
	}
	return "Fatto"
}

// uiDone redirects back to the page with a short message. A redirect (instead of
// rendering) keeps a refresh from repeating the action.
func (d *Daemon) uiDone(w http.ResponseWriter, r *http.Request, okMessage string, err error) {
	query := url.Values{}
	if err != nil {
		query.Set("ok", "0")
		query.Set("msg", "Errore: "+err.Error())
	} else {
		query.Set("ok", "1")
		if okMessage == "" {
			okMessage = "Fatto"
		}
		query.Set("msg", okMessage)
	}
	http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
}

// uiSameOrigin rejects a mutating request coming from another site (basic CSRF
// protection; the page is often reachable on the whole LAN without a token).
func (d *Daemon) uiSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		http.Error(w, "richiesta da un'altra origine rifiutata", http.StatusForbidden)
		return false
	}
	return true
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
			Hash:      view.Hash,
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
