package main

// ui.go serves a small web page on the daemon's own HTTP port
// (http://127.0.0.1:8890 by default). Gextto drives the daemon through the
// REST API; this page is for a human who opens that address in a browser, like
// qBittorrent's Web UI: session summary, torrent table, add (magnet or
// .torrent), search/sort, bulk actions, per-torrent details (files, peers,
// trackers, seed limits, move, pin), removal and the IP filter.
//
// It is served outside the API token middleware so a browser can reach it: when
// a token is configured the page asks for it (and remembers it in a cookie).
// The API stays protected; only this page uses the cookie.
//
// The cards and table are refreshed with a small fetch of the "live" fragment
// instead of a full page reload, so forms keep their state and there is no
// flicker. The detail of one torrent is a fragment too, loaded into a modal.

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/torrent"
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
	TotalDown    int64
	TotalUp      int64

	PeerPort     int
	Listen       string
	Router       string
	PortOpen     bool
	DHT          bool
	DHTNodes     int64
	UTP          bool
	Encryption   string
	Proxy        bool
	IPFilter     int
	IPFilterPath string
	CacheReadMB  int64
	CacheWB      int64
	LSDPeers     int64
	DiskFree     int64
	DiskTotal    int64

	CountAll     int
	CountDown    int
	CountSeeding int
	CountPaused  int
	CountStalled int
	CountMoving  int
	CountError   int

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
	RatioVal  float64
	ETA       string
	ETAVal    int64
	SavePath  string

	TotalDone int64
	TotalSize int64
	DLRate    int64
	ULRate    int64
}

// uiFileRow, uiPeerRow and uiTrackerRow back the torrent detail tabs.
type uiFileRow struct {
	Index  int
	Path   string
	Size   int64
	Done   int64
	Wanted bool
}

type uiPeerRow struct {
	Address     string
	Client      string
	Down        int64
	Up          int64
	Progress    float64
	Seed        bool
	Incoming    bool
	Encrypted   bool
	UTP         bool
	Source      string
	Downloading bool
	ClientInt   bool
	ClientChoke bool
	PeerInt     bool
	PeerChoke   bool
	Optimistic  bool
	Snubbed     bool
	Handshake   bool
	Connected   int64
}

type uiTrackerRow struct {
	URL      string
	Status   string
	Message  string
	Seeders  int
	Leechers int
	Next     int64
}

type uiDetailData struct {
	Hash     string
	Name     string
	Tab      string
	State    string
	Error    string
	SavePath string

	Progress   float64
	TotalSize  int64
	TotalDone  int64
	Downloaded int64
	Uploaded   int64
	DownRate   int64
	UpRate     int64
	Ratio      float64
	ETA        int64

	NumPeers      int
	NumSeeds      int
	NumComplete   int
	NumIncomplete int

	SeedRatio float64
	SeedDays  int64
	Pinned    bool
	Private   bool

	PiecesTotal     uint32
	PiecesHave      uint32
	PiecesAvailable uint32
	PiecesChecked   uint32
	PieceLength     int64
	Wasted          int64
	Allocated       int64
	FileCount       int
	AddedAt         int64
	CompletedAt     int64
	AddedStr        string
	CompletedStr    string
	PiecesPercent   float64
	Magnet          string

	Files    []uiFileRow
	Peers    []uiPeerRow
	Trackers []uiTrackerRow
}

const uiStyle = `
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font:15px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#0f1420;color:#e7ecf3}
header{padding:14px 20px;border-bottom:1px solid #223;display:flex;gap:12px;align-items:baseline;flex-wrap:wrap}
header h1{font-size:18px;margin:0;font-weight:600}
main{padding:16px 20px;max-width:none;width:100%;margin:0}
.muted{color:#93a1b5;font-size:13px}
a{color:#93c5fd}
.cards{display:flex;gap:12px;flex-wrap:wrap;margin-bottom:16px}
.card{background:#161d2c;border:1px solid #243049;border-radius:10px;padding:12px 16px;min-width:130px}
.card b{display:block;font-size:22px}
.card span{color:#93a1b5;font-size:13px}
.toolbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;background:#161d2c;border:1px solid #243049;border-radius:10px;padding:12px;margin-bottom:12px}
.toolbar form{display:flex;gap:6px;align-items:center;margin:0;flex-wrap:wrap}
.toolbar input[type=text],.toolbar input[type=search]{min-width:200px}
.chk{display:flex;gap:4px;align-items:center;color:#93a1b5;font-size:12px}
table{width:100%;border-collapse:collapse;background:#161d2c;border:1px solid #243049;border-radius:10px;overflow:hidden}
th,td{padding:9px 12px;text-align:left;border-bottom:1px solid #1f2839;font-size:14px;vertical-align:middle}
th{color:#93a1b5;font-weight:500;font-size:12px;text-transform:uppercase;letter-spacing:.03em;cursor:pointer;user-select:none}
tr:last-child td{border-bottom:0}
td.num,th.num{text-align:right;white-space:nowrap}
td.sel,th.sel{width:26px;text-align:center}
td.name,th.name{max-width:340px;word-break:break-word}
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
input[type=text],input[type=search],input[type=password],input[type=number],select{padding:8px;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit}
button{padding:6px 12px;border-radius:8px;border:1px solid #2b3a55;background:#1b2536;color:#dbe4f0;font-size:13px;cursor:pointer}
button:hover{background:#243049}
button.primary{background:#2563eb;border-color:#2563eb;color:#fff;font-weight:600}
button.danger{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
.notice{padding:9px 12px;border-radius:9px;margin-bottom:12px;background:#14351f;border:1px solid #1f6b3a}
.notice.err{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
form.token{max-width:360px;margin:80px auto;background:#161d2c;border:1px solid #243049;border-radius:12px;padding:22px}
form.token input,form.token button{width:100%;padding:8px;margin:8px 0}
.overlay{position:fixed;inset:0;background:rgba(4,8,16,.72);display:flex;align-items:flex-start;justify-content:center;padding:40px 16px;overflow:auto;z-index:50}
.modal{background:#131a28;border:1px solid #243049;border-radius:12px;width:min(1100px,100%);padding:18px}
.modal-head{display:flex;justify-content:space-between;align-items:center;gap:10px;margin-bottom:10px}
.modal-head h3{margin:0;font-size:15px;word-break:break-word}
.detail-tabs{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:12px}
.detail-tabs button.on{background:#2563eb;border-color:#2563eb;color:#fff}
.stat-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:6px 18px;margin-bottom:12px}
.stat-grid .row{display:flex;justify-content:space-between;gap:10px;border-bottom:1px dashed #1f2839;padding:3px 0}
.stat-grid .row span{color:#93a1b5;font-size:12px}
.stat-grid .row strong{font-weight:500;text-align:right;word-break:break-word}
.form-grid{display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;margin:10px 0;padding-top:10px;border-top:1px solid #1f2839}
.form-grid label{display:flex;flex-direction:column;gap:3px;color:#93a1b5;font-size:12px}
.layout{display:flex;gap:14px;align-items:flex-start}
.sidebar{display:flex;flex-direction:column;gap:4px;min-width:180px;background:#161d2c;border:1px solid #243049;border-radius:10px;padding:10px}
.filter{display:flex;justify-content:space-between;gap:8px;text-align:left;background:transparent;border:0;border-radius:8px;padding:6px 8px;color:#dbe4f0}
.filter:hover{background:#243049}
.filter.on{background:#2563eb;color:#fff}
.content{flex:1;min-width:0}
.statusbar{position:sticky;bottom:0;margin-top:12px;display:flex;gap:18px;flex-wrap:wrap;align-items:center;background:#131a28;border:1px solid #243049;border-radius:10px;padding:10px 14px;font-size:13px;color:#93a1b5}
.statusbar b{color:#e7ecf3;font-weight:600}
.toast{position:fixed;top:14px;right:14px;z-index:80;background:#14351f;border:1px solid #1f6b3a;color:#e7ecf3;padding:10px 14px;border-radius:10px;box-shadow:0 6px 24px rgba(0,0,0,.4);max-width:420px;transition:opacity .4s}
.toast.err{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
.flag{font-size:11px;padding:1px 6px;border-radius:6px;background:#243049;color:#93a1b5;white-space:nowrap}
.flag.on{background:#14532d;color:#86efac}
@media(max-width:760px){.layout{flex-direction:column}.sidebar{flex-direction:row;flex-wrap:wrap;min-width:0}}
`

