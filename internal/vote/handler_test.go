package vote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forum/internal/api"
	"forum/internal/middleware"
	"forum/internal/session"
	"forum/pkg/jwt"

	"github.com/gin-gonic/gin"
)

type testVote struct {
	user, post uint64
	direction  int64
	err        error
}

func (v *testVote) Cast(ctx context.Context, userID, postID uint64, direction int64) error {
	v.user, v.post, v.direction = userID, postID, direction
	return v.err
}

type testToken struct{}

func (testToken) Authenticate(_ context.Context, token string) (session.Authentication, error) {
	if token == "valid" {
		return session.Authentication{UserID: 9}, nil
	}
	return session.Authentication{}, jwt.ErrInvalidToken
}

func TestVoteHTTP(t *testing.T) {
	const validBody = `{"post_id":"999999999999999933","direction":-1}`
	const validToken = "Bearer valid"
	const mediaJSON = "application/json"
	for _, test := range []struct {
		name, body, auth, mediaType string
		castError                   error
		status, code                int
	}{
		{"browser", validBody, validToken, mediaJSON, nil, 200, api.Success},
		{"numeric ID", `{"post_id":999999999999999933,"direction":-1}`, validToken, mediaJSON, nil, 200, api.Success},
		{"missing direction", `{"post_id":"3"}`, validToken, mediaJSON, nil, 400, api.InvalidParam},
		{"invalid direction", `{"post_id":"3","direction":2}`, validToken, mediaJSON, nil, 400, api.InvalidParam},
		{"non-JSON", validBody, validToken, "application/x-www-form-urlencoded", nil, 415, api.InvalidParam},
		{"unauthenticated", validBody, "", mediaJSON, nil, 401, api.NeedLogin},
		{"missing post", validBody, validToken, mediaJSON, ErrPostNotFound, 404, api.NotFound},
		{"repeated vote", validBody, validToken, mediaJSON, ErrVoteRepeated, 409, api.Conflict},
		{"expired vote", validBody, validToken, mediaJSON, ErrVoteExpired, 422, api.Expired},
		{"storage error", validBody, validToken, mediaJSON, errors.New("internal"), 500, api.ServerBusy},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testVote{err: test.castError}
			router := gin.New()
			postExists := func(context.Context, uint64) (bool, error) { return true, nil }
			router.POST("/votes", middleware.Auth(testToken{}), NewHandler(store, postExists).Cast)
			req := httptest.NewRequest(http.MethodPost, "/votes", strings.NewReader(test.body))
			req.Header.Set("Content-Type", test.mediaType)
			if test.auth != "" {
				req.Header.Set("Authorization", test.auth)
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || out.Code != test.status || body.Code != test.code {
				t.Fatalf("HTTP %d %s", out.Code, out.Body.String())
			}
			if test.code == api.Success && (store.user != 9 || store.post != 999999999999999933 || store.direction != -1) {
				t.Fatalf("vote arguments were changed: %+v", store)
			}
		})
	}
}
