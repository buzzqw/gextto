package gextto

// uiweb_v2_ed2k.go is the eD2k page: aMule's connection state, the server
// list, Kad, a search on eD2k/Kad and the box for pasted ed2k:// links. The
// downloads themselves are on the Scarico page, next to the torrents.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
)

// v2Ed2kView is the eD2k page.
type v2Ed2kView struct {
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
	}
	return "kad"
}

func v2Ed2kViewFrom(s *AppState, message, detail string, isErr bool) v2Ed2kView {
	cfg := latestConfig(s)
	view := v2Ed2kView{
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
	ed2kInvalidateHealth()
	status, err := ed2kStatus(cfg)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Status = status
	_ = ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		servers, callErr := client.Servers(ctx)
		view.Servers = servers
		return callErr
	})
	return view
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
	v2Render(w, http.StatusOK, "v2_ed2k_state", v2Ed2kViewFrom(s, message, detail, isErr), dict, eng)
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
	cfg := latestConfig(s)
	query := strings.TrimSpace(r.FormValue("query"))
	network := ed2kSearchNetwork(r.FormValue("network"))
	view := v2Ed2kSearchView{Query: query, Network: v2Ed2kNetworkName(network), Done: true}
	switch {
	case !ed2kEnabled(cfg):
		view.Error = "L'integrazione con aMule è spenta."
	case query == "":
		view.Error = "Scrivi cosa cercare."
	default:
		results, err := searchAmule(r.Context(), cfg, query, &network)
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
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ed2k_results", view, dict, eng)
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
