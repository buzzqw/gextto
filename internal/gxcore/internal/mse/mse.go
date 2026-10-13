// Package mse implements Message Stream Encryption for peer connections:
// Diffie-Hellman key exchange plus RC4 obfuscation that defeats passive
// traffic identification. Per its design it hides the protocol, it does not
// authenticate peers — anyone with the info hash completes the handshake.
//
// Wire layout: http://wiki.vuze.com/w/Message_Stream_Encryption
package mse

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
)

const enableDebug = false

func debugln(args ...any) {
	if enableDebug {
		fmt.Println(args...)
	}
}

func debugf(format string, args ...any) {
	if enableDebug {
		fmt.Printf(format, args...)
	}
}

// dhPrime is the well-known 768-bit MODP prime every MSE handshake uses;
// dhGen is its generator 2. Both are protocol constants, not negotiable.
var (
	dhPrimeBytes = []byte{255, 255, 255, 255, 255, 255, 255, 255, 201, 15, 218, 162, 33, 104, 194, 52, 196, 198, 98, 139, 128, 220, 28, 209, 41, 2, 78, 8, 138, 103, 204, 116, 2, 11, 190, 166, 59, 19, 155, 34, 81, 74, 8, 121, 142, 52, 4, 221, 239, 149, 25, 179, 205, 58, 67, 27, 48, 43, 10, 109, 242, 95, 20, 55, 79, 225, 53, 109, 109, 81, 194, 69, 228, 133, 181, 118, 98, 94, 126, 198, 244, 76, 66, 233, 166, 58, 54, 33, 0, 0, 0, 0, 0, 9, 5, 99}
	dhPrime      = new(big.Int)
	dhGen        = big.NewInt(2)
	verifyConst  = make([]byte, 8)
)

func init() { dhPrime.SetBytes(dhPrimeBytes) }

// CryptoMethod is a bitfield of stream ciphers.
type CryptoMethod uint32

// Negotiable stream ciphers.
const (
	PlainText CryptoMethod = 1 << iota
	RC4
)

func (c CryptoMethod) String() string {
	switch c {
	case PlainText:
		return "PlainText"
	case RC4:
		return "RC4"
	default:
		return "unknown"
	}
}

// inPlaceStreamWriter encrypts into the caller's buffer: cipher.StreamWriter
// would allocate per Write, and the peer wire discards the buffer right
// after sending, so in-place is safe here.
type inPlaceStreamWriter struct {
	s cipher.Stream
	w io.Writer
}

func (w *inPlaceStreamWriter) Write(p []byte) (int, error) {
	w.s.XORKeyStream(p, p)
	return w.w.Write(p)
}

// Stream encrypts/decrypts a bidirectional connection once handshaked.
type Stream struct {
	raw io.ReadWriter
	r   *cipher.StreamReader
	w   *inPlaceStreamWriter
	r2  io.Reader
}

// NewStream wraps rw. Complete HandshakeIncoming or HandshakeOutgoing before
// Read/Write; handshake failures close rw when it implements io.Closer.
func NewStream(rw io.ReadWriter) *Stream { return &Stream{raw: rw} }

// Read decrypts into p.
func (s *Stream) Read(p []byte) (n int, err error) { return s.r2.Read(p) }

// Write encrypts p onto the wire.
func (s *Stream) Write(p []byte) (n int, err error) { return s.w.Write(p) }

// HandshakeOutgoing runs the initiator side: our DH public key, the peer's,
// the RC4 setup from the shared secret, our key exchange, then the peer's
// cipher selection. initialPayload rides encrypted after the exchange.
func (s *Stream) HandshakeOutgoing(sKey []byte, cryptoProvide CryptoMethod, initialPayload []byte) (selected CryptoMethod, err error) {
	if cryptoProvide == 0 {
		err = errors.New("no crypto methods are provided")
		return
	}
	if len(initialPayload) > math.MaxUint16 {
		err = errors.New("initial payload is too big")
		return
	}

	writeBuf := bytes.NewBuffer(make([]byte, 0, 96+512))

	Xa, Ya, err := keyPair()
	if err != nil {
		return
	}
	if err = sendPublicKey(s.raw, writeBuf, Ya, "out", "1"); err != nil {
		return
	}
	Yb, firstRead, err := readPeerPublicKey(s.raw, "out")
	if err != nil {
		return
	}
	S := sharedSecret(Yb, Xa)
	if err = s.initRC4("keyA", "keyB", S, sKey); err != nil {
		return
	}
	if err = s.sendKeyExchange(writeBuf, S, sKey, cryptoProvide, initialPayload); err != nil {
		return
	}
	selected, err = readCryptoSelect(s, firstRead, cryptoProvide)
	return
}

