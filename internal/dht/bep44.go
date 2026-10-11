package dht

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha1"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	bencode "github.com/jackpal/bencode-go"
)

// BEP 44: storing arbitrary data in the DHT. Immutable items are keyed by the
// SHA-1 of their bencoded value; mutable items by the SHA-1 of an ed25519 public
// key and an optional salt, with a monotonic sequence number and a signature.
//
// https://www.bittorrent.org/beps/bep_0044.html

const (
	bep44MaxValueBytes = 1000
	bep44MaxSaltBytes  = 64
	// bep44ItemTTL is how long an item is kept without being re-announced.
	bep44ItemTTL = 2 * time.Hour
	// bep44MaxItems bounds the store: a remote node with a valid token can put
	// arbitrary targets, and without a cap the map would grow forever, since
	// expired items are otherwise only noticed when they are read back.
	bep44MaxItems = 2048
)

var errItemTooBig = errors.New("bep44: value too big")

// StoredItem is one BEP 44 item hosted by this node.
type StoredItem struct {
	Target [20]byte
	// Value is the decoded bencoded value; it is re-encoded canonically when
	// stored/returned, and (for mutable items) when verified.
	Value     interface{}
	Seq       int64
	PublicKey []byte
	Signature []byte
	expiry    time.Time
}

// Mutable reports whether the item is mutable (signed).
func (it *StoredItem) Mutable() bool { return len(it.PublicKey) > 0 }

// itemStore holds the BEP 44 items, keyed by target.
type itemStore struct {
	mu    sync.Mutex
	items map[[20]byte]*StoredItem
}

func newItemStore() *itemStore { return &itemStore{items: make(map[[20]byte]*StoredItem)} }

func (s *itemStore) get(target [20]byte) *StoredItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.items[target]
	if it == nil {
		return nil
	}
	if time.Now().After(it.expiry) {
		delete(s.items, target)
		return nil
	}
	return it
}

func (s *itemStore) put(it *StoredItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneLocked(now)
	// A new target must not push the store past the cap: evict the item that
	// expires soonest (all entries carry the same TTL, so this is the least
	// recently stored one).
	if _, exists := s.items[it.Target]; !exists {
		for len(s.items) >= bep44MaxItems {
			oldest := [20]byte{}
			var oldestExpiry time.Time
			first := true
			for target, entry := range s.items {
				if first || entry.expiry.Before(oldestExpiry) {
					oldest, oldestExpiry, first = target, entry.expiry, false
				}
			}
			if first {
				break
			}
			delete(s.items, oldest)
		}
	}
	it.expiry = now.Add(bep44ItemTTL)
	s.items[it.Target] = it
}

// pruneLocked drops the entries whose TTL has passed. The caller holds s.mu.
func (s *itemStore) pruneLocked(now time.Time) {
	for target, entry := range s.items {
		if now.After(entry.expiry) {
			delete(s.items, target)
		}
	}
}

// ImmutableTarget is the DHT key of an immutable item: the SHA-1 of its
// bencoded value.
func ImmutableTarget(value []byte) [20]byte { return sha1.Sum(value) }

// MutableTarget is the DHT key of a mutable item: the SHA-1 of the ed25519
// public key concatenated with the salt.
func MutableTarget(publicKey, salt []byte) [20]byte {
	h := sha1.New()
	h.Write(publicKey)
	h.Write(salt)
	var t [20]byte
	copy(t[:], h.Sum(nil))
	return t
}

// signBuffer builds the BEP 44 buffer signed or verified for a mutable item
// (salt, if non-empty, then seq and the encoded value).
func signBuffer(seq int64, value, salt []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(value) + len(salt) + 24)
	if len(salt) > 0 {
		b.WriteString("4:salt")
		b.WriteString(strconv.Itoa(len(salt)))
		b.WriteByte(':')
		b.Write(salt)
	}
	b.WriteString("3:seqi")
	b.WriteString(strconv.FormatInt(seq, 10))
	b.WriteString("e1:v")
	// value is already the bencoded form (e.g. "12:Hello World!"), so it is
	// appended verbatim, as the "1:v" key's bencoded value.
	b.Write(value)
	return b.Bytes()
}

// SignMutable signs a mutable item with an ed25519 private key (BEP 44).
func SignMutable(priv ed25519.PrivateKey, seq int64, value, salt []byte) []byte {
	return ed25519.Sign(priv, signBuffer(seq, value, salt))
}

// VerifyMutable verifies a mutable item signature (BEP 44). The encoded value is
// the canonical bencoded form used for hashing.
func VerifyMutable(publicKey, value, salt, signature []byte, seq int64) bool {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(publicKey), signBuffer(seq, value, salt), signature)
}

// PutImmutable stores an immutable item locally and returns its target.
func (d *DHT) PutImmutable(v interface{}) ([20]byte, error) {
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, v); err != nil {
		return [20]byte{}, err
	}
	encoded := buf.Bytes()
	if len(encoded) > bep44MaxValueBytes {
		return [20]byte{}, errItemTooBig
	}
	it := &StoredItem{Target: ImmutableTarget(encoded), Value: v}
	d.items.put(it)
	return it.Target, nil
}

// PutMutable stores a signed mutable item locally and returns its target.
func (d *DHT) PutMutable(priv ed25519.PrivateKey, seq int64, v interface{}, salt []byte) ([20]byte, error) {
	if len(salt) > bep44MaxSaltBytes {
		return [20]byte{}, errors.New("bep44: salt too big")
	}
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, v); err != nil {
		return [20]byte{}, err
	}
	encoded := buf.Bytes()
	if len(encoded) > bep44MaxValueBytes {
		return [20]byte{}, errItemTooBig
	}
	pub := priv.Public().(ed25519.PublicKey)
	it := &StoredItem{
		Target:    MutableTarget(pub, salt),
		Value:     v,
		Seq:       seq,
		PublicKey: pub,
		Signature: SignMutable(priv, seq, encoded, salt),
	}
	d.items.put(it)
	return it.Target, nil
}

