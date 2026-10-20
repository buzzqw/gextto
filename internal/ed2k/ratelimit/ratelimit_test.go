package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestNilBucketAllows(t *testing.T) {
	var bucket *Bucket
	if err := bucket.Wait(context.Background(), 1<<20); err != nil {
		t.Fatal(err)
	}
}

func TestWaitEnforcesRate(t *testing.T) {
	bucket := New(100_000) // 100 kB/s, one-second burst
	start := time.Now()
	if err := bucket.Wait(context.Background(), 100_000); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("burst should be immediate, took %v", elapsed)
	}
	start = time.Now()
	if err := bucket.Wait(context.Background(), 50_000); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Fatalf("second wait too short: %v", elapsed)
	}
}

func TestWaitHonoursCancel(t *testing.T) {
	bucket := New(1_000)
	if err := bucket.Wait(context.Background(), 1_000); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := bucket.Wait(ctx, 10_000); err == nil {
		t.Fatal("wait did not respect the cancelled context")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancelled wait took %v", elapsed)
	}
}
