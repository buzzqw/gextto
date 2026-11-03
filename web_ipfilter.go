package gextto

// web_ipfilter.go fetches an IP filter list (plain, gzip or zip) and loads it
// into the active torrent engine.

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fetchIpfilter downloads an IP filter list, transparently decoding gzip
// and zip payloads.
func fetchIpfilter(ctx context.Context, rawURL string) ([]byte, string) {
	resp, err := HTTPGet(ctx, rawURL, map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
	})
	if err != nil {
		return nil, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	data, err := readLimitedBody(resp.Body, maxFeedResponseBytes)
	if err != nil {
		return nil, err.Error()
	}
	lower := strings.ToLower(strings.SplitN(rawURL, "?", 2)[0])
	if strings.HasSuffix(lower, ".gz") || (len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b) {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err.Error()
		}
		defer reader.Close()
		text, err := readLimitedBody(reader, maxDecompressedBytes)
		if err != nil {
			return nil, err.Error()
		}
		return text, ""
	}
	if strings.HasSuffix(lower, ".zip") || (len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04) {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err.Error()
		}
		for _, file := range reader.File {
			name := strings.ToLower(file.Name)
			if strings.HasSuffix(name, ".p2p") || strings.HasSuffix(name, ".dat") ||
				strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".list") {
				handle, err := file.Open()
				if err != nil {
					return nil, err.Error()
				}
				text, err := readLimitedBody(handle, maxDecompressedBytes)
				handle.Close()
				if err != nil {
					return nil, err.Error()
				}
				return text, ""
			}
		}
		return nil, "archivio zip senza lista riconosciuta"
	}
	preview := data
	if len(preview) > 300 {
		preview = preview[:300]
	}
	previewText := strings.ToLower(string(preview))
	if strings.Contains(previewText, "<html") || strings.Contains(previewText, "<body") {
		return nil, "l'URL ha restituito una pagina web invece della lista"
	}
	return data, ""
}

// applyIPFilter downloads (when the configured value is a URL) and loads the IP
// filter into the active engine, returning the number of rules.
func applyIPFilter(ctx context.Context, s *AppState) (int, string, error) {
	var loader interface {
		LoadIPFilter(path string) (int, error)
	}
	active, ok := s.activeEngine().(interface {
		LoadIPFilter(path string) (int, error)
	})
	if !ok {
		return 0, "", fmt.Errorf("the active engine does not support the IP filter")
	}
	loader = active
	cfg := latestConfig(s)
	target := strings.TrimSpace(cfg.Transfer.IpFilterPath)
	if target == "" {
		return 0, "", fmt.Errorf("no IP filter configured")
	}
	localPath := target
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		data, fetchErr := fetchIpfilter(ctx, target)
		if fetchErr != "" {
			return 0, "", fmt.Errorf("%s", fetchErr)
		}
		path := filepath.Join(cfg.DataDir, "ipfilter.dat")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return 0, "", err
		}
		localPath = path
	}
	rules, err := loader.LoadIPFilter(localPath)
	if err != nil {
		return 0, "", err
	}
	return rules, localPath, nil
}
