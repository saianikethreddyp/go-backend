package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func testAdmin() *Admin { return &Admin{ID: uuid.New()} }

func TestAccessTokenRoundTrip(t *testing.T) {
	m := NewTokenManager(testSecret, 15*time.Minute)
	a := testAdmin()

	tok, _, err := m.NewAccessToken(a, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.ParseAccessToken(tok)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if p.AdminID != a.ID {
		t.Fatalf("unexpected principal: %+v", p)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	m := NewTokenManager(testSecret, time.Minute)
	tok, _, _ := m.NewAccessToken(testAdmin(), time.Now().Add(-time.Hour))
	if _, err := m.ParseAccessToken(tok); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _, _ := NewTokenManager(testSecret, time.Minute).NewAccessToken(testAdmin(), time.Now())
	other := NewTokenManager("ffffffffffffffffffffffffffffffff", time.Minute)
	if _, err := other.ParseAccessToken(tok); err == nil {
		t.Fatal("token signed with another secret must be rejected")
	}
}

func TestNoneAlgorithmRejected(t *testing.T) {
	m := NewTokenManager(testSecret, time.Minute)
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: tokenIssuer, Subject: uuid.NewString(), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ParseAccessToken(tok); err == nil {
		t.Fatal("alg=none token must be rejected")
	}
}

func TestWrongIssuerRejected(t *testing.T) {
	m := NewTokenManager(testSecret, time.Minute)
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "someone-else", Subject: uuid.NewString(), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if _, err := m.ParseAccessToken(tok); err == nil {
		t.Fatal("token from another issuer must be rejected")
	}
}

func TestGarbageRejected(t *testing.T) {
	m := NewTokenManager(testSecret, time.Minute)
	for _, s := range []string{"", "abc", "a.b.c", strings.Repeat("x", 500)} {
		if _, err := m.ParseAccessToken(s); err == nil {
			t.Fatalf("token %q must be rejected", s)
		}
	}
}

func TestRefreshTokenHashing(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 40 || string(hash) != string(HashRefreshToken(raw)) {
		t.Fatal("refresh token or hash is malformed")
	}
	raw2, _, _ := NewRefreshToken()
	if raw == raw2 {
		t.Fatal("refresh tokens must be unique")
	}
}
