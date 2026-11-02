package jwt

import (
	"strconv"
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-only-secret-at-least-32-bytes"

func testManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := New(Options{
		Secret:   testSecret,
		Issuer:   "forum",
		Audience: "forum-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestIssueSessionAndParseClaims(t *testing.T) {
	manager := testManager(t)
	const userID = uint64(1234567890123456789)
	sid := strings.Repeat("a", 64)
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(30 * time.Minute)
	issued, err := manager.IssueSession(userID, sid, now, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.ParseClaims(issued.Value, now)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != strconv.FormatUint(userID, 10) || claims.SessionID != sid || !claims.ExpiresAt.Equal(expiresAt) || issued.ExpiresIn != 1800 {
		t.Fatal("incorrect session token claims or lifetime")
	}
	for _, invalid := range []uint64{0, 1 << 63} {
		if _, err := manager.IssueSession(invalid, sid, now, expiresAt); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}

func TestParseRejectsInvalidTokens(t *testing.T) {
	manager := testManager(t)
	now := time.Now().UTC().Truncate(time.Second)
	validClaims := func() jwtlib.RegisteredClaims {
		return jwtlib.RegisteredClaims{
			Subject:   "1234567890123456789",
			Issuer:    "forum",
			Audience:  jwtlib.ClaimStrings{"forum-api"},
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(30 * time.Minute)),
		}
	}

	cases := []struct {
		name   string
		change func(*jwtlib.RegisteredClaims)
	}{
		{"expired", func(c *jwtlib.RegisteredClaims) {
			c.IssuedAt = jwtlib.NewNumericDate(now.Add(-time.Hour))
			c.ExpiresAt = jwtlib.NewNumericDate(now.Add(-time.Minute))
		}},
		{"missing expiry", func(c *jwtlib.RegisteredClaims) { c.ExpiresAt = nil }},
		{"missing issued at", func(c *jwtlib.RegisteredClaims) { c.IssuedAt = nil }},
		{"future issued at", func(c *jwtlib.RegisteredClaims) {
			c.IssuedAt = jwtlib.NewNumericDate(now.Add(time.Minute))
		}},
		{"wrong issuer", func(c *jwtlib.RegisteredClaims) { c.Issuer = "other" }},
		{"wrong audience", func(c *jwtlib.RegisteredClaims) { c.Audience = jwtlib.ClaimStrings{"other"} }},
		{"missing subject", func(c *jwtlib.RegisteredClaims) { c.Subject = "" }},
		{"zero subject", func(c *jwtlib.RegisteredClaims) { c.Subject = "0" }},
		{"invalid subject", func(c *jwtlib.RegisteredClaims) { c.Subject = "abc" }},
		{"oversized subject", func(c *jwtlib.RegisteredClaims) { c.Subject = "18446744073709551615" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := validClaims()
			tc.change(&claims)
			encoded, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString([]byte(testSecret))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.ParseClaims(encoded, now); err == nil {
				t.Fatal("invalid claims accepted")
			}
		})
	}

	for _, method := range []jwtlib.SigningMethod{jwtlib.SigningMethodHS512, jwtlib.SigningMethodNone} {
		var key any = []byte(testSecret)
		if method == jwtlib.SigningMethodNone {
			key = jwtlib.UnsafeAllowNoneSignatureType
		}
		encoded, err := jwtlib.NewWithClaims(method, validClaims()).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.ParseClaims(encoded, now); err == nil {
			t.Fatal("unexpected signing algorithm accepted")
		}
	}

	wrongKey, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, validClaims()).
		SignedString([]byte("another-secret-at-least-32-bytes-long"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "broken.token", strings.Repeat("x", 4097), wrongKey} {
		if _, err := manager.ParseClaims(value, now); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
