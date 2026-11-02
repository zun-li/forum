package post

import (
	"errors"
	"testing"

	"forum/internal/community"
	"forum/internal/testutil/mysqltest"
	"forum/internal/user"
	"forum/pkg/snowflake"

	"gorm.io/gorm"
)

func TestMySQLCommentPaginationAndCounts(t *testing.T) {
	db := mysqltest.Open(t, &user.User{}, &community.Community{}, &Post{}, &Comment{})
	ctx := t.Context()
	for _, row := range []any{
		&user.User{ID: 1, Username: "reader", PasswordHash: "test"},
		&community.Community{ID: 1, Name: "Go"},
		&Post{ID: 1, AuthorID: 1, CommunityID: 1, Title: "older post"},
		&Post{ID: 2, AuthorID: 1, CommunityID: 1, Title: "another post"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	ids, err := snowflake.New(1)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, ids, nil)
	assertCount := func(postID uint64, want int64) {
		t.Helper()
		var p Post
		if err := s.posts(ctx).First(&p, "id = ?", postID).Error; err != nil || p.CommentCount != want {
			t.Fatalf("post %d count = %d, want %d (err %v)", postID, p.CommentCount, want, err)
		}
	}
	assertCount(1, 0)
	var created []uint64
	for range commentPageSize + 1 {
		id, err := s.createComment(ctx, 1, 1, "reader comment")
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, id)
	}
	page, err := s.listComments(ctx, 1, 0)
	if err != nil || len(page.Items) != commentPageSize || !page.HasMore {
		t.Fatalf("first page: %+v, err %v", page, err)
	}
	for i, item := range page.Items {
		if item.ID != created[i+1] || item.AuthorName != "reader" {
			t.Fatalf("unexpected comment at index %d: %+v", i, item)
		}
	}
	cursor := page.Items[0].ID
	if err := s.deleteComment(ctx, 2, cursor, 1, true); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deletion via wrong post: %v", err)
	}
	if err := s.deleteComment(ctx, 1, cursor, 1, false); err != nil {
		t.Fatal(err)
	}
	older, err := s.listComments(ctx, 1, cursor)
	if err != nil || older.HasMore || len(older.Items) != 1 || older.Items[0].ID != created[0] {
		t.Fatalf("older page after deleting the cursor: %+v, err %v", older, err)
	}
	assertCount(1, commentPageSize)
	assertCount(2, 0)
}
