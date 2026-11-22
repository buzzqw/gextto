package gextto

// trash_journal.go remembers where a trashed entry came from, so removing it
// can be undone: moving a file to the trash is reversible, and asking for a
// confirmation before a reversible action is only friction. The journal lives
// beside the entries it describes, survives a restart, and loses nothing when
// an entry is deleted (the stale line is simply never shown).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const trashJournalFile = ".gextto-trash-journal.json"

var trashJournalMu sync.Mutex

func trashJournalPath(trash string) string {
	return filepath.Join(filepath.Clean(trash), trashJournalFile)
}

func readTrashJournal(trash string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(trashJournalPath(trash))
	if err != nil || len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]string{}
	}
	return out
}

func writeTrashJournal(trash string, journal map[string]string) error {
	payload, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp := trashJournalPath(trash) + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, trashJournalPath(trash))
}

// recordTrashMove remembers the original path of a trashed entry.
func recordTrashMove(trash, source, target string) {
	trashJournalMu.Lock()
	defer trashJournalMu.Unlock()
	journal := readTrashJournal(trash)
	journal[filepath.Base(target)] = source
	_ = writeTrashJournal(trash, journal)
}

// forgetTrashMove drops the journal line of an entry that was deleted or
// restored.
func forgetTrashMove(trash, name string) {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." {
		return
	}
	trashJournalMu.Lock()
	defer trashJournalMu.Unlock()
	journal := readTrashJournal(trash)
	if _, ok := journal[name]; !ok {
		return
	}
	delete(journal, name)
	_ = writeTrashJournal(trash, journal)
}

// trashOriginalPath returns where a trashed entry came from, or "" when it was
// not recorded (an older entry, or one moved before the journal existed).
func trashOriginalPath(trash, name string) string {
	trashJournalMu.Lock()
	defer trashJournalMu.Unlock()
	return readTrashJournal(trash)[name]
}