var uiTemplate = template.Must(template.New("ui").Funcs(template.FuncMap{
	"percent": func(p float64) string { return fmt.Sprintf("%.1f", p) },
	"bytes":   uiBytes,
	"rate":    uiRate,
	"dur":     uiDuration,
}).Parse(uiPageTemplate + uiLiveTemplate + uiDetailTemplate))

const uiPageTemplate = `<!doctype html>
<html lang="it">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>gx-torrent</title>
<style>` + uiStyle + `</style>
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
      <input type="text" name="source" placeholder="incolla magnet:… oppure https://…/file.torrent" autocomplete="off">
      <input type="text" name="destination" placeholder="destinazione (vuoto = predefinita)" autocomplete="off">
      <label class="chk"><input type="checkbox" name="paused" value="1"> pausa</label>
      <label class="chk"><input type="checkbox" name="top" value="1"> in cima</label>
      <button class="primary" type="submit">Aggiungi</button>
    </form>
    <form method="post" action="/ui/add-file" enctype="multipart/form-data">
      <input type="file" name="torrent" accept=".torrent,application/x-bittorrent" required title="File .torrent da aggiungere">
      <input type="text" name="destination" placeholder="destinazione (vuoto = predefinita)" autocomplete="off">
      <label class="chk"><input type="checkbox" name="paused" value="1"> pausa</label>
      <label class="chk"><input type="checkbox" name="top" value="1"> in cima</label>
      <button type="submit">Aggiungi .torrent</button>
    </form>
    <form method="post" action="/ui/ipfilter">
      <span class="muted">Filtro IP</span>
      <input type="text" name="source" placeholder="URL o file locale" autocomplete="off">
      <button type="submit" title="Scarica (se URL) e applica subito il filtro IP">Carica filtro</button>
    </form>
    <input type="search" id="filter" placeholder="Filtra torrent…" oninput="filterRows()" autocomplete="off">
  </div>

  <div class="toolbar">
    <span class="muted">Selezionati: <b id="selcount">0</b></span>
    <button type="button" onclick="bulk('resume')">▶ Riprendi</button>
    <button type="button" onclick="bulk('pause')">⏸ Pausa</button>
    <button type="button" onclick="bulk('verify')">✓ Verifica</button>
    <button type="button" onclick="bulk('reannounce')">↻ Ri-annuncia</button>
    <button type="button" onclick="bulk('top')">⤒ In cima</button>
    <button type="button" class="danger" onclick="bulk('remove')">✕ Rimuovi</button>
    <button type="button" class="danger" onclick="bulk('remove-files')">✕ Rimuovi e cancella file</button>
  </div>

  <div id="live">{{template "live" .}}</div>
</main>

<div id="detail-modal" class="overlay" style="display:none">
  <div class="modal"><div id="detail-body"></div></div>
</div>

<script>
function openDetail(hash, tab){tab=tab||'general';fetch('/ui/detail?hash='+encodeURIComponent(hash)+'&tab='+encodeURIComponent(tab),{cache:'no-store'}).then(function(r){return r.text()}).then(function(h){document.getElementById('detail-body').innerHTML=h;document.getElementById('detail-modal').style.display='flex';});}
function closeDetail(){document.getElementById('detail-modal').style.display='none';}
function detailAction(hash,tab,path,params){var f=new URLSearchParams(params||{});f.set('hash',hash);f.set('tab',tab);fetch(path,{method:'POST',body:f,headers:{'Content-Type':'application/x-www-form-urlencoded'}}).then(function(){openDetail(hash,tab);refresh(true);});}
function refresh(force){if(!force){if(document.querySelectorAll('.rowsel:checked').length>0)return;if(document.getElementById('detail-modal').style.display==='flex')return;}fetch('/ui/live',{cache:'no-store'}).then(function(r){return r.ok?r.text():null}).then(function(t){if(t){document.getElementById('live').innerHTML=t;updateSel();applyFilters();}});}
function rowState(tr){return tr.getAttribute('data-state')||'';}
function matchesState(st,f){if(f==='all')return true;if(f==='downloading')return st==='downloading'||st==='downloading_metadata'||st==='checking_files';return st===f;}
function applyFilters(){var q=(document.getElementById('filter').value||'').toLowerCase();var f=window.__stateFilter||'all';document.querySelectorAll('#live tbody tr').forEach(function(tr){var n=(tr.getAttribute('data-name')||'').toLowerCase();tr.style.display=((!q||n.indexOf(q)>=0)&&matchesState(rowState(tr),f))?'':'none';});}
function filterRows(){applyFilters();}
function filterByState(f,btn){window.__stateFilter=f;document.querySelectorAll('.sidebar .filter').forEach(function(b){b.classList.toggle('on',b===btn);});applyFilters();}
function updateSel(){document.getElementById('selcount').textContent=document.querySelectorAll('.rowsel:checked').length;}
function selectAll(box){document.querySelectorAll('.rowsel').forEach(function(c){c.checked=box.checked});updateSel();}
function bulk(op){var hashes=Array.prototype.map.call(document.querySelectorAll('.rowsel:checked'),function(c){return c.value});if(!hashes.length){showToast('Seleziona almeno un torrent',true);return;}if(op==='remove-files'&&!confirm('Rimuovere i torrent selezionati E cancellare i file? Irreversibile.'))return;var f=new URLSearchParams();f.set('op',op);hashes.forEach(function(h){f.append('hashes',h)});fetch('/ui/bulk',{method:'POST',body:f,headers:{'Content-Type':'application/x-www-form-urlencoded'}}).then(function(){location.href='/';});}
function sortTable(idx){var tb=document.querySelector('#live tbody');if(!tb)return;var rows=Array.prototype.slice.call(tb.querySelectorAll('tr'));var asc=tb.getAttribute('data-sort')!==String(idx);rows.sort(function(a,b){var av=a.children[idx].getAttribute('data-v')||a.children[idx].textContent;var bv=b.children[idx].getAttribute('data-v')||b.children[idx].textContent;var an=parseFloat(av),bn=parseFloat(bv);if(!isNaN(an)&&!isNaN(bn))return asc?an-bn:bn-an;return asc?String(av).localeCompare(String(bv)):String(bv).localeCompare(String(av));});rows.forEach(function(r){tb.appendChild(r)});tb.setAttribute('data-sort',asc?String(idx):'');}
function showToast(msg,err){var t=document.createElement('div');t.className='toast'+(err?' err':'');t.textContent=msg;document.body.appendChild(t);setTimeout(function(){t.style.opacity='0';setTimeout(function(){t.remove();},450);},4000);}
function copyMagnet(el){var text=el.getAttribute('data-magnet')||'';if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(text).then(function(){showToast('Magnet copiato',false);},function(){showToast('Copia non riuscita',true);});}else{showToast('Copia non disponibile',true);}}
setInterval(function(){refresh(false)},5000);
(function(){document.addEventListener('change',function(e){if(e.target.classList.contains('rowsel'))updateSel();});document.addEventListener('keydown',function(e){if(e.key==='/'&&['INPUT','TEXTAREA','SELECT'].indexOf(document.activeElement.tagName)<0){e.preventDefault();document.getElementById('filter').focus();}if(e.key==='Escape'){closeDetail();}});var n=document.querySelector('.notice');if(n){showToast(n.textContent,n.classList.contains('err'));n.remove();}var p=new URLSearchParams(location.search);if(p.get('open')){openDetail(p.get('open'),p.get('tab')||'general');}})();
</script>
</body>
</html>`

