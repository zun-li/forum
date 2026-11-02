package session

import (
	"errors"
	"sync"
	"testing"
	"time"

	"forum/internal/testutil"
	"forum/pkg/jwt"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	tokens, err := jwt.New(jwt.Options{
		Secret: "test-only-secret-at-least-32-bytes", Issuer: "forum", Audience: "forum-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(tokens, testutil.Redis(t), Options{30 * time.Minute, 5 * time.Minute, 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReplacementRenewalAndRevocation(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Second)
	now := start
	m.now = func() time.Time { return now }
	first, err := m.Issue(ctx, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := m.Authenticate(ctx, first.Value)
	if err != nil || initial.RenewedToken != "" || len(initial.SessionID) != 64 {
		t.Fatalf("initial authentication: %+v, %v", initial, err)
	}
	now = start.Add(25 * time.Minute)
	boundary, err := m.Authenticate(ctx, first.Value)
	if err != nil || boundary.RenewedToken != "" {
		t.Fatal("renewed outside the renewal window", err)
	}
	now = start.Add(26 * time.Minute)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			auth, err := m.Authenticate(ctx, first.Value)
			if err != nil || auth.SessionID != initial.SessionID || auth.RenewedToken == "" {
				t.Errorf("concurrent renewal: %+v, %v", auth, err)
			}
		})
	}
	wg.Wait()
	renewed, err := m.Authenticate(ctx, first.Value)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.tokens.ParseClaims(renewed.RenewedToken, now)
	if err != nil || claims.ExpiresAt.Unix() != start.Add(56*time.Minute).Unix() {
		t.Fatal("incorrect renewed expiry", err)
	}
	absolute, err := m.client.HGet(ctx, key(7), "absolute_expiry").Int64()
	if err != nil || absolute != start.Add(7*24*time.Hour).Unix() {
		t.Fatal("renewal moved absolute deadline", err)
	}
	// A delayed renewal with an earlier timestamp cannot shorten Redis expiry.
	now = start.Add(27 * time.Minute)
	if _, err := m.Authenticate(ctx, first.Value); err != nil {
		t.Fatal(err)
	}
	now = start.Add(26 * time.Minute)
	if _, err := m.Authenticate(ctx, first.Value); err != nil {
		t.Fatal(err)
	}
	storedExpiry, err := m.client.HGet(ctx, key(7), "expires_at").Int64()
	if err != nil || storedExpiry != start.Add(57*time.Minute).Unix() {
		t.Fatal("delayed renewal shortened expiry", err)
	}
	now = start.Add(31 * time.Minute)
	if _, err := m.Authenticate(ctx, first.Value); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatal("expired JWT accepted after session renewal", err)
	}
	second, err := m.Issue(ctx, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, renewed.RenewedToken); !errors.Is(err, ErrReplaced) {
		t.Fatal("old device still authenticated", err)
	}
	if err := m.Revoke(ctx, 7, initial.SessionID); err != nil {
		t.Fatal(err)
	}
	current, err := m.Authenticate(ctx, second.Value)
	if err != nil {
		t.Fatal("old logout removed new login", err)
	}
	if err := m.Revoke(ctx, 7, current.SessionID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(26 * time.Minute)
	if _, err := m.Authenticate(ctx, second.Value); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatal("revoked session was renewed", err)
	}
	if m.client.Exists(ctx, key(7)).Val() != 0 {
		t.Fatal("revoked session was recreated")
	}
}

func TestAbsoluteDeadline(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Second)
	now := start
	m.now = func() time.Time { return now }
	token, err := m.Issue(ctx, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := start.Add(7 * 24 * time.Hour)
	for {
		claims, err := m.tokens.ParseClaims(token.Value, now)
		if err != nil {
			t.Fatal(err)
		}
		if claims.ExpiresAt.After(deadline) {
			t.Fatal("JWT exceeded absolute deadline")
		}
		if claims.ExpiresAt.Equal(deadline) {
			break
		}
		now = claims.ExpiresAt.Add(-4 * time.Minute)
		auth, err := m.Authenticate(ctx, token.Value)
		if err != nil || auth.RenewedToken == "" {
			t.Fatal("active session failed to renew", err)
		}
		token.Value = auth.RenewedToken
	}
	now = deadline.Add(-time.Second)
	auth, err := m.Authenticate(ctx, token.Value)
	if err != nil || auth.RenewedToken != "" {
		t.Fatal("last JWT should stay capped at the deadline", err)
	}
	now = deadline
	if _, err := m.Authenticate(ctx, token.Value); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatal("absolute deadline was ignored", err)
	}
}

func TestConcurrentLogoutCannotResurrectSession(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	start := time.Now().UTC().Truncate(time.Second)
	m.now = func() time.Time { return start }
	token, err := m.Issue(ctx, 9, false)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := m.Authenticate(ctx, token.Value)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return start.Add(26 * time.Minute) }
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { _, _ = m.Authenticate(ctx, token.Value) })
	}
	wg.Go(func() {
		if err := m.Revoke(ctx, 9, auth.SessionID); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	if m.client.Exists(ctx, key(9)).Val() != 0 {
		t.Fatal("concurrent renewal resurrected logged-out session")
	}
}

func TestMissingSessionIDAndRedisFailure(t *testing.T) {
	m := testManager(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	withoutSession, err := m.tokens.IssueSession(1, "", now, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Authenticate(ctx, withoutSession.Value); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatal("token without session ID bypassed session validation", err)
	}
	token, err := m.Issue(ctx, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	m.client.Close()
	if _, err := m.Authenticate(ctx, token.Value); err == nil || errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatal("Redis failure must remain a server error", err)
	}
	if _, err := m.Issue(ctx, 1, false); err == nil {
		t.Fatal("login succeeded without saving a session")
	}
}
