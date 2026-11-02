package user_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forum/internal/api"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

type loginData struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	Username string `json:"user_name"`
}

func login(t *testing.T, r http.Handler, body string) loginData {
	t.Helper()
	response := request(r, "POST", "/api/v1/login", body, "application/json")
	if response.Code != http.StatusOK || code(t, response) != api.Success {
		t.Fatalf("login returned %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login response may be cached")
	}
	var result struct {
		Data loginData `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.UserID == "" || result.Data.Username == "" || result.Data.Token == "" {
		t.Fatal("incomplete login response")
	}
	if strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "argon2") {
		t.Fatal("password leaked in login response")
	}
	return result.Data
}

func me(r http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	return response
}

func TestLoginAndCurrentUser(t *testing.T) {
	store := &memoryStore{}
	r := testRouter(t, store)
	body := `{"username":" Alice_1 ","password":" my strong password 123 "}`
	if response := request(r, "POST", "/api/v1/signup", `{"username":" Alice_1 ","password":" my strong password 123 ","re_password":" my strong password 123 "}`, "application/json"); response.Code != http.StatusCreated || code(t, response) != api.Success {
		t.Fatal(response.Body.String())
	}
	result := login(t, r, body)
	if result.Username != "alice_1" || result.UserID == "" {
		t.Fatal("incorrect login user")
	}
	response := me(r, "Bearer "+result.Token)
	if response.Code != http.StatusOK || code(t, response) != api.Success || !strings.Contains(response.Body.String(), result.UserID) ||
		strings.Contains(response.Body.String(), "password") {
		t.Fatalf("unexpected /me response: %d %s", response.Code, response.Body.String())
	}
	if response := me(r, "bearer "+result.Token); code(t, response) != api.Success {
		t.Fatal("Bearer scheme must be case insensitive")
	}

	wrongPassword := request(r, "POST", "/api/v1/login",
		`{"username":"ALICE_1","password":"my strong password 123"}`, "application/json")
	unknownUser := request(r, "POST", "/api/v1/login",
		`{"username":"nobody","password":"my strong password 123"}`, "application/json")
	if wrongPassword.Code != http.StatusUnauthorized || unknownUser.Code != http.StatusUnauthorized || code(t, wrongPassword) != api.InvalidPassword || code(t, unknownUser) != api.InvalidPassword || wrongPassword.Body.String() != unknownUser.Body.String() {
		t.Fatal("credentials errors must be indistinguishable")
	}

	store.fail = true
	if response := request(r, "POST", "/api/v1/login", body, "application/json"); response.Code != http.StatusInternalServerError || code(t, response) != api.ServerBusy {
		t.Fatal("database failure was classified as bad credentials")
	}
	if response := me(r, "Bearer "+result.Token); response.Code != http.StatusInternalServerError || code(t, response) != api.ServerBusy {
		t.Fatal("database failure was classified as invalid token")
	}
	store.fail = false
	store.users = nil
	if response := me(r, "Bearer "+result.Token); code(t, response) != api.InvalidToken {
		t.Fatal("deleted user is still authenticated by /me")
	}
}

func TestLoginValidationAndAuthFailures(t *testing.T) {
	r := testRouter(t, &memoryStore{})
	for _, body := range []string{
		`{}`,
		`{"username":"alice","password":""}`,
		`{"username":"a b","password":"password"}`,
		`{"username":"alice","password":"password","id":1}`,
		`{"username":"alice","password":"` + strings.Repeat("x", 129) + `"}`,
	} {
		if response := request(r, "POST", "/api/v1/login", body, "application/json"); response.Code != http.StatusBadRequest || code(t, response) != api.InvalidParam {
			t.Fatalf("invalid login returned %d", response.Code)
		}
	}

	claims := jwtlib.RegisteredClaims{
		Subject:   "1234567890123456789",
		Issuer:    "forum",
		Audience:  jwtlib.ClaimStrings{"forum-api"},
		IssuedAt:  jwtlib.NewNumericDate(time.Now().Add(-time.Hour)),
		ExpiresAt: jwtlib.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	expired, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).
		SignedString([]byte("test-only-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"", "Bearer", "Basic abc", "Bearer broken.token", "Bearer a b", "Bearer " + expired} {
		response := me(r, header)
		want := api.InvalidToken
		if header == "" {
			want = api.NeedLogin
		}
		if response.Code != http.StatusUnauthorized || code(t, response) != want || response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("invalid authentication returned %d", response.Code)
		}
	}
}
