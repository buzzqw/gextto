package dht

// Kademlia routing on a binary tree, without buckets.
//
// Node ids are 20 bytes; distance is XOR, so "closest" only makes sense
// against a pivot info hash — nodes cannot be pre-sorted. The tree is 160
// levels deep; a lookup walks it by the pivot's bits, and when the pivot is
// absent (the usual case) the walk continues left collecting the 8 closest.
//
// Paths compress: a lone node sits directly on its parent, and a collision
// unpacks the shared prefix bit by bit. The tree stays short, and lookups
// vanished from CPU profiles long ago.

type nTree struct {
	zero, one *nTree
	value     *remoteNode
}

const (
	// kNodes caps the nodes per reply.
	kNodes = 8
	// maxNodePendingQueries retires a node from filtered lookups past this
	// many outstanding queries.
	maxNodePendingQueries = 5
)

// insert adds a node to the tree.
func (n *nTree) insert(newNode *remoteNode) {
	n.put(newNode, 0)
}

// branchOut splits two colliding nodes: shared prefix bits extend the path,
// then both park at their first differing bit. Nothing else lives below, so
// the descent writes straight down.
func (n *nTree) branchOut(n1, n2 *remoteNode, i int) {
	bit, bit2 := treeBit(n1.id, i), treeBit(n2.id, i)
	if bit != bit2 {
		n.put(n1, i)
		n.put(n2, i)
		return
	}
	if bit != 0 {
		n.one = &nTree{}
		n.one.branchOut(n1, n2, i+1)
	} else {
		n.zero = &nTree{}
		n.zero.branchOut(n1, n2, i+1)
	}
}

// treeBit reads bit i of an id, most-significant first.
func treeBit(id string, i int) byte {
	return (byte(id[i/8]) << byte(i%8)) & 128
}

// put inserts below bit i: past the end replaces in place; a compressed
// occupant either merges (same id) or branches out (collision); otherwise
// the walk descends, materializing the child on first use.
func (n *nTree) put(newNode *remoteNode, i int) {
	if i >= len(newNode.id)*8 {
		n.value = newNode
		return
	}
	if n.value != nil {
		if n.value.id == newNode.id {
			n.value = newNode
			return
		}
		old := n.value
		n.value = nil
		n.branchOut(newNode, old, i)
		return
	}
	if treeBit(newNode.id, i) != 0 {
		if n.one == nil {
			n.one = &nTree{value: newNode}
			return
		}
		n.one.put(newNode, i+1)
	} else {
		if n.zero == nil {
			n.zero = &nTree{value: newNode}
			return
		}
		n.zero.put(newNode, i+1)
	}
}

// lookup returns up to kNodes closest to id, unfiltered.
func (n *nTree) lookup(id InfoHash) []*remoteNode {
	if n == nil || id == "" {
		return nil
	}
	return n.traverse(id, 0, make([]*remoteNode, 0, kNodes), false)
}

// lookupFiltered returns up to kNodes closest to id, skipping stale and
// recently-contacted nodes.
func (n *nTree) lookupFiltered(id InfoHash) []*remoteNode {
	if n == nil || id == "" {
		return nil
	}
	return n.traverse(id, 0, make([]*remoteNode, 0, kNodes), true)
}

// traverse walks pivot-bit-first then the other side, collecting values
// (filtered by isOK when asked) until kNodes or exhaustion.
func (n *nTree) traverse(id InfoHash, i int, ret []*remoteNode, filter bool) []*remoteNode {
	if n == nil {
		return ret
	}
	if n.value != nil {
		if !filter || n.isOK(id) {
			ret = append(ret, n.value)
		}
	}
	if i >= len(id)*8 {
		return ret
	}
	if len(ret) >= kNodes {
		return ret
	}
	var first, second *nTree
	if treeBit(string(id), i) != 0 {
		first, second = n.one, n.zero
	} else {
		first, second = n.zero, n.one
	}
	if ret = first.traverse(id, i+1, ret, filter); len(ret) >= kNodes {
		return ret
	}
	return second.traverse(id, i+1, ret, filter)
}

// cut prunes the path to id bottom-up, deleting children whose leaves all
// emptied; true asks the parent to drop this node too.
func (n *nTree) cut(id InfoHash, i int) (cutMe bool) {
	if n == nil {
		return true
	}
	if i >= len(id)*8 {
		return true
	}
	if treeBit(string(id), i) != 0 {
		if n.one.cut(id, i+1) {
			n.one = nil
			if n.zero == nil {
				return true
			}
		}
	} else {
		if n.zero.cut(id, i+1) {
			n.zero = nil
			if n.one == nil {
				return true
			}
		}
	}
	return false
}

// isOK admits a node to filtered lookups: present, uncongested and not
// contacted for ih recently.
func (n *nTree) isOK(ih InfoHash) bool {
	if n.value == nil || n.value.id == "" {
		return false
	}
	r := n.value
	if len(r.pendingQueries) > maxNodePendingQueries {
		return false
	}
	return !r.wasContactedRecently(ih)
}

// commonBits counts the shared leading bits of two ids (from jch's dht.cc).
func commonBits(s1, s2 string) int {
	id1, id2 := []byte(s1), []byte(s2)
	i := 0
	for ; i < 20; i++ {
		if id1[i] != id2[i] {
			break
		}
	}
	if i == 20 {
		return 160
	}
	xor := id1[i] ^ id2[i]
	j := 0
	for (xor & 0x80) == 0 {
		xor <<= 1
		j++
	}
	return 8*i + j
}

// hashDistance XORs two hashes for display only: the pretty form of
// Kademlia distance, slower than the bitwise walk. Empty on length mismatch.
func hashDistance(id1 InfoHash, id2 InfoHash) (distance string) {
	d := make([]byte, len(id1))
	if len(id1) != len(id2) {
		return ""
	}
	for i := 0; i < len(id1); i++ {
		d[i] = id1[i] ^ id2[i]
	}
	return string(d)
}