const uiLiveTemplate = `{{define "live"}}
  <div class="cards">
    <div class="card"><b>{{.Torrents}}</b><span>torrent</span></div>
    <div class="card"><b>{{.Down}}</b><span>in download</span></div>
    <div class="card"><b>{{.Seeding}}</b><span>in seed</span></div>
    <div class="card"><b>{{.Stalled}}</b><span>bloccati</span></div>
    <div class="card"><b>{{.Paused}}</b><span>in pausa</span></div>
    <div class="card"><b>{{.DownloadRate}}</b><span>↓ velocità</span></div>
    <div class="card"><b>{{.UploadRate}}</b><span>↑ velocità</span></div>
    <div class="card"><b>{{bytes .TotalDown}}</b><span>scaricati (sessione)</span></div>
    <div class="card"><b>{{bytes .TotalUp}}</b><span>inviati (sessione)</span></div>
    <div class="card"><b>{{.PeerPort}}</b><span>porta peer</span></div>
    <div class="card"><b>{{.Router}}</b><span>router</span></div>
    <div class="card"><b>DHT {{if .DHT}}on{{else}}off{{end}}</b><span>{{.DHTNodes}} nodi · uTP {{if .UTP}}on{{else}}off{{end}}</span></div>
    <div class="card"><b>{{if .IPFilter}}{{.IPFilter}}{{else}}nessuno{{end}}</b><span>filtro IP{{if .IPFilterPath}} · {{.IPFilterPath}}{{end}}</span></div>
    <div class="card"><b>{{.CacheReadMB}}/{{.CacheWB}} MB</b><span>cache lettura/scrittura</span></div>
    <div class="card"><b>{{bytes .DiskFree}}</b><span>spazio libero (di {{bytes .DiskTotal}})</span></div>
  </div>

  {{if .Rows}}
  <div class="layout">
    <aside class="sidebar">
      <div class="muted">Filtri</div>
      <button type="button" class="filter on" onclick="filterByState('all',this)">Tutti <b>{{.CountAll}}</b></button>
      <button type="button" class="filter" onclick="filterByState('downloading',this)">In download <b>{{.CountDown}}</b></button>
      <button type="button" class="filter" onclick="filterByState('seeding',this)">In seed <b>{{.CountSeeding}}</b></button>
      <button type="button" class="filter" onclick="filterByState('paused',this)">In pausa <b>{{.CountPaused}}</b></button>
      <button type="button" class="filter" onclick="filterByState('stalled',this)">Bloccati <b>{{.CountStalled}}</b></button>
      <button type="button" class="filter" onclick="filterByState('moving',this)">In spostamento <b>{{.CountMoving}}</b></button>
      <button type="button" class="filter" onclick="filterByState('error',this)">In errore <b>{{.CountError}}</b></button>
    </aside>
    <div class="content">
    <table id="torrents">
    <thead><tr>
      <th class="sel"><input type="checkbox" title="Seleziona tutti" onclick="selectAll(this)"></th>
      <th class="name" onclick="sortTable(1)">Nome</th>
      <th onclick="sortTable(2)">Stato</th>
      <th onclick="sortTable(3)">Progresso</th>
      <th class="num" onclick="sortTable(4)">Fatto / Dimensione</th>
      <th class="num" onclick="sortTable(5)">↓</th>
      <th class="num" onclick="sortTable(6)">↑</th>
      <th class="num" onclick="sortTable(7)">Peer</th>
      <th class="num" onclick="sortTable(8)">Seed</th>
      <th class="num" onclick="sortTable(9)">Ratio</th>
      <th class="num" onclick="sortTable(10)">ETA</th>
      <th>Azioni</th>
    </tr></thead>
    <tbody>
    {{range .Rows}}
      <tr data-name="{{.Name}}" data-state="{{.State}}">
        <td class="sel"><input class="rowsel" type="checkbox" value="{{.Hash}}"></td>
        <td class="name"><a href="#" onclick="openDetail('{{.Hash}}');return false" title="Apri i dettagli del torrent">{{.Name}}</a><div class="muted">{{.SavePath}}</div></td>
        <td><span class="state s-{{.State}}">{{.State}}</span></td>
        <td data-v="{{.Progress}}"><div>{{.ProgressS}}</div><div class="bar"><i style="width:{{percent .Progress}}%"></i></div></td>
        <td class="num" data-v="{{.TotalDone}}">{{.DoneSize}}</td>
        <td class="num" data-v="{{.DLRate}}">{{.Down}}</td>
        <td class="num" data-v="{{.ULRate}}">{{.Up}}</td>
        <td class="num" data-v="{{.Peers}}">{{.Peers}}</td>
        <td class="num" data-v="{{.Seeds}}">{{.Seeds}}</td>
        <td class="num" data-v="{{.RatioVal}}">{{.Ratio}}</td>
        <td class="num" data-v="{{.ETAVal}}">{{.ETA}}</td>
        <td class="actions">
          <button type="button" onclick="openDetail('{{.Hash}}')" title="Dettagli: file, peer, tracker">⋯</button>
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
    </div>
  </div>
  {{else}}
  <p class="muted">Nessun torrent nella sessione.</p>
  {{end}}

  <div class="statusbar">
    <span>↓ <b>{{.DownloadRate}}</b></span>
    <span>↑ <b>{{.UploadRate}}</b></span>
    <span>Totali <b>{{bytes .TotalDown}}</b> / <b>{{bytes .TotalUp}}</b></span>
    <span>Spazio libero <b>{{bytes .DiskFree}}</b></span>
    <span>DHT <b>{{if .DHT}}{{.DHTNodes}} nodi{{else}}spento{{end}}</b></span>
    <span>Porta <b>{{if .PortOpen}}aperta ({{.Router}}){{else}}non aperta{{end}}</b></span>
    <span>Cifratura <b>{{.Encryption}}</b></span>
  </div>
{{end}}`

