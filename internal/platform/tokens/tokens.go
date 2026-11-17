// Package tokens issues and verifies short-lived JWT access tokens. Admins and
// customers use separate audiences, so a token for one can never be used as
// the other even though they share a signing secret.
package tokens

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const issuer = "thandra-api"

// Audiences.
const (
	AudienceAdmin    = "admin"
	AudienceCustomer = "customer"
)

// ErrInvalid means the token failed verification for any reason.
var ErrInvalid = errors.New("invalid access token")

// Manager signs and verifies HS256 access tokens for one audience.
type Manager struct {
	secret   []byte
	ttl      time.Duration
	audience string
}

func New(secret string, ttl time.Duration, audience string) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl, audience: audience}
}

// Issue signs a token whose subject is the given id.
func (m *Manager) Issue(subject uuid.UUID, now time.Time) (string, time.Time, error) {
	exp := now.Add(m.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   subject.String(),
		Audience:  jwt.ClaimStrings{m.audience},
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// Parse verifies signature, algorithm, issuer, audience and expiry, and
// returns the subject id.
func (m *Manager) Parse(raw string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(5*time.Second),
	)
	var claims jwt.RegisteredClaims
	tok, err := parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return m.secret, nil })
	if err != nil || !tok.Valid {
		return uuid.Nil, ErrInvalid
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, ErrInvalid
	}
	return id, nil
}
