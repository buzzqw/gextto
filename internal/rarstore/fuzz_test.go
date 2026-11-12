package rarstore

import (
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// FuzzReadVolume feeds arbitrary bytes to the volume reader: archives come
// from torrents, so a damaged or hostile header must be refused, never crash
// the reader or make it read outside the file.
func FuzzReadVolume(f *testing.F) {
	f.Add(rar4Volume("Episode.mkv", []byte("data"), crc32.ChecksumIEEE([]byte("data")), false, false, rar4MethodStore, false))
	f.Add(rar5Volume("Episode.mkv", []byte("data"), crc32.ChecksumIEEE([]byte("data")), false, true, 0, true))
	f.Add([]byte("Rar!\x1a\x07\x01\x00\xff\xff\xff\xff\xff"))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "v.rar")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		chunks, _, err := readVolume(path)
		if err != nil {
			return
		}
		for _, c := range chunks {
			if c.offset < 0 || c.size < 0 || c.offset+c.size > int64(len(data)) {
				t.Fatalf("chunk outside the file: %+v (size %d)", c, len(data))
			}
		}
	})
}
