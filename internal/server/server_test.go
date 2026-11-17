package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	req := func(xff, remote string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	tests := []struct {
		name string
		r    *http.Request
		hops int
		want string
	}{
		{"no proxy ignores header", req("1.2.3.4", "9.9.9.9:1234"), 0, "9.9.9.9"},
		{"one proxy takes last entry", req("6.6.6.6, 1.2.3.4", "10.0.0.1:80"), 1, "1.2.3.4"},
		{"two proxies take second from right", req("6.6.6.6, 1.2.3.4, 10.0.0.2", "10.0.0.1:80"), 2, "1.2.3.4"},
		{"spoofed junk falls back to remote", req("not-an-ip", "9.9.9.9:1234"), 1, "9.9.9.9"},
		{"missing header falls back to remote", req("", "9.9.9.9:1234"), 1, "9.9.9.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClientIP(tt.r, tt.hops); got != tt.want {
				t.Fatalf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := securityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, k := range []string{"X-Content-Type-Options", "X-Frame-Options", "Strict-Transport-Security", "Content-Security-Policy"} {
		if rec.Header().Get(k) == "" {
			t.Fatalf("missing header %s", k)
		}
	}
}
