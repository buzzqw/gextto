package main

import (
	"encoding/json"
	"testing"
	"time"
)

// TestCloneStateKeepsTheWholeSnapshot: the copy taken under the lock must
// carry every map, pointer and time of that instant, and mutating the live
// state afterwards must not change what the snapshot will write.
func TestCloneStateKeepsTheWholeSnapshot(t *testing.T) {
	limit := int64(100)
	seeded := &persistedState{
		Version: 1,
		NextPos: 7,
		Torrents: map[string]*torrentMeta{
			"a": {ID: "a", Hash: "a", AddedAt: time.Unix(100, 0), DownloadLimitKib: &limit,
				FilePriorities: []int{0, 1, 2}, Tags: []string{"ita", "1080p"}},
		},
		FeedSeen: map[string]int64{"feed|item": 123},
		FeedSmart: map[string]smartEpisodes{
			"series|1": {"1x01": 456},
		},
		WantedTakenTorrent: map[string]map[string]string{"title": {"1x01": "hash"}},
		Categories:         map[string]string{"serie": "/media/serie"},
		Tags:               []string{"one", "two"},
	}
	want, err := json.MarshalIndent(seeded, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	snapshot := cloneState(seeded)
	// Change the live state the way a handler would right after the copy.
	seeded.NextPos = 99
	seeded.Torrents["a"].UserPaused = true
	seeded.Torrents["a"].FilePriorities[1] = 9
	*seeded.Torrents["a"].DownloadLimitKib = 1
	seeded.FeedSeen["feed|item"] = 0
	seeded.Tags[0] = "changed"
	limit = 2

	got, err := encodeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("the snapshot changed with the live state:\n%s\nwant:\n%s", got, want)
	}
}
