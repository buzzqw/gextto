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
