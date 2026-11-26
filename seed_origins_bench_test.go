package gextto

import (
	"fmt"
	"path/filepath"
	"testing"
)

// benchOriginsDB is a database with n recorded torrents, the shape the seed
// policy reads on every queue tick.
func benchOriginsDB(b *testing.B, n int) *Database {
	b.Helper()
	db, err := OpenDatabase(filepath.Join(b.TempDir(), "gextto.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	tx, err := db.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := tx.Exec("INSERT INTO torrent_meta(hash,kind,source,status,created_at,updated_at) VALUES (?1,'series','src','completed',datetime('now'),datetime('now'))", fmt.Sprintf("%040x", i)); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return db
}

// BenchmarkTorrentOriginsScan is the table scan every tick paid before the cache.
func BenchmarkTorrentOriginsScan(b *testing.B) {
	db := benchOriginsDB(b, 20000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.loadTorrentOrigins(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTorrentOriginsCached is the same read once the cache is warm.
func BenchmarkTorrentOriginsCached(b *testing.B) {
	db := benchOriginsDB(b, 20000)
	if _, err := db.TorrentOrigins(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.TorrentOrigins(); err != nil {
			b.Fatal(err)
		}
	}
}