// HandshakeIncoming runs the responder side: the initiator's DH key, ours,
// the RC4 setup once its key hash resolves, the key exchange validation,
// then our cipher selection and the pending initial payload.
func (s *Stream) HandshakeIncoming(
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	cryptoSelect func(provided CryptoMethod) (selected CryptoMethod)) (err error) {
	writeBuf := bytes.NewBuffer(make([]byte, 0, 96+512))

	Xb, Yb, err := keyPair()
	if err != nil {
		return
	}
	Ya, firstRead, err := readPeerPublicKey(s.raw, "in")
	if err != nil {
		return
	}
	S := sharedSecret(Ya, Xb)
	if err = sendPublicKey(s.raw, writeBuf, Yb, "in", "2"); err != nil {
		return
	}
	if _, err := receiveKeyExchange(s, S, getSKey, firstRead); err != nil {
		return err
	}
	return answerCryptoSelect(s, writeBuf, cryptoSelect)
}

// sendPublicKey writes our DH public key (padded to 96 bytes) plus random pad.
func sendPublicKey(w io.Writer, writeBuf *bytes.Buffer, Y *big.Int, tag, step string) error {
	writeBuf.Write(bytesWithPad(Y))
	padA, err := padRandom()
	if err != nil {
		return err
	}
	writeBuf.Write(padA)
	debugln("--- " + tag + ": writing Step " + step)
	_, err = writeBuf.WriteTo(w)
	if err != nil {
		return err
	}
	debugln("--- " + tag + ": done")
	return nil
}

// readPeerPublicKey reads the peer's 96-byte DH key (plus whatever pad
// arrived in the same firstRead), returning the key and the byte count.
func readPeerPublicKey(r io.Reader, tag string) (*big.Int, int, error) {
	b := make([]byte, 96+512)
	if tag == "out" {
		debugln("--- out: reading PubkeyB")
	} else {
		debugln("--- in: read PubkeyA")
	}
	firstRead, err := io.ReadAtLeast(r, b, 96)
	debugln("--- " + tag + ": done")
	debugf("--- "+tag+": firstRead: %d\n", firstRead)
	if err != nil {
		return nil, 0, err
	}
	Yb := new(big.Int)
	Yb.SetBytes(b[:96])
	return Yb, firstRead, nil
}

// sharedSecret raises the peer's public key to our private exponent.
func sharedSecret(peerPublic, priv *big.Int) *big.Int {
	return new(big.Int).Exp(peerPublic, priv, dhPrime)
}

// sendKeyExchange writes HASH(req1,S), HASH(req2,SKEY)xorHASH(req3,S) and
// the RC4-sealed VC, crypto_provide, pad and initial payload.
func (s *Stream) sendKeyExchange(writeBuf *bytes.Buffer, S *big.Int, sKey []byte, cryptoProvide CryptoMethod, initialPayload []byte) error {
	hashS, hashSKey := hashes(S, sKey)
	padC, err := padZero()
	if err != nil {
		return err
	}
	writeBuf.Write(hashS)
	writeBuf.Write(hashSKey)
	writeBuf.Write(verifyConst)
	_ = binary.Write(writeBuf, binary.BigEndian, cryptoProvide)
	_ = binary.Write(writeBuf, binary.BigEndian, uint16(len(padC)))
	writeBuf.Write(padC)
	_ = binary.Write(writeBuf, binary.BigEndian, uint16(len(initialPayload)))
	writeBuf.Write(initialPayload)
	// Everything from VC on is RC4-sealed; the two hashes stay in clear.
	sealed := writeBuf.Bytes()[40:]
	s.w.s.XORKeyStream(sealed, sealed)
	debugln("--- out: writing Step 3")
	_, err = writeBuf.WriteTo(s.raw)
	if err != nil {
		return err
	}
	debugln("--- out: done")
	return nil
}

