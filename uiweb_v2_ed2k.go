package gextto

// uiweb_v2_ed2k.go is the eD2k page: aMule's connection state, the server
// list, Kad, a search on eD2k/Kad and the box for pasted ed2k:// links. The
// downloads themselves are on the Scarico page, next to the torrents.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
)

// v2Ed2kView is the eD2k page.
type v2Ed2kView struct {
	// Tab selects the eD2k sub-page: "status" (default), "shared" or "log".
	Tab        string
	Enabled    bool
	Error      string
	Status     amule.Status
	Servers    []amule.Server
	Address    string
	Network    string
	Message    string
	Detail     string
	MessageErr bool
	Search     v2Ed2kSearchView
	ServersURL string
	NodesURL   string
	// Prefs are aMule's directories and file options, for the status panel.
	Prefs amule.Preferences
	// DirIssues lists aMule's incoming/temp folders that are missing here.
	DirIssues []string
	// Shared and Log back the two dedicated tabs.
	Shared v2Ed2kSharedView
	Log    map[string]any
}

// v2Ed2kSearchView is the result block of an eD2k search.
type v2Ed2kSearchView struct {
	Query   string
	Network string
	Results []v2Ed2kResult
	Error   string
	Detail  string
	Done    bool
}

// v2Ed2kResult is one search result row.
type v2Ed2kResult struct {
	Name     string
	Size     int64
	Sources  int64
	Complete int64
	Link     string
	Known    bool
}

const (
	defaultServerMetURL = "http://upd.emule-security.org/server.met"
	defaultNodesDatURL  = "http://upd.emule-security.org/nodes.dat"
)

func v2Ed2kNetworkName(value uint8) string {
	switch value {
	case amule.SearchGlobal:
		return "global"
	case amule.SearchLocal:
		return "local"
	case ed2kSearchBoth:
		return "both"
	}
	return "kad"
}

func v2Ed2kViewFrom(s *AppState, message, detail string, isErr bool, tab string) v2Ed2kView {
	cfg := latestConfig(s)
	switch strings.TrimSpace(tab) {
	case "shared", "log":
		tab = strings.TrimSpace(tab)
	default:
		tab = "status"
	}
	view := v2Ed2kView{
		Tab:        tab,
		Enabled:    ed2kEnabled(cfg),
		Message:    message,
		Detail:     detail,
		MessageErr: isErr,
		ServersURL: defaultServerMetURL,
		NodesURL:   defaultNodesDatURL,
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Address = settings.Address
	view.Network = v2Ed2kNetworkName(settings.Network)
	view.Search.Network = view.Network
	if !view.Enabled {
		return view
	}
	switch tab {
	case "shared":
		view.Shared = v2Ed2kSharedViewFor(cfg, message, isErr)
		return view
	case "log":
		view.Log = map[string]any{}
		lines, logErr := Ed2kLogLines(cfg)
		if logErr != nil {
			view.Log["Error"] = logErr.Error()
		} else {
			if len(lines) > 300 {
				lines = lines[len(lines)-300:]
			}
			view.Log["Lines"] = lines
		}
		return view
	}
	ed2kInvalidateHealth()
	status, err := ed2kStatus(cfg)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Status = status
	_ = ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		if prefs, prefsErr := client.Preferences(ctx); prefsErr == nil {
			view.Prefs = prefs
			view.DirIssues = amuleMissingDirectories(cfg, prefs)
		}
		servers, callErr := client.Servers(ctx)
		view.Servers = servers
		return callErr
	})
	return view
}

// V2Ed2kCreateDirs creates aMule's missing incoming/temp folders, so its disk
// checks stop failing, and re-renders the state panel.
func V2Ed2kCreateDirs(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	message, detail, isErr := "", "", false
	created, err := amuleCreateMissingDirectories(cfg)
	switch {
	case err != nil:
		message, detail, isErr = "Comando non riuscito:", err.Error(), true
	case len(created) == 0:
		message = "Nessuna cartella di aMule da creare."
	default:
		message = "Cartelle di aMule create:"
		detail = strings.Join(created, " · ")
		logging.Info("created the missing aMule directories", "dirs", strings.Join(created, "; "))
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=ed2k", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_state", v2Ed2kViewFrom(s, message, detail, isErr, "status"), dict, eng)
}

