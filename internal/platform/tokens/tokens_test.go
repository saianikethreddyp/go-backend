package tokens

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestRoundTrip(t *testing.T) {
	m := New(testSecret, 15*time.Minute, AudienceAdmin)
	id := uuid.New()
	tok, _, err := m.Issue(id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Parse(tok)
	if err != nil || got != id {
		t.Fatalf("Parse = %v, %v; want %v", got, err, id)
	}
}

func TestAudiencesAreNotInterchangeable(t *testing.T) {
	admin := New(testSecret, time.Minute, AudienceAdmin)
	customer := New(testSecret, time.Minute, AudienceCustomer)
	tok, _, _ := customer.Issue(uuid.New(), time.Now())
	if _, err := admin.Parse(tok); err == nil {
		t.Fatal("a customer token must not be accepted as an admin token")
	}
	tok, _, _ = admin.Issue(uuid.New(), time.Now())
	if _, err := customer.Parse(tok); err == nil {
		t.Fatal("an admin token must not be accepted as a customer token")
	}
}

func TestExpiredRejected(t *testing.T) {
	m := New(testSecret, time.Minute, AudienceAdmin)
	tok, _, _ := m.Issue(uuid.New(), time.Now().Add(-time.Hour))
	if _, err := m.Parse(tok); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _, _ := New(testSecret, time.Minute, AudienceAdmin).Issue(uuid.New(), time.Now())
	other := New("ffffffffffffffffffffffffffffffff", time.Minute, AudienceAdmin)
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("token signed with another secret must be rejected")
	}
}

func TestNoneAlgorithmAndIssuerRejected(t *testing.T) {
	m := New(testSecret, time.Minute, AudienceAdmin)
	claims := jwt.RegisteredClaims{
		Issuer: issuer, Subject: uuid.NewString(), Audience: jwt.ClaimStrings{AudienceAdmin},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := m.Parse(none); err == nil {
		t.Fatal("alg=none token must be rejected")
	}
	claims.Issuer = "someone-else"
	foreign, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if _, err := m.Parse(foreign); err == nil {
		t.Fatal("token from another issuer must be rejected")
	}
}

func TestGarbageRejected(t *testing.T) {
	m := New(testSecret, time.Minute, AudienceAdmin)
	for _, s := range []string{"", "abc", "a.b.c", strings.Repeat("x", 500)} {
		if _, err := m.Parse(s); err == nil {
			t.Fatalf("token %q must be rejected", s)
		}
	}
}
