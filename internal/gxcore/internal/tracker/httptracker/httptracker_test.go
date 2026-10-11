package httptracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/zeebo/bencode"
)

func TestScrapeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://tracker.example/announce", "http://tracker.example/scrape"},
		{"http://tracker.example/announce?passkey=abc", "http://tracker.example/scrape?passkey=abc"},
		{"http://tracker.example", "http://tracker.example/scrape"},
		{"http://tracker.example/foo", "http://tracker.example/foo/scrape"},
	}
	for _, c := range cases {
		if got := scrapeURL(c.in); got != c.want {
			t.Errorf("scrapeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHTTPScrape(t *testing.T) {
	hash := [20]byte{}
	for i := range hash {
		hash[i] = byte(i + 1)
	}

	var gotPath, gotHash string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHash = r.URL.Query().Get("info_hash")
		resp := scrapeResponse{Files: map[string]scrapeFile{
			string(hash[:]): {Complete: 7, Incomplete: 3, Downloaded: 42},
		}}
		body, err := bencode.EncodeBytes(resp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL + "/announce")
	tr := New(srv.URL+"/announce", u, 5*time.Second, &http.Transport{}, "test", 1<<20)

	resp, err := tr.Scrape(context.Background(), tracker.ScrapeRequest{Torrent: tracker.Torrent{InfoHash: hash}})
	if err != nil {
		t.Fatalf("Scrape: %s", err)
	}
	if gotPath != "/scrape" {
		t.Errorf("scrape path = %q, want /scrape", gotPath)
	}
	if gotHash != string(hash[:]) {
		t.Errorf("info_hash = %q, want the raw info hash", gotHash)
	}
	if resp.Seeders != 7 || resp.Leechers != 3 || resp.Downloaded != 42 {
		t.Errorf("got seeders=%d leechers=%d downloaded=%d", resp.Seeders, resp.Leechers, resp.Downloaded)
	}
}

func TestHTTPScrapeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body, _ := bencode.EncodeBytes(scrapeResponse{FailureReason: "scrape disabled"})
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL + "/announce")
	tr := New(srv.URL+"/announce", u, 5*time.Second, &http.Transport{}, "test", 1<<20)

	_, err := tr.Scrape(context.Background(), tracker.ScrapeRequest{})
	if err == nil {
		t.Fatal("expected an error")
	}
	terr, ok := err.(*tracker.Error)
	if !ok || terr.FailureReason != "scrape disabled" {
		t.Fatalf("got %#v, want a tracker.Error with the failure reason", err)
	}
}

// TestHTTPAnnounceFailureRetryIn checks that the BEP 31 "retry in" value from an
// announce failure is carried through the HTTP tracker: a number of minutes, or
// the "never" flag.
func TestHTTPAnnounceFailureRetryIn(t *testing.T) {
	cases := []struct {
		retryIn string
		want    time.Duration
		never   bool
	}{
		{"10", 10 * time.Minute, false},
		{"never", 0, true},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			body, _ := bencode.EncodeBytes(announceResponse{
				FailureReason: "unregistered torrent",
				RetryIn:       c.retryIn,
			})
			_, _ = w.Write(body)
		}))

		u, _ := url.Parse(srv.URL + "/announce")
		tr := New(srv.URL+"/announce", u, 5*time.Second, &http.Transport{}, "test", 1<<20)

		_, err := tr.Announce(context.Background(), tracker.AnnounceRequest{
			Torrent: tracker.Torrent{InfoHash: [20]byte{1}, PeerID: [20]byte{2}, Port: 6881},
		})
		terr, ok := err.(*tracker.Error)
		if !ok {
			srv.Close()
			t.Fatalf("retry in %q: got %#v, want a tracker.Error", c.retryIn, err)
		}
		if terr.FailureReason != "unregistered torrent" {
			t.Errorf("retry in %q: failure reason = %q", c.retryIn, terr.FailureReason)
		}
		if terr.RetryIn != c.want || terr.NeverRetry != c.never {
			t.Errorf("retry in %q: RetryIn=%v NeverRetry=%v, want %v/%v",
				c.retryIn, terr.RetryIn, terr.NeverRetry, c.want, c.never)
		}
		srv.Close()
	}
}
