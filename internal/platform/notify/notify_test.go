package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFCMSendBuildsRequest(t *testing.T) {
	var got map[string]map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewFCMWithClient(srv.Client(), srv.URL)
	err := f.Send(context.Background(), "token-123", Message{Title: "Hi", Body: "There", Data: map[string]string{"booking_id": "b1"}})
	if err != nil {
		t.Fatal(err)
	}
	msg := got["message"]
	if msg["token"] != "token-123" {
		t.Fatalf("token = %v", msg["token"])
	}
	if n := msg["notification"].(map[string]any); n["title"] != "Hi" || n["body"] != "There" {
		t.Fatalf("notification = %v", n)
	}
}

func TestFCMMapsUnregisteredTokens(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusNotFound, `{"error":{"status":"NOT_FOUND"}}`, ErrUnregistered},
		{http.StatusBadRequest, `{"error":{"status":"INVALID_ARGUMENT"}}`, ErrUnregistered},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		err := NewFCMWithClient(srv.Client(), srv.URL).Send(context.Background(), "t", Message{})
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("status %d: err = %v, want %v", tc.status, err, tc.want)
		}
	}
}

func TestFCMServerErrorIsNotUnregistered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := NewFCMWithClient(srv.Client(), srv.URL).Send(context.Background(), "t", Message{})
	if err == nil || errors.Is(err, ErrUnregistered) {
		t.Fatalf("err = %v; want a non-unregistered failure", err)
	}
}
