package user_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"forum/internal/api"
	"forum/internal/middleware"
	"forum/internal/session"
	"forum/internal/user"
	"forum/pkg/jwt"
	"forum/pkg/password"
	"forum/pkg/snowflake"

	"github.com/gin-gonic/gin"
)

type memoryStore struct {
	mu    sync.Mutex
	users []user.User
	fail  bool
}

func (m *memoryStore) Create(ctx context.Context, u *user.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("private database error")
	}
	for _, old := range m.users {
		if old.Username == u.Username {
			return user.ErrUsernameTaken
		}
	}
	u.CreatedAt = time.Now()
	m.users = append(m.users, *u)
	return nil
}

func (m *memoryStore) SetInitialPassword(ctx context.Context, id uint64, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.ID == id && u.Role == user.RoleAdmin && u.MustChangePassword {
			m.users[i].PasswordHash = hash
			m.users[i].MustChangePassword = false
			return nil
		}
	}
	return user.ErrPasswordAlreadySet
}

func (m *memoryStore) List(ctx context.Context) ([]user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]user.User(nil), m.users...), nil
}

func testRouter(t *testing.T, store user.Store) http.Handler {
	t.Helper()
	ids, err := snowflake.New(1)
	if err != nil {
		t.Fatal(err)
	}
	tokens := testTokenManager(t)
	users := user.NewHandler(user.NewService(store, ids, tokens))
	r := gin.New()
	api := r.Group("/api/v1")
	users.Register(api, api.Group("", middleware.Auth(tokens)))
	return r
}

func request(r http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	return out
}

func code(t *testing.T, r *httptest.ResponseRecorder) int {
	t.Helper()
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil || envelope.Code == 0 {
		t.Errorf("invalid API response: HTTP %d %s", r.Code, r.Body.String())
	}
	return envelope.Code
}

func TestSignupHTTP(t *testing.T) {
	store := &memoryStore{}
	r := testRouter(t, store)
	out := request(r, "POST", "/api/v1/signup", `{"username":"  Alice_1  ","password":"long password 123","re_password":"long password 123"}`, "application/json; charset=utf-8")
	if out.Code != http.StatusCreated || code(t, out) != api.Success {
		t.Fatalf("%d %s", out.Code, out.Body.String())
	}
	var result struct {
		Data struct {
			ID       string
			Username string
		}
	}
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.ID == "" || result.Data.Username != "alice_1" || strings.Contains(out.Body.String(), "password") {
		t.Fatal("invalid public response")
	}
	ok, err := password.Verify("long password 123", store.users[0].PasswordHash)
	if err != nil || !ok {
		t.Fatal("password was not hashed correctly")
	}
	duplicate := request(r, "POST", "/api/v1/signup", `{"username":"ALICE_1","password":"long password 123","re_password":"long password 123"}`, "application/json")
	if duplicate.Code != http.StatusConflict || code(t, duplicate) != api.UserExists {
		t.Fatal(duplicate.Code)
	}
	list := request(r, "GET", "/api/v1/users", "", "")
	if list.Code != http.StatusOK || code(t, list) != api.Success || strings.Contains(list.Body.String(), "argon2") || strings.Contains(list.Body.String(), "password") {
		t.Fatal("list leaks password")
	}
	store.fail = true
	out = request(r, "POST", "/api/v1/signup", `{"username":"bob","password":"long password 123","re_password":"long password 123"}`, "application/json")
	if out.Code != http.StatusInternalServerError || code(t, out) != api.ServerBusy || strings.Contains(out.Body.String(), "private") {
		t.Fatal("internal error exposed")
	}
}

