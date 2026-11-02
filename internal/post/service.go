package post

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"forum/internal/community"

	"gorm.io/gorm"
)

type Ranking interface {
	IndexPost(context.Context, uint64, uint64, time.Time) error
	RemovePost(context.Context, uint64, uint64) error
	ListIDs(context.Context, uint64, string, int64, int64) ([]uint64, error)
	Upvotes(context.Context, []uint64) (map[uint64]int64, error)
}

type IDGenerator interface {
	Next() uint64
}

type Service struct {
	db      *gorm.DB
	ids     IDGenerator
	ranking Ranking
}

func New(db *gorm.DB, ids IDGenerator, ranking Ranking) *Service {
	return &Service{db: db, ids: ids, ranking: ranking}
}

func (s *Service) create(ctx context.Context, authorID uint64, input createRequest) (uint64, error) {
	db := s.db.WithContext(ctx)
	if err := db.Select("id").First(&community.Community{}, "id = ?", input.CommunityID).Error; err != nil {
		return 0, err
	}
	p := Post{
		ID: s.ids.Next(), AuthorID: authorID, CommunityID: input.CommunityID,
		Title: input.Title, Content: input.Content, CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := db.Create(&p).Error; err != nil {
		return 0, err
	}
	s.syncAfterCommit(ctx, p)
	return p.ID, nil
}

var errNotAuthor = errors.New("只能删除自己发布的帖子")

func (s *Service) remove(ctx context.Context, id, actorID uint64, admin bool) error {
	var p Post
	if err := s.visiblePosts(ctx).Select("id", "author_id", "community_id").Take(&p, "id = ?", id).Error; err != nil {
		return err
	}
	if !admin && p.AuthorID != actorID {
		return errNotAuthor
	}
	p.Status = statusDeleted
	result := s.visiblePosts(ctx).Where("id = ? AND author_id = ?", id, p.AuthorID).
		Updates(map[string]any{"status": statusDeleted, "indexed": false})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.syncAfterCommit(ctx, p)
	return nil
}

// MySQL has committed the change. Ranked reads retry pending Redis updates.
func (s *Service) syncAfterCommit(ctx context.Context, p Post) {
	if err := s.syncIndex(ctx, p); err != nil {
		slog.Warn("post saved with pending ranking update", "post_id", p.ID, "error", err)
	}
}

func (s *Service) syncIndex(ctx context.Context, p Post) error {
	var err error
	if p.Status == statusDeleted {
		err = s.ranking.RemovePost(ctx, p.ID, p.CommunityID)
	} else {
		err = s.ranking.IndexPost(ctx, p.ID, p.CommunityID, p.CreatedAt)
	}
	if err != nil {
		return err
	}
	if p.Indexed {
		return nil
	}
	return s.db.WithContext(ctx).Model(&Post{}).Where("id = ? AND status = ? AND indexed = ?", p.ID, p.Status, false).Update("indexed", true).Error
}

// Reindex rebuilds missing ranking entries on startup. IndexPost preserves
// existing scores, including votes cast while another instance is starting.
func (s *Service) Reindex(ctx context.Context) error {
	return s.replay(ctx, false)
}

func (s *Service) replay(ctx context.Context, pendingOnly bool) error {
	query := s.db.WithContext(ctx).Model(&Post{}).Select("id", "community_id", "created_at", "status", "indexed")
	if pendingOnly {
		query = query.Where("indexed = ?", false)
	}
	var batch []Post
	return query.FindInBatches(&batch, 256, func(_ *gorm.DB, _ int) error {
		for _, p := range batch {
			if err := s.syncIndex(ctx, p); err != nil {
				return err
			}
		}
		return nil
	}).Error
}

type listOptions struct {
	Page        int64  `form:"page" binding:"min=1"`
	Size        int64  `form:"size" binding:"min=1,max=100"`
	Order       string `form:"order" binding:"oneof=time score"`
	CommunityID uint64 `form:"community_id"`
}

func (s *Service) list(ctx context.Context, opts listOptions) ([]detail, error) {
	if err := s.replay(ctx, true); err != nil {
		return nil, err
	}
	ids, err := s.ranking.ListIDs(ctx, opts.CommunityID, opts.Order, opts.Page, opts.Size)
	if err != nil || len(ids) == 0 {
		return []detail{}, err
	}
	var rows []Post
	if err := s.posts(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	// SQL IN does not preserve the order selected by Redis.
	byID := make(map[uint64]Post, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	ordered := make([]Post, 0, len(rows))
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			ordered = append(ordered, p)
		}
	}
	return s.details(ctx, ordered)
}