const uiDetailTemplate = `{{define "detail"}}
<div class="modal-head">
  <h3>{{.Name}}</h3>
  <button type="button" onclick="closeDetail()">Chiudi</button>
</div>
<div class="detail-tabs">
  <button type="button" class="{{if eq .Tab "general"}}on{{end}}" onclick="openDetail('{{.Hash}}','general')">Generale</button>
  <button type="button" class="{{if eq .Tab "files"}}on{{end}}" onclick="openDetail('{{.Hash}}','files')">File ({{len .Files}})</button>
  <button type="button" class="{{if eq .Tab "peers"}}on{{end}}" onclick="openDetail('{{.Hash}}','peers')">Peer ({{len .Peers}})</button>
  <button type="button" class="{{if eq .Tab "trackers"}}on{{end}}" onclick="openDetail('{{.Hash}}','trackers')">Tracker ({{len .Trackers}})</button>
</div>
{{if .Error}}<p class="notice err">{{.Error}}</p>{{end}}

{{if eq .Tab "general"}}
  <div class="stat-grid">
    <div class="row"><span>Stato</span><strong>{{.State}}</strong></div>
    <div class="row"><span>Progresso</span><strong>{{percent .Progress}}%</strong></div>
    <div class="row"><span>Dimensione</span><strong>{{bytes .TotalSize}}</strong></div>
    <div class="row"><span>Scaricato</span><strong>{{bytes .TotalDone}}</strong></div>
    <div class="row"><span>Download totale</span><strong>{{bytes .Downloaded}}</strong></div>
    <div class="row"><span>Caricato</span><strong>{{bytes .Uploaded}}</strong></div>
    <div class="row"><span>Ratio</span><strong>{{printf "%.2f" .Ratio}}</strong></div>
    <div class="row"><span>↓ / ↑</span><strong>{{rate .DownRate}} / {{rate .UpRate}}</strong></div>
    <div class="row"><span>Peer / Seed</span><strong>{{.NumPeers}} / {{.NumSeeds}}</strong></div>
    <div class="row"><span>Sciame (seed / peer)</span><strong>{{.NumComplete}} / {{.NumIncomplete}}</strong></div>
    <div class="row"><span>ETA</span><strong>{{if lt .ETA 0}}—{{else}}{{dur .ETA}}{{end}}</strong></div>
    <div class="row"><span>Cartella</span><strong>{{.SavePath}}</strong></div>
    <div class="row"><span>Ratio seed impostato</span><strong>{{printf "%.2f" .SeedRatio}} ({{.SeedDays}} giorni)</strong></div>
    <div class="row"><span>Pin</span><strong>{{if .Pinned}}sì{{else}}no{{end}}</strong></div>
    <div class="row"><span>Privato</span><strong>{{if .Private}}sì{{else}}no{{end}}</strong></div>
    <div class="row"><span>File</span><strong>{{.FileCount}}</strong></div>
    <div class="row"><span>Pezzi disponibili / totali</span><strong>{{.PiecesAvailable}} / {{.PiecesTotal}} ({{percent .PiecesPercent}}%)</strong></div>
    <div class="row"><span>Pezzi completati</span><strong>{{.PiecesHave}}</strong></div>
    <div class="row"><span>Dimensione pezzo</span><strong>{{bytes .PieceLength}}</strong></div>
    <div class="row"><span>Dati sprecati</span><strong>{{bytes .Wasted}}</strong></div>
    <div class="row"><span>Allocato su disco</span><strong>{{bytes .Allocated}}</strong></div>
    <div class="row"><span>Aggiunto</span><strong>{{if .AddedStr}}{{.AddedStr}}{{else}}—{{end}}</strong></div>
    <div class="row"><span>Completato</span><strong>{{if .CompletedStr}}{{.CompletedStr}}{{else}}—{{end}}</strong></div>
  </div>
  <div class="toolbar">
    <form method="post" action="/ui/pin"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general"><input type="hidden" name="pinned" value="{{if .Pinned}}0{{else}}1{{end}}"><button type="submit">{{if .Pinned}}Togli pin{{else}}Pin (fuori coda){{end}}</button></form>
    <button type="button" onclick="copyMagnet(this)" data-magnet="{{.Magnet}}">Copia magnet</button>
    <a class="btn" style="padding:5px 10px;border-radius:8px;border:1px solid #2b3a55;background:#1b2536" href="/ui/torrent-file?hash={{.Hash}}" download>Esporta .torrent</a>
  </div>
  <form class="form-grid" method="post" action="/ui/seed-limits">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general">
    <label>Ratio seed (-1 globale, 0 infinito)<input type="number" step="0.01" name="seed_ratio" value="{{.SeedRatio}}"></label>
    <label>Giorni seed (-1 globale, 0 infinito)<input type="number" name="seed_days" value="{{.SeedDays}}"></label>
    <button type="submit">Salva limiti seed</button>
  </form>
  <form class="form-grid" method="post" action="/ui/move">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general">
    <label>Sposta i dati in<input type="text" name="destination" value="{{.SavePath}}" size="48"></label>
    <button type="submit">Sposta</button>
  </form>
{{else if eq .Tab "files"}}
  {{if .Files}}
  <table>
    <thead><tr><th>File</th><th class="num">Dimensione</th><th class="num">Scaricato</th><th>Priorità</th></tr></thead>
    <tbody>
    {{range .Files}}
      <tr>
        <td class="name">{{.Path}}</td>
        <td class="num">{{bytes .Size}}</td>
        <td class="num">{{bytes .Done}}</td>
        <td><form method="post" action="/ui/file-priority" onchange="this.submit()">
          <input type="hidden" name="hash" value="{{$.Hash}}"><input type="hidden" name="tab" value="files"><input type="hidden" name="index" value="{{.Index}}">
          <select name="priority"><option value="0"{{if not .Wanted}} selected{{end}}>Salta</option><option value="4"{{if .Wanted}} selected{{end}}>Scarica</option></select>
        </form></td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">Metadati non ancora disponibili.</p>{{end}}
{{else if eq .Tab "peers"}}
  {{if .Peers}}
  <table>
    <thead><tr><th>Peer</th><th>Client</th><th>Sorgente</th><th class="num">↓</th><th class="num">↑</th><th class="num">Prog.</th><th>Seed</th><th>Flag</th><th class="num">Da</th></tr></thead>
    <tbody>
    {{range .Peers}}
      <tr><td>{{.Address}}</td><td class="name">{{if .Client}}{{.Client}}{{else}}—{{end}}</td>
      <td>{{.Source}}</td>
      <td class="num">{{rate .Down}}</td><td class="num">{{rate .Up}}</td>
      <td class="num">{{printf "%.1f" .Progress}}%</td><td>{{if .Seed}}sì{{else}}no{{end}}</td>
      <td class="flags">{{if .Incoming}}<span class="flag">entrata</span>{{else}}<span class="flag">uscita</span>{{end}}{{if .UTP}}<span class="flag">uTP</span>{{else}}<span class="flag">TCP</span>{{end}}{{if .Encrypted}}<span class="flag on">cifrata</span>{{end}}{{if .Handshake}}<span class="flag on">HS cifrato</span>{{end}}{{if .Snubbed}}<span class="flag">snubbed</span>{{end}}{{if .Optimistic}}<span class="flag on">optimistic</span>{{end}}{{if .ClientChoke}}<span class="flag">ci choka</span>{{end}}{{if .PeerChoke}}<span class="flag">lo chokiamo</span>{{end}}{{if .ClientInt}}<span class="flag">interessato</span>{{end}}{{if .PeerInt}}<span class="flag">interessato a noi</span>{{end}}{{if .Downloading}}<span class="flag">scarica</span>{{end}}</td>
      <td class="num">{{dur .Connected}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">Nessun peer connesso.</p>{{end}}
{{else if eq .Tab "trackers"}}
  {{if .Trackers}}
  <table>
    <thead><tr><th>URL</th><th>Stato</th><th>Messaggio</th><th class="num">Seed</th><th class="num">Peer</th><th class="num">Prossimo</th></tr></thead>
    <tbody>
    {{range .Trackers}}
      <tr><td class="name">{{.URL}}</td><td>{{.Status}}</td><td class="name">{{.Message}}</td>
      <td class="num">{{.Seeders}}</td><td class="num">{{.Leechers}}</td>
      <td class="num">{{if gt .Next 0}}{{dur .Next}}{{else}}—{{end}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">Nessun tracker.</p>{{end}}
  <form class="form-grid" method="post" action="/ui/trackers">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="trackers">
    <label>Aggiungi tracker (uno per riga)<textarea name="urls" rows="3" cols="60" placeholder="https://tracker.example/announce"></textarea></label>
    <button type="submit">Aggiungi tracker</button>
  </form>
{{end}}
{{end}}`

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

