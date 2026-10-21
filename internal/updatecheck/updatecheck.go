// Package updatecheck reads the release manifest (release.json) that CI
// publishes next to the archives and tells whether a newer build exists and
// which commits it brings. It is shared by the gx-torrent daemon's in-app
// update; it never calls the GitHub API (no rate limits, no token).
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Repo is the GitHub repository the releases are published in.
const Repo = "buzzqw/gextto"

// ManifestName is the manifest asset of a release.
const ManifestName = "release.json"

// Commit is one changelog entry of the manifest. Gx is true when the commit
// touches the gx-torrent daemon's code (scripts/release-manifest.sh).
type Commit struct {
	Sha     string `json:"sha"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	Gx      bool   `json:"gx"`
}

// Short is the abbreviated hash.
func (c Commit) Short() string {
	if len(c.Sha) > 7 {
		return c.Sha[:7]
	}
	return c.Sha
}

// Day is the commit date (YYYY-MM-DD).
func (c Commit) Day() string {
	if len(c.Date) >= 10 {
		return c.Date[:10]
	}
	return c.Date
}

// URL links the commit on GitHub.
func (c Commit) URL() string { return "https://github.com/" + Repo + "/commit/" + c.Sha }

// Manifest is release.json.
type Manifest struct {
	Channel    string `json:"channel"`
	Label      string `json:"label"`
	AppVersion string `json:"app_version"`
	Build      int64  `json:"build"`
	Commit     string `json:"commit"`
	BuiltAt    string `json:"built_at"`
	// GxCommit is the newest commit that changed gx-torrent's code.
	GxCommit string `json:"gx_torrent_commit"`
	// GxVersion is gx-core's own version ("<series>.<gx build>") in the
	// release;
	// empty in manifests published before it was added.
	GxVersion string `json:"gx_version,omitempty"`
	// GxBuild is gx-core's own build number in the release (0 when the
	// manifest does not carry it).
	GxBuild int64    `json:"gx_build,omitempty"`
	Commits []Commit `json:"commits"`
}

// AssetURL is the download URL of a release asset on a channel: "stable"
// (the latest tagged release) or a named release such as "continuous".
func AssetURL(channel, asset string) string {
	if channel == "" || channel == "stable" || channel == "latest" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", Repo, asset)
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repo, channel, asset)
}

// Fetch downloads and decodes a manifest.
func Fetch(ctx context.Context, client *http.Client, url, userAgent string) (*Manifest, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("this release does not publish update information yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release information: HTTP %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("release information: %w", err)
	}
	if m.Commit == "" {
		return nil, errors.New("release information without a commit")
	}
	return &m, nil
}

// sameCommit compares a full and a possibly abbreviated hash.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// Changes compares the running commit with the manifest. The manifest lists
// the newest commits first, so the ones before the running commit are what an
// update brings. With gxOnly only the commits that changed gx-torrent count:
// a release whose new commits touch only Gextto is no update for the daemon.
// complete is false when the running commit is older than the listed
// history (the list is then partial). Without a running commit (a local
// build) nothing is offered.
func Changes(installed string, m *Manifest, gxOnly bool) (available bool, changes []Commit, complete bool) {
	if installed == "" || m == nil || m.Commit == "" {
		return false, nil, false
	}
	if sameCommit(m.Commit, installed) {
		return false, nil, true
	}
	for _, c := range m.Commits {
		if sameCommit(c.Sha, installed) {
			return len(changes) > 0, changes, true
		}
		if !gxOnly || c.Gx {
			changes = append(changes, c)
		}
	}
	// The running commit is older than the listed history: newer anyway.
	return true, changes, false
}
