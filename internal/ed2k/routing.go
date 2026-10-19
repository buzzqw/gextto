package ed2k

import (
	"sort"
	"sync"
)

// routingTable is the Kad contact set. Kad uses 128 k-buckets; for a search
// client the useful operations are "add/update a contact" and "the N contacts
// closest to a target", so the table is kept as a bounded map sorted on
// demand. The bound is deliberately small: a search client does not need the
// whole network, and a smaller table is cheaper to keep fresh.
type routingTable struct {
	mu       sync.RWMutex
	contacts map[ID]Contact
	limit    int
}

func newRoutingTable(limit int) *routingTable {
	if limit <= 0 {
		limit = 256
	}
	return &routingTable{contacts: map[ID]Contact{}, limit: limit}
}

// add inserts or refreshes a contact. Contacts with an invalid address or an
// ancient protocol version are ignored. When the table is full, the contact
// farthest from our own ID is evicted.
func (t *routingTable) add(contact Contact, self ID) bool {
	if !contact.valid() || contact.ID == self {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if existing, ok := t.contacts[contact.ID]; ok {
		// Keep the freshest address and the highest protocol version.
		if contact.Version >= existing.Version {
			t.contacts[contact.ID] = contact
		}
		return false
	}
	if len(t.contacts) >= t.limit {
		var farthest ID
		found := false
		for id := range t.contacts {
			if !found || Less(Distance(self, farthest), Distance(self, id)) {
				farthest = id
				found = true
			}
		}
		if found {
			delete(t.contacts, farthest)
		}
	}
	t.contacts[contact.ID] = contact
	return true
}

func (t *routingTable) remove(id ID) {
	t.mu.Lock()
	delete(t.contacts, id)
	t.mu.Unlock()
}

func (t *routingTable) len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.contacts)
}

// closest returns up to count contacts ordered by XOR distance from target.
func (t *routingTable) closest(target ID, count int) []Contact {
	t.mu.RLock()
	out := make([]Contact, 0, len(t.contacts))
	for _, contact := range t.contacts {
		out = append(out, contact)
	}
	t.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return Less(Distance(target, out[i].ID), Distance(target, out[j].ID))
	})
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out
}

// all returns a snapshot of every contact.
func (t *routingTable) all() []Contact {
	return t.closest(ID{}, 0)
}
