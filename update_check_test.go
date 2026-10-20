package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestUpdateCheckComparesCommitsNotVersionNumbers: "is there a new version?"
// is decided by the commit (and its build time), never by ordering the version
// string, so the numbering change at the rollover cannot hide or invent an
// update.
func TestUpdateCheckComparesCommitsNotVersionNumbers(t *testing.T) {
	manifest := &updateManifest{AppVersion: "1.2.0", Build: 2000, Commit: "bbbb2222",
		Commits: []UpdateCommit{{Sha: "bbbb2222", Subject: "a new build"}}}
	if available, _, _ := updateCompare("aaaa1111", manifest); !available {
		t.Fatal("a newer commit must be an update even when the version string changed scheme")
	}
	if available, _, _ := updateCompare("bbbb2222", manifest); available {
		t.Fatal("the installed commit must not be offered again")
	}
	// The rule follows the commit, not the numbers: a manifest whose version
	// looks lower still carries the newer code.
	manifest.AppVersion, manifest.Build = "1.1.1999", 1999
	if available, _, _ := updateCompare("aaaa1111", manifest); !available {
		t.Fatal("the update check must not order version strings")
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
	for _, want := range []string{`class="v2-update-badge"`, `id="v2-update-panel"`, "1.1.5000", "feat: something new", "cccc111", `value="apply"`, ">Aggiorna</button>", ">Ricontrolla</button>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("maintenance page missing %q", want)
		}
	}
	if strings.Contains(body, "the installed one") {
		t.Fatal("the installed commit is listed as a change")
	}
}

func TestUpdateNotesPopupAfterTheUpdate(t *testing.T) {
	previous := constants.Commit
	constants.Commit = "aaaa3333"
	previousUnit, previousMarker := updatePathUnit, updateMarkerSource
	unit := filepath.Join(t.TempDir(), "gextto-update.path")
	if err := os.WriteFile(unit, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	updatePathUnit = unit
	updateMarkerSource = func() (string, bool) { return "continuous-aaaa3333", true }
	t.Cleanup(func() {
		constants.Commit = previous
		updatePathUnit, updateMarkerSource = previousUnit, previousMarker
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

	// Requesting the update records what it brings; the running (old)
	// version shows no popup.
	if err := updates.RequestUpdate(state.cfg); err != nil {
		t.Fatal(err)
	}
	notesPath := filepath.Join(state.cfg.DataDir, updateNotesFile)
	if _, err := os.Stat(notesPath); err != nil {
		t.Fatalf("no notes written: %v", err)
	}
	_, body := v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	if strings.Contains(body, `id="v2-update-notes"`) {
		t.Fatal("the popup shows before the update")
	}

	// The new version shows them once.
	constants.Commit = "cccc1111"
	_, body = v2Request(t, server, http.MethodGet, "/?view=maintenance", nil)
	for _, want := range []string{`id="v2-update-notes"`, "Gextto aggiornato", "feat: something new", `role="dialog"`, `hx-post="/maintenance/update-notes"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("popup missing %q", want)
		}
	}
	if code, _ := v2Request(t, server, http.MethodPost, "/maintenance/update-notes", url.Values{}); code != http.StatusOK {
		t.Fatalf("dismiss -> %d", code)
	}
	if _, err := os.Stat(notesPath); !os.IsNotExist(err) {
		t.Fatal("the notes survived the dismiss")
	}
	if _, body = v2Request(t, server, http.MethodGet, "/?view=maintenance", nil); strings.Contains(body, `id="v2-update-notes"`) {
		t.Fatal("the popup shows again")
	}

	// Notes of an update that never installed are dropped once stale.
	raw := []byte(`{"from_version":"1.1.1","from_commit":"cccc1111","at":"2020-01-01T00:00:00Z"}`)
	if err := os.WriteFile(notesPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := PendingUpdateNotes(state.cfg); ok {
		t.Fatal("stale notes shown")
	}
	if _, err := os.Stat(notesPath); !os.IsNotExist(err) {
		t.Fatal("stale notes kept")
	}
}
