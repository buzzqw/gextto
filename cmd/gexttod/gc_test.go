package main

import (
	"os"
	"runtime/debug"
	"testing"
)

func TestApplyGCDefaultRespectsGOGC(t *testing.T) {
	prev := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(prev) })

	t.Setenv("GOGC", "200")
	if applyGCDefault() {
		t.Fatal("GOGC in the environment must win")
	}
	if got := debug.SetGCPercent(100); got != 100 {
		t.Fatalf("GC percent changed to %d with GOGC set", got)
	}
}

func TestApplyGCDefaultSetsPercent(t *testing.T) {
	prev := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(prev) })

	t.Setenv("GOGC", "") // registers the restore of the original value
	os.Unsetenv("GOGC")
	if !applyGCDefault() {
		t.Fatal("GOGC unset: the default must apply")
	}
	if got := debug.SetGCPercent(100); got != gcPercent {
		t.Fatalf("GC percent = %d, want %d", got, gcPercent)
	}
}