// V2Ed2kRecommended applies aMule's recommended file settings (no full
// preallocation) and re-renders the state panel.
func V2Ed2kRecommended(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	message, detail, isErr := "", "", false
	if !ed2kEnabled(cfg) {
		message, isErr = "L'integrazione con aMule è spenta.", true
	} else if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.SetFilePreallocation(ctx, false)
	}); err != nil {
		message, detail, isErr = "Comando non riuscito:", err.Error(), true
	} else {
		message = "Impostazioni consigliate applicate: aMule non prealloca più i file."
		logging.Info("eD2k recommended aMule file settings applied")
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=ed2k", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_state", v2Ed2kViewFrom(s, message, detail, isErr, "status"), dict, eng)
}

// V2Ed2kAction runs a connection command and re-renders the state panel.
func V2Ed2kAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	action := strings.TrimSpace(r.FormValue("action"))
	message, detail, isErr := v2Ed2kCommand(cfg, action, r.FormValue("address"), r.FormValue("url"))
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=ed2k", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_state", v2Ed2kViewFrom(s, message, detail, isErr, "status"), dict, eng)
}

// v2Ed2kCommand returns a fixed (translatable) message, an optional detail
// such as the core's own error text, and whether it is an error.
func v2Ed2kCommand(cfg *Config, action, address, url string) (string, string, bool) {
	if !ed2kEnabled(cfg) {
		return "L'integrazione con aMule è spenta.", "", true
	}
	var done string
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		switch action {
		case "connect":
			done = "Connessione a eD2k e Kad avviata."
			return client.Connect(ctx)
		case "disconnect":
			done = "Disconnesso da eD2k e Kad."
			return client.Disconnect(ctx)
		case "kad_start":
			done = "Kad avviato."
			return client.StartKad(ctx)
		case "kad_stop":
			done = "Kad fermato."
			return client.StopKad(ctx)
		case "server_connect":
			done = "Connessione al server avviata."
			return client.ConnectServer(ctx, address)
		case "server_disconnect":
			done = "Disconnesso dal server eD2k."
			return client.DisconnectServer(ctx)
		case "servers_update":
			if !v2HTTPURL(url) {
				return fmt.Errorf("invalid address %q", url)
			}
			done = "aMule sta scaricando la nuova lista dei server."
			return client.UpdateServers(ctx, url)
		case "nodes_update":
			if !v2HTTPURL(url) {
				return fmt.Errorf("invalid address %q", url)
			}
			done = "aMule sta scaricando i nuovi nodi Kad."
			return client.UpdateNodes(ctx, url)
		}
		return fmt.Errorf("unknown action %q", action)
	})
	ed2kInvalidateHealth()
	if err != nil {
		return "Comando non riuscito:", err.Error(), true
	}
	logging.Info("eD2k command sent to aMule", "action", action)
	return done, "", false
}

func v2HTTPURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// V2Ed2kSearch searches eD2k/Kad through aMule and renders the results.
func V2Ed2kSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	network := ed2kSearchNetwork(r.FormValue("network"))
	view := v2Ed2kSearchFor(r.Context(), latestConfig(s), strings.TrimSpace(r.FormValue("query")), &network)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_results", view, dict, eng)
}

