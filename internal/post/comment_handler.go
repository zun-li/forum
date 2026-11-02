package post

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"forum/internal/api"
	"forum/internal/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (s *Service) listCommentsHandler(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	var query struct {
		Before uint64 `form:"before"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		api.Fail(c, api.InvalidParam, "留言分页参数无效")
		return
	}
	page, err := s.listComments(c.Request.Context(), id, query.Before)
	if err != nil {
		commentFailure(c, err)
		return
	}
	api.OK(c, page)
}

func (s *Service) createCommentHandler(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if !api.DecodeJSON(c, &input, 16<<10) {
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" || utf8.RuneCountInString(input.Content) > 1000 {
		api.Fail(c, api.InvalidParam, "留言须为 1–1000 个字符")
		return
	}
	authorID, _ := middleware.CurrentUserID(c)
	commentID, err := s.createComment(c.Request.Context(), id, authorID, input.Content)
	if err != nil {
		commentFailure(c, err)
		return
	}
	api.Created(c, gin.H{"id": strconv.FormatUint(commentID, 10)})
}

func (s *Service) deleteCommentHandler(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := postID(c)
		if !ok {
			return
		}
		commentID, err := strconv.ParseUint(c.Param("comment_id"), 10, 64)
		if err != nil || commentID == 0 {
			api.Fail(c, api.InvalidParam, "留言 ID 无效")
			return
		}
		actorID, _ := middleware.CurrentUserID(c)
		if err := s.deleteComment(c.Request.Context(), id, commentID, actorID, admin); err != nil {
			commentFailure(c, err)
			return
		}
		api.OK(c, nil)
	}
}

func commentFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		api.Fail(c, api.NotFound, "帖子或留言不存在")
	case errors.Is(err, errNotCommentAuthor):
		api.Fail(c, api.Forbidden, err.Error())
	default:
		fail(c, err)
	}
}
