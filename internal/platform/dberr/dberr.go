// Package dberr translates driver errors into small domain-level sentinels so
// that services never depend on pgx types.
package dberr

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrInvalidReference = errors.New("invalid reference")
)

// Error wraps a driver error with a domain kind and the violated constraint.
type Error struct {
	Kind       error
	Constraint string
	Err        error
}

func (e *Error) Error() string   { return e.Kind.Error() + ": " + e.Err.Error() }
func (e *Error) Unwrap() []error { return []error{e.Kind, e.Err} }

// Translate maps well-known pgx errors to dberr kinds; others pass through.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &Error{Kind: ErrNotFound, Err: err}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return &Error{Kind: ErrConflict, Constraint: pgErr.ConstraintName, Err: err}
		case "23503": // foreign_key_violation
			return &Error{Kind: ErrInvalidReference, Constraint: pgErr.ConstraintName, Err: err}
		}
	}
	return err
}

// Constraint returns the violated constraint name, if err carries one.
func Constraint(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Constraint
	}
	return ""
}
