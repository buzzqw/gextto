package gextto

import (
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