func TestSignupValidation(t *testing.T) {
	store := &memoryStore{}
	r := testRouter(t, store)
	for _, body := range []string{
		`{}`,
		`null`,
		`{"username":"alice","password":"long password 123"}`,
		`{"username":"alice","password":"long password 123","re_password":"mismatch"}`,
		`[]`,
		`{`,
		"{\"username\":\"alice\",\"password\":\"long password \xff\",\"re_password\":\"long password \xff\"}",
		`{"username":"ab","password":"long password 123","re_password":"long password 123"}`,
		`{"username":"a b","password":"long password 123","re_password":"long password 123"}`,
		`{"username":"用户名","password":"long password 123","re_password":"long password 123"}`,
		`{"username":"alice","password":"short","re_password":"short"}`,
		`{"username":"alice","password":"long password 123","re_password":"long password 123","id":10}`,
		`{"username":"alice","password":"long password 123","re_password":"long password 123"} {}`,
		fmt.Sprintf(`{"username":%q,"password":"long password 123","re_password":"long password 123"}`, strings.Repeat("a", 33)),
		fmt.Sprintf(`{"username":"alice","password":%[1]q,"re_password":%[1]q}`, strings.Repeat("密", 129)),
	} {
		out := request(r, "POST", "/api/v1/signup", body, "application/json")
		if out.Code != http.StatusBadRequest || code(t, out) != api.InvalidParam {
			t.Fatalf("invalid registration accepted: %s", out.Body.String())
		}
	}

	if len(store.users) != 0 {
		t.Fatal("invalid input reached storage")
	}
	for _, mediaType := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		out := request(r, "POST", "/api/v1/signup", `{}`, mediaType)
		if out.Code != http.StatusUnsupportedMediaType || code(t, out) != api.InvalidParam {
			t.Fatal(out.Code)
		}
	}
	oversized := fmt.Sprintf(`{"username":"alice","password":%q}`, strings.Repeat("x", 5000))
	out := request(r, "POST", "/api/v1/signup", oversized, "application/json")
	if out.Code != http.StatusRequestEntityTooLarge || code(t, out) != api.InvalidParam {
		t.Fatal(out.Code)
	}
	// 边界值及 Unicode 密码长度按字符计数。
	boundaries := []struct {
		username string
		password string
	}{
		{"abc", strings.Repeat("密", 6)},
		{strings.Repeat("b", 32), strings.Repeat("x", 128)},
	}
	for i, tc := range boundaries {
		body, _ := json.Marshal(map[string]string{"username": tc.username, "password": tc.password, "re_password": tc.password})
		out := request(r, "POST", "/api/v1/signup", string(body), "application/json")
		if out.Code != http.StatusCreated || code(t, out) != api.Success {
			t.Fatalf("boundary %d: %d", i, out.Code)
		}
	}
}

type testTokens struct{ *jwt.Manager }

func (tokens testTokens) Issue(_ context.Context, id uint64, _ bool) (jwt.AccessToken, error) {
	now := time.Now().UTC().Truncate(time.Second)
	return tokens.IssueSession(id, strings.Repeat("a", 64), now, now.Add(30*time.Minute))
}

func (tokens testTokens) Authenticate(_ context.Context, value string) (session.Authentication, error) {
	claims, err := tokens.ParseClaims(value, time.Now())
	if err != nil {
		return session.Authentication{}, err
	}
	id, err := strconv.ParseUint(claims.Subject, 10, 63)
	return session.Authentication{UserID: id, SessionID: claims.SessionID}, err
}

func testTokenManager(t *testing.T) testTokens {
	t.Helper()
	tokens, err := jwt.New(jwt.Options{
		Secret:   "test-only-secret-at-least-32-bytes",
		Issuer:   "forum",
		Audience: "forum-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	return testTokens{tokens}
}

func (m *memoryStore) FindByUsername(ctx context.Context, username string) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return nil, errors.New("private database error")
	}
	for _, user := range m.users {
		if user.Username == username {
			return &user, nil
		}
	}
	return nil, user.ErrUserNotFound
}

func (m *memoryStore) FindByID(ctx context.Context, id uint64) (*user.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return nil, errors.New("private database error")
	}
	for _, user := range m.users {
		if user.ID == id {
			return &user, nil
		}
	}
	return nil, user.ErrUserNotFound
}

func (m *memoryStore) HasAdmin(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Role == user.RoleAdmin {
			return true, nil
		}
	}
	return false, nil
}

func (m *memoryStore) UpdateUsername(ctx context.Context, id uint64, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID != id && u.Username == username {
			return user.ErrUsernameTaken
		}
	}
	for i := range m.users {
		if m.users[i].ID == id {
			m.users[i].Username = username
			return nil
		}
	}
	return user.ErrUserNotFound
}

func (m *memoryStore) UpdatePassword(ctx context.Context, id uint64, oldHash, newHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, u := range m.users {
		if u.ID == id && u.PasswordHash == oldHash {
			m.users[i].PasswordHash = newHash
			return nil
		}
	}
	return user.ErrWrongCurrentPassword
}
