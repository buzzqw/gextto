package dht

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
)

// dhtStore persists the node id and the routing table across restarts. It is
// only active with SaveRoutingTable: the engine leaves it off (ephemeral
// nodes), so these paths run solely for standalone users who opt in.
type dhtStore struct {
	// Id is []byte rather than string: the JSON marshaller mangles strings.
	Id      []byte
	Port    int
	Remotes map[string][]byte // Key: IP, Value: node ID.
	path    string            // Empty disables persistence.
}

// mkdirStore ensures the state directory: ~/.taipeitorrent under $HOME,
// /var/run/taipeitorrent without it. The legacy directory name is kept so
// pre-existing state keeps loading.
func mkdirStore() string {
	dir := "/var/run/taipeitorrent"
	for _, e := range os.Environ() {
		if rest, ok := strings.CutPrefix(e, "HOME="); ok {
			dir = path.Join(rest, ".taipeitorrent")
		}
	}
	// Ignore errors.
	_ = os.MkdirAll(dir, 0750)

	if s, err := os.Stat(dir); err != nil {
		log.Fatal("stat config dir", err)
	} else if !s.IsDir() {
		log.Fatalf("Dir %v expected directory, got %v", dir, s)
	}
	return dir
}

// openStore loads the state for port when enabled, tolerating a missing or
// corrupt file with a fresh store.
func openStore(port int, enabled bool) (cfg *dhtStore) {
	// TODO: File locking.
	cfg = &dhtStore{Port: port}
	if !enabled {
		return cfg
	}
	cfg.path = mkdirStore()

	p := fmt.Sprintf("%v-%v", path.Join(cfg.path, "dht"), port)
	f, err := os.Open(p)
	if err != nil {
		return cfg
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(cfg); err != nil {
		log.Println(err)
	}
	return cfg
}

// saveStore atomically replaces the state file via temp-file rename (atomic
// on Linux; best-effort remove-and-retry on Windows). Disabled stores are a
// no-op.
func saveStore(s dhtStore) {
	if s.path == "" {
		return
	}
	tmp, err := os.CreateTemp(s.path, "taipeitorrent")
	if err != nil {
		log.Println("saveStore tempfile:", err)
		return
	}
	err = json.NewEncoder(tmp).Encode(s)
	// Closed before rename: Windows cannot rename open files.
	tmp.Close()
	if err != nil {
		log.Println("saveStore json encoding:", err)
		return
	}

	p := fmt.Sprintf("%v-%v", s.path+"/dht", s.Port)
	if err := os.Rename(tmp.Name(), p); err != nil {
		// Non-atomic fallback (see the Windows note above): unlink, retry,
		// and accept the crash window between the two.
		if err := os.Remove(p); err != nil {
			log.Println("saveStore failed to remove the existing config:", err)
			return
		}
		if err := os.Rename(tmp.Name(), p); err != nil {
			log.Println("saveStore failed to rename file after deleting the original config:", err)
			return
		}
	}
}
