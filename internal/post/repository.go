package post

import (
	"context"
	"time"

	"forum/internal/community"
	"forum/internal/user"

	"gorm.io/gorm"
)

const statusDeleted uint8 = 2

type Post struct {
	ID           uint64              `gorm:"primaryKey;autoIncrement:false"`
	AuthorID     uint64              `gorm:"not null;index"`
	Author       user.User           `gorm:"foreignKey:AuthorID"`
	CommunityID  uint64              `gorm:"not null;index"`
	Community    community.Community `gorm:"foreignKey:CommunityID"`
	Status       uint8               `gorm:"not null;default:1"`
	Title        string              `gorm:"size:300;not null"`
	Content      string              `gorm:"type:text;not null"`
	CreatedAt    time.Time           `gorm:"not null;index"`
	Indexed      bool                `gorm:"not null;default:false;index"`
	CommentCount int64               `gorm:"->;-:migration"`
}

// A snowflake is larger than a JavaScript safe integer. Keep it a string
// across the JSON boundary, including references to the author.
type detail struct {
	ID            uint64              `json:"id,string"`
	AuthorID      uint64              `json:"author_id,string"`
	AuthorName    string              `json:"author_name"`
	CommunityID   uint64              `json:"community_id"`
	CommunityName string              `json:"community_name"`
	Community     community.Community `json:"community"`
	Status        uint8               `json:"status"`
	Title         string              `json:"title"`
	Content       string              `json:"content"`
	CreatedAt     time.Time           `json:"create_time"`
	VoteNum       int64               `json:"vote_num"`
	CommentCount  int64               `json:"comment_count"`
}

func (s *Service) visiblePosts(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&Post{}).Where("status <> ?", statusDeleted)
}

func (s *Service) Exists(ctx context.Context, id uint64) (bool, error) {
	var count int64
	err := s.visiblePosts(ctx).Where("id = ?", id).Count(&count).Error
	return count != 0, err
}

func (s *Service) posts(ctx context.Context) *gorm.DB {
	count := s.db.Model(&Comment{}).Select("COUNT(*)").Where("comments.post_id = posts.id")
	return s.visiblePosts(ctx).Select("posts.*, (?) AS comment_count", count).Preload("Community").Preload("Author", func(db *gorm.DB) *gorm.DB {
		return db.Select("id", "username")
	})
}

func (s *Service) details(ctx context.Context, posts []Post) ([]detail, error) {
	results := make([]detail, 0, len(posts))
	if len(posts) == 0 {
		return results, nil
	}
	ids := make([]uint64, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	votes, err := s.ranking.Upvotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range posts {
		results = append(results, detail{
			ID: p.ID, Title: p.Title, Content: p.Content, Status: p.Status, CreatedAt: p.CreatedAt,
			AuthorID: p.AuthorID, AuthorName: p.Author.Username, CommunityID: p.CommunityID,
			CommunityName: p.Community.Name, Community: p.Community, VoteNum: votes[p.ID], CommentCount: p.CommentCount,
		})
	}
	return results, nil
}
