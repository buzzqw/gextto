package gextto

// websearch_trackers.go is the public tracker list Gextto puts in the magnet
// links it builds when a search engine returns only an infohash for a release:
// with the trackers in the link the torrent finds peers before its metadata
// arrives. The list is the «best» one of the trackerslist project, kept current
// by its authors, downloaded once a day and kept on disk between restarts. The
// short list below is used only while no download has succeeded yet (first
// start, no network): a magnet without trackers would stay alone.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// websearchTrackerListURL is the list added to the magnets: the «best»
	// public trackers of the trackerslist project, one URL per line.
	websearchTrackerListURL = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
	// websearchTrackerListEvery is how often the list is downloaded again.
	websearchTrackerListEvery = 24 * time.Hour
	// websearchTrackerListRetry is the wait after a failed download.
	websearchTrackerListRetry = time.Hour
	// websearchTrackerListMax bounds the trackers taken from one list: a
	// magnet is a link, and every tracker travels in it.
	websearchTrackerListMax = 40
	// websearchTrackerListFile keeps the last list across restarts.
	websearchTrackerListFile = "magnet-trackers.txt"
)

// fallbackWebsearchTrackers is added to every magnet while the list has not
// been downloaded yet. They are the permanent entries of the same list.
var fallbackWebsearchTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// websearchTrackerState is the list read from the last successful download (or
// from the file it was saved to). The lock is read-only in the hot path: many
// searches build magnets at the same time.
var websearchTrackerState struct {
	sync.RWMutex
	trackers []string
}

// setWebsearchTrackers stores a new list (nil or empty keeps the previous one).
func setWebsearchTrackers(trackers []string) {
	if len(trackers) == 0 {
		return
	}
	websearchTrackerState.Lock()
	websearchTrackerState.trackers = trackers
	websearchTrackerState.Unlock()
}

// currentWebsearchTrackers returns the trackers to put in a magnet: the
// downloaded list when there is one, the built-in fallback otherwise.
func currentWebsearchTrackers() []string {
	websearchTrackerState.RLock()
	defer websearchTrackerState.RUnlock()
	if len(websearchTrackerState.trackers) > 0 {
		return websearchTrackerState.trackers
	}
	return fallbackWebsearchTrackers
}

// parseWebsearchTrackers keeps the valid tracker URLs of a list (http, https,
// udp, ws, wss), without duplicates, in their order, up to the cap.
func parseWebsearchTrackers(r io.Reader) []string {
	seen := map[string]bool{}
	var out []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 64<<10)
	for scanner.Scan() && len(out) < websearchTrackerListMax {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed, err := url.Parse(line)
		if err != nil || parsed.Host == "" {
			continue
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "udp", "ws", "wss":
		default:
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// fetchWebsearchTrackerList downloads and parses the list.
func fetchWebsearchTrackerList(ctx context.Context, rawURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, status, err := HTTPGetString(ctx, rawURL, map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	list := parseWebsearchTrackers(strings.NewReader(body))
	if len(list) == 0 {
		return nil, fmt.Errorf("the list holds no tracker")
	}
	return list, nil
}

// loadWebsearchTrackerCache reads the list kept on disk, if any.
func loadWebsearchTrackerCache(dataDir string) []string {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	handle, err := os.Open(filepath.Join(dataDir, websearchTrackerListFile))
	if err != nil {
		return nil
	}
	defer handle.Close()
	return parseWebsearchTrackers(handle)
}

// saveWebsearchTrackerCache writes the list, so a restart starts with it
// instead of the built-in fallback. The file is replaced atomically: a crash
// during the write must not leave half a list behind.
func saveWebsearchTrackerCache(dataDir string, trackers []string) error {
	if strings.TrimSpace(dataDir) == "" || len(trackers) == 0 {
		return nil
	}
	path := filepath.Join(dataDir, websearchTrackerListFile)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(strings.Join(trackers, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// refreshWebsearchTrackers downloads the list once, stores it and saves it on
// disk. It returns how many trackers were taken.
func refreshWebsearchTrackers(ctx context.Context, dataDir, rawURL string) (int, error) {
	list, err := fetchWebsearchTrackerList(ctx, rawURL)
	if err != nil {
		return 0, err
	}
	setWebsearchTrackers(list)
	if saveErr := saveWebsearchTrackerCache(dataDir, list); saveErr != nil {
		logging.Warn("the public tracker list could not be saved, it is used from memory until the next start", "error", saveErr)
	}
	return len(list), nil
}

// StartWebsearchTrackerRefresh loads the list kept on disk and then refreshes it
// in the background: once a day when the download works, after a short wait
// when it does not. It returns the function that stops the loop.
func StartWebsearchTrackerRefresh(dataDir string) func() {
	setWebsearchTrackers(loadWebsearchTrackerCache(dataDir))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			count, err := refreshWebsearchTrackers(ctx, dataDir, websearchTrackerListURL)
			wait := websearchTrackerListEvery
			if err != nil {
				wait = websearchTrackerListRetry
				if ctx.Err() == nil {
					logging.Warn("the public tracker list could not be downloaded; the magnets keep the trackers already known", "error", err)
				}
			} else {
				logging.Debug("public tracker list updated", "trackers", count)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
