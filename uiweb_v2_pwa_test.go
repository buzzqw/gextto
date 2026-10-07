package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSharedLinkPrefersMagnet(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test"
	if got := v2SharedLink("https://example.org/page", "guarda "+magnet+" ciao"); got != magnet {
		t.Fatalf("link = %q, want the magnet", got)
	}
	if got := v2SharedLink("", "file https://tracker.example/x.torrent"); got != "https://tracker.example/x.torrent" {
		t.Fatalf("link = %q", got)
	}
	if got := v2SharedLink("", "nothing here"); got != "" {
		t.Fatalf("link = %q, want none", got)
	}
}

func TestShareRedirectsToDownloadsWithTheLink(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	request := httptest.NewRequest(http.MethodGet, "/share?text="+url.QueryEscape("vedi "+magnet), nil)
	recorder := httptest.NewRecorder()
	V2Share(recorder, request, nil)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || location.Query().Get("view") != "downloads" || location.Query().Get("add") != magnet {
		t.Fatalf("location = %q", recorder.Header().Get("Location"))
	}
}

func TestManifestDeclaresShareTarget(t *testing.T) {
	recorder := httptest.NewRecorder()
	V2Manifest(recorder, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil), nil)
	body := recorder.Body.String()
	for _, want := range []string{`"share_target"`, `"action": "/share"`, `"display": "standalone"`, "icon-512.png"} {
		if !strings.Contains(body, want) {
			t.Fatalf("manifest misses %s", want)
		}
	}
}

func TestLogLineIsProblem(t *testing.T) {
	for line, want := range map[string]bool{
		"2026-10-07 04:21:19  WARN ⚠️ something":   true,
		"2026-10-07 04:21:19 ERROR broken":         true,
		"2026-10-07 04:21:19  INFO a WARN in text": false,
		"short": false,
	} {
		if got := v2LogLineIsProblem(line); got != want {
			t.Fatalf("%q = %v, want %v", line, got, want)
		}
	}
}
