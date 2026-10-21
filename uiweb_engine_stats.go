package gextto

// uiweb_engine_stats.go builds the "Motore torrent" panel of the Salute page:
// the live state of the active transfer engine (gx-torrent or qBittorrent) in a
// few readable rows.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

type uiEngineRow struct {
	Label string
	Value string
	// Parts is Value cut into catalog phrases and printed values. The page
	// renders each part in its own element: the translation works on whole
	// text nodes, so a phrase inside a composed row ("6 · in download 2")
	// is reached only when it stands alone.
	Parts []string
	Warn  bool
	// Detail is shown as a tooltip (technical error text).
	Detail string
}

type uiEngineStats struct {
	Backend string
	Rows    []uiEngineRow
	Error   string
}

// engineNum reads a number from a stats map whatever its Go type (JSON
// decoding yields float64, the HTTP APIs native integers).
func engineNum(values map[string]any, key string) int64 {
	switch v := values[key].(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint32:
		return int64(v)
	case uint64:
		return saturatingInt64(v)
	case float64:
		return int64(v)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

func engineBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

// engineFloat reads a fractional counter (a ratio) from the engine's stats map.
func engineFloat(values map[string]any, key string) float64 {
	switch v := values[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	}
	return 0
}

func engineMap(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func engineString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

// engineVerb matches a fmt verb (or an escaped %%) in an engineParts format.
var engineVerb = regexp.MustCompile(`%(?:%|[-+# 0]*\d*(?:\.\d+)?[a-zA-Z])`)

// engineParts formats like fmt.Sprintf, but returns the result in pieces:
// every printed argument is a piece of its own, and the literal text is cut
// at the "·" separators, so each phrase between them can match a catalog key.
// Joined, the pieces give exactly the Sprintf result.
func engineParts(format string, args ...any) []string {
	var parts []string
	literal := ""
	flush := func() {
		for i, piece := range strings.Split(literal, "·") {
			if i > 0 {
				parts = append(parts, "·")
			}
			if piece != "" {
				parts = append(parts, piece)
			}
		}
		literal = ""
	}
	next := 0
	for _, loc := range engineVerb.FindAllStringIndex(format, -1) {
		literal += format[next:loc[0]]
		next = loc[1]
		verb := format[loc[0]:loc[1]]
		if verb == "%%" {
			literal += "%"
			continue
		}
		flush()
		if len(args) == 0 {
			parts = append(parts, verb)
			continue
		}
		parts = append(parts, fmt.Sprintf(verb, args[0]))
		args = args[1:]
	}
	literal += format[next:]
	flush()
	return parts
}

func onOff(on bool, detail []string) []string {
	if !on {
		return []string{"spento"}
	}
	if len(detail) == 0 {
		return []string{"attivo"}
	}
	return append([]string{"attivo", " ", "·", " "}, detail...)
}

func uiEngineStatsFrom(s *AppState) uiEngineStats {
	if s == nil || !s.hasTorrentEngine() {
		return uiEngineStats{Error: "motore torrent non disponibile"}
	}
	engine := s.activeEngine()
	if engine == nil {
		return uiEngineStats{Backend: "-", Error: "nessun motore torrent attivo"}
	}
	switch engine.Name() {
	case BackendQbittorrent:
		return uiQbitEngineStats(engine)
	default:
		return uiGxEngineStats(engine)
	}
}

func uiGxEngineStats(engine TorrentEngine) uiEngineStats {
	stats := engine.Stats()
	out := uiEngineStats{Backend: "gx-torrent"}
	if loaded, _ := stats["session_loaded"].(bool); !loaded {
		out.Error = "gx-torrent non risponde: " + engineString(stats, "error")
		return out
	}
	session := engineMap(stats, "session")
	mapping := engineMap(stats, "port_mapping")
	lsd := engineMap(stats, "lsd")
	add := func(label string, parts []string, warn bool) {
		out.Rows = append(out.Rows, uiEngineRow{Label: label, Value: strings.Join(parts, ""), Parts: parts, Warn: warn})
	}
	add("Versione", engineParts("%s · attivo da %s", engineString(stats, "version"), logging.HumanDuration(engineNum(stats, "uptime_seconds"))), false)
	add("Torrent", engineParts("%d · in download %d · in seed %d · in coda %d · bloccati %d · lenti %d",
		engineNum(stats, "torrents"), engineNum(stats, "downloading"), engineNum(stats, "seeding"),
		engineNum(stats, "queued"), engineNum(stats, "stalled"), engineNum(stats, "slow")), false)
	add("Velocità", engineParts("↓ %s · ↑ %s", logging.HumanRate(engineNum(stats, "download_rate")), logging.HumanRate(engineNum(stats, "upload_rate"))), false)
	add("Slot coda", engineParts("download %d · seed %d · totale %d", engineNum(stats, "active_downloads"),
		engineNum(stats, "active_seeds"), engineNum(stats, "active_limit")), false)
	if port := engineNum(stats, "peer_port"); port > 0 {
		add("Porta peer", engineParts("%d · TCP e UDP · indirizzo %s", port, engineString(stats, "listen_address")), false)
	}
	switch method := engineString(mapping, "method"); {
	case method != "":
		add("Router", engineParts("porta aperta con %s · indirizzo esterno %s", strings.ToUpper(method), engineString(mapping, "external_ip")), false)
	case engineString(mapping, "error") != "":
		add("Router", engineParts("porta non aperta automaticamente (né UPnP né NAT-PMP): aprila a mano, TCP e UDP %d", engineNum(stats, "peer_port")), true)
		out.Rows[len(out.Rows)-1].Detail = engineString(mapping, "error")
	}
	add("DHT", onOff(engineBool(stats, "dht"), engineParts("%d nodi", engineNum(session, "dht_nodes"))), false)
	utpParts := engineParts("connessioni in uscita uTP %d / TCP %d · in entrata uTP %d",
		engineNum(session, "peers_outgoing_utp"), engineNum(session, "peers_outgoing_tcp"), engineNum(session, "peers_incoming_utp"))
	utpWarn := false
	if v, ok, warn := uiUTPSocketRow(session); ok {
		utpParts = append(append(utpParts, " ", "·", " "), v...)
		utpWarn = warn
	}
	add("uTP", onOff(engineBool(stats, "utp"), utpParts), utpWarn)
	add("LSD", onOff(engineBool(lsd, "enabled"), engineParts("%d peer trovati in rete locale", engineNum(lsd, "peers_found"))), engineString(lsd, "error") != "")
	out.Rows[len(out.Rows)-1].Detail = engineString(lsd, "error")
	incoming := engineNum(session, "peers_incoming_tcp") + engineNum(session, "peers_incoming_utp")
	incomingParts := []string{"nessuna finora (porta forse chiusa sul router)"}
	if incoming > 0 {
		incomingParts = engineParts("%s · %d", "sì", incoming)
	}
	add("Connessioni in entrata", incomingParts, false)
	encryption := map[int64]string{0: "disattivata", 1: "attiva", 2: "obbligatoria"}[engineNum(stats, "encryption")]
	add("Cifratura", []string{encryption}, false)
	proxy := "nessuno"
	if engineBool(stats, "proxy") {
		proxy = "attivo (DHT e tracker UDP spenti)"
	}
	add("Proxy", []string{proxy}, false)
	add("Filtro IP", engineParts("%d regole", engineNum(stats, "ip_filter_rules")), false)
	cacheMode := "impostata a mano"
	if engineBool(stats, "cache_auto") {
		cacheMode = "automatica (dalla RAM)"
	}
	add("Cache disco", engineParts("%s · lettura %d MB · scrittura %d MB · preallocazione %s", cacheMode,
		engineNum(stats, "cache_read_mb"), engineNum(stats, "cache_write_mb"), ternaryString(engineBool(stats, "preallocate"), "sì", "no")), false)
	add("I/O disco", engineParts("job in coda %d · letture %d · scritture %d",
		engineNum(session, "disk_queue_depth"), engineNum(session, "read_ops_total"), engineNum(session, "write_ops_total")), false)
	add("Disco", engineParts("lettura %s · scrittura %s · cache lettura %s · hit %d%% · buffer scrittura %s",
		logging.HumanRate(engineNum(session, "disk_read_rate")), logging.HumanRate(engineNum(session, "disk_write_rate")),
		logging.HumanBytesI64(engineNum(session, "read_cache_bytes")), engineNum(session, "read_cache_hit_percent"),
		logging.HumanBytesI64(engineNum(session, "write_cache_bytes"))), false)
	add("Totali sessione", engineParts("scaricati %s · inviati %s · overhead protocollo %s · peer connessi %d",
		logging.HumanBytesI64(engineNum(session, "bytes_downloaded")), logging.HumanBytesI64(engineNum(session, "bytes_uploaded")),
		logging.HumanBytesI64(engineNum(session, "protocol_overhead_bytes")), engineNum(stats, "peers")), false)
	// All-time totals, kept across restarts: one row per value, so every label
	// is a catalog key and the value is only a number with a unit.
	if lifetime := engineMap(stats, "lifetime"); len(lifetime) > 0 {
		add("Scaricato in totale", []string{logging.HumanBytesI64(engineNum(lifetime, "downloaded_bytes"))}, false)
		add("Inviato in totale", []string{logging.HumanBytesI64(engineNum(lifetime, "uploaded_bytes"))}, false)
		add("Rapporto totale", []string{fmt.Sprintf("%.2f", engineFloat(lifetime, "ratio"))}, false)
	}
	if v, ok := uiMergeReuseRow(session); ok {
		add("Riuso locale", v, false)
	}
	return out
}

// uiMergeReuseRow renders the Swarm Merging local-reuse row from the session
// counters, or ("", false) when nothing was reused (the row is then hidden).
func uiMergeReuseRow(session map[string]any) ([]string, bool) {
	pieces := engineNum(session, "merged_pieces")
	partial := engineNum(session, "merged_partial_pieces")
	if pieces <= 0 && partial <= 0 {
		return nil, false
	}
	row := engineParts("%d pezzi · %s riusati da altri torrent (senza rete)",
		pieces, logging.HumanBytesI64(engineNum(session, "merged_bytes")))
	if partial > 0 {
		// Phase 3: pieces filled in part from local blocks, the rest from peers.
		row = append(row, engineParts(" · %d pezzi riusati a blocchi", partial)...)
	}
	return row, true
}

// uiUTPSocketRow renders the uTP socket counters, hidden while the socket
// never dialled nor accepted. Socket errors warn only when nothing was ever
// accepted: persistent dial errors with zero accepts point at firewalled UDP
// (or CGNAT), where uTP cannot work and TCP carries everything; with accepts
// flowing the errors are noise. RTO/fast resends and dropped SYNs are the
// congestion/burst signals, shown only when they actually happened.
func uiUTPSocketRow(session map[string]any) ([]string, bool, bool) {
	dials := engineNum(session, "utp_dials")
	accepts := engineNum(session, "utp_accepts")
	if dials <= 0 && accepts <= 0 {
		return nil, false, false
	}
	dialErrs := engineNum(session, "utp_dial_errors")
	acceptErrs := engineNum(session, "utp_accept_errors")
	row := engineParts("socket: %d dial / %d accept", dials, accepts)
	if dialErrs+acceptErrs > 0 {
		row = append(row, engineParts(" · errori %d dial / %d accept", dialErrs, acceptErrs)...)
	}
	if tmo, fast := engineNum(session, "utp_resends_timeout"), engineNum(session, "utp_resends_fast"); tmo+fast > 0 {
		row = append(row, engineParts(" · resend RTO %d / fast %d", tmo, fast)...)
	}
	if drops := engineNum(session, "utp_syn_drops"); drops > 0 {
		row = append(row, engineParts(" · SYN scartati %d", drops)...)
	}
	return row, true, dialErrs > 0 && accepts == 0
}

func uiQbitEngineStats(engine TorrentEngine) uiEngineStats {
	stats := engine.Stats()
	out := uiEngineStats{Backend: "qBittorrent-nox"}
	if errText := engineString(stats, "error"); errText != "" {
		out.Error = "qBittorrent non risponde: " + errText
		return out
	}
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "backend", "dry_run", "session_loaded":
			continue
		}
		out.Rows = append(out.Rows, uiEngineRow{Label: key, Value: fmt.Sprint(stats[key])})
	}
	return out
}
