package hubs

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/saianikethreddyp/thandra-api/internal/platform/dberr"
	"github.com/saianikethreddyp/thandra-api/internal/platform/httpx"
)

type deleteRepo struct {
	Repository
	err error
}

func (d deleteRepo) Delete(context.Context, uuid.UUID) error { return d.err }

func TestServiceDelete(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int // 0 means success
	}{
		{"unused hub is deleted", nil, 0},
		{"missing hub is 404", &dberr.Error{Kind: dberr.ErrNotFound, Err: pgx.ErrNoRows}, http.StatusNotFound},
		{"hub with cars or bookings is 409", &dberr.Error{Kind: dberr.ErrInvalidReference, Err: errors.New("fk")}, http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewService(deleteRepo{err: tc.err}).Delete(context.Background(), uuid.New())
			if tc.status == 0 {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			var app *httpx.AppError
			if !errors.As(err, &app) || app.Status != tc.status {
				t.Fatalf("want status %d, got %v", tc.status, err)
			}
		})
	}
}
