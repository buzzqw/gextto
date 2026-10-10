// Package settings is a small persistent key/value store for gx-torrent's
// standalone mode: the operators' choices (language, folders, credentials)
// survive a restart. In managed mode Gextto drives the daemon through flags,
// which always win, so this file is never consulted and the fingerprint stays
// stable.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store is a JSON map of string settings, safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	data map[string]string
}

// Load reads the store at path. A missing file is not an error: it returns an
// empty store, so the first standalone run starts from defaults.
func Load(path string) (*Store, error) {
	store := &Store{path: path, data: map[string]string{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, fmt.Errorf("settings: %w", err)
	}
	if len(raw) == 0 {
		return store, nil
	}
	decoded := map[string]string{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	if decoded != nil {
		store.data = decoded
	}
	return store, nil
}

// Path is where the store is (or would be) written.
func (s *Store) Path() string { return s.path }

// Get returns the value for key, or fallback when it is unset or empty.
func (s *Store) Get(key, fallback string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value, ok := s.data[key]; ok && value != "" {
		return value
	}
	return fallback
}

// Set stores a value in memory; call Save to persist it.
func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
}

// Keys returns the stored keys, sorted, for tests and diagnostics.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Save writes the store atomically: a temporary file in the same directory is
// renamed over the target, so a crash never leaves a half-written settings
// file and a reader never sees a partial one.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("settings: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	return nil
}