// handleUILive renders only the cards and the table, for the page's periodic
// partial refresh (no full reload, form state preserved).
func (d *Daemon) handleUILive(w http.ResponseWriter, r *http.Request) {
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
	_ = uiTemplate.ExecuteTemplate(w, "live", page)
}

// handleUIDetail renders one tab of a torrent's detail, as a fragment for the
// modal.
func (d *Daemon) handleUIDetail(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	data, err := d.uiDetailData(r.URL.Query().Get("hash"), r.URL.Query().Get("tab"))
	if err != nil {
		http.Error(w, "torrent non trovato", http.StatusNotFound)
		return
	}
	// Render to a buffer first: a template error must not leave a half-written
	// fragment (the page would silently truncate).
	var buf bytes.Buffer
	if err := uiTemplate.ExecuteTemplate(&buf, "detail", data); err != nil {
		http.Error(w, "rendering error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// handleUITorrentFile streams the .torrent of one torrent, so the page can offer
// the export without exposing the API (the browser carries the page cookie).
func (d *Daemon) handleUITorrentFile(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "torrent non trovato", http.StatusNotFound)
		return
	}
	data, err := t.Torrent()
	if err != nil || len(data) == 0 {
		http.Error(w, "metadati non disponibili", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="`+t.InfoHash().String()+`.torrent"`)
	_, _ = w.Write(data)
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

// handleUIBulk applies one action to every selected torrent.
func (d *Daemon) handleUIBulk(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	hashes := r.Form["hashes"]
	if len(hashes) == 0 {
		d.uiDone(w, r, "", fmt.Errorf("nessun torrent selezionato"))
		return
	}
	var firstErr error
	for _, hash := range hashes {
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
		case "remove":
			err = d.remove(hash, false)
		case "remove-files":
			err = d.remove(hash, true)
		default:
			err = fmt.Errorf("azione sconosciuta: %s", op)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	d.uiDone(w, r, fmt.Sprintf("%d torrent: %s", len(hashes), uiActionMessage(op)), firstErr)
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

// handleUIAdd adds a torrent from a magnet or from an http(s) URL to a
// .torrent, with the usual destination/pause/top options. A local .torrent is
// handled by handleUIAddFile.
func (d *Daemon) handleUIAdd(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		source = strings.TrimSpace(r.FormValue("magnet"))
	}
	if source == "" {
		d.uiDone(w, r, "", fmt.Errorf("incolla un link magnet o un URL .torrent"))
		return
	}
	req := addRequest{
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	}
	lower := strings.ToLower(source)
	switch {
	case strings.HasPrefix(lower, "magnet:"):
		req.Magnet = source
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		data, err := fetchRemoteTorrent(source)
		if err != nil {
			d.uiDone(w, r, "", fmt.Errorf("download: %w", err))
			return
		}
		trimmed := strings.TrimSpace(string(data))
		switch {
		case strings.HasPrefix(strings.ToLower(trimmed), "magnet:"):
			req.Magnet = trimmed
		case len(data) > 0 && data[0] == 'd':
			req.TorrentData = data
		default:
			d.uiDone(w, r, "", fmt.Errorf("l'URL non contiene un file .torrent"))
			return
		}
	default:
		d.uiDone(w, r, "", fmt.Errorf("incolla un link magnet o un URL .torrent"))
		return
	}
	_, existing, err := d.add(req)
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	msg := "Torrent aggiunto"
	if existing {
		msg = "Torrent già presente"
	}
	d.uiDone(w, r, msg, nil)
}

// fetchRemoteTorrent downloads a .torrent from an http(s) URL, bounded in size
// and time. A magnet link served as plain text is returned as-is so the caller
// can still add it.
func fetchRemoteTorrent(rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gx-torrent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTorrentFile))
}

// handleUIAddFile adds a torrent from an uploaded .torrent, with the usual
// destination/pause/top options; it is the file counterpart of /ui/add.
func (d *Daemon) handleUIAddFile(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentFile+1<<20)
	if err := r.ParseMultipartForm(maxTorrentFile); err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	file, _, err := r.FormFile("torrent")
	if err != nil {
		d.uiDone(w, r, "", fmt.Errorf("file .torrent mancante"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTorrentFile))
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	_, existing, err := d.add(addRequest{
		TorrentData:    data,
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	})
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	msg := "Torrent aggiunto"
	if existing {
		msg = "Torrent già presente"
	}
	d.uiDone(w, r, msg, nil)
}

func (d *Daemon) handleUIIPFilter(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		d.uiDone(w, r, "", fmt.Errorf("indica un URL o un percorso file"))
		return
	}
	path := source
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		fetched, err := d.fetchIPFilterURL(source)
		if err != nil {
			d.uiDone(w, r, "", fmt.Errorf("download: %w", err))
			return
		}
		path = fetched
	}
	rules, err := d.loadIPFilter(path)
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	d.uiDone(w, r, fmt.Sprintf("Filtro IP caricato: %d regole", rules), nil)
}

// handleUIFilePriority selects or skips one file of a torrent. rain has no
// priority levels: 0 = skip, any positive value = download.
func (d *Daemon) handleUIFilePriority(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	index, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("index")))
	priority, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
	if priority < 0 {
		priority = 0
	}

	d.mu.Lock()
	t, meta := d.findLocked(hash)
	if t == nil {
		d.mu.Unlock()
		d.uiDoneDetail(w, r, hash, "files", "", errNotFound)
		return
	}
	previous := append([]int(nil), meta.FilePriorities...)
	d.mu.Unlock()

	files, err := t.Files()
	if err != nil {
		d.uiDoneDetail(w, r, hash, "files", "", fmt.Errorf("metadati non disponibili"))
		return
	}
	priorities := make([]int, len(files))
	for i := range priorities {
		priorities[i] = 4
	}
	for i := 0; i < len(priorities) && i < len(previous); i++ {
		priorities[i] = previous[i]
	}
	if index >= 0 && index < len(priorities) {
		priorities[index] = priority
	}
	err = d.setFilePriorities(hash, priorities)
	d.uiDoneDetail(w, r, hash, "files", "Priorità del file aggiornata", err)
}

func (d *Daemon) handleUITrackers(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	var urls []string
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		d.uiDoneDetail(w, r, hash, "trackers", "", fmt.Errorf("nessun tracker da aggiungere"))
		return
	}
	err := d.addTrackers(hash, urls)
	d.uiDoneDetail(w, r, hash, "trackers", fmt.Sprintf("%d tracker aggiunti", len(urls)), err)
}

func (d *Daemon) handleUISeedLimits(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	ratio := formFloat(r, "seed_ratio", -1)
	days := formInt(r, "seed_days", -1)
	err := d.setSeedLimits(hash, &ratio, &days)
	d.uiDoneDetail(w, r, hash, "general", "Limiti di seed salvati", err)
}

func (d *Daemon) handleUIMove(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	destination := strings.TrimSpace(r.FormValue("destination"))
	if destination == "" {
		d.uiDoneDetail(w, r, hash, "general", "", fmt.Errorf("indica la destinazione"))
		return
	}
	err := d.move(hash, destination, false)
	d.uiDoneDetail(w, r, hash, "general", "Spostamento avviato", err)
}

func (d *Daemon) handleUIPin(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	err := d.setPin(hash, formBool(r, "pinned"))
	d.uiDoneDetail(w, r, hash, "general", "Pin aggiornato", err)
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
	case "remove":
		return "rimossi (file conservati)"
	case "remove-files":
		return "rimossi con i file"
	}
	return "Fatto"
}

// uiDone redirects back to the page with a short message. A redirect (instead of
// rendering) keeps a refresh from repeating the action.
func (d *Daemon) uiDone(w http.ResponseWriter, r *http.Request, okMessage string, err error) {
	d.uiRedirect(w, r, nil, okMessage, err)
}

// uiDoneDetail is uiDone but reopens the torrent's detail tab after the action.
func (d *Daemon) uiDoneDetail(w http.ResponseWriter, r *http.Request, hash, tab, okMessage string, err error) {
	extra := url.Values{}
	if strings.TrimSpace(hash) != "" {
		extra.Set("open", hash)
		extra.Set("tab", tab)
	}
	d.uiRedirect(w, r, extra, okMessage, err)
}

func (d *Daemon) uiRedirect(w http.ResponseWriter, r *http.Request, extra url.Values, okMessage string, err error) {
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
	for key, values := range extra {
		for _, value := range values {
			query.Set(key, value)
		}
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
		TotalDown:    stats.Session["bytes_downloaded"],
		TotalUp:      stats.Session["bytes_uploaded"],
		PeerPort:     stats.PeerPort,
		Listen:       stats.ListenAddress,
		DHT:          stats.DHT,
		DHTNodes:     stats.Session["dht_nodes"],
		UTP:          stats.UTP,
		Encryption:   uiEncryption(stats.Encryption),
		Proxy:        stats.Proxy,
		IPFilter:     stats.IPFilterRules,
		IPFilterPath: stats.IPFilterPath,
		CacheReadMB:  stats.CacheReadMB,
		CacheWB:      stats.CacheWriteMB,
		LSDPeers:     stats.LSD.PeersFound,
		DiskFree:     stats.DiskFreeBytes,
		DiskTotal:    stats.DiskTotalBytes,
	}
	switch {
	case stats.PortMapping.Method != "":
		page.PortOpen = true
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
			TotalDone: view.TotalDone,
			TotalSize: view.TotalSize,
			DLRate:    int64(view.DownloadRate),
			ULRate:    int64(view.UploadRate),
		}
		if view.Progress >= 99.99 && view.TotalSize > 0 {
			if view.SeedRatio > 0 {
				row.Ratio = fmt.Sprintf("%.2f", view.SeedRatio)
				row.RatioVal = view.SeedRatio
			} else {
				row.Ratio = "∞"
			}
		} else if view.ETASeconds >= 0 {
			row.ETA = uiDuration(view.ETASeconds)
			row.ETAVal = view.ETASeconds
		}
		page.Rows = append(page.Rows, row)
	}
	sort.Slice(page.Rows, func(i, j int) bool {
		if page.Rows[i].State != page.Rows[j].State {
			return page.Rows[i].State < page.Rows[j].State
		}
		return page.Rows[i].Name < page.Rows[j].Name
	})
	page.CountAll = len(page.Rows)
	for _, row := range page.Rows {
		switch row.State {
		case "downloading", "downloading_metadata", "checking_files":
			page.CountDown++
		case "seeding":
			page.CountSeeding++
		case "stalled":
			page.CountStalled++
		case "moving":
			page.CountMoving++
		case "error":
			page.CountError++
		case "paused":
			page.CountPaused++
		}
	}
	return page, nil
}

// uiDetailData builds one torrent's detail tab. The torrent pointers are read
// under the daemon lock, the per-torrent calls (files/peers/trackers) run
// outside it, like the REST inspection does.
func (d *Daemon) uiDetailData(hash, tab string) (uiDetailData, error) {
	if tab != "files" && tab != "peers" && tab != "trackers" {
		tab = "general"
	}
	d.mu.Lock()
	t, meta := d.findLocked(hash)
	if t == nil {
		d.mu.Unlock()
		return uiDetailData{}, errNotFound
	}
	id := t.ID()
	stats := d.statsLocked(t)
	rt := d.runtimeLocked(id)
	name := stats.Name
	state := stateFor(meta, stats, d.moving[id])
	savePath := meta.SavePath
	seedRatio := meta.SeedRatio
	seedDays := meta.SeedDays
	pinned := meta.Pinned
	swarmSeeds := meta.SwarmSeeds
	swarmPeers := meta.SwarmPeers
	metaError := meta.Error
	addedAt := meta.AddedAt.Unix()
	completedAt := int64(0)
	if !meta.CompletedAt.IsZero() {
		completedAt = meta.CompletedAt.Unix()
	}
	d.mu.Unlock()

	progress := 0.0
	if stats.Bytes.Total > 0 {
		progress = float64(stats.Bytes.Completed) * 100 / float64(stats.Bytes.Total)
	}
	eta := int64(-1)
	if stats.ETA != nil {
		eta = int64(stats.ETA.Seconds())
	}
	ratio := 0.0
	if stats.Bytes.Downloaded > 0 {
		ratio = float64(stats.Bytes.Uploaded) / float64(stats.Bytes.Downloaded)
	}
	data := uiDetailData{
		Hash: t.InfoHash().String(), Name: name, Tab: tab, State: state, Error: metaError,
		SavePath: savePath, Progress: progress, TotalSize: stats.Bytes.Total,
		TotalDone: stats.Bytes.Completed, Downloaded: stats.Bytes.Downloaded, Uploaded: stats.Bytes.Uploaded,
		DownRate: int64(stats.Speed.Download), UpRate: int64(stats.Speed.Upload), Ratio: ratio, ETA: eta,
		NumPeers: stats.Peers.Total, NumSeeds: rt.numSeeds, NumComplete: swarmSeeds, NumIncomplete: swarmPeers,
		SeedRatio: seedRatio, SeedDays: seedDays, Pinned: pinned, Private: stats.Private,
		PiecesTotal: stats.Pieces.Total, PiecesHave: stats.Pieces.Have, PiecesAvailable: stats.Pieces.Available,
		PiecesChecked: stats.Pieces.Checked, PieceLength: int64(stats.PieceLength),
		Wasted: stats.Bytes.Wasted, Allocated: stats.Bytes.Allocated, FileCount: stats.FileCount,
		AddedAt: addedAt, CompletedAt: completedAt,
	}
	if data.PiecesTotal > 0 {
		data.PiecesPercent = float64(data.PiecesAvailable) * 100 / float64(data.PiecesTotal)
	}
	if addedAt > 0 {
		data.AddedStr = time.Unix(addedAt, 0).Format("2006-01-02 15:04")
	}
	if completedAt > 0 {
		data.CompletedStr = time.Unix(completedAt, 0).Format("2006-01-02 15:04")
	}

	trackers := t.Trackers()
	switch tab {
	case "files":
		files, err := t.Files()
		if err != nil {
			return data, nil
		}
		done := map[int]int64{}
		if fileStats, err := t.FileStats(); err == nil {
			for index, stat := range fileStats {
				done[index] = stat.BytesCompleted
			}
		}
		d.mu.Lock()
		meta = d.metaLocked(t)
		priorities := append([]int(nil), meta.FilePriorities...)
		d.mu.Unlock()
		for index, file := range files {
			data.Files = append(data.Files, uiFileRow{
				Index: index, Path: file.Path(), Size: file.Length(), Done: done[index],
				Wanted: filePriorityMeta(priorities, index) > 0,
			})
		}
	case "peers":
		for _, peer := range t.Peers() {
			data.Peers = append(data.Peers, uiPeerRow{
				Address: peer.Addr.String(), Client: peer.Client,
				Down: int64(peer.DownloadSpeed), Up: int64(peer.UploadSpeed), Progress: peer.Progress * 100,
				Seed: peer.Seed, Incoming: peer.Source == torrent.SourceIncoming,
				Encrypted: peer.EncryptedStream, UTP: peer.UTP,
				Source: sourceName(peer.Source), Downloading: peer.Downloading,
				ClientInt: peer.ClientInterested, ClientChoke: peer.ClientChoking,
				PeerInt: peer.PeerInterested, PeerChoke: peer.PeerChoking,
				Optimistic: peer.OptimisticUnchoked, Snubbed: peer.Snubbed,
				Handshake: peer.EncryptedHandshake,
				Connected: int64(time.Since(peer.ConnectedAt).Seconds()),
			})
		}
	case "trackers":
		for _, tracker := range trackers {
			status := "non contattato"
			switch tracker.Status {
			case torrent.Working:
				status = "funziona"
			case torrent.Contacting:
				status = "contatto in corso"
			case torrent.NotWorking:
				status = "non funziona"
			}
			message := tracker.Warning
			if tracker.Error != nil {
				message = tracker.Error.Error()
			}
			next := int64(0)
			if !tracker.NextAnnounce.IsZero() {
				next = int64(time.Until(tracker.NextAnnounce).Seconds())
			}
			data.Trackers = append(data.Trackers, uiTrackerRow{
				URL: tracker.URL, Status: status, Message: message,
				Seeders: tracker.Seeders, Leechers: tracker.Leechers, Next: next,
			})
		}
	}
	// Magnet link for the "copy magnet" button.
	data.Magnet = magnetLink(data.Hash, data.Name, trackers)
	return data, nil
}

// sourceName labels where a peer was discovered.
func sourceName(source torrent.PeerSource) string {
	switch source {
	case torrent.SourceTracker:
		return "tracker"
	case torrent.SourceDHT:
		return "DHT"
	case torrent.SourcePEX:
		return "PEX"
	case torrent.SourceIncoming:
		return "in entrata"
	case torrent.SourceManual:
		return "manuale"
	default:
		return "—"
	}
}

// magnetLink rebuilds a magnet from the info hash, name and trackers.
func magnetLink(hash, name string, trackers []torrent.Tracker) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hash)
	if strings.TrimSpace(name) != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(name))
	}
	for _, tracker := range trackers {
		if strings.TrimSpace(tracker.URL) == "" {
			continue
		}
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tracker.URL))
	}
	return b.String()
}

// filePriorityMeta reports one file's priority from a raw priority slice.
func filePriorityMeta(priorities []int, index int) int {
	if index < len(priorities) {
		if priorities[index] <= 0 {
			return 0
		}
		return priorities[index]
	}
	return 4
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
