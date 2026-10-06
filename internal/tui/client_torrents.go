package tui

import (
	"context"
	"net/url"
	"strconv"
)

// HistoryItem is one finished download of GET /api/torrents/history.
type HistoryItem struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Tag           string `json:"tag"`
	Status        string `json:"status"`
	QualityScore  int64  `json:"quality_score"`
	CompletedAt   string `json:"completed_at"`
	ProcessedPath string `json:"processed_path"`
	Error         string `json:"error"`
}

// TrackerEntry is one tracker of a torrent.
type TrackerEntry struct {
	Tier int32  `json:"tier"`
	URL  string `json:"url"`
}

func torrentPath(hash string) string { return "/api/torrents/" + url.PathEscape(hash) }

// TorrentTags maps torrent hashes to their download tag.
func (c *Client) TorrentTags(ctx context.Context) (map[string]string, error) {
	var response struct {
		Items []struct {
			Hash string `json:"hash"`
			Tag  string `json:"tag"`
		} `json:"items"`
	}
	err := c.get(ctx, "/api/torrent-tags", &response)
	tags := make(map[string]string, len(response.Items))
	for _, item := range response.Items {
		if item.Tag != "" {
			tags[item.Hash] = item.Tag
		}
	}
	return tags, err
}

// DownloadTags lists the tag catalog.
func (c *Client) DownloadTags(ctx context.Context) ([]string, error) {
	var response struct {
		Items []string `json:"items"`
	}
	err := c.get(ctx, "/api/download-tags", &response)
	return response.Items, err
}

// SetTorrentTag assigns a tag to a torrent; an empty tag removes it. A new
// tag is added to the catalog first, as the web interface does.
func (c *Client) SetTorrentTag(ctx context.Context, hash, tag string, isNew bool) error {
	if isNew && tag != "" {
		if err := c.postJSON(ctx, "/api/download-tags", map[string]any{"tag": tag}, nil); err != nil {
			return err
		}
	}
	return c.postJSON(ctx, "/api/torrent-tags", map[string]any{"hash": hash, "tag": tag}, nil)
}

// History fetches the finished downloads, newest first.
func (c *Client) History(ctx context.Context, query string) ([]HistoryItem, int, error) {
	var response struct {
		Items []HistoryItem `json:"items"`
		Total int           `json:"total"`
	}
	values := url.Values{"limit": {"200"}, "page": {"1"}}
	if query != "" {
		values.Set("q", query)
	}
	err := c.get(ctx, "/api/torrents/history?"+values.Encode(), &response)
	return response.Items, response.Total, err
}

// SetTorrentLimits sets per-torrent rates in bytes/s (-1 = unlimited) and,
// when given, the seed ratio and days.
func (c *Client) SetTorrentLimits(ctx context.Context, hash string, downloadBytes, uploadBytes int64, ratio *float64, days *int64) error {
	body := map[string]any{"download_limit": downloadBytes, "upload_limit": uploadBytes}
	if ratio != nil {
		body["seed_ratio"] = *ratio
	}
	if days != nil {
		body["seed_days"] = *days
	}
	return c.postJSON(ctx, torrentPath(hash)+"/limits", body, nil)
}

// MoveStorage moves a torrent's data to another folder.
func (c *Client) MoveStorage(ctx context.Context, hash, path string) error {
	return c.postJSON(ctx, torrentPath(hash)+"/storage", map[string]any{"path": path}, nil)
}

// MarkFailed blocklists the release and removes the torrent.
func (c *Client) MarkFailed(ctx context.Context, hash string) error {
	return c.postJSON(ctx, torrentPath(hash)+"/mark_failed", map[string]any{}, nil)
}

// SetSuperSeeding turns super-seeding on or off.
func (c *Client) SetSuperSeeding(ctx context.Context, hash string, enabled bool) error {
	return c.postJSON(ctx, torrentPath(hash)+"/super-seeding", map[string]any{"enabled": enabled}, nil)
}

// SetFilePriorities sets the priority (0 skip … 7 top) of every file.
func (c *Client) SetFilePriorities(ctx context.Context, hash string, priorities []int32) error {
	return c.postJSON(ctx, torrentPath(hash)+"/files/priority", map[string]any{"priorities": priorities}, nil)
}

// SetTrackers replaces the tracker list of a torrent.
func (c *Client) SetTrackers(ctx context.Context, hash string, trackers []TrackerEntry) error {
	return c.postJSON(ctx, torrentPath(hash)+"/trackers", map[string]any{"trackers": trackers}, nil)
}

// kibToBytes converts a KiB/s limit to the bytes/s the engine expects, with
// 0 meaning unlimited (-1).
func kibToBytes(kib int64) int64 {
	if kib <= 0 {
		return -1
	}
	return kib * 1024
}

// bytesToKib shows an engine limit in KiB/s, with unlimited as 0.
func bytesToKib(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}
	return strconv.FormatInt(bytes/1024, 10)
}
