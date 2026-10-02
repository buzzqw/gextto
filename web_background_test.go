package gextto

import "testing"

func TestCompletedArchivePresentAcceptsPersistedFullPayload(t *testing.T) {
	db, _, view, _, processed := seedTestSetup(t)
	path, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize)
	if !present || path != processed {
		t.Fatalf("archive payload = %q, %v; want %q, true", path, present, processed)
	}
}

func TestCompletedArchivePresentRejectsInsufficientPayload(t *testing.T) {
	db, _, view, _, _ := seedTestSetup(t)
	if _, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize+1); present {
		t.Fatal("undersized archive payload was accepted")
	}
}