// readCryptoSelect reads the responder's VC echo, selected cipher and pad,
// validates the selection and switches the stream to it.
func readCryptoSelect(s *Stream, firstRead int, cryptoProvide CryptoMethod) (selected CryptoMethod, err error) {
	vcEnc := make([]byte, 8)
	s.r.S.XORKeyStream(vcEnc, verifyConst)
	if err = s.readSync(vcEnc, 616-firstRead); err != nil {
		return
	}
	debugln("--- out: reading crypto_select")
	if err = binary.Read(s.r, binary.BigEndian, &selected); err != nil {
		return
	}
	debugln("--- out: done")
	debugf("--- out: selected: %#v\n", selected)
	if selected == 0 {
		err = errors.New("none of the provided methods are accepted")
		return
	}
	if !isPowerOfTwo(uint32(selected)) {
		err = fmt.Errorf("invalid crypto selected: %d", selected)
		return
	}
	if (selected & cryptoProvide) == 0 {
		err = fmt.Errorf("selected crypto was not provided: %d", selected)
		return
	}
	var lenPadD uint16
	if err = binary.Read(s.r, binary.BigEndian, &lenPadD); err != nil {
		return
	}
	debugf("--- out: lenPadD: %d\n", lenPadD)
	if _, err = io.CopyN(io.Discard, s.r, int64(lenPadD)); err != nil {
		return
	}
	s.updateCipher(selected)
	s.r2 = s.r
	debugln("--- out: end handshake")
	return
}

// receiveKeyExchange syncs on HASH(req1,S), unmasks the key hash with
// HASH(req3,S), resolves the stream key and sets up RC4 from it.
func receiveKeyExchange(s *Stream, S *big.Int, getSKey func([20]byte) []byte, firstRead int) ([]byte, error) {
	req1 := hashInt("req1", S)
	if err := s.readSync(req1, 628-firstRead); err != nil {
		return nil, err
	}
	var hashRead [20]byte
	if _, err := io.ReadFull(s.raw, hashRead[:]); err != nil {
		return nil, err
	}
	req3 := hashInt("req3", S)
	for i := range sha1.Size {
		hashRead[i] ^= req3[i]
	}
	sKey := getSKey(hashRead)
	if sKey == nil {
		return nil, errors.New("invalid SKEY hash")
	}
	if err := s.initRC4("keyB", "keyA", S, sKey); err != nil {
		return nil, err
	}
	vcRead := make([]byte, 8)
	debugln("--- in: read vc")
	if _, err := io.ReadFull(s.r, vcRead); err != nil {
		return nil, err
	}
	debugln("--- in: done")
	if !bytes.Equal(vcRead, verifyConst) {
		return nil, fmt.Errorf("invalid VC: %s", hex.EncodeToString(vcRead))
	}
	return sKey, nil
}

// answerCryptoSelect reads the initiator's offer, negotiates one cipher
// through cryptoSelect and answers with VC, selection and pad, returning the
// initial-payload length for the caller to drain.
func answerCryptoSelect(s *Stream, writeBuf *bytes.Buffer, cryptoSelect func(CryptoMethod) CryptoMethod) error {
	var cryptoProvide CryptoMethod
	if err := binary.Read(s.r, binary.BigEndian, &cryptoProvide); err != nil {
		return err
	}
	if cryptoProvide == 0 {
		return errors.New("no crypto methods are provided")
	}
	selected := cryptoSelect(cryptoProvide)
	if selected == 0 {
		return errors.New("none of the provided methods are accepted")
	}
	if !isPowerOfTwo(uint32(selected)) {
		return fmt.Errorf("invalid crypto selected: %d", selected)
	}
	if (selected & cryptoProvide) == 0 {
		return fmt.Errorf("selected crypto is not provided: %d", selected)
	}
	var lenPadC uint16
	if err := binary.Read(s.r, binary.BigEndian, &lenPadC); err != nil {
		return err
	}
	if _, err := io.CopyN(io.Discard, s.r, int64(lenPadC)); err != nil {
		return err
	}
	var lenIA uint16
	if err := binary.Read(s.r, binary.BigEndian, &lenIA); err != nil {
		return err
	}
	IA := bytes.NewBuffer(make([]byte, 0, lenIA))
	if _, err := io.CopyN(IA, s.r, int64(lenIA)); err != nil {
		return err
	}

	debugln("--- in: begin step 4")
	writeBuf.Write(verifyConst)
	_ = binary.Write(writeBuf, binary.BigEndian, selected)
	padD, err := padZero()
	if err != nil {
		return err
	}
	_ = binary.Write(writeBuf, binary.BigEndian, uint16(len(padD)))
	writeBuf.Write(padD)

	debugln("--- in: writing step 4")
	if _, err := writeBuf.WriteTo(s.w); err != nil {
		return err
	}

	s.updateCipher(selected)
	s.r2 = io.MultiReader(IA, s.r)
	debugln("--- in: done")
	debugln("--- in: end handshake")
	return nil
}

