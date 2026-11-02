package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"forum/internal/api"
	"forum/internal/session"
	"forum/pkg/jwt"

	"github.com/gin-gonic/gin"
)

const userIDKey = "auth.user_id"
const sessionIDKey = "auth.session_id"

type TokenVerifier interface {
	Authenticate(context.Context, string) (session.Authentication, error)
}

type SessionManager interface {
	TokenVerifier
	Revoke(context.Context, uint64, string) error
}

// OptionalAuth also renews sessions on public reads. Login and signup must stay
// available when the browser still holds an expired or replaced credential.
func OptionalAuth(tokens TokenVerifier) gin.HandlerFunc {
	auth := Auth(tokens)
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "/api/v1/login" || path == "/api/v1/signup" || len(c.Request.Header.Values("Authorization")) == 0 {
			c.Next()
			return
		}
		auth(c)
	}
}

func Auth(tokens TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, exists := CurrentUserID(c); exists {
			c.Next()
			return
		}
		headers := c.Request.Header.Values("Authorization")
		if len(headers) == 0 {
			unauthorized(c, api.NeedLogin, "需要登录")
			return
		}
		if len(headers) != 1 {
			unauthorized(c, api.InvalidToken, "无效的 token")
			return
		}
		parts := strings.Fields(headers[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			unauthorized(c, api.InvalidToken, "无效的 token")
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		auth, err := tokens.Authenticate(ctx, parts[1])
		if err != nil {
			switch {
			case errors.Is(err, session.ErrReplaced):
				unauthorized(c, api.InvalidToken, "账号已在其他设备登录")
			case errors.Is(err, jwt.ErrInvalidToken):
				unauthorized(c, api.InvalidToken, "登录已失效，请重新登录")
			default:
				api.Fail(c, api.ServerBusy, "服务繁忙")
			}
			return
		}

		c.Set(userIDKey, auth.UserID)
		c.Set(sessionIDKey, auth.SessionID)
		c.Header("Cache-Control", "no-store")
		initialPasswordChange := c.FullPath() == "/api/v1/me/password" && c.Request.Method == http.MethodPost
		if initialPasswordChange && !auth.MustChangePassword {
			api.Fail(c, api.Forbidden, "此接口仅用于首次登录改密")
			return
		}
		if auth.MustChangePassword && !initialPasswordChange && c.FullPath() != "/api/v1/me" && c.FullPath() != "/api/v1/logout" {
			api.Fail(c, api.PasswordChangeRequired, "请先修改初始密码")
			return
		}
		if auth.RenewedToken != "" && c.FullPath() != "/api/v1/logout" {
			c.Header("X-Renewed-Token", auth.RenewedToken)
		}
		c.Next()
	}
}

func Logout(sessions SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := RevokeCurrentSession(c, sessions); err != nil {
			api.Fail(c, api.ServerBusy, "服务繁忙")
			return
		}
		api.OK(c, nil)
	}
}

func RevokeCurrentSession(c *gin.Context, sessions SessionManager) error {
	userID, _ := CurrentUserID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	return sessions.Revoke(ctx, userID, c.GetString(sessionIDKey))
}

func CurrentUserID(c *gin.Context) (uint64, bool) {
	value, exists := c.Get(userIDKey)
	id, valid := value.(uint64)
	return id, exists && valid && id != 0
}

func unauthorized(c *gin.Context, code int, msg string) {
	c.Header("WWW-Authenticate", "Bearer")
	api.Fail(c, code, msg)
}
