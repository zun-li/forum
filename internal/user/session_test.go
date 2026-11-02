package user_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum/internal/api"
	"forum/internal/middleware"
	"forum/internal/session"
	"forum/internal/testutil"
	"forum/internal/user"
	"forum/pkg/password"
	"forum/pkg/snowflake"

	"github.com/gin-gonic/gin"
)

func TestSessionHTTP(t *testing.T) {
	tokens := testTokenManager(t).Manager
	client := testutil.Redis(t)
	sessions, err := session.New(tokens, client, session.Options{AccessTTL: 30 * time.Minute, RenewalWindow: 5 * time.Minute, MaxLifetime: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := snowflake.New(1)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	group := r.Group("/api/v1", middleware.OptionalAuth(sessions))
	protected := group.Group("", middleware.Auth(sessions))
	users := user.NewHandler(user.NewService(&memoryStore{}, ids, sessions))
	users.Register(group, protected)
	protected.POST("/logout", middleware.Logout(sessions))
	group.GET("/public", func(c *gin.Context) { api.OK(c, "public") })
	call := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/"+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	if out := request(r, "POST", "/api/v1/signup", `{"username":"alice","password":"long password 123","re_password":"long password 123"}`, "application/json"); out.Code != 201 {
		t.Fatal(out.Body.String())
	}
	first := login(t, r, `{"username":"alice","password":"long password 123"}`)
	claims, err := tokens.ParseClaims(first.Token, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Sign a short-lived JWT for the same session to reach the renewal window
	// without sleeping or changing the production clock.
	now := time.Now().UTC().Truncate(time.Second)
	userID, err := strconv.ParseUint(first.UserID, 10, 63)
	if err != nil {
		t.Fatal(err)
	}
	nearExpiry, err := tokens.IssueSession(userID, claims.SessionID, now.Add(-26*time.Minute), now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	out := call("GET", "public", nearExpiry.Value)
	renewed := out.Header().Get("X-Renewed-Token")
	if out.Code != 200 || renewed == "" || out.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public read did not renew: %d %s", out.Code, out.Body.String())
	}
	if out := call("GET", "me", renewed); out.Code != 200 {
		t.Fatal("renewed token rejected", out.Body.String())
	}
	second := login(t, r, `{"username":"alice","password":"long password 123"}`)
	if out := call("GET", "public", renewed); out.Code != 401 || !strings.Contains(out.Body.String(), "其他设备") {
		t.Fatal("old device was not rejected", out.Body.String())
	}
	if out := call("POST", "logout", first.Token); out.Code != 401 {
		t.Fatal("replaced session logged out newer session", out.Body.String())
	}
	if out := call("GET", "me", second.Token); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if out := call("POST", "logout", second.Token); out.Code != 200 || out.Header().Get("X-Renewed-Token") != "" {
		t.Fatal("logout failed", out.Body.String())
	}
	if out := call("GET", "me", second.Token); out.Code != 401 {
		t.Fatal("logged-out token remained usable", out.Body.String())
	}
	// Login must ignore the revoked credential still attached by a browser.
	req := httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(`{"username":"alice","password":"long password 123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+second.Token)
	out = httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal("stale credential prevented re-login", out.Body.String())
	}
	third := login(t, r, `{"username":"alice","password":"long password 123"}`)
	client.Close()
	if out := call("GET", "me", third.Token); out.Code != http.StatusInternalServerError || code(t, out) != api.ServerBusy {
		t.Fatal("Redis outage was classified as invalid credentials", out.Body.String())
	}
	if out := call("GET", "public", ""); out.Code != 200 {
		t.Fatal("anonymous public reads failed during session outage", out.Body.String())
	}
}

type restrictedSession struct{}

func (restrictedSession) Authenticate(context.Context, string) (session.Authentication, error) {
	return session.Authentication{UserID: 1, SessionID: "test-sid", MustChangePassword: true}, nil
}

func (restrictedSession) Revoke(context.Context, uint64, string) error {
	return errors.New("simulated Redis outage")
}

func TestPasswordCommittedBeforeSessionCleanupFailure(t *testing.T) {
	store := &memoryStore{users: []user.User{{ID: 1, Role: user.RoleAdmin, MustChangePassword: true}}}
	users := user.NewHandler(user.NewService(store, nil, nil))
	r := gin.New()
	group := r.Group("/api/v1", middleware.Auth(restrictedSession{}))
	group.POST("/me/password", users.ChangeInitialPassword(restrictedSession{}))
	group.POST("/admin/communities", users.RequireAdmin, func(c *gin.Context) { api.OK(c, nil) })
	call := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer limited")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	if out := call("me/password", `{"new_password":"六个中文字符","re_password":"六个中文字符"}`); code(t, out) != api.Success {
		t.Fatal(out.Body.String())
	}
	if out := call("admin/communities", "{}"); code(t, out) != api.PasswordChangeRequired {
		t.Fatal("old session became privileged", out.Code)
	}
	if out := call("me/password", `{"new_password":"second","re_password":"second"}`); code(t, out) != api.Conflict {
		t.Fatal("old session reset password again", out.Code)
	}
	if ok, err := password.Verify("六个中文字符", store.users[0].PasswordHash); err != nil || !ok {
		t.Fatal("saved password changed", err)
	}
}
