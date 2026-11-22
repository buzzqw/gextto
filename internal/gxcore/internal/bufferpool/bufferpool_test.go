package bufferpool

import "testing"

// TestGetClearsAndGetUnclearedKeeps pins the contract: Get always hands out
// zeroed bytes, GetUncleared leaves the caller to overwrite them.
func TestGetClearsAndGetUnclearedKeeps(t *testing.T) {
	p := New(16)
	b := p.GetUncleared(16)
	if len(b.Data) != 16 {
		t.Fatalf("GetUncleared length %d, want 16", len(b.Data))
	}
	for i := range b.Data {
		b.Data[i] = 0xff
	}
	b.Release()
	for i := 0; i < 100; i++ {
		got := p.Get(8)
		for _, v := range got.Data {
			if v != 0 {
				t.Fatalf("Get returned a dirty byte %#x", v)
			}
		}
		got.Release()
	}
}

// TestBoundedPoolRetainsAtMostTheCap: a burst of pieces must not leave an
// unbounded number of one-megabyte buffers reachable after the burst.
func TestBoundedPoolRetainsAtMostTheCap(t *testing.T) {
	p := NewBounded(4096, 4)
	// Borrow them all before releasing: a sequential borrow/release reuses the
	// same buffer and proves nothing about the cap.
	bufs := make([]Buffer, 100)
	for i := range bufs {
		bufs[i] = p.GetUncleared(4096)
	}
	for _, b := range bufs {
		b.Release()
	}
	if got := p.Retained(); got != 4 {
		t.Fatalf("retained %d buffers, want the cap of 4", got)
	}
	if got := p.TrimTo(1); got != 3 {
		t.Fatalf("TrimTo dropped %d buffers, want 3", got)
	}
	if got := p.Retained(); got != 1 {
		t.Fatalf("retained after trim = %d, want 1", got)
	}
	// A borrow after a trim still works.
	b := p.Get(16)
	if len(b.Data) != 16 {
		t.Fatalf("borrowed %d bytes, want 16", len(b.Data))
	}
	b.Release()
}
