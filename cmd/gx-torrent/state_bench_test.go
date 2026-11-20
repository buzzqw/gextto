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

// BenchmarkSaveStateLocked measures one synchronous state save: json.MarshalIndent
// of the whole state plus the atomic write, the work a handler did while holding
// d.mu so it could answer "saved". It is still the "saved before reply" path
// (saveLocked); the queue tick no longer runs it — see BenchmarkScheduleSave.
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

// BenchmarkScheduleSave measures what a dirty queue tick pays for persistence
// now: the flag and one non-blocking signal to the saver goroutine, under d.mu.
// The marshal and the file write happen once per debounce window, off the lock.
func BenchmarkScheduleSave(b *testing.B) {
	for _, tc := range []struct {
		name     string
		torrents int
	}{
		{"torrents=100", 100},
		{"torrents=1000", 1000},
		{"torrents=5000", 5000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			d := benchState(b.TempDir(), tc.torrents, 0)
			// A long window keeps the saver asleep: the benchmark measures the
			// tick, not the writer.
			d.stateDelay = time.Hour
			d.startStateSaver()
			b.Cleanup(d.stopStateSaver)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				d.mu.Lock()
				d.dirty = true
				d.scheduleSaveLocked()
				d.mu.Unlock()
			}
		})
	}
}

// BenchmarkStateFileWrite measures only the atomic replace (os.WriteFile plus
// os.Rename) of an already encoded state: the part of a save the saver now does
// outside d.mu. The marshal is measured by BenchmarkSaveStateLocked.
func BenchmarkStateFileWrite(b *testing.B) {
	d := benchState(b.TempDir(), 1000, 0)
	d.mu.Lock()
	seq, data, err := d.snapshotStateLocked()
	d.mu.Unlock()
	if err != nil {
		b.Fatal(err)
	}
	if err := d.writeState(seq, data); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.stateSeq++
		if err := d.writeState(d.stateSeq, data); err != nil {
			b.Fatal(err)
		}
	}
}
