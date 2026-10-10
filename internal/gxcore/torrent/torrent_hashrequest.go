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
	if t.info != nil {
		return t.info.HasV2
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

// handleHashRequest serves a BEP 52 hash request from the piece hashes we have.
func (t *torrent) handleHashRequest(pe *peer.Peer, msg peerprotocol.HashRequestMessage) {
	hashes, ok := t.serveHashes(msg.HashRequestHeader)
	if !ok {
		pe.SendMessage(peerprotocol.HashRejectMessage{HashRequestHeader: msg.HashRequestHeader})
		return
	}
	pe.SendMessage(peerprotocol.HashesMessage{HashRequestHeader: msg.HashRequestHeader, Hashes: hashes})
}

func (t *torrent) serveHashes(req peerprotocol.HashRequestHeader) ([][merkle.HashSize]byte, bool) {
	if t.info == nil || !t.info.V2 || !t.info.HasPieceHashes() {
		return nil, false
	}
	fi := -1
	for i, f := range t.info.V2Files {
		// Only a file larger than one piece has a piece layer to serve.
		if f.HasRoot && f.PiecesRoot == req.Root && f.Length > int64(t.info.PieceLength) {
			fi = i
			break
		}
	}
	if fi < 0 {
		return nil, false
	}
	tree := t.v2Tree(fi)
	if tree == nil {
		return nil, false
	}
	got := tree.GetHashes(int(req.Base), int(req.Index), int(req.Length), int(req.ProofLayers))
	if got == nil {
		return nil, false
	}
	return got, true
}

// v2Tree returns (and caches) the merkle tree at and above the piece layer for
// a v2 file, built from its attached piece hashes.
func (t *torrent) v2Tree(fi int) *merkle.LayerTree {
	f := t.info.V2Files[fi]
	key := string(f.PiecesRoot[:])
	if tree, ok := t.v2Trees[key]; ok {
		return tree
	}
	raw := t.info.V2FilePieces(fi)
	if len(raw) == 0 || len(raw)%merkle.HashSize != 0 {
		return nil
	}
	level := merkle.PieceLevel(int(t.info.PieceLength))
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
	level := merkle.PieceLevel(int(t.info.PieceLength))
	if level < 0 {
		return
	}
	for fi, f := range t.info.V2Files {
		if f.Length == 0 || !f.HasRoot || f.Length <= int64(t.info.PieceLength) {
			continue
		}
		numPieces := int((f.Length + int64(t.info.PieceLength) - 1) / int64(t.info.PieceLength))
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
	if t.info == nil || !t.info.V2 {
		return
	}
	if !t.info.NeedsV2Layers() {
		// Nothing to fetch: either the layers are already attached or every
		// file fits in one piece.
		if !t.info.HasPieceHashes() && t.v2LayerByRoot == nil {
			t.finishV2Layers()
		}
		return
	}
	if t.info.HasPieceHashes() {
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
	if t.info == nil || t.info.HasPieceHashes() {
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
	if err := t.info.AttachV2Pieces(layers); err != nil {
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
