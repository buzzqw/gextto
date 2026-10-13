package unchoker

import (
	"cmp"
	"math/rand/v2"
	"slices"
)

// Unchoker rotates upload slots across interested peers by speed: the
// fastest downloaders (or uploaders, once complete) hold the regular slots,
// with an optimistic slot probing a random peer every third round.
type Unchoker struct {
	numUnchoked           int
	numOptimisticUnchoked int

	// Every 3rd round runs the optimistic logic.
	round uint8

	peersUnchoked           map[Peer]struct{}
	peersUnchokedOptimistic map[Peer]struct{}
}

// Peer abstracts what choking decisions need from a connection.
type Peer interface {
	// Choke/Unchoke set the local choking state.
	Choke()
	Unchoke()

	// Choking reports the local choke state.
	Choking() bool

	// Interested reports the remote peer's interest.
	Interested() bool

	// SetOptimistic/Optimistic track the optimistic slot holder.
	SetOptimistic(value bool)
	Optimistic() bool

	DownloadSpeed() int
	UploadSpeed() int
}

// New sizes an unchoker for numUnchoked regular plus numOptimisticUnchoked
// optimistic slots.
func New(numUnchoked, numOptimisticUnchoked int) *Unchoker {
	return &Unchoker{
		numUnchoked:             numUnchoked,
		numOptimisticUnchoked:   numOptimisticUnchoked,
		peersUnchoked:           make(map[Peer]struct{}, numUnchoked),
		peersUnchokedOptimistic: make(map[Peer]struct{}, numUnchoked),
	}
}

// HandleDisconnect forgets a peer from both slot sets.
func (u *Unchoker) HandleDisconnect(pe Peer) {
	delete(u.peersUnchoked, pe)
	delete(u.peersUnchokedOptimistic, pe)
}

// SetNumUnchoked resizes the regular slots (minimum one). Call it from the
// same goroutine that runs TickUnchoke.
func (u *Unchoker) SetNumUnchoked(n int) {
	if n < 1 {
		n = 1
	}
	u.numUnchoked = n
}

// TickUnchoke re-deals the slots: fastest interested peers first (by upload
// once complete, by download otherwise), skipping optimistic holders on
// non-optimistic rounds; leftovers choke. Round 0 additionally spends the
// optimistic slots on random remaining peers.
func (u *Unchoker) TickUnchoke(allPeers []Peer, torrentCompleted bool) {
	optimistic := u.round == 0
	peers := interestedOnly(allPeers)
	sortBySpeed(peers, torrentCompleted)

	rest := dealRegular(u, peers, optimistic)
	if optimistic {
		rest = dealOptimistic(u, rest)
	}
	for _, pe := range rest {
		u.chokePeer(pe)
	}
	u.round = (u.round + 1) % 3
}

// interestedOnly filters the slot contenders in place.
func interestedOnly(allPeers []Peer) []Peer {
	peers := allPeers[:0]
	for _, pe := range allPeers {
		if pe.Interested() {
			peers = append(peers, pe)
		}
	}
	return peers
}

// sortBySpeed ranks contenders fastest-first.
func sortBySpeed(peers []Peer, completed bool) {
	byUpload := func(a, b Peer) int { return cmp.Compare(b.UploadSpeed(), a.UploadSpeed()) }
	byDownload := func(a, b Peer) int { return cmp.Compare(b.DownloadSpeed(), a.DownloadSpeed()) }
	if completed {
		slices.SortFunc(peers, byUpload)
	} else {
		slices.SortFunc(peers, byDownload)
	}
}

// dealRegular fills the regular slots from the ranked contenders, returning
// the undealt remainder.
func dealRegular(u *Unchoker, peers []Peer, optimistic bool) []Peer {
	var dealt int
	i := 0
	for ; i < len(peers) && dealt < u.numUnchoked; i++ {
		if !optimistic && peers[i].Optimistic() {
			continue
		}
		u.unchokePeer(peers[i])
		dealt++
	}
	return peers[i:]
}

// dealOptimistic spends the optimistic slots on random remainder peers.
func dealOptimistic(u *Unchoker, peers []Peer) []Peer {
	for i := 0; i < u.numOptimisticUnchoked && len(peers) > 0; i++ {
		n := rand.IntN(len(peers)) // nolint: gosec
		u.optimisticUnchokePeer(peers[n])
		peers[n], peers = peers[len(peers)-1], peers[:len(peers)-1]
	}
	return peers
}

func (u *Unchoker) chokePeer(pe Peer) {
	if pe.Choking() {
		return
	}
	pe.Choke()
	pe.SetOptimistic(false)
	delete(u.peersUnchoked, pe)
	delete(u.peersUnchokedOptimistic, pe)
}

func (u *Unchoker) unchokePeer(pe Peer) {
	if !pe.Choking() {
		if pe.Optimistic() {
			// Promote into the regular set.
			pe.SetOptimistic(false)
			delete(u.peersUnchokedOptimistic, pe)
			u.peersUnchoked[pe] = struct{}{}
		}
		return
	}
	pe.Unchoke()
	u.peersUnchoked[pe] = struct{}{}
	pe.SetOptimistic(false)
}

func (u *Unchoker) optimisticUnchokePeer(pe Peer) {
	if !pe.Choking() {
		if !pe.Optimistic() {
			// Promote into the optimistic set.
			pe.SetOptimistic(true)
			delete(u.peersUnchoked, pe)
			u.peersUnchokedOptimistic[pe] = struct{}{}
		}
		return
	}
	pe.Unchoke()
	u.peersUnchokedOptimistic[pe] = struct{}{}
	pe.SetOptimistic(true)
}

// FastUnchoke unchokes an interested peer immediately when slots remain,
// instead of making it wait for the next round.
func (u *Unchoker) FastUnchoke(pe Peer) {
	if pe.Choking() && pe.Interested() && len(u.peersUnchoked) < u.numUnchoked {
		u.unchokePeer(pe)
	}
	if pe.Choking() && pe.Interested() && len(u.peersUnchokedOptimistic) < u.numOptimisticUnchoked {
		u.optimisticUnchokePeer(pe)
	}
}
