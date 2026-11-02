// Package apidocs serves the forum's API reference and interactive Swagger UI.
package apidocs

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/swaggest/swgui/v5emb"
)

//go:embed openapi.yaml
var spec []byte

func Register(r *gin.Engine) {
	const specPath = "/openapi.yaml"
	r.GET(specPath, func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", spec)
	})
	r.GET("/swagger/*path", gin.WrapH(v5emb.New("论坛 API", specPath, "/swagger/")))
}
