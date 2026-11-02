package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// DecodeJSON 限制请求大小，并拒绝未知字段、非对象及多个 JSON 值。
func DecodeJSON(c *gin.Context, destination any, maxBytes int64) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(c, http.StatusUnsupportedMediaType, InvalidParam, "请使用 application/json")
		return false
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge, InvalidParam, "请求体过大")
		} else {
			Fail(c, InvalidParam, "读取请求体失败")
		}
		return false
	}

	body = bytes.TrimSpace(body)
	// 标准 JSON 解码器会替换非法 UTF-8；密码必须保留客户端原始字符。
	if !utf8.Valid(body) || len(body) == 0 || body[0] != '{' {
		writeInvalidJSON(c)
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeInvalidJSON(c)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeInvalidJSON(c)
		return false
	}
	return true
}

func writeInvalidJSON(c *gin.Context) {
	Fail(c, InvalidParam, "请求须为合法 JSON 对象，不能包含未知字段或多个 JSON 值")
}
