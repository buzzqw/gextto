package subtitles

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func legacyLoginResponse() string {
	return `<?xml version="1.0"?><methodResponse><params><param><value><struct><member><name>token</name><value><string>legacy-token</string></value></member><member><name>status</name><value><string>200 OK</string></value></member></struct></value></param></params></methodResponse>`
}

func TestLegacySearchAndDownload(t *testing.T) {
	var searches, downloads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file.gz" {
			var compressed bytes.Buffer
			writer := gzip.NewWriter(&compressed)
			_, _ = io.WriteString(writer, "1\n00:00:00,000 --> 00:00:01,000\nCiao\n")
			_ = writer.Close()
			_, _ = w.Write(compressed.Bytes())
			return
		}
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		switch {
		case strings.Contains(text, "<methodName>LogIn</methodName>"):
			if r.Header.Get("User-Agent") != "Gextto Test" {
				t.Errorf("login user agent = %q", r.Header.Get("User-Agent"))
			}
			_, _ = io.WriteString(w, legacyLoginResponse())
		case strings.Contains(text, "<methodName>SearchSubtitles</methodName>"):
			searches.Add(1)
			if !strings.Contains(text, "legacy-token") || !strings.Contains(text, "Iron Lung 2026") || !strings.Contains(text, "eng") {
				t.Errorf("search request = %s", text)
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param><value><struct>
				<member><name>status</name><value><string>200 OK</string></value></member>
				<member><name>data</name><value><array><data><value><struct>
					<member><name>MatchedBy</name><value><string>fulltext</string></value></member>
					<member><name>IDSubtitleFile</name><value><string>1962367247</string></value></member>
					<member><name>SubFileName</name><value><string>IRON.LUNG.2026.srt</string></value></member>
					<member><name>SubLanguageID</name><value><string>eng</string></value></member>
					<member><name>ISO639</name><value><string>en</string></value></member>
					<member><name>SubFormat</name><value><string>srt</string></value></member>
					<member><name>SubDownloadsCnt</name><value><string>51356</string></value></member>
					<member><name>SubFromTrusted</name><value><string>1</string></value></member>
					<member><name>SubAutoTranslation</name><value><string>0</string></value></member>
					<member><name>SubHearingImpaired</name><value><string>0</string></value></member>
					<member><name>SubForeignPartsOnly</name><value><string>0</string></value></member>
					<member><name>MovieReleaseName</name><value><string>IRON.LUNG.2026.1080p.WEB-DL-SCOPE</string></value></member>
					<member><name>MovieName</name><value><string>Iron Lung</string></value></member>
					<member><name>MovieYear</name><value><string>2026</string></value></member>
					<member><name>MovieFPS</name><value><string>23.976</string></value></member>
				</struct></value><value><struct>
					<member><name>IDSubtitleFile</name><value><string>99</string></value></member>
					<member><name>SubFormat</name><value><string>sub</string></value></member>
				</struct></value></data></array></value></member>
			</struct></value></param></params></methodResponse>`)
		case strings.Contains(text, "<methodName>DownloadSubtitles</methodName>"):
			downloads.Add(1)
			if !strings.Contains(text, "legacy-token") || !strings.Contains(text, "1962367247") {
				t.Errorf("download request = %s", text)
			}
			_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param><value><struct>
				<member><name>status</name><value><string>200 OK</string></value></member>
				<member><name>data</name><value><array><data><value><struct>
					<member><name>Link</name><value><string>`+server.URL+`/file.gz</string></value></member>
					<member><name>SubFileName</name><value><string>IRON.LUNG.2026.srt.gz</string></value></member>
				</struct></value></data></array></value></member>
			</struct></value></param></params></methodResponse>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewLegacy(Config{BaseURL: server.URL, UserAgent: "Gextto Test", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.Search(context.Background(), SearchQuery{Language: "en", Kind: "movie", Title: "Iron Lung", Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Language != "en" || !results[0].HashMatch && results[0].Release() == "" {
		t.Fatalf("legacy results = %+v", results)
	}
	if results[0].FPS != 23.976 || results[0].DownloadCount != 51356 || results[0].FromTrusted == nil || !*results[0].FromTrusted {
		t.Fatalf("legacy reputation = %+v", results[0])
	}
	file, ok := results[0].PrimaryFile()
	if !ok || file.ID != 1962367247 {
		t.Fatalf("primary file = %+v, %v", file, ok)
	}
	download, err := client.Download(context.Background(), int64(file.ID), "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(download.Data), "Ciao") || download.FileName != "IRON.LUNG.2026.srt" {
		t.Fatalf("legacy download = %+v", download)
	}
	if searches.Load() != 1 || downloads.Load() != 1 {
		t.Fatalf("searches=%d downloads=%d", searches.Load(), downloads.Load())
	}
}

func TestLegacyLoginFallsBackToAnonymous(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "<methodName>LogIn</methodName>") {
			http.NotFound(w, r)
			return
		}
		if logins.Add(1) == 1 {
			_, _ = io.WriteString(w, `<?xml version="1.0"?><methodResponse><params><param><value><struct><member><name>status</name><value><string>401 Unauthorized</string></value></member></struct></value></param></params></methodResponse>`)
			return
		}
		_, _ = io.WriteString(w, legacyLoginResponse())
	}))
	defer server.Close()
	client, err := NewLegacy(Config{BaseURL: server.URL, Username: "com-user", Password: "com-pass", UserAgent: "Gextto Test", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 || client.token != "legacy-token" {
		t.Fatalf("logins=%d token=%q", logins.Load(), client.token)
	}
}

func TestLegacyLanguages(t *testing.T) {
	if got := legacyLanguages(""); got != "all" {
		t.Fatalf("empty languages = %q", got)
	}
	if got := legacyLanguages("it,en"); got != "ita,eng" {
		t.Fatalf("mapped languages = %q", got)
	}
}
