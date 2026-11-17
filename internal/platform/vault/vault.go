// Package vault encrypts personal data at rest and produces keyed hashes for
// deduplication. Keys are derived from one master key with HKDF, so a single
// secret is configured but encryption and hashing never share a key.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Vault seals and opens data and computes keyed hashes.
type Vault struct {
	aead   cipher.AEAD
	macKey []byte
}

// ErrDecrypt means the data could not be authenticated or decrypted.
var ErrDecrypt = errors.New("vault: decrypt failed")

// New builds a Vault from a base64-encoded master key of at least 32 bytes.
func New(masterKeyB64 string) (*Vault, error) {
	master, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("vault: master key is not valid base64: %w", err)
	}
	if len(master) < 32 {
		return nil, errors.New("vault: master key must decode to at least 32 bytes")
	}

	derive := func(label string) ([]byte, error) {
		key := make([]byte, 32)
		if _, err := io.ReadFull(hkdf.New(sha256.New, master, nil, []byte(label)), key); err != nil {
			return nil, err
		}
		return key, nil
	}
	encKey, err := derive("thandra/contacts/encryption/v1")
	if err != nil {
		return nil, err
	}
	macKey, err := derive("thandra/contacts/hash/v1")
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, macKey: macKey}, nil
}

// Hash returns HMAC-SHA256 of s. Equal inputs give equal hashes, so it can be
// used for unique constraints without storing the value in the clear.
func (v *Vault) Hash(s string) []byte {
	m := hmac.New(sha256.New, v.macKey)
	m.Write([]byte(s))
	return m.Sum(nil)
}

// Seal encrypts plaintext. aad (for example the owner's id) is authenticated
// but not stored, so a ciphertext cannot be moved to another owner.
func (v *Vault) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open decrypts data produced by Seal with the same aad.
func (v *Vault) Open(sealed, aad []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if len(sealed) < n+v.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plain, err := v.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
