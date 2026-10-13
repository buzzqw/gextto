// Package bencodecheck screens bencoded bytes against the decoder used across
// the engine (zeebo/bencode), which allocates a string's declared length
// before reading it. A small hostile input — a crafted .torrent, a peer
// extension message, a tracker reply — can declare a gigabyte and starve the
// daemon, so the declared lengths are checked against the bytes actually
// present before the decoder is handed the data.
package bencodecheck

import (
	"errors"
	"io"

	"github.com/zeebo/bencode"
)

// MaxDepth bounds nesting while scanning; real protocol messages stay shallow.
const MaxDepth = 128

// Validate walks the leading bencoded value and rejects string lengths larger
// than the bytes actually present, plus nesting deeper than MaxDepth. It
// accepts the same grammar as the decoder, does not enforce dict key ordering,
// and ignores any trailing bytes (so it never rejects what the decoder reads).
func Validate(data []byte) error {
	_, err := scanValue(data, 0, 0)
	return err
}

// DecodeBytes validates data and then decodes it with zeebo/bencode, so a
// declared string length beyond the input is refused before the decoder
// allocates it.
func DecodeBytes(data []byte, v any) error {
	if err := Validate(data); err != nil {
		return err
	}
	return bencode.DecodeBytes(data, v)
}

func scanValue(data []byte, i, depth int) (int, error) {
	if depth > MaxDepth {
		return 0, errors.New("bencode nested too deeply")
	}
	if i >= len(data) {
		return 0, io.ErrUnexpectedEOF
	}
	switch c := data[i]; {
	case c == 'i':
		j := i + 1
		for j < len(data) && data[j] != 'e' {
			j++
		}
		if j >= len(data) {
			return 0, io.ErrUnexpectedEOF
		}
		return j + 1, nil
	case c >= '0' && c <= '9':
		j := i
		var length uint64
		for j < len(data) && data[j] >= '0' && data[j] <= '9' {
			length = length*10 + uint64(data[j]-'0')
			if length > uint64(len(data)) { // also bounds the accumulator
				return 0, errors.New("bencode string length exceeds input")
			}
			j++
		}
		if j >= len(data) || data[j] != ':' {
			return 0, errors.New("invalid bencode string")
		}
		j++
		if length > uint64(len(data)-j) {
			return 0, io.ErrUnexpectedEOF
		}
		return j + int(length), nil
	case c == 'l':
		i++
		for {
			if i < len(data) && data[i] == 'e' {
				return i + 1, nil
			}
			n, err := scanValue(data, i, depth+1)
			if err != nil {
				return 0, err
			}
			i = n
		}
	case c == 'd':
		i++
		for {
			if i < len(data) && data[i] == 'e' {
				return i + 1, nil
			}
			n, err := scanValue(data, i, depth+1) // key
			if err != nil {
				return 0, err
			}
			n, err = scanValue(data, n, depth+1) // value
			if err != nil {
				return 0, err
			}
			i = n
		}
	default:
		return 0, errors.New("invalid bencode")
	}
}
