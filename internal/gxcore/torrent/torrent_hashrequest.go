package torrent

import (
	"crypto/sha1"
	"crypto/sha256"
	"errors"

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

// v2LayerFile tracks the download of one file's piece layer.
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

	next          int // next padded index to request
	inflightIndex int // index of the request in flight (-1 = none)
	inflightCount int
	inflightProof int
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
	t.v2Pending = make(map[*peer.Peer]*v2LayerFile)
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
		lf := &v2LayerFile{
			root:          f.PiecesRoot,
			fileIdx:       fi,
			numPieces:     numPieces,
			padded:        paddedLeaves >> level,
			paddedLeaves:  paddedLeaves,
			level:         level,
			data:          make([]byte, numPieces*merkle.HashSize),
			inflightIndex: -1,
		}
		t.v2LayerFiles = append(t.v2LayerFiles, lf)
		t.v2LayerByRoot[string(f.PiecesRoot[:])] = lf
	}
}

// nextV2LayerFile returns the first file whose layer still needs a request.
func (t *torrent) nextV2LayerFile() *v2LayerFile {
	for _, lf := range t.v2LayerFiles {
		if lf.next < lf.padded && lf.inflightIndex < 0 {
			return lf
		}
	}
	return nil
}

// startV2LayerDownload issues hash requests for the piece layers still missing.
// It runs on the torrent loop, the only goroutine that touches this state.
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
	for pe := range t.peers {
		if _, ok := t.v2Pending[pe]; ok {
			continue
		}
		if _, bad := t.v2NoHashPeers[pe]; bad {
			continue
		}
		lf := t.nextV2LayerFile()
		if lf == nil {
			return
		}
		count := lf.padded
		if count > v2ChunkMax {
			count = v2ChunkMax
		}
		if count < 1 {
			continue
		}
		proofLayers := merkle.Log2(lf.paddedLeaves) - lf.level - 1
		if proofLayers < 0 {
			proofLayers = 0
		}
		hdr := peerprotocol.HashRequestHeader{
			Root:        lf.root,
			Base:        uint32(lf.level),
			Index:       uint32(lf.next),
			Length:      uint32(count),
			ProofLayers: uint32(proofLayers),
		}
		lf.inflightIndex = lf.next
		lf.inflightCount = count
		lf.inflightProof = proofLayers
		t.v2Pending[pe] = lf
		pe.SendMessage(peerprotocol.HashRequestMessage{HashRequestHeader: hdr})
	}
}

// handleHashes verifies a "hashes" reply and stores the requested layer range.
func (t *torrent) handleHashes(pe *peer.Peer, msg peerprotocol.HashesMessage) {
	lf, ok := t.v2Pending[pe]
	if !ok {
		return
	}
	delete(t.v2Pending, pe)
	if lf.inflightIndex < 0 {
		return
	}
	index, count := lf.inflightIndex, lf.inflightCount
	expected := peerprotocol.HashRequestHeader{
		Root:        lf.root,
		Base:        uint32(lf.level),
		Index:       uint32(index),
		Length:      uint32(count),
		ProofLayers: uint32(lf.inflightProof),
	}
	if msg.HashRequestHeader != expected {
		// Not a reply to our request; leave the request in flight.
		lf.inflightIndex = -1
		t.startV2LayerDownload()
		return
	}
	uncles := lf.inflightProof - merkle.Log2(count) + 1
	if uncles < 0 {
		uncles = 0
	}
	if len(msg.Hashes) != count+uncles {
		t.v2LayerFailed(pe, lf)
		return
	}
	if !merkle.VerifyHashes(lf.root, lf.paddedLeaves, lf.level, index, count, msg.Hashes[:count], msg.Hashes[count:]) {
		// A peer that serves a hash range not anchored to the file's pieces
		// root is dropped; another peer retries the same range.
		t.closePeer(pe)
		t.v2LayerFailed(pe, lf)
		return
	}
	for i := 0; i < count; i++ {
		p := index + i
		if p >= lf.numPieces {
			break
		}
		copy(lf.data[p*merkle.HashSize:], msg.Hashes[i][:])
	}
	lf.next = index + count
	lf.inflightIndex = -1
	if t.v2LayersComplete() {
		t.finishV2Layers()
		return
	}
	t.startV2LayerDownload()
}

// handleHashReject marks a peer as unable to serve hash requests and retries
// the request elsewhere.
func (t *torrent) handleHashReject(pe *peer.Peer, msg peerprotocol.HashRejectMessage) {
	lf, ok := t.v2Pending[pe]
	if !ok {
		return
	}
	delete(t.v2Pending, pe)
	if lf != nil && lf.inflightIndex >= 0 && msg.Root == lf.root {
		lf.inflightIndex = -1
	}
	if t.v2NoHashPeers == nil {
		t.v2NoHashPeers = make(map[*peer.Peer]struct{})
	}
	t.v2NoHashPeers[pe] = struct{}{}
	t.startV2LayerDownload()
}

// v2LayerFailed resets the in-flight request so another peer can retry it.
func (t *torrent) v2LayerFailed(pe *peer.Peer, lf *v2LayerFile) {
	if lf != nil {
		lf.inflightIndex = -1
	}
	if t.v2NoHashPeers == nil {
		t.v2NoHashPeers = make(map[*peer.Peer]struct{})
	}
	t.v2NoHashPeers[pe] = struct{}{}
	t.startV2LayerDownload()
}

func (t *torrent) v2LayersComplete() bool {
	if len(t.v2LayerFiles) == 0 {
		return true
	}
	for _, lf := range t.v2LayerFiles {
		if lf.next < lf.padded {
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
