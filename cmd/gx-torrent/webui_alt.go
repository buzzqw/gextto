package main

// webui_alt.go is the alternative web interface (qBittorrent's alternative
// WebUI): the operator downloads VueTorrent from its GitHub release into the
// data folder and switches the root page to it. The standard interface stays
// always available at /ui (the way back when the alternative is enabled).

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// altWebUISettingKey turns the alternative interface on at the root address.
	altWebUISettingKey = "webui-alternative"
	// altWebUIReleaseAPI is the VueTorrent release endpoint.
	altWebUIReleaseAPI = "https://api.github.com/repos/VueTorrent/VueTorrent/releases/latest"
	// altWebUIAsset is the release asset with the built files.
	altWebUIAsset = "vuetorrent.zip"
	// altWebUIMaxDownload caps the release download.
	altWebUIMaxDownload = 128 << 20
)

// altWebUIRoot is the install root inside the data folder.
func (d *Daemon) altWebUIRoot() string {
	return filepath.Join(d.opts.DataDir, "vuetorrent")
}

// altWebUIPublic is the webroot of the alternative interface (the release zip
// holds "vuetorrent/public").
func (d *Daemon) altWebUIPublic() string {
	return filepath.Join(d.altWebUIRoot(), "public")
}

func (d *Daemon) altWebUIEnabled() bool {
	return d.opts.Settings != nil && d.settingBool(altWebUISettingKey, false)
}

func (d *Daemon) altWebUIAvailable() bool {
	_, err := os.Stat(filepath.Join(d.altWebUIPublic(), "index.html"))
	return err == nil
}

// altWebUIVersion reads the installed VueTorrent version ("" when absent).
func (d *Daemon) altWebUIVersion() string {
	raw, err := os.ReadFile(filepath.Join(d.altWebUIRoot(), "version.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// altWebUIFallback serves the alternative interface for any GET the standard
// mux does not know, without a catch-all pattern (/api/ and /{path...} would
// conflict in ServeMux). /api and /ui always go to the mux: the API keeps
// working and /ui stays the standard page, the way back.
func (d *Daemon) altWebUIFallback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !d.altWebUIEnabled() || !d.altWebUIAvailable() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || p == "/ui" || strings.HasPrefix(p, "/ui/") {
			next.ServeHTTP(w, r)
			return
		}
		// Buffer the answer so a 404 (no standard route) can be replaced by the
		// alternative interface.
		bw := &bufferedWriter{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(bw, r)
		if bw.status == http.StatusNotFound {
			d.handleAltWebUI(w, r)
			return
		}
		for key, values := range bw.header {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}
		w.WriteHeader(bw.status)
		_, _ = w.Write(bw.body.Bytes())
	})
}

// bufferedWriter captures a response so a 404 can be replaced by the alternative
// interface.
type bufferedWriter struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (b *bufferedWriter) Header() http.Header { return b.header }

func (b *bufferedWriter) WriteHeader(code int) {
	if !b.wroteHeader {
		b.status = code
		b.wroteHeader = true
	}
}

func (b *bufferedWriter) Write(p []byte) (int, error) {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
	return b.body.Write(p)
}

// handleAltWebUI serves the alternative interface static files with a
// single-page-app fallback to index.html.
func (d *Daemon) handleAltWebUI(w http.ResponseWriter, r *http.Request) {
	if !d.altWebUIEnabled() || !d.altWebUIAvailable() {
		http.NotFound(w, r)
		return
	}
	// VueTorrent's optional companion backend lives under /backend: a 404 tells
	// it there is none (as on a plain qBittorrent).
	if strings.HasPrefix(r.URL.Path, "/backend/") {
		http.NotFound(w, r)
		return
	}
	root := d.altWebUIPublic()
	rel := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	target := filepath.Join(root, rel)
	// Never leave the webroot.
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		http.ServeFile(w, r, target)
		return
	}
	// A missing file with an extension is a missing asset, not a route.
	if filepath.Ext(rel) != "" {
		http.NotFound(w, r)
		return
	}
	// SPA fallback: an unknown route is served the app shell.
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}

// installAltWebUI downloads the latest VueTorrent release and installs it under
// the data folder, returning the version. It replaces a previous install.
func (d *Daemon) installAltWebUI() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	releaseURL, tag, err := altWebUIReleaseURL(ctx)
	if err != nil {
		return "", err
	}
	data, err := altWebUIDownload(ctx, releaseURL)
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp(d.opts.DataDir, "vuetorrent-install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := altWebUIExtract(data, tmp); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(tmp, "vuetorrent", "public", "index.html")); err != nil {
		return "", errors.New("the release archive does not contain vuetorrent/public/index.html")
	}

	// Swap the install in place: the previous one is kept until the new tree is
	// in position, so a failure never leaves the interface missing.
	root := d.altWebUIRoot()
	old := root + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, old); err != nil {
			return "", err
		}
	}
	if err := os.Rename(filepath.Join(tmp, "vuetorrent"), root); err != nil {
		_ = os.Rename(old, root) // put the previous one back
		return "", err
	}
	_ = os.RemoveAll(old)
	_ = tag
	version := d.altWebUIVersion()
	if version == "" {
		version = "unknown"
	}
	logf("installed VueTorrent %s in %s", version, root)
	return version, nil
}

// altWebUIReleaseURL reads the latest release and returns the asset URL and tag.
func altWebUIReleaseURL(ctx context.Context) (url, tag string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, altWebUIReleaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("the release list returned HTTP %d", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", "", err
	}
	for _, asset := range release.Assets {
		if asset.Name == altWebUIAsset {
			return asset.URL, release.TagName, nil
		}
	}
	return "", "", fmt.Errorf("the latest release has no %s asset", altWebUIAsset)
}

// altWebUIDownload fetches the release asset, size-capped.
func altWebUIDownload(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, altWebUIMaxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > altWebUIMaxDownload {
		return nil, errors.New("the download is too large")
	}
	return data, nil
}

// altWebUIExtract unpacks the release zip into dir, refusing entries that escape
// it (zip-slip).
func altWebUIExtract(data []byte, dir string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range reader.File {
		name := filepath.Join(dir, filepath.Clean("/"+f.Name))
		if name != dir && !strings.HasPrefix(name, dir+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q escapes the destination", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		written, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(written, io.LimitReader(src, 1<<30))
		written.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// handleUIWebUIInstall downloads/updates VueTorrent and returns to Settings.
func (d *Daemon) handleUIWebUIInstall(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	version, err := d.installAltWebUI()
	if err != nil {
		d.uiRedirect(w, r, nil, "", fmt.Errorf("VueTorrent: %w", err))
		return
	}
	if err := d.applyAltWebUICheckbox(r); err != nil {
		d.uiRedirect(w, r, nil, "", fmt.Errorf("VueTorrent: %w", err))
		return
	}
	d.uiRedirect(w, r, nil, "VueTorrent "+version+" installed", nil)
}

// applyAltWebUICheckbox turns the alternative interface on when the install
// submission carried the checked box (the button lives in the settings form),
// so "check + install" enables it in one step, and persists the store: the
// values live in memory until a save, and an unsaved switch is lost on restart.
// An unchecked box is left alone — installing or updating must not disable it.
func (d *Daemon) applyAltWebUICheckbox(r *http.Request) error {
	if !formBool(r, "webui-alternative") {
		return nil
	}
	d.opts.Settings.Set(altWebUISettingKey, "true")
	if err := d.opts.Settings.Save(); err != nil {
		return fmt.Errorf("cannot save the settings: %w", err)
	}
	return nil
}
