package dht

import (
	"github.com/golang/groupcache/lru"
)

// dhtContact is one peer of an info hash: its compact address, whether the
// last announce said it is still alive, and its BEP 33 seed flag. Keeping the
// flag here avoids the third map the store used to hold per contact.
type dhtContact struct {
	addr  string
	alive bool
	seed  bool
}

// peerContactsSet holds one info hash's contacts in a slice, rotated by an
// index, with one map from address to slice position. A ring node and a second
// map per contact were both allocations the store paid at the default cap.
type peerContactsSet struct {
	contacts []dhtContact
	index    map[string]int
	cursor   int
}

func newPeerContactsSet() *peerContactsSet {
	return &peerContactsSet{index: make(map[string]int)}
}

// markIP updates the seed flag of an announced contact already in the set.
// Short contacts carry no IP; a contact not yet stored gets its flag in put.
func (p *peerContactsSet) markIP(peerContact string, seed bool) {
	if i, ok := p.index[peerContact]; ok {
		p.contacts[i].seed = seed
	}
}

// next returns up to 8 contacts, rotating so consecutive calls spread. Live
// contacts come first; a short second pass fills from the rest.
func (p *peerContactsSet) next() []string {
	n := len(p.contacts)
	if n == 0 {
		return nil
	}
	count := kNodes
	if count > n {
		count = n
	}
	xx := make([]string, 0, count)
	seen := make(map[string]bool, count)
	for pass := 0; pass < 2 && len(xx) < count; pass++ {
		for i := 0; i < n && len(xx) < count; i++ {
			c := p.contacts[(p.cursor+i)%n]
			if seen[c.addr] || (pass == 0 && !c.alive) {
				continue
			}
			seen[c.addr] = true
			xx = append(xx, c.addr)
		}
	}
	p.cursor = (p.cursor + count) % n
	return xx
}

// put stores one contact, with the seed flag already recorded by markIP when
// it was an update. Duplicates report false.
func (p *peerContactsSet) put(peerContact string) bool {
	if len(peerContact) < 6 {
		return false
	}
	if _, ok := p.index[peerContact]; ok {
		return false
	}
	p.index[peerContact] = len(p.contacts)
	p.contacts = append(p.contacts, dhtContact{addr: peerContact, alive: true})
	return true
}

// putSeed stores a contact and its announce flag in one step.
func (p *peerContactsSet) putSeed(peerContact string, seed bool) bool {
	if !p.put(peerContact) {
		p.markIP(peerContact, seed)
		return false
	}
	p.contacts[len(p.contacts)-1].seed = seed
	return true
}

// removeAt deletes a contact by slice position, moving the last one into its
// place and fixing both the index and the rotation point.
func (p *peerContactsSet) removeAt(i int) string {
	last := len(p.contacts) - 1
	removed := p.contacts[i].addr
	delete(p.index, removed)
	if i != last {
		p.contacts[i] = p.contacts[last]
		p.index[p.contacts[i].addr] = i
	}
	p.contacts[last] = dhtContact{}
	p.contacts = p.contacts[:last]
	if len(p.contacts) == 0 {
		p.cursor = 0
	} else if i < p.cursor {
		p.cursor--
	} else if p.cursor >= len(p.contacts) {
		p.cursor = 0
	}
	return removed
}

// drop removes one contact: the named one, else a dead one first, else the
// one at the rotation point. It returns what left.
func (p *peerContactsSet) drop(peerContact string) string {
	if len(p.contacts) == 0 {
		return ""
	}
	if peerContact == "" {
		if c := p.dropDead(); c != "" {
			return c
		}
		peerContact = p.contacts[p.cursor%len(p.contacts)].addr
	}
	if i, ok := p.index[peerContact]; ok {
		return p.removeAt(i)
	}
	return ""
}

// dropDead removes the first dead contact from the rotation point on.
func (p *peerContactsSet) dropDead() string {
	n := len(p.contacts)
	for i := 0; i < n; i++ {
		at := (p.cursor + i) % n
		if !p.contacts[at].alive {
			return p.removeAt(at)
		}
	}
	return ""
}

// kill marks a contact dead without removing it.
func (p *peerContactsSet) kill(peerContact string) {
	if i, ok := p.index[peerContact]; ok {
		p.contacts[i].alive = false
	}
}

// isAlive reports whether the contact is still in the set and alive.
func (p *peerContactsSet) isAlive(peerContact string) bool {
	if i, ok := p.index[peerContact]; ok {
		return p.contacts[i].alive
	}
	return false
}

// isEmpty reports a set with no contacts left.
func (p *peerContactsSet) isEmpty() bool { return len(p.contacts) == 0 }

// Size counts known contacts, dead or alive.
func (p *peerContactsSet) Size() int { return len(p.contacts) }

// Alive counts the live contacts.
func (p *peerContactsSet) Alive() int {
	var ret int
	for _, c := range p.contacts {
		if c.alive {
			ret++
		}
	}
	return ret
}

