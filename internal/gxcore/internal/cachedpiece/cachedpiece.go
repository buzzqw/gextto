package cachedpiece

import (
	"encoding/binary"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piececache"
)

// CachedPiece serves the blocks of one piece through the shared read cache,
// so repeated uploads of the same block hit memory instead of the disk.
type CachedPiece struct {
	pi       *piece.Piece
	cache    *piececache.Cache
	readSize int64
	peerID   []byte
	prefetch bool
}

// New binds pi to the cache. Disk is read in readSize chunks; peerID scopes
// the cache keys. prefetch loads the next block in the background: leave it off on
// a mapped file, because the load outlives the request and reading a mapping after
// the torrent closed it is not safe the way a read of a closed file is.
func New(pi *piece.Piece, cache *piececache.Cache, readSize int64, peerID [20]byte, prefetch bool) *CachedPiece {
	return &CachedPiece{pi: pi, cache: cache, readSize: readSize, peerID: peerID[:], prefetch: prefetch}
}

// cacheKey identifies one cached block: peer + piece index + block number.
func (c *CachedPiece) cacheKey(block uint32) string {
	key := make([]byte, 20+4+4)
	copy(key, c.peerID)
	binary.BigEndian.PutUint32(key[20:24], c.pi.Index)
	binary.BigEndian.PutUint32(key[24:28], block)
	return string(key)
}

// ReadAt implements io.ReaderAt. Only the block containing off is loaded; the
// requested span is then copied out of it. Starting a block also loads the next
// one of the piece in the background, so the disk works while this block goes out
// on the socket: serving a peer alternates the two otherwise, and each waits for
// the other.
func (c *CachedPiece) ReadAt(p []byte, off int64) (n int, err error) {
	block := uint32(off / c.readSize)
	blockBegin := int64(block) * c.readSize

	buf, err := c.cache.Get(c.cacheKey(block), c.loader(block))
	if err != nil {
		return 0, err
	}
	if c.prefetch && off == blockBegin {
		if next := blockBegin + c.readSize; next < int64(c.pi.Length) {
			c.cache.Prefetch(c.cacheKey(block+1), c.loader(block+1))
		}
	}
	return copy(p, buf[off-blockBegin:]), nil
}

// loader reads one block of the piece from storage.
func (c *CachedPiece) loader(block uint32) piececache.Loader {
	return func() ([]byte, error) {
		begin := int64(block) * c.readSize
		end := min(begin+c.readSize, int64(c.pi.Length))
		b := make([]byte, end-begin)
		_, err := c.pi.Data.ReadAt(b, begin)
		return b, err
	}
}
