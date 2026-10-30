package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// benchState builds a daemon whose in-memory state holds nt torrents and nf
// feed entries, the two parts that grow with the session and with the added
// releases. It is the state saveLocked writes while holding d.mu.
func benchState(dir string, nt, nf int) *Daemon {
	d := &Daemon{}
	d.opts.StatePath = filepath.Join(dir, "state.json")
	d.state = persistedState{
		Version:  1,
		NextPos:  int64(nt),
		Torrents: make(map[string]*torrentMeta, nt),
	}
	if nf > 0 {
		d.state.FeedSeen = make(map[string]int64, nf)
	}
	added := time.Unix(1_700_000_000, 0)
	for i := 0; i < nt; i++ {
		id := fmt.Sprintf("%040x", i)
		limit := int64(2048)
		d.state.Torrents[id] = &torrentMeta{
			ID:               id,
			Hash:             id,
			SavePath:         "/home/andres/trasferimento/Serie TV",
			AddedAt:          added,
			Pos:              int64(i),
			SeedRatio:        2.0,
			SeedDays:         -1,
			DownloadLimitKib: &limit,
			DoneBytes:        1 << 30,
			Category:         "serie",
			Tags:             []string{"ita", "1080p"},
			FilePriorities:   []int{0, 1, 4, 4, 4, 4, 4, 4},
		}
	}
	for i := 0; i < nf; i++ {
		d.state.FeedSeen[fmt.Sprintf("feed-%d|item-%040x", i%7, i)] = added.Unix()
	}
	return d
}

// BenchmarkSaveStateLocked measures one state save: json.MarshalIndent of the
// whole state plus the atomic write, the work the daemon did while holding
// d.mu. The figures decide whether the save belongs on a writer goroutine.
func BenchmarkSaveStateLocked(b *testing.B) {
	for _, tc := range []struct {
		name            string
		torrents, feeds int
	}{
		{"torrents=100", 100, 0},
		{"torrents=1000", 1000, 0},
		{"torrents=5000", 5000, 0},
		{"torrents=1000/feeds=20000", 1000, 20000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			d := benchState(b.TempDir(), tc.torrents, tc.feeds)
			d.dirty = true
			d.saveLocked()
			info, err := os.Stat(d.opts.StatePath)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(info.Size())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.dirty = true
				d.saveLocked()
			}
		})
	}
}
