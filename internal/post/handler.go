package post

import (
	"errors"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"forum/internal/api"
	"forum/internal/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createRequest struct {
	CommunityID uint64 `json:"community_id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
}

func (s *Service) Register(public, authenticated *gin.RouterGroup) {
	public.GET("/posts", s.listHandler)
	public.GET("/posts/:id", s.detailHandler)
	public.GET("/posts/:id/comments", s.listCommentsHandler)
	authenticated.POST("/posts", s.createHandler)
	authenticated.POST("/posts/:id/comments", s.createCommentHandler)
	authenticated.DELETE("/posts/:id/comments/:comment_id", s.deleteCommentHandler(false))
	authenticated.DELETE("/posts/:id", func(c *gin.Context) { s.deleteHandler(c, false) })
}

func (s *Service) RegisterAdmin(admin *gin.RouterGroup) {
	admin.DELETE("/posts/:id", func(c *gin.Context) { s.deleteHandler(c, true) })
	admin.DELETE("/posts/:id/comments/:comment_id", s.deleteCommentHandler(true))
}

func (s *Service) listHandler(c *gin.Context) {
	opts := listOptions{Page: 1, Size: 10, Order: "time"}
	if err := c.ShouldBindQuery(&opts); err != nil || opts.Page > math.MaxInt/opts.Size {
		api.Fail(c, api.InvalidParam, "分页或排序参数无效")
		return
	}
	data, err := s.list(c.Request.Context(), opts)
	if err != nil {
		fail(c, err)
		return
	}
	api.OK(c, data)
}

func postID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		api.Fail(c, api.InvalidParam, "帖子 ID 无效")
		return 0, false
	}
	return id, true
}

func (s *Service) detailHandler(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var p Post
	err := s.posts(ctx).First(&p, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		api.Fail(c, api.NotFound, "帖子不存在")
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	data, err := s.details(ctx, []Post{p})
	if err != nil {
		fail(c, err)
		return
	}
	api.OK(c, data[0])
}

func (s *Service) createHandler(c *gin.Context) {
	authorID, ok := middleware.CurrentUserID(c)
	if !ok {
		api.Fail(c, api.NeedLogin, "请先登录")
		return
	}
	var input createRequest
	if !api.DecodeJSON(c, &input, 64<<10) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.CommunityID == 0 || input.Title == "" || utf8.RuneCountInString(input.Title) > 300 ||
		strings.TrimSpace(input.Content) == "" || utf8.RuneCountInString(input.Content) > 8192 {
		api.Fail(c, api.InvalidParam, "请选择社区并填写标题和正文（最多 300 / 8192 字）")
		return
	}
	id, err := s.create(c.Request.Context(), authorID, input)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		api.Fail(c, api.NotFound, "社区不存在")
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	api.Created(c, gin.H{"id": strconv.FormatUint(id, 10)})
}

func fail(c *gin.Context, err error) {
	slog.Error("forum post request failed", "route", c.FullPath(), "error", err)
	api.Fail(c, api.ServerBusy, "服务繁忙")
}

func (s *Service) deleteHandler(c *gin.Context, admin bool) {
	authorID, ok := middleware.CurrentUserID(c)
	if !ok {
		api.Fail(c, api.NeedLogin, "请先登录")
		return
	}
	id, ok := postID(c)
	if !ok {
		return
	}
	err := s.remove(c.Request.Context(), id, authorID, admin)
	switch {
	case err == nil:
		api.OK(c, nil)
	case errors.Is(err, gorm.ErrRecordNotFound):
		api.Fail(c, api.NotFound, "帖子不存在")
	case errors.Is(err, errNotAuthor):
		api.Fail(c, api.Forbidden, errNotAuthor.Error())
	default:
		fail(c, err)
	}
}