// v2Ed2kSearchFor runs one search for the eD2k page or the archive; a nil
// network uses the one chosen in the settings.
func v2Ed2kSearchFor(ctx context.Context, cfg *Config, query string, network *uint8) v2Ed2kSearchView {
	view := v2Ed2kSearchView{Query: query, Done: true}
	if network != nil {
		view.Network = v2Ed2kNetworkName(*network)
	}
	switch {
	case !ed2kEnabled(cfg):
		view.Error = "L'integrazione con aMule è spenta."
	case query == "":
		view.Error = "Scrivi cosa cercare."
	default:
		results, err := searchAmule(ctx, cfg, query, network)
		if err != nil {
			view.Error = "Ricerca non riuscita:"
			view.Detail = err.Error()
		}
		for _, result := range results {
			view.Results = append(view.Results, v2Ed2kResult{
				Name:     result.Name,
				Size:     result.Size,
				Sources:  result.Sources,
				Complete: result.CompleteSources,
				Link:     result.Link(),
				Known:    result.Status != 0,
			})
		}
	}
	return view
}

// V2Ed2kAdd queues one or more ed2k:// links (one result's button, or the
// pasted-links box) and registers them like a manual download.
func V2Ed2kAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	links := extractEd2kLinks(r.FormValue("links"))
	added, failed := 0, []string{}
	for _, link := range links {
		file, err := ed2k.ParseFileLink(link)
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		release := ParseEd2kRelease(file.Name, link, "ed2k:link", file.Size, 0, time.Now().UTC())
		if release == nil {
			failed = append(failed, file.Name)
			continue
		}
		status, value := gh3AddParsedRelease(s, *release)
		if status == http.StatusAccepted {
			added++
			continue
		}
		reason := file.Name
		if payload, ok := value.(map[string]any); ok {
			if text, ok := payload["error"].(string); ok && text != "" {
				reason = file.Name + ": " + text
			}
		}
		failed = append(failed, reason)
	}
	message, detail, isErr := "", "", false
	switch {
	case len(links) == 0:
		message, isErr = "Nessun link eD2k valido trovato nel testo incollato.", true
	case len(failed) == 0:
		message, detail = "Download eD2k aggiunti ad aMule, li trovi in Scarico:", strconv.Itoa(added)
	default:
		message, detail, isErr = "Alcuni link non sono stati aggiunti ad aMule:", strings.Join(failed, "; "), true
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_message", map[string]any{"Message": message, "Detail": detail, "Error": isErr}, dict, eng)
}

// v2Ed2kDetailView is the detail dialog of one aMule download.
type v2Ed2kDetailView struct {
	Found        bool
	Hash         string
	Name         string
	State        string
	ProgressPct  string
	Done         int64
	Size         int64
	Speed        int64
	Active       int64
	Sources      int64
	LastReceived string
	Priority     string
	Link         string
	Message      string
	Detail       string
	Error        bool
}

func v2Ed2kDetailFor(cfg *Config, hash string) v2Ed2kDetailView {
	hash = strings.ToLower(strings.TrimSpace(hash))
	view := v2Ed2kDetailView{Hash: hash, Name: hash}
	for _, download := range Ed2kDownloads(cfg) {
		if download.Hash != hash {
			continue
		}
		row := ed2kDownloadJSONFrom(download)
		for _, uiRow := range uiEd2kRows([]amule.Download{download}) {
			view.State = uiRow.State
			view.ProgressPct = uiRow.ProgressPct
		}
		view.Found = true
		view.Name = download.Name
		view.Done, view.Size, view.Speed = download.Done, download.Size, download.Speed
		view.Active, view.Sources = download.Active, download.Sources
		view.Priority = row.Priority
		view.Link = row.Link
		if download.LastRecv > 0 {
			view.LastReceived = time.Unix(download.LastRecv, 0).Format("2006-01-02 15:04")
		}
		break
	}
	return view
}

// V2Ed2kDetail renders the detail dialog of an aMule download.
func V2Ed2kDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_detail", v2Ed2kDetailFor(latestConfig(s), r.FormValue("hash")), dict, eng)
}

