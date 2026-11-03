package torrent

// torrent_webseed_parallel_test.go covers the parallel ranges of one webseed:
// the same URL is opened several times so a host that answers each file in
// seconds is not bound by that latency, while the user keeps seeing one entry
// per URL.

import (
	"errors"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

var errWebseedTest = errors.New("webseed failed")

// fixedMeter reports a speed without waiting for the meter's one-minute
// window, so the folded speed of the replicas is visible at once.
type fixedMeter struct{ rate float64 }

func (m fixedMeter) Count() int64            { return 0 }
func (m fixedMeter) Mark(int64)              {}
func (m fixedMeter) Rate1() float64          { return m.rate }
func (m fixedMeter) Rate5() float64          { return m.rate }
func (m fixedMeter) Rate15() float64         { return m.rate }
func (m fixedMeter) RateMean() float64       { return m.rate }
func (m fixedMeter) Snapshot() metrics.Meter { return m }
func (m fixedMeter) Stop()                   {}

func TestExpandWebseedSourcesInterleavesReplicas(t *testing.T) {
	primary := testWebseedSources(2)
	got := expandWebseedSources(primary, 3)
	if len(got) != 6 {
		t.Fatalf("expandWebseedSources gave %d sources, want 6", len(got))
	}
	wantURLs := []string{
		primary[0].URL, primary[1].URL,
		primary[0].URL, primary[1].URL,
		primary[0].URL, primary[1].URL,
	}
	for i, src := range got {
		if src.URL != wantURLs[i] {
			t.Fatalf("source %d is %q, want %q: the copies must be interleaved so the budget is shared", i, src.URL, wantURLs[i])
		}
		wantReplica := i >= len(primary)
		if src.Replica != wantReplica {
			t.Fatalf("source %d (%s): Replica=%v, want %v", i, src.URL, src.Replica, wantReplica)
		}
	}
	if got[0] != primary[0] || got[1] != primary[1] {
		t.Fatal("the primary sources must be the caller's own, not copies")
	}
	if &got[2] == &got[3] {
		t.Fatal("two replicas of different URLs are the same object")
	}

	// One range per source is the old behaviour, and a list that is not
	// expanded must come back untouched.
	if same := expandWebseedSources(primary, 1); len(same) != 2 || same[0] != primary[0] {
		t.Fatalf("expandWebseedSources(parallel 1) = %d sources, want the original list", len(same))
	}
	if empty := expandWebseedSources(nil, 4); empty != nil {
		t.Fatalf("expandWebseedSources(nil) = %v, want nil", empty)
	}
}

func TestNewWebseedSourcesMarksTheCopies(t *testing.T) {
	sources := newWebseedSources("http://seed.test/file", 3)
	if len(sources) != 3 {
		t.Fatalf("newWebseedSources(parallel 3) = %d sources, want 3", len(sources))
	}
	for i, src := range sources {
		if src.URL != "http://seed.test/file" {
			t.Fatalf("source %d URL = %q", i, src.URL)
		}
		if want := i > 0; src.Replica != want {
			t.Fatalf("source %d Replica=%v, want %v", i, src.Replica, want)
		}
	}
	if single := newWebseedSources("http://seed.test/file", 0); len(single) != 1 || single[0].Replica {
		t.Fatalf("parallel 0 must keep one primary source, got %+v", single)
	}
}

func TestGetWebseedsFoldsTheReplicasIntoOneEntry(t *testing.T) {
	primary := &webseedsource.WebseedSource{URL: "http://a.test/f", DownloadSpeed: fixedMeter{rate: 1000}}
	replica := &webseedsource.WebseedSource{URL: "http://a.test/f", DownloadSpeed: fixedMeter{rate: 2500}, Replica: true}
	other := &webseedsource.WebseedSource{URL: "http://b.test/f", DownloadSpeed: fixedMeter{rate: 700}}
	replica.LastError = errWebseedTest

	tr := &torrent{webseedSources: []*webseedsource.WebseedSource{primary, replica, other}}
	webseeds := tr.getWebseeds()
	if len(webseeds) != 2 {
		t.Fatalf("getWebseeds returned %d entries, want one per URL (2)", len(webseeds))
	}
	if webseeds[0].URL != primary.URL {
		t.Fatalf("first entry is %q, want %q", webseeds[0].URL, primary.URL)
	}
	if webseeds[0].DownloadSpeed != 3500 {
		t.Fatalf("the replica's speed is missing: %d, want 3500", webseeds[0].DownloadSpeed)
	}
	if webseeds[0].Error == nil {
		t.Fatal("the replica's error must reach the entry the user sees")
	}
	if webseeds[1].URL != other.URL || webseeds[1].Error != nil {
		t.Fatalf("second entry = %+v, want %q without error", webseeds[1], other.URL)
	}
}

// Disabling a URL parks every copy of it, and only the ones actually
// fetching are closed.
func TestDisableSourceParksEveryCopy(t *testing.T) {
	a1 := &webseedsource.WebseedSource{URL: "http://a.test/f", DownloadSpeed: fixedMeter{}}
	a2 := &webseedsource.WebseedSource{URL: "http://a.test/f", DownloadSpeed: fixedMeter{}, Replica: true}
	b := &webseedsource.WebseedSource{URL: "http://b.test/f", DownloadSpeed: fixedMeter{}}
	tr := &torrent{webseedSources: []*webseedsource.WebseedSource{a1, a2, b}, webseedActiveDownloads: 0}

	tr.disableSource(a1.URL, errWebseedTest, false)
	if !a1.Disabled || !a2.Disabled {
		t.Fatalf("the copies that are not fetching must be parked too: %v %v", a1.Disabled, a2.Disabled)
	}
	if a1.LastError == nil || a2.LastError == nil {
		t.Fatal("the error must be recorded on every copy")
	}
	if b.Disabled {
		t.Fatal("a different URL was disabled")
	}
	if tr.webseedActiveDownloads != 0 {
		t.Fatalf("no downloader was running, slot count = %d, want 0", tr.webseedActiveDownloads)
	}
}
