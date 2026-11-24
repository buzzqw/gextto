package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestWebPolicyGxTorrentRoutesAddFrameHeaders(t *testing.T) {
	managed := &Daemon{opts: Options{Mode: ModeManaged}}
	handler := managed.routes()
	for _, tc := range []struct {
		name   string
		path   string
		status int
	}{
		{name: "API response", path: "/api/v1/health", status: http.StatusOK},
		{name: "HTML fragment", path: "/ui/live", status: http.StatusOK},
		{name: "not found", path: "/missing", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890"+tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			assertWebPolicyFrameHeaders(t, rec)
		})
	}

	standalone := standaloneTestDaemon(t, "", true)
	handler = standalone.routes()
	t.Run("setup redirect", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui", nil))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		assertWebPolicyFrameHeaders(t, rec)
	})
	t.Run("cross-origin refusal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/config", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
		assertWebPolicyFrameHeaders(t, rec)
	})
}

func TestWebPolicyDHTFeedPublishIsPostOnly(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	handler := d.routes()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/api/v1/dht-feed/publish?name=web", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET publish status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodPost) {
		t.Fatalf("Allow = %q, want POST", allow)
	}

	// An invalid JSON body proves POST still reaches the handler without
	// starting a publish or needing a running torrent session.
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/dht-feed/publish", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid JSON body") {
		t.Fatalf("POST publish = %d %q, want the JSON validation error", rec.Code, rec.Body.String())
	}
}

func TestWebPolicySettingsRowLabelsEscapeTranslatedValues(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	payload := `HD" onfocus="alert(1)"><script>alert(1)</script>&`
	raw, err := json.Marshal([]qualityProfile{{Name: payload, Qualities: []string{"720p"}, Cutoff: "720p"}})
	if err != nil {
		t.Fatal(err)
	}
	d.opts.Settings.Set(qualityProfilesKey, string(raw))

	t.Run("quality profile labels", func(t *testing.T) {
		rows := d.qualityProfileRows("en")
		if strings.Contains(rows, "<script") {
			t.Fatal("the profile name injected a script element")
		}
		for _, tc := range []struct {
			tag  string
			name string
			want string
		}{
			{tag: "input", name: "qualities", want: uiTr("en", "Qualities of %s", payload)},
			{tag: "select", name: "cutoff", want: uiTr("en", "Cutoff of %s", payload)},
		} {
			control := webPolicyHTMLControl(t, rows, tc.tag, tc.name)
			if got, ok := webPolicyHTMLAttr(control, "aria-label"); !ok || got != tc.want {
				t.Fatalf("%s aria-label = %q (present=%v), want %q", tc.name, got, ok, tc.want)
			}
			for _, attr := range control.Attr {
				if strings.HasPrefix(strings.ToLower(attr.Key), "on") {
					t.Fatalf("the profile name injected the %q attribute", attr.Key)
				}
			}
		}
	})

	t.Run("category label", func(t *testing.T) {
		d.state.Categories = map[string]string{payload: "/downloads"}
		rows := d.categoryRows("en")
		if strings.Contains(rows, "<script") {
			t.Fatal("the category name injected a script element")
		}
		control := webPolicyHTMLControl(t, rows, "input", "savepath")
		want := uiTr("en", "Save path of %s", payload)
		if got, ok := webPolicyHTMLAttr(control, "aria-label"); !ok || got != want {
			t.Fatalf("savepath aria-label = %q (present=%v), want %q", got, ok, want)
		}
		for _, attr := range control.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Key), "on") {
				t.Fatalf("the category name injected the %q attribute", attr.Key)
			}
		}
	})
}

func webPolicyHTMLControl(t *testing.T, fragment, tag, name string) *xhtml.Node {
	t.Helper()
	doc, err := xhtml.Parse(strings.NewReader("<table><tbody>" + fragment + "</tbody></table>"))
	if err != nil {
		t.Fatalf("parse the HTML fragment: %v", err)
	}
	var find func(*xhtml.Node) *xhtml.Node
	find = func(node *xhtml.Node) *xhtml.Node {
		if node.Type == xhtml.ElementNode && node.Data == tag {
			if got, ok := webPolicyHTMLAttr(node, "name"); ok && got == name {
				return node
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := find(child); found != nil {
				return found
			}
		}
		return nil
	}
	control := find(doc)
	if control == nil {
		t.Fatalf("no <%s name=%q> in the fragment", tag, name)
	}
	return control
}

func webPolicyHTMLAttr(node *xhtml.Node, key string) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val, true
		}
	}
	return "", false
}

func assertWebPolicyFrameHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy = %q, want frame-ancestors 'none'", got)
	}
}
