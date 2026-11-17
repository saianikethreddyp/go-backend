package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	v, err := New(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSealOpenRoundTrip(t *testing.T) {
	v := newTestVault(t)
	sealed, err := v.Seal([]byte("Mom +919876543210"), []byte("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("Mom")) {
		t.Fatal("ciphertext must not contain plaintext")
	}
	plain, err := v.Open(sealed, []byte("user-1"))
	if err != nil || string(plain) != "Mom +919876543210" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
}

func TestOpenRejectsWrongOwnerAndTampering(t *testing.T) {
	v := newTestVault(t)
	sealed, _ := v.Seal([]byte("secret"), []byte("user-1"))
	if _, err := v.Open(sealed, []byte("user-2")); err == nil {
		t.Fatal("ciphertext must not open for a different owner")
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := v.Open(sealed, []byte("user-1")); err == nil {
		t.Fatal("tampered ciphertext must be rejected")
	}
	if _, err := v.Open([]byte("short"), nil); err == nil {
		t.Fatal("truncated ciphertext must be rejected")
	}
}

func TestSealIsRandomised(t *testing.T) {
	v := newTestVault(t)
	a, _ := v.Seal([]byte("same"), nil)
	b, _ := v.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same data must differ")
	}
}

func TestHashIsStableAndKeyed(t *testing.T) {
	v1, v2 := newTestVault(t), newTestVault(t)
	if !bytes.Equal(v1.Hash("+919876543210"), v1.Hash("+919876543210")) {
		t.Fatal("hash must be deterministic")
	}
	if bytes.Equal(v1.Hash("+919876543210"), v2.Hash("+919876543210")) {
		t.Fatal("different keys must give different hashes")
	}
	if bytes.Equal(v1.Hash("+919876543210"), v1.Hash("+919876543211")) {
		t.Fatal("different inputs must give different hashes")
	}
}

func TestNewRejectsWeakKeys(t *testing.T) {
	if _, err := New("not base64!!"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := New(base64.StdEncoding.EncodeToString([]byte("too short"))); err == nil {
		t.Fatal("expected error for short key")
	}
}
