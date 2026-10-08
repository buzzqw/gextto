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

	"github.com/cenkalti/rain/v2/torrent"
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
	ExternalIP   string
	PortOpen     bool
	DHT          bool
	DHTNodes     int64
	UTP          bool
	Encryption   string
	Proxy        bool
	IPFilter     int
	IPFilterPath string
	// IPFilterSource prefills the IP filter field with Gextto's setting.
	IPFilterSource string
	// GexttoLog enables the Gextto log tab.
	GexttoLog   bool
	CacheReadMB int64
	CacheWB     int64
	LSDPeers    int64
	DiskFree    int64
	DiskTotal   int64

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
body{margin:0;font:16px/1.55 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#0f1420;color:#e7ecf3}
header{padding:14px 20px;border-bottom:1px solid #223;display:flex;gap:12px;align-items:baseline;flex-wrap:wrap}
header h1{font-size:20px;margin:0;font-weight:600}
main{padding:16px 20px;max-width:none;width:100%;margin:0}
.muted{color:#93a1b5;font-size:14px}
a{color:#93c5fd}
.cards{display:grid;grid-template-columns:repeat(8,minmax(0,1fr));gap:6px;margin-bottom:12px}
.card{min-width:0;background:#161d2c;border:1px solid #243049;border-radius:8px;padding:6px 10px}
.card b{display:block;font-size:16px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.card span{display:block;color:#93a1b5;font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
@media(max-width:1100px){.cards{grid-template-columns:repeat(auto-fill,minmax(140px,1fr))}}
.toolbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;background:#161d2c;border:1px solid #243049;border-radius:10px;padding:12px;margin-bottom:12px}
.toolbar form{display:flex;gap:6px;align-items:center;margin:0;flex-wrap:wrap}
.toolbar input[type=text],.toolbar input[type=search]{min-width:200px}
.chk{display:flex;gap:4px;align-items:center;color:#93a1b5;font-size:12px}
table{width:100%;border-collapse:collapse;background:#161d2c;border:1px solid #243049;border-radius:10px;overflow:hidden}
th,td{padding:10px 14px;text-align:left;border-bottom:1px solid #1f2839;font-size:15px;vertical-align:middle}
th{color:#93a1b5;font-weight:500;font-size:13px;text-transform:uppercase;letter-spacing:.03em;cursor:pointer;user-select:none}
tr:last-child td{border-bottom:0}
td.num,th.num{text-align:right;white-space:nowrap}
td.sel,th.sel{width:26px;text-align:center}
tbody.t td{border-bottom:0;padding:4px 8px}
#torrents thead th{padding:6px 8px}
tbody.t tr.l1 td{padding-top:9px}
tbody.t tr.l2 td{padding-bottom:9px;font-size:14px}
tbody.t+tbody.t tr.l1 td{border-top:1px solid #1f2839}
tbody.t:hover td{background:#1a2234}
thead tr.h1 th{border-bottom:0;padding-bottom:2px}
td.name{overflow-wrap:anywhere}
td.name a{display:block;max-width:min(70ch,100%);font-weight:600;text-decoration:none}
td.name a:hover{text-decoration:underline}
td.name .muted{display:block;font-size:12px}
#torrents th.num,#torrents td.num{min-width:72px}
td.prog{min-width:110px}
td.prog .bar{margin-top:0}
td.prog small{color:#93a1b5;font-size:12px}
td.actions{vertical-align:middle;width:1%}
td.actions .btns{display:flex;flex-wrap:wrap;gap:3px;justify-content:flex-end;width:150px;margin-left:auto}
td.actions button{padding:3px 8px;font-size:13px}
td.actions form{margin:0}
.state{font-size:12px;padding:1px 8px;border-radius:999px;background:#243049;white-space:nowrap}
.s-seeding{background:#14532d;color:#86efac}
.s-downloading,.s-checking_files,.s-downloading_metadata{background:#172f4f;color:#93c5fd}
.s-stalled,.s-paused{background:#4a3410;color:#fcd34d}
.s-error{background:#4a1520;color:#fca5a5}
.s-moving{background:#3b2a4a;color:#d8b4fe}
.bar{height:7px;background:#243049;border-radius:99px;overflow:hidden;margin-top:3px;min-width:80px}
.bar i{display:block;height:100%;background:#3b82f6}
.actions{white-space:nowrap;text-align:right}
.actions form{display:inline-block;margin:0 2px 2px 0}
input[type=text],input[type=search],input[type=password],input[type=number],select{padding:10px;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit;font-size:16px}
button{padding:8px 14px;border-radius:8px;border:1px solid #2b3a55;background:#1b2536;color:#dbe4f0;font-size:15px;cursor:pointer}
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
.stat-grid .row span{color:#93a1b5;font-size:13px}
.stat-grid .row strong{font-weight:500;text-align:right;word-break:break-word}
.form-grid{display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;margin:10px 0;padding-top:10px;border-top:1px solid #1f2839}
.form-grid label{display:flex;flex-direction:column;gap:3px;color:#93a1b5;font-size:13px}
.layout{display:flex;gap:14px;align-items:flex-start}
.sidebar{display:flex;flex-direction:column;gap:4px;min-width:180px;background:#161d2c;border:1px solid #243049;border-radius:10px;padding:10px}
.filter{display:flex;justify-content:space-between;gap:8px;text-align:left;background:transparent;border:0;border-radius:8px;padding:6px 8px;color:#dbe4f0}
.filter:hover{background:#243049}
.filter.on{background:#2563eb;color:#fff}
.content{flex:1;min-width:0}
.statusbar{position:sticky;bottom:0;margin-top:12px;display:flex;gap:18px;flex-wrap:wrap;align-items:center;background:#131a28;border:1px solid #243049;border-radius:10px;padding:10px 14px;font-size:14px;color:#93a1b5}
.statusbar b{color:#e7ecf3;font-weight:600}
.toast{position:fixed;top:14px;right:14px;z-index:80;background:#14351f;border:1px solid #1f6b3a;color:#e7ecf3;padding:10px 14px;border-radius:10px;box-shadow:0 6px 24px rgba(0,0,0,.4);max-width:420px;transition:opacity .4s}
.toast.err{background:#3a1417;border-color:#7f1d1d;color:#fca5a5}
.tabs{display:flex;gap:6px;margin-bottom:12px}
.tabs button.on{background:#2563eb;border-color:#2563eb;color:#fff}
.logbar{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-bottom:8px}
.logview{background:#0b0f18;border:1px solid #243049;border-radius:10px;padding:10px 12px;margin:0;max-height:75vh;overflow:auto;font:12.5px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}
.logview .warn{color:#fcd34d}
.logview .error{color:#fca5a5}
.logview .debug{color:#7c8aa0}
.flag{font-size:12px;padding:1px 6px;border-radius:6px;background:#243049;color:#93a1b5;white-space:nowrap}
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
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>gx-torrent</title>
<style>` + uiStyle + `</style>
</head>
<body>
<header>
  <h1>gx-torrent</h1>
  <span class="muted">{{.Version}} · up for {{.Uptime}} · {{.Now}}</span>
</header>
<main>
  {{if .Notice}}<div class="notice{{if .Error}} err{{end}}">{{.Notice}}</div>{{end}}

  <div class="toolbar">
    <form method="post" action="/ui/add">
      <input type="text" name="source" placeholder="paste magnet:… or https://…/file.torrent" autocomplete="off">
      <input type="text" name="destination" placeholder="destination (empty = default)" autocomplete="off">
      <label class="chk"><input type="checkbox" name="paused" value="1"> paused</label>
      <label class="chk"><input type="checkbox" name="top" value="1"> top</label>
      <label class="chk" title="Download pieces in order (streaming); slower overall"><input type="checkbox" name="sequential" value="1"> sequential</label>
      <label class="chk" title="Download the ends of every file first"><input type="checkbox" name="first_last" value="1"> first/last</label>
      <button class="primary" type="submit">Add</button>
    </form>
    <form method="post" action="/ui/add-file" enctype="multipart/form-data">
      <input type="file" name="torrent" accept=".torrent,application/x-bittorrent" required title=".torrent file to add">
      <input type="text" name="destination" placeholder="destination (empty = default)" autocomplete="off">
      <label class="chk"><input type="checkbox" name="paused" value="1"> paused</label>
      <label class="chk"><input type="checkbox" name="top" value="1"> top</label>
      <label class="chk" title="Download pieces in order (streaming); slower overall"><input type="checkbox" name="sequential" value="1"> sequential</label>
      <label class="chk" title="Download the ends of every file first"><input type="checkbox" name="first_last" value="1"> first/last</label>
      <button type="submit">Add .torrent</button>
    </form>
    <form method="post" action="/ui/ipfilter">
      <span class="muted">IP filter</span>
      <input type="text" name="source" placeholder="URL or local file" autocomplete="off" value="{{.IPFilterSource}}" style="min-width:min(520px,70vw)" title="Prefilled with the IP filter configured in Gextto">
      <button type="submit" title="Download (if a URL) and apply the IP filter now">Load filter</button>
    </form>
    <input type="search" id="filter" placeholder="Filter torrents…" oninput="filterRows()" autocomplete="off">
  </div>

  <div class="toolbar">
    <span class="muted">Selected: <b id="selcount">0</b></span>
    <button type="button" onclick="bulk('resume')">▶ Resume</button>
    <button type="button" onclick="bulk('pause')">⏸ Pause</button>
    <button type="button" onclick="bulk('verify')">✓ Recheck</button>
    <button type="button" onclick="bulk('reannounce')">↻ Reannounce</button>
    <button type="button" onclick="bulk('top')">⤒ Top</button>
    <button type="button" class="danger" onclick="bulk('remove')">✕ Remove</button>
    <button type="button" class="danger" onclick="bulk('remove-files')">✕ Remove and delete files</button>
  </div>

  {{if .GexttoLog}}
  <div class="tabs">
    <button type="button" class="on" id="tab-torrents" onclick="showTab('torrents')">Torrents</button>
    <button type="button" id="tab-log" onclick="showTab('log')">Gextto log</button>
  </div>
  {{end}}
  <div id="pane-torrents"><div id="live">{{template "live" .}}</div></div>
  {{if .GexttoLog}}
  <div id="pane-log" style="display:none">
    <div class="logbar">
      <label class="chk">Lines <select id="log-lines" onchange="loadLog()"><option>200</option><option selected>500</option><option>1000</option><option>2000</option></select></label>
      <input type="search" id="log-filter" placeholder="Filter lines…" oninput="renderLog()" autocomplete="off">
      <label class="chk"><input type="checkbox" id="log-nodebug" checked onchange="renderLog()"> hide DEBUG</label>
      <button type="button" onclick="loadLog()">↻ Reload</button>
      <span class="muted" id="log-info"></span>
    </div>
    <pre class="logview" id="log-view">Loading…</pre>
  </div>
  {{end}}
</main>

<div id="detail-modal" class="overlay" style="display:none">
  <div class="modal"><div id="detail-body"></div></div>
</div>

<script>
function openDetail(hash, tab){tab=tab||'general';fetch('/ui/detail?hash='+encodeURIComponent(hash)+'&tab='+encodeURIComponent(tab),{cache:'no-store'}).then(function(r){return r.text()}).then(function(h){document.getElementById('detail-body').innerHTML=h;document.getElementById('detail-modal').style.display='flex';});}
function closeDetail(){document.getElementById('detail-modal').style.display='none';}
function detailAction(hash,tab,path,params){var f=new URLSearchParams(params||{});f.set('hash',hash);f.set('tab',tab);fetch(path,{method:'POST',body:f,headers:{'Content-Type':'application/x-www-form-urlencoded'}}).then(function(){openDetail(hash,tab);refresh(true);});}
function refresh(force){if(window.__tab==='log')return;if(!force){if(document.querySelectorAll('.rowsel:checked').length>0)return;if(document.getElementById('detail-modal').style.display==='flex')return;}fetch('/ui/live',{cache:'no-store'}).then(function(r){return r.ok?r.text():null}).then(function(t){if(t){document.getElementById('live').innerHTML=t;applySort();updateSel();applyFilters();}});}
function rowState(tr){return tr.getAttribute('data-state')||'';}
function matchesState(st,f){if(f==='all')return true;if(f==='downloading')return st==='downloading'||st==='downloading_metadata'||st==='checking_files';return st===f;}
function applyFilters(){var q=(document.getElementById('filter').value||'').toLowerCase();var f=window.__stateFilter||'all';document.querySelectorAll('#live tbody.t').forEach(function(tr){var n=(tr.getAttribute('data-name')||'').toLowerCase();tr.style.display=((!q||n.indexOf(q)>=0)&&matchesState(rowState(tr),f))?'':'none';});}
function filterRows(){applyFilters();}
function filterByState(f,btn){window.__stateFilter=f;document.querySelectorAll('.sidebar .filter').forEach(function(b){b.classList.toggle('on',b===btn);});applyFilters();}
function updateSel(){document.getElementById('selcount').textContent=document.querySelectorAll('.rowsel:checked').length;}
function selectAll(box){document.querySelectorAll('.rowsel').forEach(function(c){c.checked=box.checked});updateSel();}
function bulk(op){var hashes=Array.prototype.map.call(document.querySelectorAll('.rowsel:checked'),function(c){return c.value});if(!hashes.length){showToast('Select at least one torrent',true);return;}if(op==='remove-files'&&!confirm('Remove the selected torrents AND delete the files? Irreversible.'))return;var f=new URLSearchParams();f.set('op',op);hashes.forEach(function(h){f.append('hashes',h)});fetch('/ui/bulk',{method:'POST',body:f,headers:{'Content-Type':'application/x-www-form-urlencoded'}}).then(function(){location.href='/';});}
function applySort(){var st=window.__sort;var tb=document.getElementById('torrents');if(!st||!tb)return;var groups=Array.prototype.slice.call(tb.querySelectorAll('tbody.t'));var attr='data-k-'+st.key;groups.sort(function(a,b){var av=a.getAttribute(attr)||'',bv=b.getAttribute(attr)||'';var an=parseFloat(av),bn=parseFloat(bv);var r=(!isNaN(an)&&!isNaN(bn))?an-bn:String(av).localeCompare(String(bv));return st.asc?r:-r;});groups.forEach(function(g){tb.appendChild(g)});}
function sortTable(key){var st=window.__sort;window.__sort={key:key,asc:!(st&&st.key===key&&st.asc)};applySort();}
function showToast(msg,err){var t=document.createElement('div');t.className='toast'+(err?' err':'');t.textContent=msg;document.body.appendChild(t);setTimeout(function(){t.style.opacity='0';setTimeout(function(){t.remove();},450);},4000);}
function copyMagnet(el){var text=el.getAttribute('data-magnet')||'';if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(text).then(function(){showToast('Magnet copied',false);},function(){showToast('Copy failed',true);});}else{showToast('Copy unavailable',true);}}
function showTab(name){window.__tab=name;document.getElementById('pane-torrents').style.display=name==='torrents'?'':'none';document.getElementById('pane-log').style.display=name==='log'?'':'none';document.getElementById('tab-torrents').classList.toggle('on',name==='torrents');document.getElementById('tab-log').classList.toggle('on',name==='log');if(name==='log'){loadLog();}else{refresh(true);}}
function loadLog(){var n=document.getElementById('log-lines').value;document.getElementById('log-info').textContent='loading…';fetch('/ui/gextto-log?lines='+encodeURIComponent(n),{cache:'no-store'}).then(function(r){return r.ok?r.json():r.text().then(function(t){throw new Error(t)})}).then(function(d){window.__log=d.lines||[];document.getElementById('log-info').textContent=d.path+' · '+(d.lines||[]).length+' lines · '+new Date().toLocaleTimeString();renderLog(true);}).catch(function(e){document.getElementById('log-view').textContent='Cannot read the Gextto log: '+e.message;document.getElementById('log-info').textContent='';});}
function renderLog(scroll){var view=document.getElementById('log-view');var q=(document.getElementById('log-filter').value||'').toLowerCase();var nodebug=document.getElementById('log-nodebug').checked;var frag=document.createDocumentFragment();(window.__log||[]).forEach(function(line){if(nodebug&&/\sDEBUG\s/.test(line))return;if(q&&line.toLowerCase().indexOf(q)<0)return;var row=document.createElement('div');if(/\s(ERROR|FATAL)\s/.test(line))row.className='error';else if(/\sWARN(ING)?\s/.test(line))row.className='warn';else if(/\sDEBUG\s/.test(line))row.className='debug';row.textContent=line;frag.appendChild(row);});view.textContent='';view.appendChild(frag);if(scroll!==false)view.scrollTop=view.scrollHeight;}
setInterval(function(){refresh(false)},2000);
(function(){document.addEventListener('change',function(e){if(e.target.classList.contains('rowsel'))updateSel();});document.addEventListener('keydown',function(e){if(e.key==='/'&&['INPUT','TEXTAREA','SELECT'].indexOf(document.activeElement.tagName)<0){e.preventDefault();document.getElementById('filter').focus();}if(e.key==='Escape'){closeDetail();}});var n=document.querySelector('.notice');if(n){showToast(n.textContent,n.classList.contains('err'));n.remove();}var p=new URLSearchParams(location.search);if(p.get('open')){openDetail(p.get('open'),p.get('tab')||'general');}})();
</script>
</body>
</html>`

const uiLiveTemplate = `{{define "live"}}
  <div class="cards">
    <div class="card"><b>{{.Torrents}}</b><span>torrent</span></div>
    <div class="card"><b>{{.Down}}</b><span>downloading</span></div>
    <div class="card"><b>{{.Seeding}}</b><span>seeding</span></div>
    <div class="card"><b>{{.Stalled}}</b><span>stalled</span></div>
    <div class="card"><b>{{.Paused}}</b><span>paused</span></div>
    <div class="card"><b>{{.DownloadRate}}</b><span>↓ speed</span></div>
    <div class="card"><b>{{.UploadRate}}</b><span>↑ speed</span></div>
    <div class="card" title="Downloaded in this session"><b>{{bytes .TotalDown}}</b><span>downloaded</span></div>
    <div class="card" title="Uploaded in this session"><b>{{bytes .TotalUp}}</b><span>uploaded</span></div>
    <div class="card"><b>{{.PeerPort}}</b><span>peer port</span></div>
    <div class="card" title="Router port mapping"><b>{{.Router}}</b><span>{{if .ExternalIP}}{{.ExternalIP}}{{else}}router{{end}}</span></div>
    <div class="card"><b>DHT {{if .DHT}}on{{else}}off{{end}}</b><span>{{.DHTNodes}} nodes · uTP {{if .UTP}}on{{else}}off{{end}}</span></div>
    <div class="card"{{if .IPFilterPath}} title="{{.IPFilterPath}}"{{end}}><b>{{if .IPFilter}}{{.IPFilter}}{{else}}none{{end}}</b><span>IP filter rules</span></div>
    <div class="card" title="Read / write cache"><b>{{.CacheReadMB}}/{{.CacheWB}} MB</b><span>cache r/w</span></div>
    <div class="card" title="Free space of {{bytes .DiskTotal}}"><b>{{bytes .DiskFree}}</b><span>free of {{bytes .DiskTotal}}</span></div>
  </div>

  {{if .Rows}}
  <div class="layout">
    <aside class="sidebar">
      <div class="muted">Filters</div>
      <button type="button" class="filter on" onclick="filterByState('all',this)">All <b>{{.CountAll}}</b></button>
      <button type="button" class="filter" onclick="filterByState('downloading',this)">Downloading <b>{{.CountDown}}</b></button>
      <button type="button" class="filter" onclick="filterByState('seeding',this)">Seeding <b>{{.CountSeeding}}</b></button>
      <button type="button" class="filter" onclick="filterByState('paused',this)">Paused <b>{{.CountPaused}}</b></button>
      <button type="button" class="filter" onclick="filterByState('stalled',this)">Stalled <b>{{.CountStalled}}</b></button>
      <button type="button" class="filter" onclick="filterByState('moving',this)">Moving <b>{{.CountMoving}}</b></button>
      <button type="button" class="filter" onclick="filterByState('error',this)">Error <b>{{.CountError}}</b></button>
    </aside>
    <div class="content">
    <table id="torrents">
    <thead>
    <tr class="h1">
      <th class="sel" rowspan="2"><input type="checkbox" title="Select all" onclick="selectAll(this)"></th>
      <th colspan="9" onclick="sortTable('name')">Name</th>
      <th class="actions" rowspan="2">Actions</th>
    </tr>
    <tr>
      <th onclick="sortTable('progress')">Progress</th>
      <th onclick="sortTable('state')">State</th>
      <th class="num" onclick="sortTable('done')">Done / Size</th>
      <th class="num" onclick="sortTable('down')">↓</th>
      <th class="num" onclick="sortTable('up')">↑</th>
      <th class="num" onclick="sortTable('peers')">Peer</th>
      <th class="num" onclick="sortTable('seeds')">Seed</th>
      <th class="num" onclick="sortTable('ratio')">Ratio</th>
      <th class="num" onclick="sortTable('eta')">ETA</th>
    </tr>
    </thead>
    {{range .Rows}}
    <tbody class="t" data-name="{{.Name}}" data-state="{{.State}}" data-k-name="{{.Name}}" data-k-progress="{{.Progress}}" data-k-state="{{.State}}" data-k-done="{{.TotalDone}}" data-k-down="{{.DLRate}}" data-k-up="{{.ULRate}}" data-k-peers="{{.Peers}}" data-k-seeds="{{.Seeds}}" data-k-ratio="{{.RatioVal}}" data-k-eta="{{.ETAVal}}">
      <tr class="l1">
        <td class="sel" rowspan="2"><input class="rowsel" type="checkbox" value="{{.Hash}}"></td>
        <td class="name" colspan="9"><a href="#" onclick="openDetail('{{.Hash}}');return false" title="Open torrent details">{{.Name}}</a><span class="muted">{{.SavePath}}</span></td>
        <td class="actions" rowspan="2"><div class="btns">
          <button type="button" onclick="openDetail('{{.Hash}}')" title="Details: files, peers, trackers">⋯</button>
          {{if eq .State "paused"}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="resume"><button title="Resume">▶</button></form>
          {{else}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="pause"><button title="Pause">⏸</button></form>
          {{end}}
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="verify"><button title="Recheck data on disk">✓</button></form>
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="reannounce"><button title="Reannounce to trackers">↻</button></form>
          <form method="post" action="/ui/action"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="op" value="top"><button title="Move to the top of the queue">⤒</button></form>
          <form method="post" action="/ui/remove" onsubmit="return confirm('Remove the torrent? Files stay on disk.');"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="files" value="0"><button title="Remove from the session (files stay)">✕</button></form>
          <form method="post" action="/ui/remove" onsubmit="return confirm('Remove the torrent AND DELETE the files? irreversible.');"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="files" value="1"><button class="danger" title="Remove and delete the files">✕ file</button></form>
        </div></td>
      </tr>
      <tr class="l2">
        <td class="prog"><div class="bar"><i style="width:{{percent .Progress}}%"></i></div><small>{{.ProgressS}}</small></td>
        <td><span class="state s-{{.State}}">{{.State}}</span></td>
        <td class="num">{{.DoneSize}}</td>
        <td class="num">{{.Down}}</td>
        <td class="num">{{.Up}}</td>
        <td class="num">{{.Peers}}</td>
        <td class="num">{{.Seeds}}</td>
        <td class="num">{{.Ratio}}</td>
        <td class="num">{{.ETA}}</td>
      </tr>
    </tbody>
    {{end}}
    </table>
    </div>
  </div>
  {{else}}
  <p class="muted">No torrents in the session.</p>
  {{end}}

  <div class="statusbar">
    <span>↓ <b>{{.DownloadRate}}</b></span>
    <span>↑ <b>{{.UploadRate}}</b></span>
    <span>Totals <b>{{bytes .TotalDown}}</b> / <b>{{bytes .TotalUp}}</b></span>
    <span>Free space <b>{{bytes .DiskFree}}</b></span>
    <span>DHT <b>{{if .DHT}}{{.DHTNodes}} nodi{{else}}off{{end}}</b></span>
    <span>Port <b>{{if .PortOpen}}open ({{.Router}}{{if .ExternalIP}} {{.ExternalIP}}{{end}}){{else}}not open{{end}}</b></span>
    <span>Encryption <b>{{.Encryption}}</b></span>
  </div>
{{end}}`

const uiDetailTemplate = `{{define "detail"}}
<div class="modal-head">
  <h3>{{.Name}}</h3>
  <button type="button" onclick="closeDetail()">Close</button>
</div>
<div class="detail-tabs">
  <button type="button" class="{{if eq .Tab "general"}}on{{end}}" onclick="openDetail('{{.Hash}}','general')">General</button>
  <button type="button" class="{{if eq .Tab "files"}}on{{end}}" onclick="openDetail('{{.Hash}}','files')">Files ({{len .Files}})</button>
  <button type="button" class="{{if eq .Tab "peers"}}on{{end}}" onclick="openDetail('{{.Hash}}','peers')">Peer ({{len .Peers}})</button>
  <button type="button" class="{{if eq .Tab "trackers"}}on{{end}}" onclick="openDetail('{{.Hash}}','trackers')">Tracker ({{len .Trackers}})</button>
</div>
{{if .Error}}<p class="notice err">{{.Error}}</p>{{end}}

{{if eq .Tab "general"}}
  <div class="stat-grid">
    <div class="row"><span>State</span><strong>{{.State}}</strong></div>
    <div class="row"><span>Progress</span><strong>{{percent .Progress}}%</strong></div>
    <div class="row"><span>Size</span><strong>{{bytes .TotalSize}}</strong></div>
    <div class="row"><span>Done</span><strong>{{bytes .TotalDone}}</strong></div>
    <div class="row"><span>Downloaded</span><strong>{{bytes .Downloaded}}</strong></div>
    <div class="row"><span>Uploaded</span><strong>{{bytes .Uploaded}}</strong></div>
    <div class="row"><span>Ratio</span><strong>{{printf "%.2f" .Ratio}}</strong></div>
    <div class="row"><span>↓ / ↑</span><strong>{{rate .DownRate}} / {{rate .UpRate}}</strong></div>
    <div class="row"><span>Peer / Seed</span><strong>{{.NumPeers}} / {{.NumSeeds}}</strong></div>
    <div class="row"><span>Swarm (seeds / peers)</span><strong>{{.NumComplete}} / {{.NumIncomplete}}</strong></div>
    <div class="row"><span>ETA</span><strong>{{if lt .ETA 0}}—{{else}}{{dur .ETA}}{{end}}</strong></div>
    <div class="row"><span>Folder</span><strong>{{.SavePath}}</strong></div>
    <div class="row"><span>Seed ratio set</span><strong>{{printf "%.2f" .SeedRatio}} ({{.SeedDays}} giorni)</strong></div>
    <div class="row"><span>Pin</span><strong>{{if .Pinned}}yes{{else}}no{{end}}</strong></div>
    <div class="row"><span>Private</span><strong>{{if .Private}}yes{{else}}no{{end}}</strong></div>
    <div class="row"><span>File</span><strong>{{.FileCount}}</strong></div>
    <div class="row"><span>Available / total pieces</span><strong>{{.PiecesAvailable}} / {{.PiecesTotal}} ({{percent .PiecesPercent}}%)</strong></div>
    <div class="row"><span>Completed pieces</span><strong>{{.PiecesHave}}</strong></div>
    <div class="row"><span>Piece size</span><strong>{{bytes .PieceLength}}</strong></div>
    <div class="row"><span>Wasted</span><strong>{{bytes .Wasted}}</strong></div>
    <div class="row"><span>Allocated on disk</span><strong>{{bytes .Allocated}}</strong></div>
    <div class="row"><span>Added</span><strong>{{if .AddedStr}}{{.AddedStr}}{{else}}—{{end}}</strong></div>
    <div class="row"><span>Completed</span><strong>{{if .CompletedStr}}{{.CompletedStr}}{{else}}—{{end}}</strong></div>
  </div>
  <div class="toolbar">
    <form method="post" action="/ui/pin"><input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general"><input type="hidden" name="pinned" value="{{if .Pinned}}0{{else}}1{{end}}"><button type="submit">{{if .Pinned}}Unpin{{else}}Pin (outside the queue){{end}}</button></form>
    <button type="button" onclick="copyMagnet(this)" data-magnet="{{.Magnet}}">Copy magnet</button>
    <a class="btn" style="padding:5px 10px;border-radius:8px;border:1px solid #2b3a55;background:#1b2536" href="/ui/torrent-file?hash={{.Hash}}" download>Export .torrent</a>
  </div>
  <form class="form-grid" method="post" action="/ui/seed-limits">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general">
    <label>Seed ratio (-1 global, 0 unlimited)<input type="number" step="0.01" name="seed_ratio" value="{{.SeedRatio}}"></label>
    <label>Seed days (-1 global, 0 unlimited)<input type="number" name="seed_days" value="{{.SeedDays}}"></label>
    <button type="submit">Save seed limits</button>
  </form>
  <form class="form-grid" method="post" action="/ui/move">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="general">
    <label>Move data to<input type="text" name="destination" value="{{.SavePath}}" size="48"></label>
    <button type="submit">Move</button>
  </form>
{{else if eq .Tab "files"}}
  {{if .Files}}
  <table>
    <thead><tr><th>File</th><th class="num">Size</th><th class="num">Done</th><th>Priority</th></tr></thead>
    <tbody>
    {{range .Files}}
      <tr>
        <td class="name">{{.Path}}</td>
        <td class="num">{{bytes .Size}}</td>
        <td class="num">{{bytes .Done}}</td>
        <td><form method="post" action="/ui/file-priority" onchange="this.submit()">
          <input type="hidden" name="hash" value="{{$.Hash}}"><input type="hidden" name="tab" value="files"><input type="hidden" name="index" value="{{.Index}}">
          <select name="priority"><option value="0"{{if not .Wanted}} selected{{end}}>Skip</option><option value="4"{{if .Wanted}} selected{{end}}>Download</option></select>
        </form></td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">Metadata not available yet.</p>{{end}}
{{else if eq .Tab "peers"}}
  {{if .Peers}}
  <table>
    <thead><tr><th>Peer</th><th>Client</th><th>Source</th><th class="num">↓</th><th class="num">↑</th><th class="num">Prog.</th><th>Seed</th><th>Flag</th><th class="num">For</th></tr></thead>
    <tbody>
    {{range .Peers}}
      <tr><td>{{.Address}}</td><td class="name">{{if .Client}}{{.Client}}{{else}}—{{end}}</td>
      <td>{{.Source}}</td>
      <td class="num">{{rate .Down}}</td><td class="num">{{rate .Up}}</td>
      <td class="num">{{printf "%.1f" .Progress}}%</td><td>{{if .Seed}}yes{{else}}no{{end}}</td>
      <td class="flags">{{if .Incoming}}<span class="flag">incoming</span>{{else}}<span class="flag">outgoing</span>{{end}}{{if .UTP}}<span class="flag">uTP</span>{{else}}<span class="flag">TCP</span>{{end}}{{if .Encrypted}}<span class="flag on">encrypted</span>{{end}}{{if .Handshake}}<span class="flag on">encrypted HS</span>{{end}}{{if .Snubbed}}<span class="flag">snubbed</span>{{end}}{{if .Optimistic}}<span class="flag on">optimistic</span>{{end}}{{if .ClientChoke}}<span class="flag">choking us</span>{{end}}{{if .PeerChoke}}<span class="flag">we choke</span>{{end}}{{if .ClientInt}}<span class="flag">interested</span>{{end}}{{if .PeerInt}}<span class="flag">interested in us</span>{{end}}{{if .Downloading}}<span class="flag">downloading</span>{{end}}</td>
      <td class="num">{{dur .Connected}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">No peers connected.</p>{{end}}
{{else if eq .Tab "trackers"}}
  {{if .Trackers}}
  <table>
    <thead><tr><th>URL</th><th>State</th><th>Messaggio</th><th class="num">Seed</th><th class="num">Peer</th><th class="num">Prossimo</th></tr></thead>
    <tbody>
    {{range .Trackers}}
      <tr><td class="name">{{.URL}}</td><td>{{.Status}}</td><td class="name">{{.Message}}</td>
      <td class="num">{{.Seeders}}</td><td class="num">{{.Leechers}}</td>
      <td class="num">{{if gt .Next 0}}{{dur .Next}}{{else}}—{{end}}</td></tr>
    {{end}}
    </tbody>
  </table>
  {{else}}<p class="muted">No trackers.</p>{{end}}
  <form class="form-grid" method="post" action="/ui/trackers">
    <input type="hidden" name="hash" value="{{.Hash}}"><input type="hidden" name="tab" value="trackers">
    <label>Add trackers (one per line)<textarea name="urls" rows="3" cols="60" placeholder="https://tracker.example/announce"></textarea></label>
    <button type="submit">Add trackers</button>
  </form>
{{end}}
{{end}}`

const uiTokenPage = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>gx-torrent</title>
<style>body{margin:0;font:16px system-ui,sans-serif;background:#0f1420;color:#e7ecf3}
form{max-width:360px;margin:80px auto;background:#161d2c;border:1px solid #243049;border-radius:12px;padding:22px}
input,button{width:100%%;padding:8px;margin:8px 0;border-radius:8px;border:1px solid #243049;background:#0f1420;color:inherit}
button{background:#2563eb;color:#fff;font-weight:600;border:0;cursor:pointer}.m{color:#93a1b5}</style></head>
<body><form method="get" action="/">
<h2 style="margin:0 0 6px">gx-torrent</h2>
<p class="m">This instance requires the token configured in Gextto.</p>
<input type="password" name="token" placeholder="token" autofocus autocomplete="off">
<button type="submit">Sign in</button>
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
// handleUIGexttoLog returns the tail of the Gextto log for the log tab. It is
// read only when the tab is opened or reloaded.
func (d *Daemon) handleUIGexttoLog(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if d.opts.GexttoLog == "" {
		http.Error(w, "no Gextto log configured", http.StatusNotFound)
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	lines = min(max(lines, 50), 5000)
	tail, err := tailLines(d.opts.GexttoLog, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"path": d.opts.GexttoLog, "lines": tail})
}

func (d *Daemon) handleUIDetail(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	data, err := d.uiDetailData(r.URL.Query().Get("hash"), r.URL.Query().Get("tab"))
	if err != nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
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
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	data, err := t.Torrent()
	if err != nil || len(data) == 0 {
		http.Error(w, "metadata not available", http.StatusNotFound)
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
		err = fmt.Errorf("unknown action: %s", op)
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
		d.uiDone(w, r, "", fmt.Errorf("no torrents selected"))
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
			err = fmt.Errorf("unknown action: %s", op)
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
	d.uiDone(w, r, "Torrent removed", err)
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
		d.uiDone(w, r, "", fmt.Errorf("paste a magnet link or a .torrent URL"))
		return
	}
	req := addRequest{
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		Sequential:     formBool(r, "sequential"),
		FirstLast:      formBool(r, "first_last"),
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
			d.uiDone(w, r, "", fmt.Errorf("the URL does not contain a .torrent file"))
			return
		}
	default:
		d.uiDone(w, r, "", fmt.Errorf("paste a magnet link or a .torrent URL"))
		return
	}
	_, existing, err := d.add(req)
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	msg := "Torrent added"
	if existing {
		msg = "Torrent already present"
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
		d.uiDone(w, r, "", fmt.Errorf("missing .torrent file"))
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
		Sequential:     formBool(r, "sequential"),
		FirstLast:      formBool(r, "first_last"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	})
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	msg := "Torrent added"
	if existing {
		msg = "Torrent already present"
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
		d.uiDone(w, r, "", fmt.Errorf("provide a URL or a file path"))
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
	d.uiDone(w, r, fmt.Sprintf("IP filter loaded: %d rules", rules), nil)
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
		d.uiDoneDetail(w, r, hash, "files", "", fmt.Errorf("metadata not available"))
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
	d.uiDoneDetail(w, r, hash, "files", "File priority updated", err)
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
		d.uiDoneDetail(w, r, hash, "trackers", "", fmt.Errorf("no trackers to add"))
		return
	}
	err := d.addTrackers(hash, urls)
	d.uiDoneDetail(w, r, hash, "trackers", fmt.Sprintf("%d trackers added", len(urls)), err)
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
	d.uiDoneDetail(w, r, hash, "general", "Seed limits saved", err)
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
		d.uiDoneDetail(w, r, hash, "general", "", fmt.Errorf("specify the destination"))
		return
	}
	err := d.move(hash, destination, false)
	d.uiDoneDetail(w, r, hash, "general", "Move started", err)
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
	d.uiDoneDetail(w, r, hash, "general", "Pin updated", err)
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
		return "Paused"
	case "resume", "resume-all":
		return "Resumed"
	case "verify", "verify-all":
		return "Recheck started"
	case "reannounce":
		return "Reannounce sent"
	case "top":
		return "Moved to the top of the queue"
	case "remove":
		return "removed (files kept)"
	case "remove-files":
		return "removed with files"
	}
	return "Done"
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
		query.Set("msg", "Error: "+err.Error())
	} else {
		query.Set("ok", "1")
		if okMessage == "" {
			okMessage = "Done"
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
		Version:        stats.Version,
		Uptime:         uiDuration(stats.UptimeSeconds),
		Now:            time.Now().Format("15:04:05"),
		Torrents:       stats.Torrents,
		Down:           stats.Downloading,
		Seeding:        stats.Seeding,
		Stalled:        stats.Stalled,
		Paused:         stats.Paused,
		Moving:         stats.Moving,
		DownloadRate:   uiRate(stats.DownloadRate),
		UploadRate:     uiRate(stats.UploadRate),
		TotalDown:      stats.Session["bytes_downloaded"],
		TotalUp:        stats.Session["bytes_uploaded"],
		PeerPort:       stats.PeerPort,
		Listen:         stats.ListenAddress,
		DHT:            stats.DHT,
		DHTNodes:       stats.Session["dht_nodes"],
		UTP:            stats.UTP,
		Encryption:     uiEncryption(stats.Encryption),
		Proxy:          stats.Proxy,
		IPFilter:       stats.IPFilterRules,
		IPFilterPath:   stats.IPFilterPath,
		IPFilterSource: d.opts.IPFilterSource,
		GexttoLog:      d.opts.GexttoLog != "",
		CacheReadMB:    stats.CacheReadMB,
		CacheWB:        stats.CacheWriteMB,
		LSDPeers:       stats.LSD.PeersFound,
		DiskFree:       stats.DiskFreeBytes,
		DiskTotal:      stats.DiskTotalBytes,
	}
	switch {
	case stats.PortMapping.Method != "":
		page.PortOpen = true
		page.Router = strings.ToUpper(stats.PortMapping.Method)
		page.ExternalIP = stats.PortMapping.ExternalIP
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
			status := "not contacted"
			switch tracker.Status {
			case torrent.Working:
				status = "working"
			case torrent.Contacting:
				status = "contacting"
			case torrent.NotWorking:
				status = "not working"
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
		return "incoming"
	case torrent.SourceManual:
		return "manual"
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
		return "forced"
	default:
		return "on"
	}
}
