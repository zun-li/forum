package vote

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"forum/internal/api"
	"forum/internal/middleware"

	"github.com/gin-gonic/gin"
)

type Caster interface {
	Cast(context.Context, uint64, uint64, int64) error
}

type Handler struct {
	store      Caster
	postExists func(context.Context, uint64) (bool, error)
}

func NewHandler(store Caster, postExists func(context.Context, uint64) (bool, error)) *Handler {
	return &Handler{store: store, postExists: postExists}
}

func (h *Handler) cast(ctx context.Context, userID, postID uint64, direction int64) error {
	// A committed deletion takes effect even if Redis cleanup is pending.
	exists, err := h.postExists(ctx, postID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrPostNotFound
	}
	return h.store.Cast(ctx, userID, postID, direction)
}

func (h *Handler) Cast(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		api.Fail(c, api.NeedLogin, "需要登录")
		return
	}

	// Read numeric tokens without rounding a Snowflake ID. json.Number
	// also accepts a decimal string, the representation used by browsers.
	var input struct {
		PostID    json.Number `json:"post_id"`
		Direction json.Number `json:"direction"`
	}
	if !api.DecodeJSON(c, &input, 4096) {
		return
	}
	postID, idErr := strconv.ParseUint(input.PostID.String(), 10, 64)
	direction, voteErr := input.Direction.Int64()
	if idErr != nil || postID == 0 || voteErr != nil || direction < -1 || direction > 1 {
		api.Fail(c, api.InvalidParam, ErrInvalidVote.Error())
		return
	}

	err := h.cast(c.Request.Context(), userID, postID, direction)
	switch {
	case err == nil:
		api.OK(c, nil)
	case errors.Is(err, ErrPostNotFound):
		api.Fail(c, api.NotFound, ErrPostNotFound.Error())
	case errors.Is(err, ErrVoteExpired):
		api.Fail(c, api.Expired, ErrVoteExpired.Error())
	case errors.Is(err, ErrVoteRepeated):
		api.Fail(c, api.Conflict, ErrVoteRepeated.Error())
	case errors.Is(err, ErrInvalidVote):
		api.Fail(c, api.InvalidParam, err.Error())
	default:
		_ = c.Error(err)
		api.Fail(c, api.ServerBusy, "服务繁忙")
	}
}
