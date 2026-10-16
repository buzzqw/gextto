package gextto

import (
	"fmt"
	"strings"
)

// uiweb_v2_ports.go: the peer port in the configuration. The ports belong to the
// user (a firewall or a router may need them forwarded), but when the engine
// opened them by itself (UPnP/NAT-PMP) there is nothing to ask: the panel says so
// and, in Facilitata, the rows step aside.

// uiPeerPortStatus is what the engine reports about opening the peer port.
type uiPeerPortStatus struct {
	Auto       bool
	Method     string
	ExternalIP string
	Error      string
	// Known is true when the engine reported something about the mapping; false
	// (qBittorrent, engine down) means we cannot tell and must keep asking.
	Known bool
}

// uiPeerPortStatusFrom reads the port-mapping status out of the engine stats.
func uiPeerPortStatusFrom(stats map[string]any) uiPeerPortStatus {
	mapping := engineMap(stats, "port_mapping")
	method := engineString(mapping, "method")
	errorText := engineString(mapping, "error")
	status := uiPeerPortStatus{
		Method:     method,
		ExternalIP: engineString(mapping, "external_ip"),
		Error:      errorText,
	}
	status.Auto = method != ""
	status.Known = status.Auto || errorText != "" || len(mapping) > 0
	return status
}

// uiPeerPortKeys are the two settings the peer ports live in.
func uiPeerPortKey(key string) bool {
	return key == "torrent_port_min" || key == "torrent_port_max"
}

// activeEngineStats returns the active engine's stats, or nil when there is no
// engine (a test state, an engine not selected yet).
func activeEngineStats(s *AppState) map[string]any {
	if s == nil {
		return nil
	}
	engine := s.activeEngine()
	if engine == nil {
		return nil
	}
	return engine.Stats()
}

// uiPeerPortNotice is the line shown on the ports panel: a confirmation when the
// port is open automatically, a warning with the exact range when it is not, or
// a hint when the engine could not tell.
func uiPeerPortNotice(s *AppState, status uiPeerPortStatus, minPort, maxPort int64) (string, bool) {
	tr := func(italian string) string { return uiText(s, italian) }
	rangeText := fmt.Sprintf("%d", minPort)
	if maxPort > 0 && maxPort != minPort {
		rangeText = fmt.Sprintf("%d–%d", minPort, maxPort)
	}
	switch {
	case status.Auto:
		external := strings.TrimSpace(status.ExternalIP)
		if external == "" {
			external = tr("indirizzo esterno non indicato")
		}
		return fmt.Sprintf(tr("Porta aperta automaticamente via %s · indirizzo esterno %s."), strings.ToUpper(status.Method), external), false
	case status.Known:
		if status.Error != "" {
			return fmt.Sprintf(tr("Gextto non è riuscito ad aprire le porte da solo (%s): inoltra sul router TCP e UDP %s."), status.Error, rangeText), true
		}
		return fmt.Sprintf(tr("Le porte non risultano aperte automaticamente: inoltra sul router TCP e UDP %s."), rangeText), true
	default:
		return fmt.Sprintf(tr("Apri o inoltra sul router TCP e UDP %s, oppure lascia che Gextto provi ad aprirle via UPnP."), rangeText), false
	}
}
