// bencode.go: a small, allocation-bounded BEP 44 request decoder.
//
// The DHT module otherwise relies on github.com/jackpal/bencode-go. Its
// Unmarshal panics ("reflect: reflect.Value.Set using unaddressable value")
// when a dictionary is decoded into a Go map with interface{} values that hold
// strings, which is exactly the shape of a BEP 44 "a" dictionary: every
// get/put query contains a string "target"/"token"/"id", so bencode-go would
// crash the whole node on any query. This decoder returns an interface{} tree
// (int64, string, []interface{}, map[string]interface{}) without reflection and
// with a hard depth bound, so hostile nesting cannot exhaust the stack.

package dht

import (
	"bytes"
	"errors"
	"io"
	"strconv"
)

// maxBencodeDepth bounds recursion so a deeply nested packet cannot overflow
// the stack. Real BEP 44 requests are only a few levels deep.
const maxBencodeDepth = 32

// decodeBencode decodes one bencoded value into an interface{} tree. Trailing
// bytes are ignored; malformed input returns an error instead of panicking.
func decodeBencode(data []byte) (interface{}, error) {
	p := &bencodeReader{b: data}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// validBencode reports whether data holds one well-formed bencode value. It
// validates every declared string length against the buffer and bounds the
// recursion, without allocating: it is the cheap screen used before handing a
// packet to a decoder that trusts the declared lengths (bencode-go allocates a
// string of its declared length before checking it, so a hostile packet like
// "d999999999:d" would OOM the node).
func validBencode(data []byte) error {
	p := &bencodeReader{b: data}
	if err := p.skip(0); err != nil {
		return err
	}
	return nil
}

func (p *bencodeReader) skip(depth int) error {
	if depth > maxBencodeDepth {
		return errors.New("bencode: nesting too deep")
	}
	if p.i >= len(p.b) {
		return io.ErrUnexpectedEOF
	}
	switch c := p.b[p.i]; c {
	case 'i':
		return p.skipInteger()
	case 'l':
		p.i++
		for {
			if p.i >= len(p.b) {
				return io.ErrUnexpectedEOF
			}
			if p.b[p.i] == 'e' {
				p.i++
				return nil
			}
			if err := p.skip(depth + 1); err != nil {
				return err
			}
		}
	case 'd':
		p.i++
		for {
			if p.i >= len(p.b) {
				return io.ErrUnexpectedEOF
			}
			if p.b[p.i] == 'e' {
				p.i++
				return nil
			}
			if err := p.skipString(); err != nil { // key
				return err
			}
			if err := p.skip(depth + 1); err != nil {
				return err
			}
		}
	default:
		return p.skipString()
	}
}

func (p *bencodeReader) skipInteger() error {
	p.i++ // 'i'
	start := p.i
	for p.i < len(p.b) && p.b[p.i] != 'e' {
		p.i++
	}
	if p.i >= len(p.b) {
		return io.ErrUnexpectedEOF
	}
	if p.i == start {
		return errors.New("bencode: empty integer")
	}
	p.i++ // 'e'
	return nil
}

// skipString validates a bencode byte string without materializing it: the
// length prefix is parsed by hand and clamped to the buffer, so a huge declared
// length is rejected instead of allocating.
func (p *bencodeReader) skipString() error {
	start := p.i
	for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
		p.i++
	}
	if p.i == start || p.i >= len(p.b) || p.b[p.i] != ':' {
		return errors.New("bencode: invalid string length")
	}
	n := 0
	for _, c := range p.b[start:p.i] {
		n = n*10 + int(c-'0')
		if n > len(p.b) {
			return errors.New("bencode: string length exceeds buffer")
		}
	}
	p.i++ // ':'
	if n > len(p.b)-p.i {
		return io.ErrUnexpectedEOF
	}
	p.i += n
	return nil
}

type bencodeReader struct {
	b []byte
	i int
}

func (p *bencodeReader) value(depth int) (interface{}, error) {
	if depth > maxBencodeDepth {
		return nil, errors.New("bencode: nesting too deep")
	}
	if p.i >= len(p.b) {
		return nil, io.ErrUnexpectedEOF
	}
	switch c := p.b[p.i]; c {
	case 'i':
		return p.integer()
	case 'l':
		return p.list(depth)
	case 'd':
		return p.dict(depth)
	default:
		return p.str()
	}
}

func (p *bencodeReader) integer() (int64, error) {
	p.i++ // 'i'
	j := bytes.IndexByte(p.b[p.i:], 'e')
	if j < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := strconv.ParseInt(string(p.b[p.i:p.i+j]), 10, 64)
	if err != nil {
		return 0, err
	}
	p.i += j + 1
	return n, nil
}

func (p *bencodeReader) str() (string, error) {
	j := bytes.IndexByte(p.b[p.i:], ':')
	if j < 0 {
		return "", io.ErrUnexpectedEOF
	}
	n, err := strconv.Atoi(string(p.b[p.i : p.i+j]))
	if err != nil || n < 0 {
		return "", errors.New("bencode: invalid string length")
	}
	p.i += j + 1
	if n > len(p.b)-p.i {
		return "", io.ErrUnexpectedEOF
	}
	s := string(p.b[p.i : p.i+n])
	p.i += n
	return s, nil
}

func (p *bencodeReader) list(depth int) ([]interface{}, error) {
	p.i++ // 'l'
	out := []interface{}{}
	for {
		if p.i >= len(p.b) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.b[p.i] == 'e' {
			p.i++
			return out, nil
		}
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

func (p *bencodeReader) dict(depth int) (map[string]interface{}, error) {
	p.i++ // 'd'
	out := map[string]interface{}{}
	for {
		if p.i >= len(p.b) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.b[p.i] == 'e' {
			p.i++
			return out, nil
		}
		k, err := p.str()
		if err != nil {
			return nil, err
		}
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
}

// bep44RequestDecode decodes a BEP 44 get/put query without bencode-go. It
// mirrors the fields of bep44Request: the transaction id and the "a" args map.
func bep44RequestDecode(raw []byte) (*bep44Request, error) {
	top, err := decodeBencode(raw)
	if err != nil {
		return nil, err
	}
	m, ok := top.(map[string]interface{})
	if !ok {
		return nil, errors.New("bencode: not a dictionary")
	}
	req := &bep44Request{}
	if t, ok := m["t"].(string); ok {
		req.T = t
	}
	if a, ok := m["a"].(map[string]interface{}); ok {
		req.A = a
	}
	return req, nil
}
