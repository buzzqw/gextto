package gextto

import (
	"strings"
	"testing"
)

func TestPeerPortStatusFromEngineStats(t *testing.T) {
	auto := uiPeerPortStatusFrom(map[string]any{"port_mapping": map[string]any{"method": "upnp", "external_ip": "1.2.3.4"}})
	if !auto.Auto || !auto.Known || auto.ExternalIP != "1.2.3.4" {
		t.Fatalf("mapped port status = %+v", auto)
	}
	failed := uiPeerPortStatusFrom(map[string]any{"port_mapping": map[string]any{"error": "no router"}})
	if failed.Auto || !failed.Known || failed.Error == "" {
		t.Fatalf("failed mapping = %+v", failed)
	}
	unknown := uiPeerPortStatusFrom(map[string]any{})
	if unknown.Known || unknown.Auto {
		t.Fatalf("no mapping must be unknown = %+v", unknown)
	}
}

func TestPeerPortsAreEssentialAndTheNoticeWarns(t *testing.T) {
	if !uiEssentialKeys["torrent_port_min"] || !uiEssentialKeys["torrent_port_max"] {
		t.Fatal("the peer ports must be available in Facilitata")
	}
	state := newTestAppState(t)

	text, warn := uiPeerPortNotice(state, uiPeerPortStatus{Auto: true, Method: "upnp", ExternalIP: "1.2.3.4", Known: true}, 6881, 6889)
	if warn || !strings.Contains(text, "1.2.3.4") {
		t.Fatalf("open port notice = %q warn=%v", text, warn)
	}
	text, warn = uiPeerPortNotice(state, uiPeerPortStatus{Known: true, Error: "no router"}, 6881, 6889)
	if !warn || !strings.Contains(text, "6881") {
		t.Fatalf("manual-forward notice = %q warn=%v", text, warn)
	}
	text, warn = uiPeerPortNotice(state, uiPeerPortStatus{}, 6881, 6881)
	if warn || !strings.Contains(text, "6881") {
		t.Fatalf("unknown notice = %q warn=%v", text, warn)
	}
}

// The watched-folders editor offers the server folder picker.
func TestWatchedFoldersEditorHasBrowse(t *testing.T) {
	editor, ok := v2ListEditorByKey("watched_folders")
	if !ok {
		t.Fatal("watched_folders editor not found")
	}
	browse := false
	for _, field := range editor.Fields {
		if field.Name == "path" && field.Browse {
			browse = true
		}
	}
	if !browse {
		t.Fatal("the watched-folders path field must offer Sfoglia")
	}
}
