package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/updatecheck"
)

const (
	updateArchiveLimit = 128 << 20
	updateBinaryLimit  = 128 << 20
)

// handleUpdateApply downloads the newer standalone archive, verifies its
// published SHA-256, checks that the extracted binary reports the expected
// version, replaces the executable and execs it. Windows is excluded: a
// running .exe cannot be replaced; the page keeps the download link there.
func (d *Daemon) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	current := runtimeVersion()
	channel := strings.TrimSpace(constants.GxEd2kChannel)
	if channel == "" {
		writeJSON(w, http.StatusConflict, updateView{OK: false, Current: current, Error: "local build: update from the source code"})
		return
	}
	if runtime.GOOS == "windows" {
		writeJSON(w, http.StatusConflict, updateView{OK: false, Current: current, Channel: channel, Error: "automatic update is not supported on Windows: install the zip package"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Minute}
	manifest, err := updatecheck.Fetch(ctx, client, updatecheck.AssetURL(channel, updatecheck.ManifestName), "gx-ed2k/"+current)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	currentBuild, _ := strconv.ParseInt(constants.GxEd2kBuild, 10, 64)
	if manifest.GxEd2kBuild <= currentBuild || manifest.GxEd2kBuild == 0 {
		writeJSON(w, http.StatusConflict, updateView{OK: false, Current: current, Channel: channel, Error: "already up to date"})
		return
	}

	archiveURL := updatecheck.AssetURL(channel, updateAssetName())
	archive, err := downloadUpdate(ctx, client, archiveURL, updateArchiveLimit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	checksumRaw, err := downloadUpdate(ctx, client, archiveURL+".sha256", 1<<20)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	want, err := parseSHA256(checksumRaw)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: "downloaded archive does not match its checksum"})
		return
	}
	binary, err := extractTarGz(archive, "gx-ed2k", updateBinaryLimit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".gx-ed2k-update-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	if err := tmp.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	// A corrupt or wrong-architecture binary must fail here, before the old
	// executable is touched.
	if out, err := exec.Command(tmpName, "--version").CombinedOutput(); err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: fmt.Sprintf("the new binary does not run: %v: %s", err, strings.TrimSpace(string(out)))})
		return
	} else if !strings.Contains(string(out), manifest.GxEd2kVersion) {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: fmt.Sprintf("the new binary reports %q, expected %s", strings.TrimSpace(string(out)), manifest.GxEd2kVersion)})
		return
	}
	if err := copyFile(exe, exe+".old"); err != nil {
		log.Printf("gx-ed2k: update backup: %v", err)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		writeJSON(w, http.StatusInternalServerError, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updateView{OK: true, Current: current, Latest: manifest.GxEd2kVersion, Channel: channel, Available: true})
	go restartProcess(exe)
}

func downloadUpdate(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download is larger than the allowed limit")
	}
	return data, nil
}

func parseSHA256(raw []byte) (string, error) {
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("malformed sha256 file")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("malformed sha256 file")
	}
	return strings.ToLower(fields[0]), nil
}

// extractTarGz returns one regular file from the archive. Only the exact
// cleaned name is accepted, so a traversal entry can never be extracted.
func extractTarGz(data []byte, name string, limit int64) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || path.Clean(header.Name) != name {
			continue
		}
		if header.Size > limit {
			return nil, errors.New("binary in the archive is larger than the allowed limit")
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, limit))
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}
		return binary, nil
	}
	return nil, fmt.Errorf("archive does not contain %s", name)
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}
