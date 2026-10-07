package gextto

// uiweb_engine_stats.go builds the "Motore torrent" panel of the Salute page:
// the live state of the active transfer engine (gx-torrent, libtorrent or
// qBittorrent) in a few readable rows.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

type uiEngineRow struct {
	Label string
	Value string
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
// decoding yields float64, the embedded engine native integers).
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

func onOff(on bool, detail string) string {
	if !on {
		return "spento"
	}
	if detail == "" {
		return "attivo"
	}
	return "attivo · " + detail
}

func uiEngineStatsFrom(s *AppState) uiEngineStats {
	if s == nil || !s.hasTorrentEngine() {
		return uiEngineStats{Error: "motore torrent non disponibile"}
	}
	engine := s.activeEngine()
	switch engine.Name() {
	case BackendGxTorrent:
		return uiGxEngineStats(engine)
	case BackendQbittorrent:
		return uiQbitEngineStats(engine)
	default:
		return uiEmbeddedEngineStats(s, engine)
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
	add := func(label, value string, warn bool) {
		out.Rows = append(out.Rows, uiEngineRow{Label: label, Value: value, Warn: warn})
	}
	add("Versione", engineString(stats, "version")+" · attivo da "+logging.HumanDuration(engineNum(stats, "uptime_seconds")), false)
	add("Torrent", fmt.Sprintf("%d · in download %d · in seed %d · in coda %d · bloccati %d · lenti %d",
		engineNum(stats, "torrents"), engineNum(stats, "downloading"), engineNum(stats, "seeding"),
		engineNum(stats, "queued"), engineNum(stats, "stalled"), engineNum(stats, "slow")), false)
	add("Velocità", fmt.Sprintf("↓ %s · ↑ %s", logging.HumanRate(engineNum(stats, "download_rate")), logging.HumanRate(engineNum(stats, "upload_rate"))), false)
	add("Slot coda", fmt.Sprintf("download %d · seed %d · totale %d", engineNum(stats, "active_downloads"),
		engineNum(stats, "active_seeds"), engineNum(stats, "active_limit")), false)
	if port := engineNum(stats, "peer_port"); port > 0 {
		add("Porta peer", fmt.Sprintf("%d (TCP e UDP) su %s", port, engineString(stats, "listen_address")), false)
	}
	switch method := engineString(mapping, "method"); {
	case method != "":
		add("Router", fmt.Sprintf("porta aperta con %s · indirizzo esterno %s", strings.ToUpper(method), engineString(mapping, "external_ip")), false)
	case engineString(mapping, "error") != "":
		add("Router", fmt.Sprintf("porta non aperta automaticamente (né UPnP né NAT-PMP): aprila a mano, TCP e UDP %d", engineNum(stats, "peer_port")), true)
		out.Rows[len(out.Rows)-1].Detail = engineString(mapping, "error")
	}
	add("DHT", onOff(engineBool(stats, "dht"), fmt.Sprintf("%d nodi", engineNum(session, "dht_nodes"))), false)
	add("uTP", onOff(engineBool(stats, "utp"), fmt.Sprintf("connessioni in uscita uTP %d / TCP %d · in entrata uTP %d",
		engineNum(session, "peers_outgoing_utp"), engineNum(session, "peers_outgoing_tcp"), engineNum(session, "peers_incoming_utp"))), false)
	lsdText := onOff(engineBool(lsd, "enabled"), fmt.Sprintf("%d peer trovati in rete locale", engineNum(lsd, "peers_found")))
	add("LSD", lsdText, engineString(lsd, "error") != "")
	out.Rows[len(out.Rows)-1].Detail = engineString(lsd, "error")
	encryption := map[int64]string{0: "disattivata", 1: "attiva", 2: "obbligatoria"}[engineNum(stats, "encryption")]
	add("Cifratura", encryption, false)
	proxy := "nessuno"
	if engineBool(stats, "proxy") {
		proxy = "attivo (DHT e tracker UDP spenti)"
	}
	add("Proxy", proxy, false)
	add("Filtro IP", fmt.Sprintf("%d regole", engineNum(stats, "ip_filter_rules")), false)
	add("Disco", fmt.Sprintf("lettura %s · scrittura %s · cache lettura %s (hit %d%%) · buffer scrittura %s",
		logging.HumanRate(engineNum(session, "disk_read_rate")), logging.HumanRate(engineNum(session, "disk_write_rate")),
		logging.HumanBytesI64(engineNum(session, "read_cache_bytes")), engineNum(session, "read_cache_hit_percent"),
		logging.HumanBytesI64(engineNum(session, "write_cache_bytes"))), false)
	add("Totali sessione", fmt.Sprintf("scaricati %s · inviati %s · peer connessi %d",
		logging.HumanBytesI64(engineNum(session, "bytes_downloaded")), logging.HumanBytesI64(engineNum(session, "bytes_uploaded")),
		engineNum(stats, "peers")), false)
	return out
}

func uiEmbeddedEngineStats(s *AppState, engine TorrentEngine) uiEngineStats {
	out := uiEngineStats{Backend: "libtorrent " + LibtorrentVersion()}
	stats := engine.Stats()
	add := func(label, value string) {
		out.Rows = append(out.Rows, uiEngineRow{Label: label, Value: value})
	}
	add("Torrent", fmt.Sprintf("%d · in download %d · in seed %d · in coda %d · bloccati %d",
		engineNum(stats, "count"), engineNum(stats, "downloading"), engineNum(stats, "seeding"),
		engineNum(stats, "queued"), engineNum(stats, "stalled")))
	client, err := s.requireEmbedded("session_stats")
	if err != nil || client == nil {
		return out
	}
	counters, err := client.SessionStats()
	if err != nil || len(counters) == 0 {
		return out
	}
	get := func(name string) int64 { return counters[name] }
	add("DHT", fmt.Sprintf("%d nodi", get("dht.dht_nodes")))
	add("Peer", fmt.Sprintf("TCP %d · uTP %d", get("peer.num_tcp_peers"), get("peer.num_utp_peers")))
	incoming := "nessuna finora (porta forse chiusa sul router)"
	if get("net.has_incoming_connections") > 0 {
		incoming = "sì"
	}
	add("Connessioni in entrata", incoming)
	add("Disco", fmt.Sprintf("job in coda %d · letture %d · scritture %d", get("disk.queued_disk_jobs"),
		get("disk.num_read_ops"), get("disk.num_write_ops")))
	add("Totali sessione", fmt.Sprintf("scaricati %s · inviati %s",
		logging.HumanBytesI64(get("net.recv_payload_bytes")), logging.HumanBytesI64(get("net.sent_payload_bytes"))))
	return out
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
