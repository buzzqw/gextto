package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// indexarrConfirmEvent is the event name of the confirmation contract. indexarr
// records a grab when it hands a link over; this event is the client's word
// that the file really arrived.
const indexarrConfirmEvent = "download.completed"

// indexarrConfirmTimeout bounds one confirmation. It runs off the download
// path, so a slow or stopped indexarr cannot delay a torrent or an eD2k file.
const indexarrConfirmTimeout = 4 * time.Second

// indexarrConfirmTarget is one indexarr manager that receives completions.
type indexarrConfirmTarget struct {
	URL    string
	APIKey string
}

// indexarrConfirmation is the JSON body of POST /api/v2/downloads/confirm.
type indexarrConfirmation struct {
	Event       string `json:"event"`
	Protocol    string `json:"protocol"`
	Hash        string `json:"hash"`
	Title       string `json:"title,omitempty"`
	Client      string `json:"client"`
	Size        int64  `json:"size,omitempty"`
	CompletedAt string `json:"completed_at"`
}

// indexarrConfirmed keeps one completion per download for this process: a
// torrent_finished followed by a storage_moved must not report it twice. A
// failed report is removed so a later event can try again.
var indexarrConfirmed sync.Map

// indexarrConfirmTargets returns the enabled indexarr managers Gextto can tell
// about a finished download. Duplicates (two entries pointing at the same
// instance with the same key) are collapsed.
func indexarrConfirmTargets(cfg *Config) []indexarrConfirmTarget {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	targets := make([]indexarrConfirmTarget, 0, 1)
	for _, indexer := range cfg.Indexers {
		if !indexer.Enabled {
			continue
		}
		if managerKind(indexer) != ManagerIndexarr {
			continue
		}
		key := strings.TrimSpace(indexer.APIKey)
		endpoint := indexarrConfirmURL(indexer.URL)
		if key == "" || endpoint == "" {
			continue
		}
		dedup := endpoint + "\x00" + key
		if _, ok := seen[dedup]; ok {
			continue
		}
		seen[dedup] = struct{}{}
		targets = append(targets, indexarrConfirmTarget{URL: endpoint, APIKey: key})
	}
	return targets
}

// indexarrConfirmURL turns a configured manager address into the confirmation
// endpoint. The list accepts the base URL or a complete Torznab path, so the
// same address that searches also works here.
func indexarrConfirmURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	path := strings.TrimRight(parsed.Path, "/")
	lower := strings.ToLower(path)
	for _, suffix := range []string{
		"/api/v2.0/indexers/all/results/torznab/api",
		"/api/v1/search",
		"/torznab/api",
		"/newznab/api",
		"/api",
		"/torznab",
	} {
		if strings.HasSuffix(lower, suffix) {
			path = strings.TrimRight(path[:len(path)-len(suffix)], "/")
			break
		}
	}
	parsed.Path = path + "/api/v2/downloads/confirm"
	return parsed.String()
}

// indexarrProtocol names the network of a completed download. Gextto sets
// Network on every release; the hash length is the fallback for an older row
// or a foreign torrent.
func indexarrProtocol(network, hash string) string {
	if strings.EqualFold(strings.TrimSpace(network), "ed2k") {
		return "ed2k"
	}
	if len(strings.TrimSpace(hash)) == 32 {
		return "ed2k"
	}
	return "torrent"
}

// indexarrCompletionTitle picks the title indexarr served, so a confirmation
// whose hash indexarr did not know can still find the grab.
func indexarrCompletionTitle(title, fallback string) string {
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

// notifyIndexarrCompletion reports a finished download to every configured
// indexarr manager. It never blocks the caller and never fails a download: the
// report is best effort, with one retry after a short pause.
func notifyIndexarrCompletion(cfg *Config, hash, protocol, title string, size int64, completedAt time.Time) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return
	}
	targets := indexarrConfirmTargets(cfg)
	if len(targets) == 0 {
		return
	}
	key := protocol + ":" + hash
	if _, loaded := indexarrConfirmed.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	payload := indexarrConfirmation{
		Event:       indexarrConfirmEvent,
		Protocol:    protocol,
		Hash:        hash,
		Title:       strings.TrimSpace(title),
		Client:      "gextto",
		Size:        size,
		CompletedAt: completedAt.UTC().Format(time.RFC3339),
	}
	go func() {
		defer recoverGoroutine("indexarr download confirmation")
		sent := false
		for attempt := 0; attempt < 2 && !sent; attempt++ {
			sent = true
			for _, target := range targets {
				ctx, cancel := context.WithTimeout(context.Background(), indexarrConfirmTimeout)
				err := sendIndexarrConfirmation(ctx, http.DefaultClient, target, payload)
				cancel()
				if err != nil {
					sent = false
					logging.Debug("indexarr download confirmation failed",
						"host", indexarrTargetHost(target.URL), "protocol", protocol, "error", err)
				}
			}
			if !sent && attempt == 0 {
				time.Sleep(2 * time.Second)
			}
		}
		if !sent {
			// Let a later completion event try again: indexarr is idempotent.
			indexarrConfirmed.Delete(key)
		}
	}()
}

// sendIndexarrConfirmation posts one confirmation. It is separate from the
// asynchronous wrapper so a test can drive it directly.
func sendIndexarrConfirmation(ctx context.Context, client *http.Client, target indexarrConfirmTarget, payload indexarrConfirmation) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", target.APIKey)
	request.Header.Set("User-Agent", "gextto")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &indexarrConfirmStatusError{Status: response.StatusCode}
	}
	return nil
}

// indexarrTargetHost is the host of a confirmation target, for a log line that
// never prints the API key.
func indexarrTargetHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// indexarrConfirmStatusError is an HTTP status from indexarr, without the body
// (the key is in a header and must never end up in a message).
type indexarrConfirmStatusError struct {
	Status int
}

func (e *indexarrConfirmStatusError) Error() string {
	return fmt.Sprintf("indexarr answered HTTP %d", e.Status)
}
