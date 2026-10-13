// Package resumer defines the persistence contract the torrent package uses
// to save and restore download progress across restarts.
package resumer

// Stats carries the lifetime counters of a torrent.
type Stats struct {
	BytesDownloaded int64
	BytesUploaded   int64
	BytesWasted     int64
	SeededFor       int64 // time.Duration
}