// seed reports the BEP 33 flag of a contact.
func (p *peerContactsSet) seed(peerContact string) bool {
	if i, ok := p.index[peerContact]; ok {
		return p.contacts[i].seed
	}
	return false
}

// newPeerStore sizes the info-hash LRU and the per-hash contact cap.
func newPeerStore(maxInfoHashes, maxInfoHashPeers int) *peerStore {
	return &peerStore{
		infoHashPeers:        lru.New(maxInfoHashes),
		localActiveDownloads: make(map[InfoHash]int),
		maxInfoHashes:        maxInfoHashes,
		maxInfoHashPeers:     maxInfoHashPeers,
	}
}

// peerStore caches swarm contacts per info hash (LRU) plus our own active
// downloads (info hash to port).
type peerStore struct {
	infoHashPeers        *lru.Cache
	localActiveDownloads map[InfoHash]int
	maxInfoHashes        int
	maxInfoHashPeers     int
}

// get returns the contact set for ih, or nil.
func (h *peerStore) get(ih InfoHash) *peerContactsSet {
	c, ok := h.infoHashPeers.Get(string(ih))
	if !ok {
		return nil
	}
	contacts := c.(*peerContactsSet)
	return contacts
}

// count totals the known peers for ih.
func (h *peerStore) count(ih InfoHash) int {
	peers := h.get(ih)
	if peers == nil {
		return 0
	}
	return peers.Size()
}

// alive totals the live peers for ih.
func (h *peerStore) alive(ih InfoHash) int {
	peers := h.get(ih)
	if peers == nil {
		return 0
	}
	return peers.Alive()
}

// peerContacts returns up to 8 peers for ih; with noseed, seeded IPs drop
// out (BEP 33), unless no IP data exists yet.
func (h *peerStore) peerContacts(ih InfoHash, noseed bool) []string {
	peers := h.get(ih)
	if peers == nil {
		return nil
	}
	list := peers.next()
	if !noseed {
		return list
	}
	kept := list[:0]
	for _, c := range list {
		// A seed is dropped only when we know an IP for the contact: a short
		// contact has no flag and stays, as in the old per-IP map.
		if len(c) >= 6 && peers.seed(c) {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// scrapeFilters builds the BEP 33 peer/seed bloom pair for ih, or nils when
// there is nothing (or no IP data) to report.
func (h *peerStore) scrapeFilters(ih InfoHash) (peersBloom, seedsBloom *scrapeBloom) {
	peers := h.get(ih)
	if peers == nil || len(peers.contacts) == 0 {
		return nil, nil
	}
	peersBloom = new(scrapeBloom)
	seedsBloom = new(scrapeBloom)
	anyIP := false
	for _, c := range peers.contacts {
		if len(c.addr) < 6 {
			continue
		}
		anyIP = true
		ip := c.addr[:len(c.addr)-2]
		if c.seed {
			seedsBloom.insert([]byte(ip))
		} else {
			peersBloom.insert([]byte(ip))
		}
	}
	if !anyIP {
		return nil, nil
	}
	return peersBloom, seedsBloom
}

// addContact stores a peer for ih with its seed flag. Full sets evict
// (dropping dead first) to make room; duplicates and invalid contacts fail.
// A set of unexpected shape resets rather than mixing.
func (h *peerStore) addContact(ih InfoHash, peerContact string, seed bool) bool {
	var peers *peerContactsSet
	p, ok := h.infoHashPeers.Get(string(ih))
	if ok {
		var okType bool
		peers, okType = p.(*peerContactsSet)
		if okType && peers != nil {
			if peers.Size() >= h.maxInfoHashPeers {
				if _, ok := peers.index[peerContact]; ok {
					return false
				}
				if peers.drop("") == "" {
					return false
				}
			}
			h.infoHashPeers.Add(string(ih), peers)
			return peers.putSeed(peerContact, seed)
		}
	}
	peers = newPeerContactsSet()
	h.infoHashPeers.Add(string(ih), peers)
	return peers.putSeed(peerContact, seed)
}

// killContact marks a contact dead in every locally-downloaded swarm.
func (h *peerStore) killContact(peerContact string) {
	if h == nil {
		return
	}
	for ih := range h.localActiveDownloads {
		if p := h.get(ih); p != nil {
			p.kill(peerContact)
		}
	}
}

// addLocalDownload registers our own download of ih on port.
func (h *peerStore) addLocalDownload(ih InfoHash, port int) {
	h.localActiveDownloads[ih] = port
}

// hasLocalDownload returns our download port for ih, if any.
func (h *peerStore) hasLocalDownload(ih InfoHash) (port int) {
	port, _ = h.localActiveDownloads[ih]
	return
}

// removeLocalDownload forgets our download of ih.
func (h *peerStore) removeLocalDownload(ih InfoHash) {
	delete(h.localActiveDownloads, ih)
}
