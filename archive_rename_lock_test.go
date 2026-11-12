package gextto

import (
	"strings"
	"testing"
	"time"
)

func TestArchiveImportCountsOverlappingImports(t *testing.T) {
	name := "Lock Test Overlap"
	first := AcquireArchiveImport(&name)
	second := AcquireArchiveImport(&name)
	first.Release()
	if !ArchiveImportBusyContains(name) {
		t.Fatal("second import still running: the series must stay busy")
	}
	second.Release()
	if ArchiveImportBusyContains(name) {
		t.Fatal("all imports released: the series must be free")
	}
}

func TestRenameSkipsDuringImport(t *testing.T) {
	name := "Lock Test Import"
	guard := AcquireArchiveImport(&name)
	if _, _, ok := TryAcquireArchiveRename(name); ok {
		t.Fatal("rename must be refused while an import runs")
	}
	guard.Release()
	release, _, ok := TryAcquireArchiveRename(name)
	if !ok {
		t.Fatal("rename must be allowed once the import is over")
	}
	if _, _, again := TryAcquireArchiveRename(name); again {
		t.Fatal("a second rename of the same series must be refused")
	}
	release()
	release() // idempotent
}

func TestImportWaitsForRename(t *testing.T) {
	name := "Lock Test Wait"
	release, _, ok := TryAcquireArchiveRename(name)
	if !ok {
		t.Fatal("rename reservation failed")
	}
	acquired := make(chan *ArchiveImportGuard)
	go func() { acquired <- AcquireArchiveImport(&name) }()
	select {
	case <-acquired:
		t.Fatal("import must wait for the running rename")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case guard := <-acquired:
		guard.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("import not resumed after the rename finished")
	}
}

// TestRenameSkipNamesTheOperationHoldingTheArchive is the regression for the
// library check that logged "a download of this series is being copied into the
// library" while the night audio cleanup held the archive: the guard knew only
// that something was running, so the sentence named the wrong operation and
// people looked for a copy that never existed.
func TestRenameSkipNamesTheOperationHoldingTheArchive(t *testing.T) {
	name := "Lock Test Cleanup"
	cleanup := AcquireArchiveImportFor(&name, archiveHoldAudioCleanup)
	if _, reason, ok := TryAcquireArchiveRename(name); ok || reason != archiveHoldAudioCleanup {
		t.Fatalf("reason = %q, ok = %v: the audio cleanup must be named", reason, ok)
	}
	if text := archiveRenameBusyText(archiveHoldAudioCleanup); !strings.Contains(text, "audio cleanup") {
		t.Fatalf("sentence = %q: it must name the audio cleanup", text)
	}

	// A copy and the audio cleanup may overlap: the copy is the operation worth
	// naming, and releasing it must leave the cleanup holding the archive.
	copyGuard := AcquireArchiveImport(&name)
	if _, reason, ok := TryAcquireArchiveRename(name); ok || reason != archiveHoldCopy {
		t.Fatalf("reason = %q, ok = %v: a copy must win over the audio cleanup", reason, ok)
	}
	copyGuard.Release()
	if !ArchiveImportBusyContains(name) {
		t.Fatal("the audio cleanup must still hold the archive")
	}
	if _, reason, ok := TryAcquireArchiveRename(name); ok || reason != archiveHoldAudioCleanup {
		t.Fatalf("reason = %q, ok = %v after the copy ended", reason, ok)
	}

	cleanup.Release()
	if ArchiveImportBusyContains(name) {
		t.Fatal("both operations released: the archive must be free")
	}
	releaseRename, reason, ok := TryAcquireArchiveRename(name)
	if !ok || reason != "" {
		t.Fatalf("a free archive must accept a rename: reason = %q, ok = %v", reason, ok)
	}
	releaseRename()
}
