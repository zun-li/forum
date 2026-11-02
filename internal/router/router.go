package router

import (
	"net/http"

	"forum/internal/apidocs"
	"forum/internal/community"
	"forum/internal/middleware"
	"forum/internal/post"
	"forum/internal/user"
	"forum/internal/vote"
	"forum/web"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func New(db *gorm.DB, users *user.Handler, posts *post.Service, votes *vote.Handler, tokens middleware.SessionManager) *gin.Engine {
	r := gin.Default()
	r.SetTrustedProxies(nil)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	web.Register(r, posts.Exists)
	apidocs.Register(r)

	api := r.Group("/api/v1", middleware.OptionalAuth(tokens))
	authenticated := api.Group("", middleware.Auth(tokens))
	authenticated.POST("/logout", middleware.Logout(tokens))
	authenticated.POST("/me/password", users.ChangeInitialPassword(tokens))
	users.Register(api, authenticated)
	admin := authenticated.Group("/admin", users.RequireAdmin)
	community.RegisterAdmin(admin, db)
	posts.RegisterAdmin(admin)
	community.Register(api, db)
	posts.Register(api, authenticated)
	authenticated.POST("/votes", votes.Cast)
	return r
}
