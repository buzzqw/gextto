package torrent

import (
	"testing"

	"github.com/zeebo/bencode"
)

// pieceLimitSeriesPieces is the piece count of a real 4K season pack that
// Gextto used to refuse: "cannot parse info bytes: too many pieces: 370198".
// The metadata was genuine (it matched the info hash); only the 64 Ki piece
// ceiling was too low for a season of a hundred gigabytes and more.
const pieceLimitSeriesPieces = 370198

// pieceLimitInfoBytes bencodes a single-file info dict with the given number
// of 20-byte SHA-1 piece hashes.
func pieceLimitInfoBytes(t *testing.T, pieces uint32) []byte {
	t.Helper()
	hashes := make([]byte, int(pieces)*20)
	raw, err := bencode.EncodeBytes(map[string]interface{}{
		"name":         "Series.S01.2160p",
		"piece length": int64(512 << 10),
		"length":       int64(pieces) * (512 << 10),
		"pieces":       string(hashes),
	})
	if err != nil {
		t.Fatalf("encode info dict: %v", err)
	}
	return raw
}

// TestParseAcceptsALargePieceCount pins the ceiling of a real season pack: the
// metadata comes from the peers and matches the info hash, so refusing it made
// a legitimate download impossible.
func TestParseAcceptsALargePieceCount(t *testing.T) {
	raw := pieceLimitInfoBytes(t, pieceLimitSeriesPieces)
	session := &Session{config: DefaultConfig}
	info, err := session.parseInfo(raw, 1, nil)
	if err != nil {
		t.Fatalf("a %d-piece torrent was refused: %v", pieceLimitSeriesPieces, err)
	}
	if info.NumPieces != pieceLimitSeriesPieces {
		t.Fatalf("NumPieces = %d, want %d", info.NumPieces, pieceLimitSeriesPieces)
	}
}

// TestParseStillRefusesAbsurdPieceCounts keeps the guard: the ceiling exists
// to stop a hostile or broken record, not to allow any number.
func TestParseStillRefusesAbsurdPieceCounts(t *testing.T) {
	raw := pieceLimitInfoBytes(t, pieceLimitSeriesPieces)
	strict := &Session{config: Config{MaxPieces: pieceLimitSeriesPieces - 1}}
	if _, err := strict.parseInfo(raw, 1, nil); err == nil {
		t.Fatal("a torrent above the configured ceiling was accepted")
	}
	if DefaultConfig.MaxPieces <= pieceLimitSeriesPieces {
		t.Fatalf("the default ceiling (%d) is at or below a real season pack", DefaultConfig.MaxPieces)
	}
}
