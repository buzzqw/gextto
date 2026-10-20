package partfile

import (
	"bytes"
	"crypto/sha1"
	"testing"
)

func testAICHData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*17 + 11)
	}
	return data
}

func TestAICHTreeRecoveryRoundTrip(t *testing.T) {
	for _, size := range []int{500_000, int(PartSize) + 300_000} {
		data := testAICHData(size)
		tree, err := BuildAICHTreeReader(bytes.NewReader(data), int64(size))
		if err != nil {
			t.Fatal(err)
		}
		root, err := AICHRootReader(bytes.NewReader(data), int64(size))
		if err != nil {
			t.Fatal(err)
		}
		if tree.RootHash() != root {
			t.Fatalf("size %d: tree root %x != reader root %x", size, tree.RootHash(), root)
		}

		for part := 0; part < PartCount(int64(size)); part++ {
			recovery, err := tree.RecoveryData(part)
			if err != nil {
				t.Fatalf("size %d part %d recovery: %v", size, part, err)
			}
			recovered, err := NewAICHTree(int64(size), root)
			if err != nil {
				t.Fatal(err)
			}
			if err := recovered.ReadRecoveryData(part, recovery); err != nil {
				t.Fatalf("size %d part %d read recovery: %v", size, part, err)
			}
			hashes, err := recovered.PartHashes(part)
			if err != nil {
				t.Fatalf("size %d part %d part hashes: %v", size, part, err)
			}
			start := int64(part) * PartSize
			length := PartLength(int64(size), part)
			for block := int64(0); block < length; block += AICHBlockSize {
				end := block + AICHBlockSize
				if end > length {
					end = length
				}
				want := sha1.Sum(data[start+block : start+end])
				if hashes[block/AICHBlockSize] != want {
					t.Fatalf("size %d part %d block %d hash mismatch", size, part, block/AICHBlockSize)
				}
			}
		}
	}
}

func TestAICHTreeRejectsTamperedRecovery(t *testing.T) {
	data := testAICHData(500_000)
	tree, err := BuildAICHTreeReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := tree.RecoveryData(0)
	if err != nil {
		t.Fatal(err)
	}
	recovery[3] ^= 0xFF
	recovered, err := NewAICHTree(int64(len(data)), tree.RootHash())
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.ReadRecoveryData(0, recovery); err == nil {
		t.Fatal("tampered recovery data was accepted")
	}
}
