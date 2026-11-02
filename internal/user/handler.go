package user

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"forum/internal/api"
	"forum/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const (
	credentialTimeout = 5 * time.Second
	listUsersTimeout  = 3 * time.Second
)

type Handler struct {
	service *Service
}

func NewHandler(users *Service) *Handler {
	return &Handler{service: users}
}

func (h *Handler) Register(api, authenticated *gin.RouterGroup) {
	api.GET("/users", h.List)
	api.POST("/signup", h.Signup)
	api.POST("/login", h.Login)
	authenticated.GET("/me", h.Me)
	authenticated.PUT("/me/username", h.ChangeUsername)
	authenticated.PUT("/me/password", h.ChangePassword)
}

func (h *Handler) Signup(c *gin.Context) {
	var request SignupInput
	if !api.DecodeJSON(c, &request, 4096) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), credentialTimeout)
	defer cancel()

	user, err := h.service.Signup(ctx, request)
	if err != nil {
		writeError(c, err)
		return
	}

	api.Created(c, publicUser(*user))
}

func (h *Handler) List(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), listUsersTimeout)
	defer cancel()

	users, err := h.service.List(ctx)
	if err != nil {
		writeError(c, err)
		return
	}

	// 初始化为空切片，让没有用户时返回 []，而不是 null。
	data := make([]userResponse, 0, len(users))
	for _, user := range users {
		data = append(data, publicUser(user))
	}
	api.OK(c, data)
}

func (h *Handler) Login(c *gin.Context) {
	// 登录响应含凭证，禁止浏览器或代理缓存。
	c.Header("Cache-Control", "no-store")
	var request LoginInput
	if !api.DecodeJSON(c, &request, 4096) {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), credentialTimeout)
	defer cancel()

	result, err := h.service.Login(ctx, request)
	if err != nil {
		writeError(c, err)
		return
	}

	response := loginResponse{
		Token:              result.Token.Value,
		UserID:             result.User.ID,
		Username:           result.User.Username,
		Role:               result.User.Role,
		MustChangePassword: result.User.MustChangePassword,
	}
	api.OK(c, response)
}

func (h *Handler) Me(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, exists := middleware.CurrentUserID(c)
	if !exists {
		c.Header("WWW-Authenticate", "Bearer")
		api.Fail(c, api.InvalidToken, "请重新登录")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), listUsersTimeout)
	defer cancel()

	user, err := h.service.CurrentUser(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		c.Header("WWW-Authenticate", "Bearer")
		api.Fail(c, api.InvalidToken, "请重新登录")
		return
	}
	if err != nil {
		writeError(c, err)
		return
	}
	api.OK(c, struct {
		userResponse
		Role               string `json:"role"`
		MustChangePassword bool   `json:"must_change_password,omitempty"`
	}{publicUser(*user), user.Role, user.MustChangePassword})
}

func (h *Handler) ChangeInitialPassword(sessions middleware.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input InitialPasswordInput
		if !api.DecodeJSON(c, &input, 4096) {
			return
		}
		id, _ := middleware.CurrentUserID(c)
		ctx, cancel := context.WithTimeout(c.Request.Context(), credentialTimeout)
		defer cancel()
		if err := h.service.changeInitialPassword(ctx, id, input); err != nil {
			writeError(c, err)
			return
		}
		// The password is committed. Even if Redis is down, this session stays
		// restricted and the database rejects a second initial password change.
		if err := middleware.RevokeCurrentSession(c, sessions); err != nil {
			slog.Warn("could not close initial-password session", "user_id", id, "error", err)
		}
		c.Header("X-Renewed-Token", "")
		api.OK(c, nil)
	}
}

func (h *Handler) ChangeUsername(c *gin.Context) {
	var input UsernameInput
	if !api.DecodeJSON(c, &input, 4096) {
		return
	}
	id, _ := middleware.CurrentUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), credentialTimeout)
	defer cancel()
	name, err := h.service.changeUsername(ctx, id, input)
	if errors.Is(err, ErrUsernameTaken) {
		api.Fail(c, api.UserExists, "名称已被占用")
		return
	}
	if err != nil {
		writeError(c, err)
		return
	}
	api.OK(c, gin.H{"username": name})
}

func (h *Handler) ChangePassword(c *gin.Context) {
	var input PasswordInput
	if !api.DecodeJSON(c, &input, 4096) {
		return
	}
	id, _ := middleware.CurrentUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), credentialTimeout)
	defer cancel()
	if err := h.service.changePassword(ctx, id, input); err != nil {
		writeError(c, err)
		return
	}
	api.OK(c, nil)
}

func (h *Handler) RequireAdmin(c *gin.Context) {
	id, _ := middleware.CurrentUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), listUsersTimeout)
	defer cancel()
	current, err := h.service.CurrentUser(ctx, id)
	switch {
	case errors.Is(err, ErrUserNotFound):
		api.Fail(c, api.InvalidToken, "请重新登录")
	case err != nil:
		writeError(c, err)
	case current.Role != RoleAdmin:
		api.Fail(c, api.Forbidden, "需要管理员权限")
	case current.MustChangePassword:
		api.Fail(c, api.PasswordChangeRequired, "请先修改初始密码")
	default:
		c.Next()
	}
}

func writeError(c *gin.Context, err error) {
	var validationErrors validator.ValidationErrors
	switch {
	case errors.As(err, &validationErrors):
		msg := "请求参数错误"
		switch validationErrors[0].Field() {
		case "Username":
			msg = "用户名须为 3–32 位英文字母、数字或下划线"
		case "Password":
			msg = "请输入密码，长度不超过 128 个字符，注册时至少需要 6 个字符"
		case "CurrentPassword":
			msg = "请输入当前密码，长度不超过 128 个字符"
		case "NewPassword":
			msg = "新密码须为 6–128 个字符"
		case "RePassword", "Confirmation":
			msg = "两次输入的密码须一致"
		}
		api.Fail(c, api.InvalidParam, msg)
	case errors.Is(err, ErrUsernameTaken):
		api.Fail(c, api.UserExists, "用户名已存在")
	case errors.Is(err, errAdminRename):
		api.Fail(c, api.Forbidden, err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		api.Fail(c, api.InvalidPassword, "用户名或密码错误")
	case errors.Is(err, ErrWrongCurrentPassword):
		api.Fail(c, api.InvalidPassword, err.Error())
	case errors.Is(err, ErrUserNotFound):
		c.Header("WWW-Authenticate", "Bearer")
		api.Fail(c, api.InvalidToken, "请重新登录")
	case errors.Is(err, errDefaultPassword):
		api.Fail(c, api.InvalidParam, err.Error())
	case errors.Is(err, ErrPasswordAlreadySet):
		api.Fail(c, api.Conflict, err.Error())
	default:
		// 数据库错误可能包含密码哈希，仅记录错误类型。
		slog.Error("user operation failed", "error_type", fmt.Sprintf("%T", err))
		api.Fail(c, api.ServerBusy, "服务繁忙")
	}
}
