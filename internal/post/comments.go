package post

import (
	"context"
	"errors"
	"slices"
	"time"

	"forum/internal/user"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Comment struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement:false"`
	PostID    uint64    `gorm:"not null;index"`
	Post      Post      `gorm:"foreignKey:PostID"`
	AuthorID  uint64    `gorm:"not null;index"`
	Author    user.User `gorm:"foreignKey:AuthorID"`
	Content   string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

type commentDetail struct {
	ID         uint64    `json:"id,string"`
	AuthorID   uint64    `json:"author_id,string"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"create_time"`
}

type commentPage struct {
	Items   []commentDetail `json:"items"`
	HasMore bool            `json:"has_more"`
}

const commentPageSize = 20

var errNotCommentAuthor = errors.New("只能删除自己发表的留言")

// Keep the parent visible until the comment operation commits. Concurrent
// comments can share the lock, while soft-deleting the post must wait.
func (s *Service) withCommentPost(ctx context.Context, postID uint64, work func(*gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").
			Take(&Post{}, "id = ? AND status <> ?", postID, statusDeleted).Error
		if err != nil {
			return err
		}
		return work(tx)
	})
}

func (s *Service) listComments(ctx context.Context, postID, before uint64) (commentPage, error) {
	var page commentPage
	err := s.withCommentPost(ctx, postID, func(tx *gorm.DB) error {
		query := tx.Model(&Comment{}).Joins("JOIN users ON users.id = comments.author_id").
			Select("comments.id, comments.author_id, users.username AS author_name, comments.content, comments.created_at").
			Where("comments.post_id = ?", postID)
		if before != 0 {
			query = query.Where("comments.id < ?", before)
		}
		if err := query.Order("comments.id DESC").Limit(commentPageSize + 1).Find(&page.Items).Error; err != nil {
			return err
		}
		if len(page.Items) > commentPageSize {
			page.HasMore = true
			page.Items = page.Items[:commentPageSize]
		}
		slices.Reverse(page.Items)
		return nil
	})
	return page, err
}

func (s *Service) createComment(ctx context.Context, postID, authorID uint64, content string) (uint64, error) {
	var comment Comment
	err := s.withCommentPost(ctx, postID, func(tx *gorm.DB) error {
		comment = Comment{ID: s.ids.Next(), PostID: postID, AuthorID: authorID, Content: content}
		return tx.Create(&comment).Error
	})
	return comment.ID, err
}

func (s *Service) deleteComment(ctx context.Context, postID, commentID, actorID uint64, admin bool) error {
	return s.withCommentPost(ctx, postID, func(tx *gorm.DB) error {
		var comment Comment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "author_id").
			Take(&comment, "id = ? AND post_id = ?", commentID, postID).Error; err != nil {
			return err
		}
		if !admin && comment.AuthorID != actorID {
			return errNotCommentAuthor
		}
		return tx.Delete(&comment).Error
	})
}