// V2Ed2kPriority changes the priority of an aMule download from the detail
// dialog and renders its body again.
func V2Ed2kPriority(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	hash := r.FormValue("hash")
	err := Ed2kSetPriority(cfg, hash, r.FormValue("priority"))
	view := v2Ed2kDetailFor(cfg, hash)
	if err != nil {
		view.Message, view.Detail, view.Error = "Priorità non cambiata:", err.Error(), true
	} else {
		view.Message = "Priorità salvata."
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_detail_body", view, dict, eng)
}

// V2Ed2kLog renders aMule's log on the eD2k page.
func V2Ed2kLog(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := map[string]any{}
	lines, err := Ed2kLogLines(latestConfig(s))
	if err != nil {
		view["Error"] = err.Error()
	} else {
		if len(lines) > 300 {
			lines = lines[len(lines)-300:]
		}
		view["Lines"] = lines
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_log", view, dict, eng)
}

// v2Ed2kSharedRow is one shared file on the eD2k page.
type v2Ed2kSharedRow struct {
	Name           string
	Size           int64
	Transferred    int64
	TransferredAll int64
	Requests       int64
	RequestsAll    int64
	Accepts        int64
	Queued         int64
	Priority       string
	Link           string
}

// v2Ed2kSharedView backs the shared-files panel of the eD2k page.
type v2Ed2kSharedView struct {
	Enabled     bool
	Error       string
	Message     string
	Detail      string
	MessageErr  bool
	Incoming    string
	Dirs        []string
	UploadRate  int64
	UploadQueue int64
	Files       []v2Ed2kSharedRow
	Uploads     []amule.UploadClient
}

// v2Ed2kPriorityLabel names a shared file's upload priority, ready for the
// HTML translation pass.
func v2Ed2kPriorityLabel(priority int, auto bool) string {
	if auto {
		return "Automatica"
	}
	switch priority {
	case amule.PriorityLow:
		return "Bassa"
	case amule.PriorityHigh:
		return "Alta"
	}
	return "Normale"
}

func v2Ed2kSharedViewFor(cfg *Config, message string, messageErr bool) v2Ed2kSharedView {
	view := v2Ed2kSharedView{Message: message, MessageErr: messageErr, Enabled: ed2kEnabled(cfg)}
	if !view.Enabled {
		return view
	}
	snapshot, err := ed2kSharedSnapshotFor(cfg)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Incoming = snapshot.Incoming
	view.Dirs = snapshot.SharedDirs
	view.UploadRate = snapshot.UploadRate
	view.UploadQueue = snapshot.UploadQueue
	view.Uploads = snapshot.Uploads
	for _, file := range snapshot.Files {
		view.Files = append(view.Files, v2Ed2kSharedRow{
			Name:           file.Name,
			Size:           file.Size,
			Transferred:    file.Transferred,
			TransferredAll: file.TransferredAll,
			Requests:       file.Requests,
			RequestsAll:    file.RequestsAll,
			Accepts:        file.Accepts,
			Queued:         file.Queued,
			Priority:       v2Ed2kPriorityLabel(file.Priority, file.AutoPriority),
			Link:           ed2k.FileLink{Name: file.Name, Size: file.Size, Hash: file.Hash}.String(),
		})
	}
	return view
}

// V2Ed2kSharedReload asks aMule to rescan its shared folders, then renders the
// panel again with the fresh list.
func V2Ed2kSharedReload(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	view := v2Ed2kSharedViewFor(cfg, "", false)
	if !view.Enabled {
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_ed2k_shared", view, dict, eng)
		return
	}
	message, isErr, detail := "Scansione dei file condivisi richiesta ad aMule.", false, ""
	if err := ed2kReloadShared(cfg); err != nil {
		logging.Warn("eD2k shared files reload failed", "error", err.Error())
		message, isErr, detail = "Comando non riuscito:", true, err.Error()
	}
	if r.Header.Get("HX-Request") == "" {
		target := "/?view=ed2k&tab=shared&msg=" + url.QueryEscape(message)
		if isErr {
			target += "&msg_err=1"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	view.Message, view.MessageErr, view.Detail = message, isErr, detail
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_shared", view, dict, eng)
}
