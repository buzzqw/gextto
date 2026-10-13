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
}

// New binds pi to the cache. Disk is read in readSize chunks; peerID scopes
// the cache keys to the requesting peer.
func New(pi *piece.Piece, cache *piececache.Cache, readSize int64, peerID [20]byte) *CachedPiece {
	return &CachedPiece{pi: pi, cache: cache, readSize: readSize, peerID: peerID[:]}
}

// cacheKey identifies one cached block: peer + piece index + block number.
func (c *CachedPiece) cacheKey(block uint32) string {
	key := make([]byte, 20+4+4)
	copy(key, c.peerID)
	binary.BigEndian.PutUint32(key[20:24], c.pi.Index)
	binary.BigEndian.PutUint32(key[24:28], block)
	return string(key)
}

// ReadAt implements io.ReaderAt. Only the block containing off is loaded;
// the requested span is then copied out of it.
func (c *CachedPiece) ReadAt(p []byte, off int64) (n int, err error) {
	block := uint32(off / c.readSize)
	blockBegin := int64(block) * c.readSize
	blockEnd := min(blockBegin+c.readSize, int64(c.pi.Length))

	buf, err := c.cache.Get(c.cacheKey(block), func() ([]byte, error) {
		b := make([]byte, blockEnd-blockBegin)
		_, err := c.pi.Data.ReadAt(b, blockBegin)
		return b, err
	})
	if err != nil {
		return 0, err
	}
	return copy(p, buf[off-blockBegin:]), nil
}
