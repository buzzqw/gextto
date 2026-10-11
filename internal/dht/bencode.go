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