// initRC4 derives the directional RC4 ciphers from the shared secret and
// the stream key, discarding the first 1024 keystream bytes per the MSE
// spec, and wires them into the stream.
func (s *Stream) initRC4(encKey, decKey string, S *big.Int, sKey []byte) error { //nolint:gocritic
	cipherEnc, err := rc4.NewCipher(rc4Key(encKey, S, sKey))
	if err != nil {
		return err
	}
	cipherDec, err := rc4.NewCipher(rc4Key(decKey, S, sKey))
	if err != nil {
		return err
	}
	var buf [1024]byte
	discard := buf[:]
	cipherEnc.XORKeyStream(discard, discard)
	cipherDec.XORKeyStream(discard, discard)
	s.w = &inPlaceStreamWriter{s: cipherEnc, w: s.raw}
	s.r = &cipher.StreamReader{S: cipherDec, R: s.raw}
	return nil
}

// updateCipher drops the stream to plaintext when negotiated; RC4 keeps the
// ciphers initRC4 installed.
func (s *Stream) updateCipher(selected CryptoMethod) {
	switch selected {
	case RC4:
	case PlainText:
		s.r = &cipher.StreamReader{S: plainTextCipher{}, R: s.raw}
		s.w = &inPlaceStreamWriter{s: plainTextCipher{}, w: s.raw}
	}
}

// readSync consumes the pad until key aligns at the head: the peer hides the
// sync point in up to remaining random bytes, so the window slides one byte
// at a time.
func (s *Stream) readSync(key []byte, remaining int) error {
	var window bytes.Buffer
	if _, err := io.CopyN(&window, s.raw, int64(len(key))); err != nil {
		return err
	}
	remaining -= len(key)
	for {
		if bytes.Equal(window.Bytes(), key) {
			return nil
		}
		if remaining <= 0 {
			return errors.New("sync point is not found")
		}
		if _, err := io.CopyN(&window, s.raw, 1); err != nil {
			return err
		}
		remaining--
		if _, err := io.CopyN(io.Discard, &window, 1); err != nil {
			return err
		}
	}
}

// privateKey draws a 160-bit Diffie-Hellman secret.
func privateKey() (*big.Int, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	var n big.Int
	return n.SetBytes(b), nil
}

// publicKey raises the generator to a private secret.
func publicKey(private *big.Int) *big.Int {
	var n big.Int
	return n.Exp(dhGen, private, dhPrime)
}

// keyPair draws a fresh DH pair.
func keyPair() (private, public *big.Int, err error) {
	private, err = privateKey()
	if err != nil {
		return
	}
	public = publicKey(private)
	return
}

// bytesWithPad left-pads a key to the 96-byte wire width.
func bytesWithPad(key *big.Int) []byte {
	b := key.Bytes()
	if pad := 96 - len(b); pad > 0 {
		padded := make([]byte, 96)
		copy(padded[pad:], key.Bytes())
		return padded
	}
	return b
}

func isPowerOfTwo(x uint32) bool { return (x != 0) && ((x & (x - 1)) == 0) }

// hashes derives the exchange authenticators: HASH(req1,S) and
// HASH(req2,SKEY)xorHASH(req3,S).
func hashes(S *big.Int, sKey []byte) (hashS, hashSKey []byte) { // nolint:gocritic
	req1 := hashInt("req1", S)
	req2 := HashSKey(sKey)
	req3 := hashInt("req3", S)
	for i := range sha1.Size {
		req3[i] ^= req2[i]
	}
	return req1, req3
}

func hashInt(prefix string, i *big.Int) []byte {
	h := sha1.New()
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write(bytesWithPad(i))
	return h.Sum(nil)
}

// HashSKey hashes a stream key as HASH("req2", key).
func HashSKey(key []byte) [20]byte {
	var sum [20]byte
	h := sha1.New()
	_, _ = h.Write([]byte("req2"))
	_, _ = h.Write(key)
	copy(sum[:], h.Sum(nil))
	return sum
}

// rc4Key derives one directional RC4 key as HASH(prefix, S, sKey).
func rc4Key(prefix string, S *big.Int, sKey []byte) []byte { // nolint:gocritic
	h := sha1.New()
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write(bytesWithPad(S))
	_, _ = h.Write(sKey)
	return h.Sum(nil)
}

// padRandom returns up to 512 random pad bytes.
func padRandom() ([]byte, error) {
	b, err := padZero()
	if err != nil {
		return nil, err
	}
	_, err = rand.Read(b)
	return b, err
}

// padZero returns a zeroed pad of random sub-512 length.
func padZero() ([]byte, error) {
	padLen, err := rand.Int(rand.Reader, big.NewInt(512))
	if err != nil {
		return nil, err
	}
	return make([]byte, int(padLen.Int64())), nil
}

// plainTextCipher is the identity "cipher" for plaintext-mode streams.
type plainTextCipher struct{}

func (plainTextCipher) XORKeyStream(dst, src []byte) { copy(dst, src) }