// Get returns the locally stored item for target, or nil if absent/expired.
func (d *DHT) Get(target [20]byte) *StoredItem { return d.items.get(target) }

// bep44Request is the decoded "a" dictionary of a get/put query.
type bep44Request struct {
	T string                 `bencode:"t"`
	A map[string]interface{} `bencode:"a"`
}

// errorMessage is a KRPC error reply (BEP 5).
type errorMessage struct {
	T string        `bencode:"t"`
	Y string        `bencode:"y"`
	E []interface{} `bencode:"e"`
}

func mapString(m map[string]interface{}, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func mapInt(m map[string]interface{}, key string) int64 {
	if n, ok := m[key].(int64); ok {
		return n
	}
	return 0
}

func (d *DHT) replyError(addr net.UDPAddr, t string, code int, msg string) {
	sendMsg(d.conn, addr, errorMessage{T: t, Y: "e", E: []interface{}{code, msg}}, d.DebugLogger)
}

// replyPut handles a BEP 44 "put": it validates the token, the salt, the size
// and (for mutable items) the signature and the sequence number, then stores it.
func (d *DHT) replyPut(addr net.UDPAddr, raw []byte) {
	req, err := bep44RequestDecode(raw)
	if err != nil {
		return
	}
	if !d.checkToken(addr, nil, mapString(req.A, "token")) {
		d.replyError(addr, req.T, 203, "invalid token")
		return
	}
	v, ok := req.A["v"]
	if !ok {
		d.replyError(addr, req.T, 203, "missing value")
		return
	}
	var valueBuf bytes.Buffer
	if err := bencode.Marshal(&valueBuf, v); err != nil || valueBuf.Len() > bep44MaxValueBytes {
		d.replyError(addr, req.T, 205, "value too big")
		return
	}
	encoded := valueBuf.Bytes()
	salt := []byte(mapString(req.A, "salt"))
	if len(salt) > bep44MaxSaltBytes {
		d.replyError(addr, req.T, 207, "salt too big")
		return
	}

	item := &StoredItem{Value: v}
	if k := mapString(req.A, "k"); k != "" {
		pub := []byte(k)
		sig := []byte(mapString(req.A, "sig"))
		seq := mapInt(req.A, "seq")
		if seq < 0 {
			d.replyError(addr, req.T, 203, "negative sequence number")
			return
		}
		if !VerifyMutable(pub, encoded, salt, sig, seq) {
			d.replyError(addr, req.T, 206, "invalid signature")
			return
		}
		item.PublicKey, item.Signature, item.Seq = pub, sig, seq
		item.Target = MutableTarget(pub, salt)
		if cur := d.items.get(item.Target); cur != nil && cur.Mutable() {
			if _, hasCas := req.A["cas"]; hasCas && mapInt(req.A, "cas") != cur.Seq {
				d.replyError(addr, req.T, 301, "cas mismatch")
				return
			}
			if seq < cur.Seq {
				d.replyError(addr, req.T, 302, "sequence number less than current")
				return
			}
		}
	} else {
		item.Target = ImmutableTarget(encoded)
	}
	d.items.put(item)
	sendMsg(d.conn, addr, replyMessage{T: req.T, Y: "r", R: map[string]interface{}{"id": d.nodeId}}, d.DebugLogger)
}

// replyGet handles a BEP 44 "get": it returns the stored item (the optional seq
// suppresses an unchanged mutable value), a write token and the close nodes.
func (d *DHT) replyGet(addr net.UDPAddr, raw []byte) {
	req, err := bep44RequestDecode(raw)
	if err != nil {
		return
	}
	target := []byte(mapString(req.A, "target"))
	if len(target) != 20 {
		return
	}
	r0 := map[string]interface{}{"id": d.nodeId, "token": d.hostToken(addr, nil, d.tokenSecrets[0])}
	if n4, n6 := d.nodesForInfoHash(InfoHash(string(target))); n4 != "" || n6 != "" {
		if n4 != "" {
			r0["nodes"] = n4
		}
		if n6 != "" {
			r0["nodes6"] = n6
		}
	}
	var key [20]byte
	copy(key[:], target)
	if it := d.items.get(key); it != nil {
		_, hasSeq := req.A["seq"]
		unchanged := it.Mutable() && hasSeq && it.Seq <= mapInt(req.A, "seq")
		if !unchanged {
			r0["v"] = it.Value
			if it.Mutable() {
				r0["k"] = string(it.PublicKey)
				r0["sig"] = string(it.Signature)
				r0["seq"] = it.Seq
			}
		}
	}
	sendMsg(d.conn, addr, replyMessage{T: req.T, Y: "r", R: r0}, d.DebugLogger)
}

// putQuery sends a BEP 44 put to a node (used to publish an item to the closest
// nodes found by a lookup).
func (d *DHT) putQuery(r *remoteNode, args map[string]interface{}) {
	transId := r.newQuery("put")
	query := queryMessage{transId, "q", "put", args, d.readOnlyFlag()}
	sendMsg(d.conn, r.address, query, d.DebugLogger)
}

// getQuery sends a BEP 44 get to a node.
func (d *DHT) getQuery(r *remoteNode, target [20]byte, seq *int64) {
	transId := r.newQuery("get")
	args := map[string]interface{}{"id": d.nodeId, "target": string(target[:])}
	if seq != nil {
		args["seq"] = *seq
	}
	query := queryMessage{transId, "q", "get", args, d.readOnlyFlag()}
	sendMsg(d.conn, r.address, query, d.DebugLogger)
}
