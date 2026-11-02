// Package web serves the forum frontend from the Go binary.
package web

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

//go:embed dist/static
var assets embed.FS

//go:embed dist/index.html
var index []byte

func Register(r *gin.Engine, postExists func(context.Context, uint64) (bool, error)) {
	static, err := fs.Sub(assets, "dist/static")
	if err != nil {
		panic(err)
	}
	r.StaticFS("/static", http.FS(static))
	// Vue Router uses history mode. Serve its entry point on direct navigation.
	for _, path := range []string{"/", "/login", "/signup", "/publish", "/change-password", "/change-username", "/admin/communities/new"} {
		r.GET(path, func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", index)
		})
	}
	r.GET("/post/:id", func(c *gin.Context) {
		status := http.StatusNotFound
		if id, err := strconv.ParseUint(c.Param("id"), 10, 64); err == nil && id != 0 {
			exists, err := postExists(c.Request.Context(), id)
			switch {
			case err != nil:
				status = http.StatusInternalServerError
			case exists:
				status = http.StatusOK
			}
		}
		c.Data(status, "text/html; charset=utf-8", index)
	})
}
