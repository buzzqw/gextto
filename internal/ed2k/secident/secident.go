// Package secident implements the RSA identity used by the eMule "secure
// identification" handshake. eMule and aMule sign a challenge with a 384-bit
// RSA key and publish the matching DER public key; the identity ties a peer's
// credits to the same key across sessions.
package secident

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// KeyBits is the key size used by eMule and aMule. A 384-bit key yields a
// 48-byte signature and a 58-byte DER public key; both fit the one-byte length
// prefix of the wire packets.
const KeyBits = 384

// Key is the local secure-identification identity.
type Key struct {
	priv *rsa.PrivateKey
	der  []byte
}

// LoadOrCreate reads the PEM-encoded PKCS#1 private key at path, generating
// and storing a new 384-bit key when the file does not exist yet.
func LoadOrCreate(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("secident: %s: no PEM block", path)
		}
		priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("secident: %s: %w", path, err)
		}
		return newKey(priv), nil
	case errors.Is(err, os.ErrNotExist):
		priv, err := rsa.GenerateKey(rand.Reader, KeyBits)
		if err != nil {
			return nil, fmt.Errorf("secident: generate key: %w", err)
		}
		if err := writeKey(path, priv); err != nil {
			return nil, err
		}
		return newKey(priv), nil
	default:
		return nil, fmt.Errorf("secident: %s: %w", path, err)
	}
}

func writeKey(path string, priv *rsa.PrivateKey) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("secident: %w", err)
		}
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("secident: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secident: %w", err)
	}
	return nil
}

func newKey(priv *rsa.PrivateKey) *Key {
	// eMule and aMule exchange the public key as an X.509
	// SubjectPublicKeyInfo (the format Crypto++ loads), not as PKCS#1.
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		// A freshly generated RSA key always marshals; keep the PKCS#1 form
		// as a fallback rather than panicking on an impossible error.
		der = x509.MarshalPKCS1PublicKey(&priv.PublicKey)
	}
	return &Key{priv: priv, der: der}
}

// PublicKey returns a copy of the DER-encoded PKCS#1 public key.
func (k *Key) PublicKey() []byte {
	return append([]byte(nil), k.der...)
}

// Sign signs data with SHA-1 and PKCS#1 v1.5, the scheme eMule and aMule use.
func (k *Key) Sign(data []byte) ([]byte, error) {
	digest := sha1.Sum(data)
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.priv, crypto.SHA1, digest[:])
	if err != nil {
		return nil, fmt.Errorf("secident: sign: %w", err)
	}
	return sig, nil
}

// Verify checks a peer signature made with its DER public key. The key is
// normally a SubjectPublicKeyInfo, but older peers may send PKCS#1.
func Verify(publicDER, data, signature []byte) error {
	pub, err := parsePublicKey(publicDER)
	if err != nil {
		return err
	}
	digest := sha1.Sum(data)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA1, digest[:], signature); err != nil {
		return fmt.Errorf("secident: signature: %w", err)
	}
	return nil
}

// ValidPublicKey reports whether der is a parseable RSA public key in either
// SubjectPublicKeyInfo or PKCS#1 form.
func ValidPublicKey(der []byte) error {
	_, err := parsePublicKey(der)
	return err
}

func parsePublicKey(der []byte) (*rsa.PublicKey, error) {
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		if pub, ok := parsed.(*rsa.PublicKey); ok {
			return pub, nil
		}
	}
	pub, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("secident: public key: %w", err)
	}
	return pub, nil
}
