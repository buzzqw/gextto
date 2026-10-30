package filestorage

import (
	"os"
	"runtime"
	"testing"
)

// TestWithoutNoAtime pins the fallback that lets a torrent open data the process
// does not own: O_NOATIME is refused with EPERM to anyone but the file's owner
// (or CAP_FOWNER), so the open is retried without it.
func TestWithoutNoAtime(t *testing.T) {
	flags := applyNoAtimeFlag(os.O_RDWR)
	plain, dropped := withoutNoAtime(flags)
	if runtime.GOOS != "linux" {
		if dropped {
			t.Fatal("a platform without O_NOATIME reported a flag to drop")
		}
		return
	}
	if !dropped {
		t.Fatal("O_NOATIME was not recognised")
	}
	if plain != os.O_RDWR {
		t.Fatalf("flags after dropping O_NOATIME = %#x, want %#x", plain, os.O_RDWR)
	}
	if _, dropped := withoutNoAtime(os.O_RDWR); dropped {
		t.Fatal("a flag set without O_NOATIME reported a drop")
	}
}
