package post

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"forum/internal/api"
	"forum/internal/community"
	"forum/internal/middleware"
	"forum/internal/session"
	"forum/internal/testutil/mysqltest"
	"forum/internal/user"
	"forum/pkg/snowflake"

	"github.com/gin-gonic/gin"
)

type testAuthor uint64

func (a testAuthor) Authenticate(context.Context, string) (session.Authentication, error) {
	return session.Authentication{UserID: uint64(a)}, nil
}

type mockRanking struct {
	unavailable bool
	indexed     map[uint64]uint64
	order       []uint64
}

func (r *mockRanking) IndexPost(_ context.Context, id, communityID uint64, _ time.Time) error {
	if r.unavailable {
		return errors.New("simulated connection failure")
	}
	r.indexed[id] = communityID
	return nil
}

func (r *mockRanking) RemovePost(_ context.Context, id, communityID uint64) error {
	if r.unavailable {
		return errors.New("simulated connection failure")
	}
	delete(r.indexed, id)
	return nil
}

func (r *mockRanking) ListIDs(_ context.Context, communityID uint64, order string, page, size int64) ([]uint64, error) {
	if communityID != 1 || order != "score" || page != 2 || size != 2 {
		return nil, fmt.Errorf("unexpected query: %d %s %d %d", communityID, order, page, size)
	}
	return r.order, nil
}

func (r *mockRanking) Upvotes(_ context.Context, _ []uint64) (map[uint64]int64, error) {
	return map[uint64]int64{r.order[0]: 7}, nil
}

func TestMySQLRankedReadRepairsFailedIndex(t *testing.T) {
	db := mysqltest.Open(t, &user.User{}, &community.Community{}, &Post{}, &Comment{})
	ctx := t.Context()
	if err := community.Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&user.User{ID: 7, Username: "alice", PasswordHash: "dummy test hash"}).Error; err != nil {
		t.Fatal(err)
	}

	rank := &mockRanking{unavailable: true, indexed: make(map[uint64]uint64)}
	ids, err := snowflake.New(1)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, ids, rank)
	r := gin.New()
	apiGroup := r.Group("/api/v1")
	s.Register(apiGroup, apiGroup.Group("", middleware.Auth(testAuthor(7))))
	community.Register(apiGroup, db)
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	w := request("POST", "/posts", `{"community_id":1,"title":"one","content":"first post"}`, http.StatusCreated)
	var response struct {
		Data struct {
			ID uint64 `json:"id,string"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.ID == 0 {
		t.Fatalf("expected created post ID: %s", w.Body.String())
	}
	first := response.Data.ID
	rank.unavailable = false
	second, err := s.create(ctx, 7, createRequest{1, "two", "second post"})
	if err != nil {
		t.Fatal(err)
	}
	request("POST", "/posts", `{"community_id":99,"title":"absent","content":"community"}`, http.StatusNotFound)
	request("GET", "/communities/99", "", http.StatusNotFound)
	request("GET", "/posts/1", "", http.StatusNotFound)
	request("GET", "/communities/1", "", http.StatusOK)
	rank.order = []uint64{second, first}
	result, err := s.list(ctx, listOptions{Order: "score", Page: 2, Size: 2, CommunityID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != second || result[1].ID != first || result[0].VoteNum != 7 ||
		result[0].AuthorName != "alice" || result[0].Community.Name != "编程与软件工程" || rank.indexed[first] != 1 {
		t.Fatalf("unexpected ranked results or index: %#v %v", result, rank.indexed)
	}
	var pending int64
	if err := db.Model(&Post{}).Where("indexed = ?", false).Count(&pending).Error; err != nil || pending != 0 {
		t.Fatalf("pending index rows: %d (%v)", pending, err)
	}
	clear(rank.indexed)
	if err := s.Reindex(ctx); err != nil || len(rank.indexed) != 2 {
		t.Fatalf("startup reindex: %v (%v)", rank.indexed, err)
	}

	// Delete while Redis is unavailable. A stale ranked ID must not expose
	// the post, and the next ranked read must finish the pending cleanup.
	rank.unavailable = true
	path := fmt.Sprintf("/posts/%d", second)
	request("DELETE", path, "", http.StatusOK)
	request("DELETE", path, "", http.StatusNotFound)
	request("GET", path, "", http.StatusNotFound)
	if exists, err := s.Exists(ctx, second); err != nil || exists {
		t.Fatalf("deleted post visible during pending cleanup: %t, %v", exists, err)
	}
	rank.unavailable = false
	result, err = s.list(ctx, listOptions{Order: "score", Page: 2, Size: 2, CommunityID: 1})
	if err != nil || len(result) != 1 || result[0].ID != first || rank.indexed[second] != 0 {
		t.Fatalf("delete replay: posts %v, index %v, err %v", result, rank.indexed, err)
	}
	clear(rank.indexed)
	if err := s.Reindex(ctx); err != nil || len(rank.indexed) != 1 || rank.indexed[first] != 1 {
		t.Fatalf("startup after delete: %v (%v)", rank.indexed, err)
	}
}

func TestRejectInvalidListQueryBeforePersistence(t *testing.T) {
	r := gin.New()
	s := new(Service)
	s.Register(r.Group("/api/v1"), r.Group("/api/v1"))
	for _, query := range []string{"size=0", "size=101", "page=-1", "page=9223372036854775807", "order=invalid", "community_id=-1"} {
		req := httptest.NewRequest("GET", "/api/v1/posts?"+query, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body struct{ Code int }
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusBadRequest || body.Code != api.InvalidParam {
			t.Errorf("query %q: %d %s", query, w.Code, w.Body.String())
		}
	}
}
