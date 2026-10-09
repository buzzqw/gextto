package gextto

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
)

func TestChannelFromMarker(t *testing.T) {
	for marker, want := range map[string]string{
		"v1.2.3":                "latest",
		"latest":                "latest",
		"continuous":            "continuous",
		"continuous-a8e74238bb": "continuous",
		"rollback":              "continuous",
		"version":               "continuous",
	} {
		if got := ChannelFromMarker(marker); got != want {
			t.Errorf("ChannelFromMarker(%q) = %q, want %q", marker, got, want)
		}
	}
}

func TestUpdateCompareListsCommitsSinceInstalled(t *testing.T) {
	manifest := &updateManifest{Commit: "cccc1111", Commits: []UpdateCommit{
		{Sha: "cccc1111", Subject: "third"}, {Sha: "bbbb2222", Subject: "second"}, {Sha: "aaaa3333", Subject: "first"},
	}}
	available, changes, complete := updateCompare("aaaa3333", manifest)
	if !available || !complete || len(changes) != 2 || changes[0].Subject != "third" {
		t.Fatalf("older install: available=%v complete=%v changes=%v", available, complete, changes)
	}
	// A short marker hash (continuous-<sha12>) matches the full one.
	if available, _, _ := updateCompare("cccc", manifest); available {
		t.Fatal("same commit reported as an update")
	}
	available, changes, complete = updateCompare("ffff9999", manifest)
	if !available || complete || len(changes) != 3 {
		t.Fatalf("install older than the history: available=%v complete=%v changes=%d", available, complete, len(changes))
	}
	if available, _, _ := updateCompare("", manifest); available {
		t.Fatal("a build without commit must not be offered updates")
	}
}

func TestV2UpdateBadgeAndPanelShowTheNewVersion(t *testing.T) {
	previous := constants.Commit
	constants.Commit = "aaaa3333"
	t.Cleanup(func() {
		constants.Commit = previous
		updates.mu.Lock()
		updates.manifest = nil
		updates.mu.Unlock()
	})
	updates.mu.Lock()
	updates.manifest = &updateManifest{AppVersion: "1.1.5000", Commit: "cccc1111", Commits: []UpdateCommit{
		{Sha: "cccc1111", Date: "2026-10-09T08:00:00+02:00", Subject: "feat: something new"},
		{Sha: "aaaa3333", Subject: "the installed one"},
	}}
	updates.checkedAt = time.Now()
	updates.mu.Unlock()

	state := newTestAppState(t)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// A source checkout (no release marker): no badge, no button.
	code, body := v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance -> %d", code)
	}
	if strings.Contains(body, `class="v2-update-badge"`) || strings.Contains(body, `value="apply"`) {
		t.Fatal("a source build is offered an update")
	}

	// An installed release with the updater unit.
	unit := filepath.Join(t.TempDir(), "gextto-update.path")
	if err := os.WriteFile(unit, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	previousUnit, previousMarker := updatePathUnit, updateMarkerSource
	updatePathUnit = unit
	updateMarkerSource = func() (string, bool) { return "continuous-aaaa3333", true }
	t.Cleanup(func() { updatePathUnit, updateMarkerSource = previousUnit, previousMarker })

	code, body = v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance -> %d", code)
	}
	for _, want := range []string{`class="v2-update-badge"`, `id="v2-update-panel"`, "1.1.5000", "feat: something new", "cccc111", `value="apply"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("maintenance page missing %q", want)
		}
	}
	if strings.Contains(body, "the installed one") {
		t.Fatal("the installed commit is listed as a change")
	}
}
