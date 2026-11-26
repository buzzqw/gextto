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
// on Linux). On Windows, where replacing an existing file can fail, the old
// file is set aside first and put back if the new one cannot take its place,
// so a failed save never loses the previous state. The temporary file never
// outlives a failed save. Disabled stores are a no-op.
func saveStore(s dhtStore) {
	if s.path == "" {
		return
	}
	tmp, err := os.CreateTemp(s.path, "taipeitorrent")
	if err != nil {
		log.Println("saveStore tempfile:", err)
		return
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tmp.Name())
		}
	}()
	err = json.NewEncoder(tmp).Encode(s)
	// Closed before rename: Windows cannot rename open files.
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Println("saveStore json encoding:", err)
		return
	}

	p := fmt.Sprintf("%v-%v", s.path+"/dht", s.Port)
	if err := os.Rename(tmp.Name(), p); err == nil {
		installed = true
		return
	}
	// Fallback for a platform that refuses to replace: move the old state
	// aside, install the new one, and restore the old one on failure.
	aside := p + ".old"
	_ = os.Remove(aside)
	if err := os.Rename(p, aside); err != nil {
		log.Println("saveStore failed to set the existing state aside:", err)
		return
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		log.Println("saveStore failed to install the new state, the previous one is kept:", err)
		_ = os.Rename(aside, p)
		return
	}
	installed = true
	_ = os.Remove(aside)
}
