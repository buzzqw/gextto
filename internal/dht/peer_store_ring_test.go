package dht

// peer_store_ring_test.go covers the contact ring behind a get_peers reply:
// one reply carries up to eight contacts, consecutive replies continue where
// the previous one stopped, and a contact can be removed wherever it sits in
// the ring. container/ring does not advance on Move, so the cursor is easy to
// lose and a reply silently shrinks to one peer.

import (
	"fmt"
	"testing"
)

func contactsRingSet(t *testing.T, count int) *peerContactsSet {
	t.Helper()
	p := &peerContactsSet{set: make(map[string]bool)}
	for i := 0; i < count; i++ {
		contact := encodeCompactPeerAddr(fmt.Sprintf("10.0.0.%d:6881", i+1))
		if contact == "" {
			t.Fatalf("contact %d did not encode", i)
		}
		if !p.put(contact) {
			t.Fatalf("put(%d) failed", i)
		}
	}
	return p
}

func TestNextReturnsUpToEightContacts(t *testing.T) {
	p := contactsRingSet(t, 10)
	first := p.next()
	if len(first) != 8 {
		t.Fatalf("one reply carries %d contacts, want 8", len(first))
	}
	second := p.next()
	if len(second) != 8 {
		t.Fatalf("the second reply carries %d contacts, want 8", len(second))
	}
	seen := make(map[string]bool, 16)
	for _, c := range first {
		seen[c] = true
	}
	for _, c := range second {
		seen[c] = true
	}
	if len(seen) != 10 {
		t.Fatalf("two consecutive replies cover %d contacts, want all 10 (the ring must rotate)", len(seen))
	}
}

func TestNextStopsAtTheSetSize(t *testing.T) {
	p := contactsRingSet(t, 3)
	if got := len(p.next()); got != 3 {
		t.Fatalf("a small swarm returns %d contacts, want 3", got)
	}
	if got := contactsRingSet(t, 0).next(); got != nil {
		t.Fatalf("an empty set returns %v, want nil", got)
	}
}

func TestNextPutsLiveContactsBeforeDeadOnes(t *testing.T) {
	p := contactsRingSet(t, 10)
	for i := 1; i <= 3; i++ {
		p.kill(encodeCompactPeerAddr(fmt.Sprintf("10.0.0.%d:6881", i)))
	}
	reply := p.next()
	if len(reply) != 8 {
		t.Fatalf("reply carries %d contacts, want 8", len(reply))
	}
	live := 0
	for _, c := range reply {
		if p.set[c] {
			live++
		}
	}
	if live != 7 {
		t.Fatalf("the reply carries %d live contacts, want all 7 (dead ones fill only the rest)", live)
	}
}

func TestDropRemovesAContactAnywhereInTheRing(t *testing.T) {
	p := contactsRingSet(t, 5)
	target := encodeCompactPeerAddr("10.0.0.3:6881")
	if got := p.drop(target); got != target {
		t.Fatalf("drop returned %q, want %q", got, target)
	}
	if p.Size() != 4 {
		t.Fatalf("size after drop = %d, want 4", p.Size())
	}
	if p.set[target] {
		t.Fatal("the dropped contact is still in the set")
	}
	for _, c := range p.next() {
		if c == target {
			t.Fatal("the dropped contact is still handed out")
		}
	}
	// Dropping an unknown contact changes nothing.
	if got := p.drop(encodeCompactPeerAddr("10.0.0.9:6881")); got != "" {
		t.Fatalf("drop of an unknown contact = %q, want empty", got)
	}
	if p.Size() != 4 {
		t.Fatalf("size after a missed drop = %d, want 4", p.Size())
	}
}

func TestDropDeadRemovesTheDeadContact(t *testing.T) {
	p := contactsRingSet(t, 3)
	dead := encodeCompactPeerAddr("10.0.0.2:6881")
	p.kill(dead)
	if got := p.dropDead(); got != dead {
		t.Fatalf("dropDead returned %q, want %q", got, dead)
	}
	if p.Size() != 2 {
		t.Fatalf("size after dropDead = %d, want 2", p.Size())
	}
	if got := p.dropDead(); got != "" {
		t.Fatalf("dropDead with only live contacts returned %q, want empty", got)
	}
}

// drop("") is the eviction path used when a set is full: it must free a slot
// and leave the ring usable.
func TestDropWithoutATargetFreesASlot(t *testing.T) {
	p := contactsRingSet(t, 2)
	if got := p.drop(""); got == "" {
		t.Fatal("drop of the default contact removed nothing")
	}
	if p.Size() != 1 {
		t.Fatalf("size after eviction = %d, want 1", p.Size())
	}
	if !p.put(encodeCompactPeerAddr("10.0.0.7:6881")) {
		t.Fatal("the slot freed by the eviction is not reusable")
	}
}

func TestRemoveLeavesAnEmptySetClean(t *testing.T) {
	p := contactsRingSet(t, 1)
	only := encodeCompactPeerAddr("10.0.0.1:6881")
	if got := p.drop(only); got != only {
		t.Fatalf("drop returned %q, want %q", got, only)
	}
	if p.ring != nil {
		t.Fatal("the ring is not empty after removing the last contact")
	}
	if got := p.next(); got != nil {
		t.Fatalf("next on an empty ring = %v, want nil", got)
	}
	if !p.put(encodeCompactPeerAddr("10.0.0.2:6881")) {
		t.Fatal("the ring does not accept a contact after being emptied")
	}
	if got := len(p.next()); got != 1 {
		t.Fatalf("next after re-adding = %d contacts, want 1", got)
	}
}
