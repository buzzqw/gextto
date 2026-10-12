package dht

// bep44_client.go is the BEP 44 outbound client: it publishes (put) and fetches
// (get) immutable and mutable items on the closest nodes, with a bounded
// Kademlia traversal. The node already answers get/put as a server (bep44.go);
// this is the missing client side, needed by BEP 46 (magnet "xs=urn:btpk:").

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"sort"
	"strings"
	"time"

	bencode "github.com/jackpal/bencode-go"
)

// splitRouters returns the non-empty bootstrap routers from the config string.
func splitRouters(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

var errItemNotFound = errors.New("bep44: item not found")

const (
	// bep44LookupMaxNodes caps how many nodes one lookup contacts: a bounded
	// traversal cannot be turned into a network amplifier by a hostile reply.
	bep44LookupMaxNodes = 32
	// bep44LookupFanout is how many of the closest nodes seed a lookup.
	bep44LookupFanout = 8
	// bep44PutNodes is how many of the closest nodes receive a put (BEP 44:
	// the item is stored on the K closest nodes to its target).
	bep44PutNodes = 8
)

// bep44Token is a write token from a get reply, with the responder's node id
// (for the XOR distance to the target) and the node to send the put to.
type bep44Token struct {
	node  *remoteNode
	id    string
	token string
}

// bep44LookupRequest is one get or publish submitted to the run loop.
type bep44LookupRequest struct {
	target  [20]byte
	mutable bool
	pub     ed25519.PublicKey
	salt    []byte
	seq     *int64
	// publish, when set, is the item to store on the closest nodes; the
	// traversal collects the write tokens first with a get. Nil means a plain
	// fetch.
	publish *StoredItem
	timeout time.Duration
	result  chan bep44Result
}

type bep44Result struct {
	item *StoredItem
	err  error
}

// bep44Lookup is the run-loop state of one get/publish.
type bep44Lookup struct {
	id      uint64
	req     bep44LookupRequest
	queried map[string]bool       // node address -> already queried
	tokens  map[string]bep44Token // node address -> write token from a get reply
	best    *StoredItem
	sent    int
	putSent int
}

// GetImmutable fetches the immutable BEP 44 item whose target is given.
func (d *DHT) GetImmutable(target [20]byte, timeout time.Duration) (*StoredItem, error) {
	return d.bep44Lookup(bep44LookupRequest{target: target, timeout: timeout})
}

// GetMutable fetches a mutable BEP 44 item by its public key and salt. seq, when
// non-nil, is our known sequence (the responder may then answer without the
// value). The signature is verified against pub before the value is accepted.
func (d *DHT) GetMutable(pub ed25519.PublicKey, salt []byte, seq *int64, timeout time.Duration) (*StoredItem, error) {
	return d.bep44Lookup(bep44LookupRequest{
		target:  MutableTarget(pub, salt),
		mutable: true,
		pub:     pub,
		salt:    salt,
		seq:     seq,
		timeout: timeout,
	})
}

// PublishImmutable stores value on the closest nodes and returns its target.
func (d *DHT) PublishImmutable(value interface{}, timeout time.Duration) ([20]byte, error) {
	target, err := d.PutImmutable(value)
	if err != nil {
		return target, err
	}
	it := d.Get(target)
	if it == nil {
		return target, errors.New("bep44: local store lost the item")
	}
	_, err = d.bep44Lookup(bep44LookupRequest{target: target, publish: it, timeout: timeout})
	return target, err
}

// PublishMutable stores a signed mutable item on the closest nodes and returns
// its target. The item is also kept locally so it can be refreshed.
func (d *DHT) PublishMutable(priv ed25519.PrivateKey, seq int64, value interface{}, salt []byte, timeout time.Duration) ([20]byte, error) {
	target, err := d.PutMutable(priv, seq, value, salt)
	if err != nil {
		return target, err
	}
	pub, _ := priv.Public().(ed25519.PublicKey)
	it := d.Get(target)
	if it == nil {
		return target, errors.New("bep44: local store lost the item")
	}
	_, err = d.bep44Lookup(bep44LookupRequest{
		target: target, mutable: true, pub: pub, salt: salt, publish: it, timeout: timeout,
	})
	return target, err
}

// bep44Lookup submits req to the run loop and waits for the result.
func (d *DHT) bep44Lookup(req bep44LookupRequest) (*StoredItem, error) {
	if req.timeout <= 0 {
		req.timeout = 5 * time.Second
	}
	result := make(chan bep44Result, 1)
	req.result = result
	select {
	case d.bep44Request <- req:
	case <-d.stop:
		return nil, errors.New("dht: stopped")
	}
	select {
	case res := <-result:
		return res.item, res.err
	case <-time.After(req.timeout + 2*time.Second):
		return nil, errors.New("dht: bep44 timeout")
	}
}

// startBep44 is called in the run loop: it seeds the lookup with the closest
// nodes and schedules its end.
func (d *DHT) startBep44(req bep44LookupRequest) {
	d.bep44Seq++
	lk := &bep44Lookup{
		id:      d.bep44Seq,
		req:     req,
		queried: map[string]bool{},
		tokens:  map[string]bep44Token{},
	}
	d.bep44s[lk.id] = lk

	closest := d.routingTable.lookupFiltered(InfoHash(string(req.target[:])))
	for i, r := range closest {
		if i >= bep44LookupFanout {
			break
		}
		d.bep44SendGet(lk, r)
	}
	if len(closest) == 0 {
		// Fall back to the bootstrap routers.
		for _, s := range splitRouters(d.config.DHTRouters) {
			if r, err := d.routingTable.getOrCreateNode("", s, d.config.UDPProto); err == nil {
				d.bep44SendGet(lk, r)
			}
		}
	}

	go func() {
		timer := time.NewTimer(req.timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-d.stop:
		}
		select {
		case d.bep44Done <- lk.id:
		case <-d.stop:
		}
	}()
}

// finishBep44 is called in the run loop when the lookup window closes.
func (d *DHT) finishBep44(id uint64) {
	lk, ok := d.bep44s[id]
	if !ok {
		return
	}
	delete(d.bep44s, id)
	if lk.req.publish != nil {
		d.bep44PutClosest(lk)
		if lk.putSent == 0 {
			lk.req.result <- bep44Result{err: errors.New("bep44: no node accepted the put")}
			return
		}
		lk.req.result <- bep44Result{item: lk.req.publish}
		return
	}
	if lk.best == nil {
		lk.req.result <- bep44Result{err: errItemNotFound}
		return
	}
	lk.req.result <- bep44Result{item: lk.best}
}

// bep44SendGet sends a "get" to one node for the lookup's target.
func (d *DHT) bep44SendGet(lk *bep44Lookup, r *remoteNode) {
	if r == nil {
		return
	}
	key := r.address.String()
	if lk.queried[key] || lk.sent >= bep44LookupMaxNodes {
		return
	}
	lk.queried[key] = true
	lk.sent++
	transId := r.newQuery("get")
	if q, ok := r.pendingQueries[transId]; ok {
		q.bep44 = lk.id
	}
	args := map[string]interface{}{"id": d.nodeId, "target": string(lk.req.target[:])}
	if lk.req.mutable && lk.req.seq != nil {
		args["seq"] = *lk.req.seq
	}
	r.lastSearchTime = time.Now()
	sendMsg(d.conn, r.address, queryMessage{transId, "q", "get", args, d.readOnlyFlag()}, d.DebugLogger)
}

// bep44SendPut sends a "put" to one node with the token from its get reply.
func (d *DHT) bep44SendPut(lk *bep44Lookup, r *remoteNode, token string) {
	it := lk.req.publish
	if it == nil || token == "" {
		return
	}
	lk.putSent++
	transId := r.newQuery("put")
	if q, ok := r.pendingQueries[transId]; ok {
		q.bep44 = lk.id
	}
	args := map[string]interface{}{"id": d.nodeId, "token": token, "v": it.Value}
	if it.Mutable() {
		args["k"] = string(it.PublicKey)
		args["sig"] = string(it.Signature)
		args["seq"] = it.Seq
		if len(lk.req.salt) > 0 {
			args["salt"] = string(lk.req.salt)
		}
	}
	sendMsg(d.conn, r.address, queryMessage{transId, "q", "put", args, d.readOnlyFlag()}, d.DebugLogger)
}

// handleBep44Reply processes a get/put reply inside the run loop: it records the
// value and the write token, publishes when asked, and continues the traversal
// from the closest nodes the responder returned.
func (d *DHT) handleBep44Reply(node *remoteNode, p packetType, q *queryType) {
	lk, ok := d.bep44s[q.bep44]
	if !ok {
		return
	}
	// The reply dictionary may carry a value the generic struct decoder drops,
	// so it is decoded here with the panic-free decoder from bencode.go.
	top, err := decodeBencode(p.b)
	if err != nil {
		return
	}
	m, ok := top.(map[string]interface{})
	if !ok {
		return
	}
	rmap, ok := m["r"].(map[string]interface{})
	if !ok {
		return
	}
	if q.Type != "get" {
		return
	}

	if token, ok := rmap["token"].(string); ok && token != "" {
		nodeID, _ := rmap["id"].(string)
		lk.tokens[node.address.String()] = bep44Token{node: node, id: nodeID, token: token}
	}
	if v, ok := rmap["v"]; ok {
		if it := d.bep44Accept(lk, rmap, v); it != nil {
			if lk.best == nil || !lk.best.Mutable() || it.Seq >= lk.best.Seq {
				lk.best = it
			}
		}
	}
	d.bep44Traverse(lk, rmap, "nodes", "udp4")
	d.bep44Traverse(lk, rmap, "nodes6", "udp6")
}

// bep44PutClosest sends the put of a publish to the bep44PutNodes nodes closest
// to the target among those that gave a write token. The puts wait for the
// traversal to end: putting on every node met on the way would store the item
// far from where it is looked up and waste traffic.
func (d *DHT) bep44PutClosest(lk *bep44Lookup) {
	candidates := make([]bep44Token, 0, len(lk.tokens))
	for _, tok := range lk.tokens {
		if len(tok.id) == 20 {
			candidates = append(candidates, tok)
		}
	}
	target := InfoHash(string(lk.req.target[:]))
	sort.Slice(candidates, func(i, j int) bool {
		return hashDistance(InfoHash(candidates[i].id), target) < hashDistance(InfoHash(candidates[j].id), target)
	})
	for i, tok := range candidates {
		if i >= bep44PutNodes {
			break
		}
		d.bep44SendPut(lk, tok.node, tok.token)
	}
}

// bep44Traverse sends a get to the nodes a reply returned.
func (d *DHT) bep44Traverse(lk *bep44Lookup, rmap map[string]interface{}, field, proto string) {
	raw, ok := rmap[field].(string)
	if !ok || raw == "" {
		return
	}
	for _, addr := range parseNodesString(raw, proto, d.DebugLogger) {
		if r, err := d.routingTable.getOrCreateNode("", addr, proto); err == nil {
			d.bep44SendGet(lk, r)
		}
	}
}

// bep44Accept validates a value from a get reply and, when it is valid, builds
// the StoredItem to hand back to the caller.
func (d *DHT) bep44Accept(lk *bep44Lookup, rmap map[string]interface{}, v interface{}) *StoredItem {
	encoded, err := encodeBencodeValue(v)
	if err != nil {
		return nil
	}
	if !lk.req.mutable {
		if ImmutableTarget(encoded) != lk.req.target {
			return nil
		}
		return &StoredItem{Target: lk.req.target, Value: v}
	}
	k, _ := rmap["k"].(string)
	sig, _ := rmap["sig"].(string)
	seq, _ := rmap["seq"].(int64)
	if len(k) != ed25519.PublicKeySize || !bytes.Equal([]byte(k), lk.req.pub) {
		return nil
	}
	if !VerifyMutable([]byte(k), encoded, lk.req.salt, []byte(sig), seq) {
		return nil
	}
	return &StoredItem{
		Target:    lk.req.target,
		Value:     v,
		Seq:       seq,
		PublicKey: []byte(k),
		Signature: []byte(sig),
	}
}

// encodeBencodeValue returns the canonical bencoded form of a decoded value, the
// same bytes a BEP 44 signature covers.
func encodeBencodeValue(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
