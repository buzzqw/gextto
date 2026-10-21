package torrent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/magnet"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	bencode "github.com/jackpal/bencode-go"
)

func TestBEP46InfoHash(t *testing.T) {
	ih := strings.Repeat("i", 20)
	got, err := bep46InfoHash(map[string]interface{}{"ih": ih})
	if err != nil || string(got[:]) != ih {
		t.Fatalf("bep46InfoHash = %x err=%v", got, err)
	}
	for name, value := range map[string]interface{}{
		"not a dictionary": "x",
		"missing ih":       map[string]interface{}{"v": "x"},
		"short ih":         map[string]interface{}{"ih": strings.Repeat("i", 19)},
	} {
		if _, err := bep46InfoHash(value); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestBEP46Feed(t *testing.T) {
	a := strings.Repeat("a", 20)
	b := strings.Repeat("b", 20)
	items, err := bep46Feed(map[string]interface{}{
		"v":    int64(1),
		"name": "curator",
		"items": []interface{}{
			map[string]interface{}{"ih": a, "name": "first"},
			map[string]interface{}{"ih": b},
			map[string]interface{}{"ih": "short"},   // skipped
			"junk",                                  // skipped
			map[string]interface{}{"name": "no ih"}, // skipped
		},
	})
	if err != nil {
		t.Fatalf("bep46Feed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if string(items[0].InfoHash[:]) != a || items[0].Name != "first" {
		t.Fatalf("item 0 = %q %q", items[0].InfoHash, items[0].Name)
	}
	if string(items[1].InfoHash[:]) != b || items[1].Name != "" {
		t.Fatalf("item 1 = %q %q", items[1].InfoHash, items[1].Name)
	}

	// The single-torrent form still yields exactly one item.
	single, err := bep46Feed(map[string]interface{}{"ih": a})
	if err != nil || len(single) != 1 || string(single[0].InfoHash[:]) != a {
		t.Fatalf("single form = %+v err=%v", single, err)
	}

	// A feed with a name but a broken first "ih" still keeps valid entries.
	partial, err := bep46Feed(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"ih": "x"},
			map[string]interface{}{"ih": b},
		},
	})
	if err != nil || len(partial) != 1 || string(partial[0].InfoHash[:]) != b {
		t.Fatalf("partial feed = %+v err=%v", partial, err)
	}

	for name, value := range map[string]interface{}{
		"not a dictionary": "x",
		"empty dictionary": map[string]interface{}{},
		"items wrong type": map[string]interface{}{"items": "x"},
	} {
		if _, err := bep46Feed(value); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestMagnetInfoHashMatches(t *testing.T) {
	v1 := [20]byte{}
	for i := range v1 {
		v1[i] = byte(i + 1)
	}
	v1ma := &magnet.Magnet{InfoHash: v1, HasV1: true}
	if !magnetInfoHashMatches(v1ma, &metainfo.Info{Hash: v1}) {
		t.Fatal("a matching v1 info hash must match")
	}
	if magnetInfoHashMatches(v1ma, &metainfo.Info{Hash: [20]byte{}}) {
		t.Fatal("a different v1 info hash must not match")
	}

	// A v2 magnet matches on the truncated SHA-256.
	v2ma := &magnet.Magnet{InfoHash: v1, HasV2: true}
	info := &metainfo.Info{}
	copy(info.V2Hash[:], v1[:])
	if !magnetInfoHashMatches(v2ma, info) {
		t.Fatal("a matching v2 info hash must match")
	}
}

func TestTrimDHTFeedFitsOneBEP44Item(t *testing.T) {
	items := make([]DHTFeedItem, 0, 100)
	for i := 0; i < 100; i++ {
		var ih [20]byte
		ih[19] = byte(i)
		items = append(items, DHTFeedItem{InfoHash: ih, Name: "A rather long release name that will not fit many times"})
	}
	got := trimDHTFeed("night feed", items)
	if len(got) == 0 || len(got) >= len(items) {
		t.Fatalf("trim kept %d of %d entries", len(got), len(items))
	}
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, bep46FeedValue("night feed", got)); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > bep46FeedMaxValue {
		t.Fatalf("trimmed feed is %d bytes, want <= %d", buf.Len(), bep46FeedMaxValue)
	}
	// A feed that cannot hold even one entry is refused, not sent empty.
	if got := trimDHTFeed(strings.Repeat("x", 2000), items[:1]); got != nil {
		t.Fatalf("an oversized name must trim everything, got %d", len(got))
	}
}
