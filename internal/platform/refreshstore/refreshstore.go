// Package refreshstore implements single-use, rotating refresh tokens. Only a
// SHA-256 hash of each token is stored. Presenting a token that was already
// used revokes its whole family, which limits the damage of a stolen token.
//
// One Store serves one owner type (admins or customers); the table names come
// from a fixed allowlist, never from request data.
package refreshstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrInvalid means the token is unknown, expired, revoked or its owner is disabled.
	ErrInvalid = errors.New("invalid refresh token")
	// ErrReuse means a used token was presented again; its family is now revoked.
	ErrReuse = errors.New("refresh token reuse detected")
)

// Kind selects which tables a Store works on.
type Kind struct {
	tokens   string // refresh token table
	ownerCol string // column in the token table pointing at the owner
	owners   string // owner table (must have an "active" column)
}

var (
	Admins    = Kind{tokens: "refresh_tokens", ownerCol: "admin_id", owners: "admins"}
	Customers = Kind{tokens: "user_refresh_tokens", ownerCol: "user_id", owners: "users"}
)

// Store persists refresh tokens for one Kind.
type Store struct {
	pool *pgxpool.Pool
	k    Kind
}

func New(pool *pgxpool.Pool, k Kind) *Store { return &Store{pool: pool, k: k} }

// NewToken returns a random opaque token and its storage hash.
func NewToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, Hash(raw), nil
}

// Hash returns the storage hash of a raw token.
func Hash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// Insert stores a new token, starting a new family when familyID is new.
func (s *Store) Insert(ctx context.Context, ownerID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %s (%s, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`, s.k.tokens, s.k.ownerCol),
		ownerID, familyID, hash, expiresAt)
	return err
}

// Rotate atomically consumes oldHash and stores newHash in the same family.
func (s *Store) Rotate(ctx context.Context, oldHash, newHash []byte, newExpiry time.Time) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		ownerID, familyID uuid.UUID
		expiresAt         time.Time
		usedAt, revokedAt *time.Time
		active            bool
	)
	err = tx.QueryRow(ctx, fmt.Sprintf(
		`SELECT t.%[2]s, t.family_id, t.expires_at, t.used_at, t.revoked_at, o.active
		   FROM %[1]s t
		   JOIN %[3]s o ON o.id = t.%[2]s
		  WHERE t.token_hash = $1
		    FOR UPDATE OF t`, s.k.tokens, s.k.ownerCol, s.k.owners), oldHash).
		Scan(&ownerID, &familyID, &expiresAt, &usedAt, &revokedAt, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrInvalid
	}
	if err != nil {
		return uuid.Nil, err
	}

	if usedAt != nil {
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`UPDATE %s SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`, s.k.tokens), familyID); err != nil {
			return uuid.Nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, err
		}
		return uuid.Nil, ErrReuse
	}
	if revokedAt != nil || !expiresAt.After(time.Now()) || !active {
		return uuid.Nil, ErrInvalid
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET used_at = now() WHERE token_hash = $1`, s.k.tokens), oldHash); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (%s, family_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`, s.k.tokens, s.k.ownerCol),
		ownerID, familyID, newHash, newExpiry); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return ownerID, nil
}

// RevokeFamily revokes every token in the family of the given token.
func (s *Store) RevokeFamily(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %[1]s SET revoked_at = now()
		  WHERE revoked_at IS NULL
		    AND family_id = (SELECT family_id FROM %[1]s WHERE token_hash = $1)`, s.k.tokens), hash)
	return err
}

// DeleteStale removes tokens that can no longer be used.
func (s *Store) DeleteStale(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE expires_at < now() - interval '1 day'
		    OR (revoked_at IS NOT NULL AND revoked_at < now() - interval '1 day')
		    OR (used_at IS NOT NULL AND used_at < now() - interval '1 day')`, s.k.tokens))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
