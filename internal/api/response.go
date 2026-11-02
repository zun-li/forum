// Package api writes the forum API responses.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	Success = 1000 + iota
	InvalidParam
	UserExists
	UserNotFound
	InvalidPassword
	ServerBusy
	NeedLogin
	InvalidToken
	NotFound
	Conflict
	Expired
	Forbidden
	PasswordChangeRequired
)

type response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, response{Code: Success, Msg: "success", Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, response{Code: Success, Msg: "success", Data: data})
}

func Fail(c *gin.Context, code int, msg string) {
	status := http.StatusInternalServerError
	switch code {
	case InvalidParam:
		status = http.StatusBadRequest
	case UserExists, Conflict:
		status = http.StatusConflict
	case InvalidPassword, NeedLogin, InvalidToken:
		status = http.StatusUnauthorized
	case Forbidden, PasswordChangeRequired:
		status = http.StatusForbidden
	case NotFound, UserNotFound:
		status = http.StatusNotFound
	case Expired:
		status = http.StatusUnprocessableEntity
	}
	fail(c, status, code, msg)
}

func fail(c *gin.Context, status, code int, msg string) {
	c.AbortWithStatusJSON(status, response{Code: code, Msg: msg})
}
