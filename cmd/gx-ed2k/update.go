package main

import (
	"context"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/updatecheck"
)

// updateView is the answer of /api/v1/update: the running build, the latest
// standalone gx-ed2k build in the release manifest, and where to download it.
type updateView struct {
	OK         bool   `json:"ok"`
	LocalBuild bool   `json:"local_build,omitempty"`
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Available  bool   `json:"available,omitempty"`
	URL        string `json:"url,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Error      string `json:"error,omitempty"`
}

// handleUpdate checks release.json, which carries gx_ed2k_build, and never
// calls the GitHub API. A local build has no release channel and is not
// offered an update: the checkout may be newer than the published archive.
func (d *Daemon) handleUpdate(w http.ResponseWriter, r *http.Request) {
	current := runtimeVersion()
	channel := strings.TrimSpace(constants.GxEd2kChannel)
	if channel == "" {
		writeJSON(w, http.StatusOK, updateView{OK: true, LocalBuild: true, Current: current})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	manifest, err := updatecheck.Fetch(ctx, &http.Client{Timeout: 20 * time.Second}, updatecheck.AssetURL(channel, updatecheck.ManifestName), "gx-ed2k/"+current)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, updateView{OK: false, Current: current, Channel: channel, Error: err.Error()})
		return
	}
	currentBuild, _ := strconv.ParseInt(constants.GxEd2kBuild, 10, 64)
	writeJSON(w, http.StatusOK, updateViewFor(current, currentBuild, channel, manifest))
}

func updateViewFor(current string, currentBuild int64, channel string, manifest *updatecheck.Manifest) updateView {
	available := manifest != nil && manifest.GxEd2kBuild > currentBuild && manifest.GxEd2kBuild > 0
	view := updateView{OK: true, Current: current, Channel: channel, Available: available}
	if manifest != nil {
		view.Latest = manifest.GxEd2kVersion
	}
	if available {
		arch := "x86_64"
		if runtime.GOARCH == "arm64" {
			arch = "aarch64"
		}
		view.URL = updatecheck.AssetURL(channel, "gx-ed2k-linux-"+arch+".tar.gz")
	}
	return view
}
