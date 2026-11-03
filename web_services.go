package gextto

// web_services.go reports on the services Gextto depends on and on the machine
// it runs on: the systemd units (Gextto itself, gx-torrent, aMule), the network
// interfaces and ports, the provider guidance shown in Salute, and the probes
// behind them.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// activeProvider reports whether a saved backoff row still belongs to an
// integration that is configured and active. An indexer names its row by the
// configured name or by "<manager> · <indexer>" when the row belongs to one lane
// behind a manager; a feed is named by feedSourceName, the same name the backoff
// table stores. Only those two kinds are written by the search, so a row of any
// other kind is left alone.
func activeProvider(cfg *Config, kind, provider string) bool {
	if cfg == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "indexer":
		for _, indexer := range cfg.Indexers {
			if !indexer.Enabled {
				continue
			}
			base := strings.ToLower(strings.TrimSpace(indexer.Name))
			if base != "" && (name == base || strings.HasPrefix(name, base+" · ")) {
				return true
			}
		}
		return false
	case "feed":
		for _, feed := range cfg.FeedURLs {
			if strings.EqualFold(strings.TrimSpace(feedSourceName(feed)), strings.TrimSpace(provider)) ||
				strings.EqualFold(strings.TrimSpace(feed), strings.TrimSpace(provider)) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// healthyIndexerStatuses returns a row for every enabled configured indexer
// that has no saved failure and no live row yet, so the provider list is always
// complete. The caller passes the set of names already known; this function adds
// the new ones to it.
func healthyIndexerStatuses(cfg *Config, known map[string]struct{}) []models.ProviderStatus {
	var out []models.ProviderStatus
	for _, indexer := range cfg.Indexers {
		name := strings.TrimSpace(indexer.Name)
		key := strings.ToLower(name)
		if !indexer.Enabled || key == "" {
			continue
		}
		if _, ok := known[key]; ok {
			continue
		}
		known[key] = struct{}{}
		out = append(out, models.ProviderStatus{
			Provider:        name,
			Kind:            "indexer",
			URL:             providerURL(cfg, name),
			UserMessage:     "Nessun problema rilevato.",
			SuggestedAction: "Nessuna azione necessaria.",
		})
	}
	return out
}

// gxTorrentServiceStatus reports whether the active gx-torrent engine is up
// and links to its web page. It is only added when gx-torrent is the active
// engine.
func gxTorrentServiceStatus(publicBase string, s *AppState, r *http.Request, cfg *Config) *models.ProviderStatus {
	if s == nil || s.activeEngine().Name() != BackendGxTorrent {
		return nil
	}
	entry := &models.ProviderStatus{
		Provider: "gx-torrent",
		Kind:     "servizio",
		URL:      gxTorrentURL(publicBase, r, cfg),
	}
	up := false
	if engine, ok := s.activeEngine().(*gxTorrentEngine); ok {
		up = engine.SessionHealthy()
	} else if settings, err := gxTorrentSettingsFromConfig(cfg); err == nil {
		up = gxHealthCheck(settings)
	}
	if up {
		entry.UserMessage = "Motore torrent gx-torrent attivo e raggiungibile."
		entry.SuggestedAction = "Nessuna azione necessaria. Apri la sua pagina web dal link."
	} else {
		entry.Level = 1
		entry.LastError = "gx-torrent non risponde"
		entry.UserMessage = "Motore torrent gx-torrent non raggiungibile."
		entry.SuggestedAction = "Il demone è avviato e sorvegliato da Gextto: controlla l'indirizzo gxtorrent_url e i log. Se non riparte, Gextto passa a qBittorrent."
	}
	return entry
}

// gxTorrentURL builds the browser-facing URL of the gx-torrent web page:
// the host (and scheme) the client used to reach Gextto, with the daemon's
// listen port. It never returns the server-side loopback URL.
func gxTorrentURL(publicBase string, r *http.Request, cfg *Config) string {
	port := gxTorrentClientPort(cfg)
	scheme, host := "http", ""
	if base := strings.TrimSpace(publicBase); base != "" {
		if parsed, err := url.Parse(base); err == nil && parsed.Hostname() != "" {
			scheme = parsed.Scheme
			host = parsed.Hostname()
		}
	}
	if host == "" && r != nil {
		if r.TLS != nil {
			scheme = "https"
		}
		value := strings.TrimSpace(r.Host)
		if value == "" {
			value = strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
		}
		if name, _, err := net.SplitHostPort(value); err == nil {
			host = name
		} else {
			host = value
		}
	}
	if host == "" || port == "" {
		return ""
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/"
}

// gxTorrentClientPort is the port clients use to reach the gx-torrent web
// page: the listen port, then the URL port, then the 8890 default.
func gxTorrentClientPort(cfg *Config) string {
	if cfg == nil {
		return "8890"
	}
	if listen := strings.TrimSpace(cfg.Settings["gxtorrent_listen"]); listen != "" {
		if _, port, err := net.SplitHostPort(listen); err == nil && port != "" {
			return port
		}
	}
	if raw := strings.TrimSpace(cfg.Settings["gxtorrent_url"]); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Port() != "" {
			return parsed.Port()
		}
	}
	return "8890"
}

// mirCrewLoginState reports whether the forum login of the service at base
// has failed, with the reason when the service provides one.
func mirCrewLoginState(base string) (string, bool) {
	loginError, failed, _ := mirCrewLoginProbe(base)
	return loginError, failed
}

func mirCrewServiceStatus(cfg *Config) *models.ProviderStatus {
	indexer := mirCrewIndexer(cfg)
	if indexer == nil {
		// A disabled or removed mircrew indexer must not keep showing the row
		// cached from when it was active.
		mirCrewProbe.Lock()
		mirCrewProbe.entry = nil
		mirCrewProbe.Unlock()
		return nil
	}
	mirCrewProbe.Lock()
	defer mirCrewProbe.Unlock()
	if mirCrewProbe.entry != nil && time.Since(mirCrewProbe.at) < 20*time.Second {
		cached := *mirCrewProbe.entry
		return &cached
	}
	status, reachable, body := servicesProbe(HealthProbeURL(*indexer), 5*time.Second)
	entry := models.ProviderStatus{Provider: indexer.Name, Kind: "servizio", URL: providerURL(cfg, indexer.Name)}
	switch {
	case !reachable:
		entry.UserMessage = "Servizio mircrew-indexer non raggiungibile."
		entry.SuggestedAction = "Avvia il servizio (systemctl --user start mircrew-indexer) e verifica URL/porta dell’indexer."
	case status < 200 || status >= 300 || TorznabError(body) != "":
		entry.UserMessage = fmt.Sprintf("Il servizio ha risposto con HTTP %d.", status)
		entry.SuggestedAction = "Apri la web UI del servizio e controlla la API key dell’indexer."
	default:
		entry.UserMessage = "Servizio mircrew-indexer attivo e raggiungibile."
		entry.SuggestedAction = "Nessuna azione necessaria."
		// The Torznab API answers from the local index even when the forum
		// login is broken; then no new magnet can be unlocked. The service's
		// /status (no API key, no side effects) tells the login state.
		if loginError, failed := mirCrewLoginState(entry.URL); failed {
			// The message is a fixed phrase (translated by the catalog); the
			// service's own error text goes to the technical detail column.
			entry.UserMessage = "Servizio mircrew-indexer raggiungibile, ma il login al forum MirCrew non è riuscito."
			if loginError != "" {
				entry.LastError = loginError
			}
			entry.SuggestedAction = "Controlla utente e password nella web UI del servizio (Impostazioni) e usa «Rifai login»."
		}
	}
	mirCrewProbe.at = time.Now()
	mirCrewProbe.entry = &entry
	result := entry
	return &result
}

type ProwlarrProbeEntry struct {
	at     time.Time
	status models.ProviderStatus
}

// prowlarrServiceStatuses returns a live service-status row for every
// enabled Prowlarr manager. This is separate from per-indexer search backoff:
// a reachable manager can still report an individual indexer failure in Sources.
func prowlarrServiceStatuses(cfg *Config) []models.ProviderStatus {
	if cfg == nil {
		return nil
	}
	items := []models.ProviderStatus{}
	for _, indexer := range cfg.Indexers {
		if !indexer.Enabled || managerKind(indexer) != ManagerProwlarr {
			continue
		}
		key := strings.Join([]string{indexer.Name, indexer.URL, indexer.APIKey}, "\x00")
		prowlarrProbe.Lock()
		if cached, ok := prowlarrProbe.entries[key]; ok && time.Since(cached.at) < 20*time.Second {
			items = append(items, cached.status)
			prowlarrProbe.Unlock()
			continue
		}
		prowlarrProbe.Unlock()

		status := models.ProviderStatus{
			Provider: indexer.Name,
			Kind:     "servizio",
			URL:      providerURL(cfg, indexer.Name),
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := managerGet(ctx, HealthProbeURL(indexer), nil)
		cancel()
		if err != nil {
			status.Level = 1
			status.LastError = utils.RedactURLSecrets(err.Error())
			status.UserMessage, status.SuggestedAction = providerGuidance(status)
		} else {
			status.UserMessage = "Nessun problema rilevato."
			status.SuggestedAction = "Nessuna azione necessaria."
		}

		prowlarrProbe.Lock()
		if prowlarrProbe.entries == nil {
			prowlarrProbe.entries = make(map[string]ProwlarrProbeEntry)
		}
		prowlarrProbe.entries[key] = ProwlarrProbeEntry{at: time.Now(), status: status}
		prowlarrProbe.Unlock()
		items = append(items, status)
	}
	return items
}

// providerGuidance keeps connection details in the diagnostic field while
// giving the person operating Gextto an immediately useful explanation. The
// provider is retried automatically after its backoff; "Azzera" is only useful
// once the external cause has been fixed.
func providerGuidance(status models.ProviderStatus) (string, string) {
	if status.Level == 0 && strings.TrimSpace(status.LastError) == "" && strings.TrimSpace(status.DisabledTill) == "" {
		return "Nessun problema rilevato.", "Nessuna azione necessaria."
	}
	errText := strings.ToLower(status.LastError)
	switch {
	case strings.Contains(errText, "429") || strings.Contains(errText, "rate limit"):
		return "Il provider ha limitato temporaneamente le richieste.", "Attendi il nuovo tentativo automatico; riduci le ricerche manuali se il problema continua."
	case strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "unauthorized") || strings.Contains(errText, "forbidden"):
		return "Il provider ha rifiutato l’accesso.", "Controlla credenziali, API key o cookie del provider; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "404"):
		return "L’indirizzo configurato del provider non è stato trovato.", "Controlla URL e percorso del provider; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "timeout") || strings.Contains(errText, "deadline exceeded"):
		return "Il provider non ha risposto entro il tempo previsto.", "Verifica rete e disponibilità del provider; Gextto riproverà automaticamente."
	case strings.Contains(errText, "no such host") || strings.Contains(errText, "network is unreachable") || strings.Contains(errText, "connection refused") || strings.Contains(errText, "x509"):
		return "Il provider non è raggiungibile dalla rete di Gextto.", "Verifica DNS, rete, certificato e URL; poi usa Azzera per riprovare subito."
	case strings.Contains(errText, "5") && strings.Contains(errText, "http"):
		return "Il provider ha segnalato un errore temporaneo del proprio servizio.", "Attendi il nuovo tentativo automatico; non è necessaria una modifica locale."
	default:
		return "Il provider non ha completato l’ultima richiesta.", "Consulta il dettaglio tecnico, verifica la configurazione e usa Azzera dopo la correzione."
	}
}

// ServiceRestart starts, stops or restarts the Gextto systemd service.
func ServiceRestart(w http.ResponseWriter, r *http.Request, s *AppState) {
	action := "restart"
	body, bodyErr := readOptionalBody(r)
	if bodyErr != nil {
		jsonError(w, http.StatusBadRequest, bodyErr.Error())
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var input struct {
			Action *string `json:"action"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Action != nil {
			candidate := strings.ToLower(strings.TrimSpace(*input.Action))
			if candidate == "restart" || candidate == "start" || candidate == "stop" {
				action = candidate
			}
		}
	}

	// Validate the action once and map it to a literal so the command never
	// receives a value derived from the request.
	verb := "restart"
	switch action {
	case "start":
		verb = "start"
	case "stop":
		verb = "stop"
	}

	// A `systemctl --user` unit runs under the same user as the daemon, so it
	// can be controlled directly without the root helper.
	if scope, scopeName := serviceScope("gextto.service"); scopeName == "user" {
		go func() {
			defer recoverGoroutine("g5 background")
			time.Sleep(800 * time.Millisecond)
			_ = exec.Command("systemctl", append(append([]string{}, scope...), verb, "gextto.service")...).Run()
		}()
		jsonResponse(w, map[string]any{
			"ok":      true,
			"action":  action,
			"scope":   "user",
			"message": "azione sul servizio richiesta",
		})
		return
	}

	const restartHelper = "/usr/local/bin/gextto-restart"
	if _, err := os.Stat(restartHelper); err != nil {
		jsonError(w, http.StatusPreconditionRequired, "Helper di riavvio non installato. Da root, una volta: sudo install -m 0755 scripts/gextto-restart /usr/local/bin/gextto-restart && sudo install -m 0440 systemd/gextto.sudoers /etc/sudoers.d/gextto")
		return
	}
	allowed := exec.Command("sudo", "-n", "-l", restartHelper).Run() == nil
	if !allowed {
		jsonError(w, http.StatusForbidden, "Utente non autorizzato al riavvio: installa la regola sudoers systemd/gextto.sudoers.")
		return
	}
	go func() {
		defer recoverGoroutine("g5 background")
		time.Sleep(800 * time.Millisecond)
		_ = exec.Command("sudo", "-n", restartHelper, verb).Run()
	}()
	jsonResponse(w, map[string]any{
		"ok":      true,
		"action":  action,
		"message": "azione sul servizio richiesta",
	})
}

func systemctlField(scope []string, verb, unit string) string {
	args := append(append([]string{}, scope...), verb, unit)
	output, _ := exec.Command("systemctl", args...).Output()
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "unknown"
	}
	return value
}

// ServicesStatus reports the state of the Gextto unit, of every enabled indexer
// and of FlareSolverr.
func ServicesStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	const unit = "gextto.service"
	scope, scopeName := serviceScope(unit)
	service := map[string]any{
		"unit":    unit,
		"scope":   scopeName,
		"active":  systemctlField(scope, "is-active", unit),
		"enabled": systemctlField(scope, "is-enabled", unit),
	}
	cfg := latestConfig(s)
	indexers := []map[string]any{}
	for _, indexer := range cfg.Indexers {
		if !indexer.Enabled {
			continue
		}
		status, reachable, body := servicesProbe(HealthProbeURL(indexer), 5*time.Second)
		var statusValue any
		if reachable {
			statusValue = status
		}
		errorText := ""
		if reachable {
			errorText = TorznabError(body)
		}
		var errorValue any
		if errorText != "" {
			errorValue = errorText
		}
		healthy := reachable && status >= 200 && status < 300 && errorText == ""
		indexers = append(indexers, map[string]any{
			"name":      indexer.Name,
			"url":       indexer.URL,
			"reachable": reachable,
			"healthy":   healthy,
			"status":    statusValue,
			"error":     errorValue,
		})
	}
	if cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "" {
		url := *cfg.FlaresolverrURL
		status, reachable, _ := servicesProbe(url, 5*time.Second)
		var statusValue any
		if reachable {
			statusValue = status
		}
		indexers = append(indexers, map[string]any{
			"kind":      "flaresolverr",
			"name":      "FlareSolverr",
			"url":       url,
			"reachable": reachable,
			"status":    statusValue,
		})
	}
	jsonResponse(w, map[string]any{
		"ok":       true,
		"services": []map[string]any{service},
		"indexers": indexers,
	})
}

// NetworkInterfacesView handles GET /api/network/interfaces.
func NetworkInterfacesView(w http.ResponseWriter, r *http.Request, s *AppState) {
	interfaces := map[string]any{}
	for _, iface := range utils.NetworkInterfaces() {
		interfaces[iface.Name] = map[string]any{"ip": iface.IP, "type": iface.Kind}
	}
	jsonResponse(w, map[string]any{"ok": true, "interfaces": interfaces})
}

// serviceScope returns the systemd scope arguments for a local unit. When
// the daemon itself runs as a `systemctl --user` unit (the checkout installer),
// querying the system scope reports "inactive"/"not-found" even though the
// service is healthy; prefer the user scope when its unit file is present.
func serviceScope(unit string) ([]string, string) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil, "system"
		}
		base = filepath.Join(home, ".config")
	}
	if _, err := os.Stat(filepath.Join(base, "systemd", "user", unit)); err == nil {
		return []string{"--user"}, "user"
	}
	return nil, "system"
}

func servicesProbe(rawURL string, timeout time.Duration) (int, bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := HTTPGet(ctx, rawURL, nil)
	if err != nil {
		return 0, false, ""
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, true, string(body)
}
