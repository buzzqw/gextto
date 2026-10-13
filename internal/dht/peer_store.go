package dht

import (
	"container/ring"

	"github.com/golang/groupcache/lru"
)

// peerContactsSet holds one info hash's contacts: the set for membership,
// a ring for rotating replies, and per-IP seed flags (BEP 33) for noseed
// filtering and scrape blooms.
type peerContactsSet struct {
	set map[string]bool
	// ring cycles so repeated queries spread across contacts.
	ring *ring.Ring
	// ips maps raw IP (contact minus the 2-byte port) to seed status; the
	// latest announce wins.
	ips map[string]bool
}

// markIP records a contact's seed flag. Short contacts carry no IP.
func (p *peerContactsSet) markIP(peerContact string, seed bool) {
	if len(peerContact) < 6 {
		return
	}
	if p.ips == nil {
		p.ips = make(map[string]bool)
	}
	p.ips[peerContact[:len(peerContact)-2]] = seed
}

// next returns up to 8 contacts, rotating so consecutive calls spread.
// Live contacts come first; a short second pass fills from the rest.
func (p *peerContactsSet) next() []string {
	count := kNodes
	if count > len(p.set) {
		count = len(p.set)
	}
	xx := make(map[string]bool)
	for range p.set {
		nid := p.ring.Move(1).Value.(string)
		if _, ok := xx[nid]; p.set[nid] && !ok {
			xx[nid] = true
		}
		if len(xx) >= count {
			break
		}
	}
	if len(xx) < count {
		for range p.set {
			nid := p.ring.Move(1).Value.(string)
			if _, ok := xx[nid]; ok {
				continue
			}
			xx[nid] = true
			if len(xx) >= count {
				break
			}
		}
	}
	x := make([]string, 0, count)
	for id := range xx {
		x = append(x, id)
	}
	return x
}

// put stores a binary contact (6-byte v4 or 18-byte v6, per
// encodeCompactPeerAddr): too short is rejected, duplicates report false.
func (p *peerContactsSet) put(peerContact string) bool {
	if len(peerContact) < 6 {
		return false
	}
	if ok := p.set[peerContact]; ok {
		return false
	}
	p.set[peerContact] = true
	r := &ring.Ring{Value: peerContact}
	if p.ring == nil {
		p.ring = r
	} else {
		p.ring.Link(r)
	}
	return true
}

// drop removes one contact: the named one when given, else a dead one
// first, else the ring head. It returns what left.
func (p *peerContactsSet) drop(peerContact string) string {
	if peerContact == "" {
		if c := p.dropDead(); c != "" {
			return c
		}
		return p.drop(p.ring.Next().Value.(string))
	}
	for i := 0; i < p.ring.Len()+1; i++ {
		if p.ring.Move(1).Value.(string) == peerContact {
			dn := p.ring.Unlink(1).Value.(string)
			delete(p.set, dn)
			return dn
		}
	}
	return ""
}

// dropDead unlinks the first dead contact it cycles past.
func (p *peerContactsSet) dropDead() string {
	for i := 0; i < p.ring.Len()+1; i++ {
		if !p.set[p.ring.Move(1).Value.(string)] {
			dn := p.ring.Unlink(1).Value.(string)
			delete(p.set, dn)
			return dn
		}
	}
	return ""
}

// kill marks a contact dead without unlinking it.
func (p *peerContactsSet) kill(peerContact string) {
	if ok := p.set[peerContact]; ok {
		p.set[peerContact] = false
	}
}

// Size counts known contacts, dead or alive.
func (p *peerContactsSet) Size() int {
	return len(p.set)
}

// Alive counts the live contacts.
func (p *peerContactsSet) Alive() int {
	var ret int = 0
	for ih := range p.set {
		if p.set[ih] {
			ret++
		}
	}
	return ret
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
	if !noseed || len(peers.ips) == 0 {
		return list
	}
	kept := list[:0]
	for _, c := range list {
		if len(c) < 6 {
			continue
		}
		if seed := peers.ips[c[:len(c)-2]]; seed {
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
	if peers == nil || len(peers.ips) == 0 {
		return nil, nil
	}
	peersBloom = new(scrapeBloom)
	seedsBloom = new(scrapeBloom)
	for ip, seed := range peers.ips {
		if seed {
			seedsBloom.insert([]byte(ip))
		} else {
			peersBloom.insert([]byte(ip))
		}
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
			peers.markIP(peerContact, seed)
			if peers.Size() >= h.maxInfoHashPeers {
				if _, ok := peers.set[peerContact]; ok {
					return false
				}
				if peers.drop("") == "" {
					return false
				}
			}
			h.infoHashPeers.Add(string(ih), peers)
			return peers.put(peerContact)
		}
	}
	peers = &peerContactsSet{set: make(map[string]bool)}
	peers.markIP(peerContact, seed)
	h.infoHashPeers.Add(string(ih), peers)
	return peers.put(peerContact)
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
