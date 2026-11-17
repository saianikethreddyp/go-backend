package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const tokenIssuer = "thandra-api"

// Claims are the JWT claims carried by an access token.
type Claims struct {
	jwt.RegisteredClaims
}

// TokenManager issues and verifies access tokens (HS256).
type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

// NewAccessToken signs an access token for admin.
func (m *TokenManager) NewAccessToken(a *Admin, now time.Time) (string, time.Time, error) {
	exp := now.Add(m.ttl)
	claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Subject:   a.ID.String(),
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// ErrInvalidToken means the access token failed verification.
var ErrInvalidToken = errors.New("invalid access token")

// ParseAccessToken verifies signature, algorithm, issuer and expiry.
func (m *TokenManager) ParseAccessToken(raw string) (*Principal, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(5*time.Second),
	)
	var claims Claims
	tok, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return m.secret, nil })
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	adminID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return &Principal{AdminID: adminID}, nil
}

// NewRefreshToken returns a random opaque token and its SHA-256 hash. Only the
// hash is stored, so a database leak does not expose usable tokens.
func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken returns the storage hash of a raw refresh token.
func HashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
