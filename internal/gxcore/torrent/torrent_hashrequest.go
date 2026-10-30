package torrent

import (
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// BEP 52 "hash request"/"hashes"/"hash reject". A v2 magnet resolves its info
// dict over BEP 9 first; the per-file "piece layers" that make piece
// verification possible are then fetched from peers with a hash request whose
// base layer is the piece layer. A seeder answers from the piece hashes it
// already has (its "piece layers"), so no file data has to be read.

// v2ChunkMax bounds one hash request; BEP 52 suggests at most 512 hashes.
const v2ChunkMax = 512

// v2HashTimeout is how long a hash request may stay unanswered before the peer
// is dropped from the pool and the chunk is retried elsewhere. Several chunks
// are in flight at once (one per peer), so a silent peer no longer stalls a
// file's whole piece layer.
const v2HashTimeout = 30 * time.Second

// v2LayerFile tracks the download of one file's piece layer. The layer is split
// into fixed chunks of at most v2ChunkMax hashes; each chunk is requested from
// one peer, and different chunks can be in flight at different peers at the
// same time, so a large file's layer is fetched in parallel.
type v2LayerFile struct {
	root    [32]byte
	fileIdx int
	// numPieces real hashes, padded to a power of two (piece-layer nodes).
	numPieces int
	padded    int
	// paddedLeaves is the file's block count rounded to a power of two.
	paddedLeaves int
	level        int
	data         []byte // numPieces * 32 bytes

	chunk     int          // hashes per request (a power of two, <= v2ChunkMax)
	chunks    int          // number of chunks (padded / chunk)
	nextChunk int          // first chunk never requested
	inflight  map[int]bool // chunk numbers currently requested
	retry     []int        // chunk numbers to request again
}

// v2HashRequest is one chunk request in flight at a peer.
type v2HashRequest struct {
	file  *v2LayerFile
	index int // base-layer index of the first hash (chunk * chunk size)
	count int
	proof int
	at    time.Time
}

// allocateChunk hands out the next chunk to request (a retry first), reporting
// false when the file has nothing left to ask for.
func (lf *v2LayerFile) allocateChunk() (int, bool) {
	if n := len(lf.retry); n > 0 {
		chunk := lf.retry[n-1]
		lf.retry = lf.retry[:n-1]
		return chunk, true
	}
	if lf.nextChunk < lf.chunks {
		chunk := lf.nextChunk
		lf.nextChunk++
		return chunk, true
	}
	return 0, false
}

// failChunk releases a chunk that was in flight and queues it for another peer.
func (lf *v2LayerFile) failChunk(chunk int) {
	if lf.inflight != nil {
		delete(lf.inflight, chunk)
	}
	lf.retry = append(lf.retry, chunk)
}

// done reports whether every chunk has been received and none is pending.
func (lf *v2LayerFile) done() bool {
	return lf.nextChunk >= lf.chunks && len(lf.inflight) == 0 && len(lf.retry) == 0
}

// advertisesV2 reports whether this torrent has a BitTorrent v2 identity (a
// v2-only torrent, or the v2 side of a hybrid): only then may the v2 reserved
// bit be set in the handshake. Setting it on a v1 torrent makes libtorrent
// reject the connection.
func (t *torrent) advertisesV2() bool {
	if t.info() != nil {
		return t.info().HasV2
	}
	return t.hasV2Hint
}

// ourExtensions is the session's reserved bits plus the BEP 52 v2 bit (byte 7,
// 0x10) when this torrent has a v2 identity.
func (t *torrent) ourExtensions() [8]byte {
	ext := t.session.extensions
	if t.advertisesV2() {
		ext[7] |= 0x10
	}
	return ext
}

// matchesInfoHash reports whether the raw info dict belongs to this torrent. A
// v2 torrent is identified by the first 20 bytes of its SHA-256; a v1 (and the
// v1 side of a hybrid) torrent by its SHA-1.
func (t *torrent) matchesInfoHash(b []byte) bool {
	sum1 := sha1.Sum(b)
	if sum1 == t.infoHash {
		return true
	}
	sum256 := sha256.Sum256(b)
	return [20]byte(sum256[:20]) == t.infoHash
}

// handleHashRequest answers a BEP 52 hash request, or rejects it when we cannot
// serve it.
func (t *torrent) handleHashRequest(pe *peer.Peer, msg peerprotocol.HashRequestMessage) {
	hashes, ok := t.serveHashes(msg.HashRequestHeader)
	if !ok {
		pe.SendMessage(peerprotocol.HashRejectMessage{HashRequestHeader: msg.HashRequestHeader})
		return
	}
	pe.SendMessage(peerprotocol.HashesMessage{HashRequestHeader: msg.HashRequestHeader, Hashes: hashes})
}

// allowServedHashRequest takes the file bytes an answer will read from the
// torrent's hash-answer budget: a burst, then a steady rate.
func (t *torrent) allowServedHashRequest(now time.Time, bytes int) bool {
	if t.hashReqRefill.IsZero() {
		t.hashReqBudget = hashServeBurstBytes
		t.hashReqRefill = now
	}
	if elapsed := now.Sub(t.hashReqRefill); elapsed >= time.Second {
		seconds := int(elapsed / time.Second)
		t.hashReqBudget = min(hashServeBurstBytes, t.hashReqBudget+seconds*hashServePerSecond)
		t.hashReqRefill = t.hashReqRefill.Add(time.Duration(seconds) * time.Second)
	}
	if bytes > t.hashReqBudget {
		return false
	}
	t.hashReqBudget -= bytes
	return true
}

func (t *torrent) serveHashes(req peerprotocol.HashRequestHeader) ([][merkle.HashSize]byte, bool) {
	if t.info() == nil || !t.info().V2 || !t.info().HasPieceHashes() {
		return nil, false
	}
	fi := -1
	for i, f := range t.info().V2Files {
		if f.HasRoot && f.PiecesRoot == req.Root {
			fi = i
			break
		}
	}
	if fi < 0 {
		return nil, false
	}
	level := merkle.PieceLevel(int(t.info().PieceLength))
	if level < 0 {
		return nil, false
	}
	// At or above the piece layer the answer comes from the attached piece
	// hashes: no file data is needed, so a partly downloaded file serves it too.
	if int(req.Base) >= level {
		return getHashes(t.v2Tree(fi), req)
	}
	// Below the piece layer the answer needs the file's 16 KiB blocks.
	if t.v2FileComplete(fi) {
		if hashes, ok := getHashes(t.v2BlockTree(fi), req); ok {
			return hashes, true
		}
		// The block tree is still being built off the loop: a request that
		// spans whole pieces is answered from the pieces meanwhile.
	}
	// A partly downloaded file can still serve the leaf layer when the request
	// spans at least one piece, so its proof lives at or above the piece layer.
	// This path reads the blocks from disk on the run loop: such requests are
	// paced, so a peer cannot use them to drive the disk. The answers above
	// come from memory and are not limited.
	if !t.allowServedHashRequest(time.Now(), int(req.Length)*merkle.BlockSize) {
		return nil, false
	}
	return t.servePartialBlockHashes(fi, level, req)
}

// getHashes wraps LayerTree.GetHashes, mapping "cannot serve" to a hash reject.
func getHashes(tree *merkle.LayerTree, req peerprotocol.HashRequestHeader) ([][merkle.HashSize]byte, bool) {
	if tree == nil {
		return nil, false
	}
	got := tree.GetHashes(int(req.Base), int(req.Index), int(req.Length), int(req.ProofLayers))
	if got == nil {
		return nil, false
	}
	return got, true
}

// servePartialBlockHashes serves a leaf-layer (base 0) hash request from a v2
// file whose block tree is not available (partly downloaded, or complete but
// still being built): it computes just the requested 16 KiB leaf hashes from
// the verified pieces and anchors them with the piece layer's uncles, so a
// leecher can still fetch block hashes for the parts the seed has. The reply is
// exactly what LayerTree.GetHashes returns on the full block tree. It refuses
// (hash reject) when a requested leaf sits in a missing piece, when the range
// folds to a node below the piece layer (its uncles would need missing blocks)
// or when the request is larger than v2ChunkMax (the leaves are read from disk
// on the torrent loop, so the read stays bounded).
func (t *torrent) servePartialBlockHashes(fi, level int, req peerprotocol.HashRequestHeader) ([][merkle.HashSize]byte, bool) {
	length := int(req.Length)
	index := int(req.Index)
	proof := int(req.ProofLayers)
	if req.Base != 0 || length < 1 || length > v2ChunkMax || length&(length-1) != 0 || index < 0 || index%length != 0 || proof < 0 {
		return nil, false
	}
	m := merkle.Log2(length)
	if m < level {
		return nil, false
	}
	tree := t.v2Tree(fi)
	if tree == nil {
		return nil, false
	}
	// Same bounds as GetHashes on the block tree (base 0, same height).
	height := tree.Height()
	if proof >= height || index+length > 1<<height {
		return nil, false
	}
	leaves, ok := t.blockLeavesRange(fi, index, length)
	if !ok {
		return nil, false
	}
	if proof < m {
		// GetHashes adds uncles for levels m..proof only: none here.
		return leaves, true
	}
	// The range folds to the node at level m (in the piece tree); its uncles
	// at levels m..proof are the proof.
	up, ok := getHashes(tree, peerprotocol.HashRequestHeader{
		Root:        req.Root,
		Base:        uint32(m),
		Index:       uint32(index >> m),
		Length:      1,
		ProofLayers: uint32(proof - m),
	})
	if !ok || len(up) != proof-m+2 {
		return nil, false
	}
	return append(leaves, up[1:]...), true // drop the node itself, keep the uncles
}

// blockLeavesRange computes the 16 KiB leaf hashes of the file blocks
// [index, index+length) of the padded block layer. Every covering piece must
// be verified, otherwise the leaves cannot be trusted and it reports false;
// leaves past the end of the file are the zero padding hash (BEP 52).
func (t *torrent) blockLeavesRange(fi, index, length int) ([][merkle.HashSize]byte, bool) {
	if t.pieces == nil || t.bitfield == nil || index < 0 || length < 1 {
		return nil, false
	}
	f := t.info().V2Files[fi]
	totalBlocks := int((f.Length + merkle.BlockSize - 1) / merkle.BlockSize)
	pieceLength := int(t.info().PieceLength)
	if pieceLength < merkle.BlockSize || pieceLength%merkle.BlockSize != 0 {
		return nil, false
	}
	blocksPerPiece := pieceLength / merkle.BlockSize
	start, count := t.v2FilePieceRange(fi)
	if count == 0 {
		return nil, false
	}
	end := min(index+length, totalBlocks)
	leaves := make([][merkle.HashSize]byte, 0, length)
	var buf []byte
	for first := index; first < end; {
		p := first / blocksPerPiece
		global := start + p
		if p >= count || global >= len(t.pieces) || !t.bitfield.Test(uint32(global)) || t.seedUnverified(uint32(global)) {
			return nil, false
		}
		pc := &t.pieces[global]
		if int(pc.Length) > len(buf) {
			buf = make([]byte, pc.Length)
		}
		data := buf[:pc.Length]
		if _, err := pc.Data.ReadAt(data, 0); err != nil {
			return nil, false
		}
		ls := merkle.LeafHashes(data)
		lo := first - p*blocksPerPiece
		hi := min(end-p*blocksPerPiece, len(ls))
		if lo >= hi {
			return nil, false
		}
		leaves = append(leaves, ls[lo:hi]...)
		first = p*blocksPerPiece + hi
	}
	// Past the end of the file the padded layer holds zero hashes.
	for len(leaves) < length {
		leaves = append(leaves, [merkle.HashSize]byte{})
	}
	return leaves, true
}

// v2BlockCacheBudget bounds the memory the block-layer trees of one torrent may
// hold. A tree is big (a 40 GB file is a few hundred MB including the padded
// layers); the budget keeps a large file from pinning that for the whole
// torrent lifetime while still caching the most recently used one. A build in
// flight reserves its estimated size, so several requests cannot exceed the
// budget before the trees exist.
const v2BlockCacheBudget = 256 << 20

// maxV2BlockBuilds caps the block-tree builds running at the same time for one
// torrent. Each one reads a whole file from disk: a peer asking for the hashes
// of every file of a large torrent must not turn into N parallel reads.
const maxV2BlockBuilds = 1

// Leaf-layer answers are served on the run loop and read the file from disk
// (up to v2ChunkMax blocks, 8 MiB, for one request). They are paced by the bytes
// they read, not by the request count: a downloader fetching hashes for a big
// file pays only what it reads, while a peer that asks for the whole file over
// and over cannot use the run loop to drive the disk. Piece-layer answers come
// from memory and are not limited.
const (
	hashServeBurstBytes = 32 << 20
	hashServePerSecond  = 16 << 20
)

// v2BlockRetryAfter is how long a failed block-tree build is remembered, so a
// peer asking again does not trigger a rebuild storm.
const v2BlockRetryAfter = time.Minute

// v2BlockTreeEntry is one cached block-layer tree with its size and last use.
type v2BlockTreeEntry struct {
	tree     *merkle.LayerTree
	bytes    int64
	lastUsed time.Time
}

// v2BlockTree returns the cached block-layer tree (base=0) of a v2 file, or nil
// when it is not ready. A missing tree starts a background build and the
// request is refused (hash reject) until it is done: the build reads the whole
// file from disk, which on a slow HDD can take minutes and must never block the
// torrent loop. A file that is not fully present, or whose data no longer
// matches its pieces root, is not served.
func (t *torrent) v2BlockTree(fi int) *merkle.LayerTree {
	f := t.info().V2Files[fi]
	key := string(f.PiecesRoot[:])
	now := time.Now()
	t.v2BlockMu.Lock()
	if entry, ok := t.v2BlockTrees[key]; ok {
		entry.lastUsed = now
		tree := entry.tree
		t.v2BlockMu.Unlock()
		return tree
	}
	if t.v2BlockBusy[key] {
		t.v2BlockMu.Unlock()
		return nil
	}
	if until, ok := t.v2BlockFailed[key]; ok && now.Before(until) {
		t.v2BlockMu.Unlock()
		return nil
	}
	if !t.v2FileComplete(fi) {
		t.v2BlockMu.Unlock()
		return nil
	}
	if t.v2BlockBuilds >= maxV2BlockBuilds {
		t.v2BlockMu.Unlock()
		return nil
	}
	if t.v2BlockBusy == nil {
		t.v2BlockBusy = map[string]bool{}
	}
	t.v2BlockBusy[key] = true
	t.v2BlockBuilds++
	// Reserve the estimated tree size for the duration of the build, so the
	// in-flight reads count against the cache budget too.
	reserved := expectedV2BlockTreeBytes(f.Length)
	t.v2BlockBytes += reserved
	t.v2BlockMu.Unlock()
	go t.buildV2BlockTree(fi, key, reserved)
	return nil
}

// expectedV2BlockTreeBytes estimates the memory a file's block-layer tree takes:
// one 32-byte hash per 16 KiB block, doubled for the intermediate layers.
func expectedV2BlockTreeBytes(length int64) int64 {
	blocks := (length + merkle.BlockSize - 1) / merkle.BlockSize
	return blocks * merkle.HashSize * 2
}

// v2FileComplete reports whether every piece of a v2 file is present.
func (t *torrent) v2FileComplete(fi int) bool {
	if t.pieces == nil || t.bitfield == nil {
		return false
	}
	start, count := t.v2FilePieceRange(fi)
	if count == 0 || start+count > len(t.pieces) {
		return false
	}
	for i := start; i < start+count; i++ {
		if !t.bitfield.Test(uint32(i)) {
			return false
		}
	}
	return true
}

// buildV2BlockTree reads a v2 file's verified data off the torrent loop, builds
// its block-layer tree, verifies it against the file's pieces root and caches
// it. On any failure the file is put on a short cooldown so another request
// does not immediately retry.
func (t *torrent) buildV2BlockTree(fi int, key string, reserved int64) {
	tree := t.buildV2BlockTreeData(fi)
	t.v2BlockMu.Lock()
	delete(t.v2BlockBusy, key)
	t.v2BlockBuilds--
	t.v2BlockBytes -= reserved
	if tree == nil {
		if t.v2BlockFailed == nil {
			t.v2BlockFailed = map[string]time.Time{}
		}
		t.v2BlockFailed[key] = time.Now().Add(v2BlockRetryAfter)
		t.v2BlockMu.Unlock()
		return
	}
	if t.v2BlockTrees == nil {
		t.v2BlockTrees = map[string]*v2BlockTreeEntry{}
	}
	size := int64(tree.Size())
	t.v2BlockTrees[key] = &v2BlockTreeEntry{tree: tree, bytes: size, lastUsed: time.Now()}
	t.v2BlockBytes += size
	t.evictV2BlockTreesLocked()
	delete(t.v2BlockFailed, key)
	t.v2BlockMu.Unlock()
}

// buildV2BlockTreeData does the disk read and the root check. The read buffer is
// reused across a file's pieces: allocating p.Length bytes per piece would be
// wasteful for large pieces.
func (t *torrent) buildV2BlockTreeData(fi int) *merkle.LayerTree {
	f := t.info().V2Files[fi]
	if !t.v2FileComplete(fi) {
		return nil
	}
	start, count := t.v2FilePieceRange(fi)
	buf := make([]byte, int(t.info().PieceLength))
	// The leaves of the base-0 tree are 16 KiB blocks, not pieces: seed the
	// capacity with the block count so large files do not reallocate the slice
	// dozens of times while appending.
	totalBlocks := int((f.Length + merkle.BlockSize - 1) / merkle.BlockSize)
	leaves := make([][merkle.HashSize]byte, 0, totalBlocks)
	for i := start; i < start+count; i++ {
		// A torrent removed while the tree is building (a multi-GB read) must not
		// keep reading the disk to the end for a peer that is no longer there.
		select {
		case <-t.closeC:
			return nil
		default:
		}
		p := t.pieces[i]
		if int(p.Length) > len(buf) {
			buf = make([]byte, p.Length)
		}
		data := buf[:p.Length]
		if _, err := p.Data.ReadAt(data, 0); err != nil {
			return nil
		}
		leaves = appendBlockLeaves(leaves, data)
	}
	tree := merkle.NewLayerTree(0, leaves)
	if tree == nil || tree.Root() != f.PiecesRoot {
		// The data on disk does not anchor to the file's root (changed since it
		// was verified): never serve hashes that would fail another peer's
		// verification.
		return nil
	}
	return tree
}

// evictV2BlockTreesLocked drops the least recently used trees until the cache
// is under its budget. At least the newest entry is kept even when it alone
// exceeds the budget, so a very large file is still cached. The caller holds
// v2BlockMu.
func (t *torrent) evictV2BlockTreesLocked() {
	for t.v2BlockBytes > v2BlockCacheBudget && len(t.v2BlockTrees) > 1 {
		var oldestKey string
		var oldest time.Time
		for key, entry := range t.v2BlockTrees {
			if oldestKey == "" || entry.lastUsed.Before(oldest) {
				oldestKey, oldest = key, entry.lastUsed
			}
		}
		t.v2BlockBytes -= t.v2BlockTrees[oldestKey].bytes
		delete(t.v2BlockTrees, oldestKey)
	}
}

// v2FilePieceRange returns the file's piece index range in the global piece
// address space (v2 pieces are file-aligned).
func (t *torrent) v2FilePieceRange(fi int) (start, count int) {
	perFile := func(i int) int {
		f := t.info().V2Files[i]
		switch {
		case f.Length == 0:
			return 0
		case f.Length <= int64(t.info().PieceLength):
			return 1
		default:
			return int((f.Length + int64(t.info().PieceLength) - 1) / int64(t.info().PieceLength))
		}
	}
	for i := 0; i < fi; i++ {
		start += perFile(i)
	}
	return start, perFile(fi)
}

// appendBlockLeaves appends the 16 KiB block hashes of one piece's data to
// leaves, so concatenating a file's pieces in order yields its block layer.
func appendBlockLeaves(leaves [][merkle.HashSize]byte, data []byte) [][merkle.HashSize]byte {
	return append(leaves, merkle.LeafHashes(data)...)
}

// v2Tree returns (and caches) the merkle tree at and above the piece layer for
// a v2 file, built from its attached piece hashes.
func (t *torrent) v2Tree(fi int) *merkle.LayerTree {
	f := t.info().V2Files[fi]
	key := string(f.PiecesRoot[:])
	if tree, ok := t.v2Trees[key]; ok {
		return tree
	}
	raw := t.info().V2FilePieces(fi)
	if len(raw) == 0 || len(raw)%merkle.HashSize != 0 {
		return nil
	}
	level := merkle.PieceLevel(int(t.info().PieceLength))
	if level < 0 {
		return nil
	}
	hashes := make([][merkle.HashSize]byte, len(raw)/merkle.HashSize)
	for i := range hashes {
		copy(hashes[i][:], raw[i*merkle.HashSize:])
	}
	tree := merkle.NewLayerTree(level, hashes)
	if tree == nil {
		return nil
	}
	if t.v2Trees == nil {
		t.v2Trees = make(map[string]*merkle.LayerTree)
	}
	t.v2Trees[key] = tree
	return tree
}

// initV2Layers prepares the download of the piece layers a v2 magnet needs.
func (t *torrent) initV2Layers() {
	if t.v2LayerByRoot != nil {
		return
	}
	t.v2LayerByRoot = make(map[string]*v2LayerFile)
	t.v2Pending = make(map[*peer.Peer]*v2HashRequest)
	t.v2NoHashPeers = make(map[*peer.Peer]struct{})
	level := merkle.PieceLevel(int(t.info().PieceLength))
	if level < 0 {
		return
	}
	for fi, f := range t.info().V2Files {
		if f.Length == 0 || !f.HasRoot || f.Length <= int64(t.info().PieceLength) {
			continue
		}
		numPieces := int((f.Length + int64(t.info().PieceLength) - 1) / int64(t.info().PieceLength))
		numBlocks := int((f.Length + int64(merkle.BlockSize) - 1) / int64(merkle.BlockSize))
		paddedLeaves := merkle.NextPowerOfTwo(numBlocks)
		padded := paddedLeaves >> level
		chunk := padded
		if chunk > v2ChunkMax {
			chunk = v2ChunkMax
		}
		if chunk < 1 {
			continue
		}
		lf := &v2LayerFile{
			root:         f.PiecesRoot,
			fileIdx:      fi,
			numPieces:    numPieces,
			padded:       padded,
			paddedLeaves: paddedLeaves,
			level:        level,
			data:         make([]byte, numPieces*merkle.HashSize),
			chunk:        chunk,
			chunks:       padded / chunk,
			inflight:     make(map[int]bool),
		}
		t.v2LayerFiles = append(t.v2LayerFiles, lf)
		t.v2LayerByRoot[string(f.PiecesRoot[:])] = lf
	}
}

// nextV2Chunk returns the next file with a chunk to request and the chunk
// number, reserving that chunk.
func (t *torrent) nextV2Chunk() (*v2LayerFile, int, bool) {
	for _, lf := range t.v2LayerFiles {
		if chunk, ok := lf.allocateChunk(); ok {
			return lf, chunk, true
		}
	}
	return nil, 0, false
}

// startV2LayerDownload issues hash requests for the piece layers still missing,
// one chunk per free peer, so several peers fetch different chunks of a file at
// the same time. It runs on the torrent loop, the only goroutine touching this
// state.
func (t *torrent) startV2LayerDownload() {
	if t.info() == nil || !t.info().V2 {
		return
	}
	if !t.info().NeedsV2Layers() {
		// Nothing to fetch: either the layers are already attached or every
		// file fits in one piece.
		if !t.info().HasPieceHashes() && t.v2LayerByRoot == nil {
			t.finishV2Layers()
		}
		return
	}
	if t.info().HasPieceHashes() {
		return
	}
	t.initV2Layers()
	if len(t.v2LayerFiles) == 0 {
		t.finishV2Layers()
		return
	}
	proofLayers := func(lf *v2LayerFile) int {
		proof := merkle.Log2(lf.paddedLeaves) - lf.level - 1
		if proof < 0 {
			proof = 0
		}
		return proof
	}
	for pe := range t.peers {
		if _, ok := t.v2Pending[pe]; ok {
			continue
		}
		if _, bad := t.v2NoHashPeers[pe]; bad {
			continue
		}
		// Only a peer that set the v2 reserved bit speaks BEP 52; a v1 client
		// may drop the connection on the unknown message id.
		if !pe.ProtocolV2 {
			continue
		}
		lf, chunk, ok := t.nextV2Chunk()
		if !ok {
			return
		}
		index := chunk * lf.chunk
		proof := proofLayers(lf)
		hdr := peerprotocol.HashRequestHeader{
			Root:        lf.root,
			Base:        uint32(lf.level),
			Index:       uint32(index),
			Length:      uint32(lf.chunk),
			ProofLayers: uint32(proof),
		}
		lf.inflight[chunk] = true
		t.v2Pending[pe] = &v2HashRequest{
			file: lf, index: index, count: lf.chunk, proof: proof, at: time.Now(),
		}
		pe.SendMessage(peerprotocol.HashRequestMessage{HashRequestHeader: hdr})
	}
}

// handleHashes verifies a "hashes" reply and stores the requested layer range.
func (t *torrent) handleHashes(pe *peer.Peer, msg peerprotocol.HashesMessage) {
	req, ok := t.v2Pending[pe]
	if !ok {
		return
	}
	delete(t.v2Pending, pe)
	if req == nil || req.file == nil {
		return
	}
	lf := req.file
	expected := peerprotocol.HashRequestHeader{
		Root:        lf.root,
		Base:        uint32(lf.level),
		Index:       uint32(req.index),
		Length:      uint32(req.count),
		ProofLayers: uint32(req.proof),
	}
	if msg.HashRequestHeader != expected {
		// Not the reply to the request we sent: retry this chunk elsewhere.
		lf.failChunk(req.index / lf.chunk)
		t.startV2LayerDownload()
		return
	}
	uncles := req.proof - merkle.Log2(req.count) + 1
	if uncles < 0 {
		uncles = 0
	}
	if len(msg.Hashes) != req.count+uncles {
		t.v2LayerFailed(pe, lf, req)
		return
	}
	if !merkle.VerifyHashes(lf.root, lf.paddedLeaves, lf.level, req.index, req.count, msg.Hashes[:req.count], msg.Hashes[req.count:]) {
		// A peer that serves a hash range not anchored to the file's pieces
		// root is dropped; another peer retries the same chunk.
		t.closePeer(pe)
		t.v2LayerFailed(pe, lf, req)
		return
	}
	for i := 0; i < req.count; i++ {
		p := req.index + i
		if p >= lf.numPieces {
			break
		}
		copy(lf.data[p*merkle.HashSize:], msg.Hashes[i][:])
	}
	delete(lf.inflight, req.index/lf.chunk)
	if t.v2LayersComplete() {
		t.finishV2Layers()
		return
	}
	t.startV2LayerDownload()
}

// handleHashReject marks a peer as unable to serve hash requests and retries
// the chunk elsewhere.
func (t *torrent) handleHashReject(pe *peer.Peer, msg peerprotocol.HashRejectMessage) {
	req, ok := t.v2Pending[pe]
	if !ok {
		return
	}
	delete(t.v2Pending, pe)
	if req != nil && req.file != nil && msg.Root == req.file.root {
		req.file.failChunk(req.index / req.file.chunk)
	}
	if t.v2NoHashPeers == nil {
		t.v2NoHashPeers = make(map[*peer.Peer]struct{})
	}
	t.v2NoHashPeers[pe] = struct{}{}
	t.startV2LayerDownload()
}

// expireV2HashRequests gives up on hash requests unanswered for longer than
// v2HashTimeout: the peer is not asked again and the chunk goes to another one.
// It runs on the torrent loop's periodic tick.
func (t *torrent) expireV2HashRequests(now time.Time) {
	expired := false
	for pe, req := range t.v2Pending {
		if req == nil || req.file == nil || now.Sub(req.at) < v2HashTimeout {
			continue
		}
		delete(t.v2Pending, pe)
		req.file.failChunk(req.index / req.file.chunk)
		if t.v2NoHashPeers == nil {
			t.v2NoHashPeers = make(map[*peer.Peer]struct{})
		}
		t.v2NoHashPeers[pe] = struct{}{}
		expired = true
	}
	if expired {
		t.startV2LayerDownload()
	}
}

// v2LayerFailed releases a failed chunk for another peer and drops the peer.
func (t *torrent) v2LayerFailed(pe *peer.Peer, lf *v2LayerFile, req *v2HashRequest) {
	if lf != nil && req != nil {
		lf.failChunk(req.index / lf.chunk)
	}
	if t.v2NoHashPeers == nil {
		t.v2NoHashPeers = make(map[*peer.Peer]struct{})
	}
	t.v2NoHashPeers[pe] = struct{}{}
	t.startV2LayerDownload()
}

func (t *torrent) v2LayersComplete() bool {
	for _, lf := range t.v2LayerFiles {
		if !lf.done() {
			return false
		}
	}
	return true
}

// finishV2Layers attaches the fetched layers, persists them and starts the
// allocator so the download can begin.
func (t *torrent) finishV2Layers() {
	if t.info() == nil || t.info().HasPieceHashes() {
		return
	}
	layers := make(map[string][]byte, len(t.v2LayerByRoot))
	for key, lf := range t.v2LayerByRoot {
		tree := merkle.NewLayerTree(lf.level, layerHashes(lf.data, lf.numPieces))
		if tree == nil || tree.Root() != lf.root {
			t.stop(errors.New("fetched piece layers do not match the pieces root"))
			return
		}
		layers[key] = lf.data
	}
	if err := t.info().AttachV2Pieces(layers); err != nil {
		t.stop(err)
		return
	}
	if len(layers) > 0 {
		t.pieceLayers = layers
		if encoded, err := metainfo.EncodePieceLayers(layers); err == nil && encoded != nil {
			if werr := t.session.resumer.WritePieceLayers(t.id, encoded); werr != nil {
				t.log.Errorf("cannot write piece layers to resume db: %s", werr)
			}
		}
	}
	if t.stopAfterMetadata {
		t.stopAndSetStoppedOnMetadata()
		return
	}
	if s := t.status(); s == Stopped || s == Stopping {
		return
	}
	t.startAllocator()
}

func layerHashes(data []byte, n int) [][merkle.HashSize]byte {
	out := make([][merkle.HashSize]byte, n)
	for i := 0; i < n; i++ {
		copy(out[i][:], data[i*merkle.HashSize:])
	}
	return out
}
